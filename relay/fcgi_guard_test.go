package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/fcgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"
)

// Exercise the real stdlib FastCGI server with a peer that can stop reading
// or truncate a request. net.Pipe has no buffering, so stalled writes are
// deterministic, unlike relying on the size of a kernel TCP send buffer.
type guardTestListener struct {
	conns chan net.Conn
	done  chan struct{}
}

func (l *guardTestListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *guardTestListener) Close() error   { close(l.done); return nil }
func (l *guardTestListener) Addr() net.Addr { return nil }

func guardStart(t *testing.T, handler http.Handler, rd, wr time.Duration) *guardTestListener {
	t.Helper()
	l := &guardTestListener{make(chan net.Conn), make(chan struct{})}
	done := make(chan struct{})
	go func() { defer close(done); _ = fcgi.Serve(fcgiDeadlineListener{l, rd, wr}, handler) }()
	t.Cleanup(func() { _ = l.Close(); <-done })
	return l
}
func guardDial(t *testing.T, l *guardTestListener) net.Conn {
	t.Helper()
	a, b := net.Pipe()
	l.conns <- a
	_ = b.SetDeadline(time.Now().Add(3 * time.Second))
	t.Cleanup(func() { _ = b.Close() })
	return b
}
func guardRecord(w io.Writer, kind byte, body []byte) error {
	h := []byte{1, kind, 0, 1, 0, 0, 0, 0}
	binary.BigEndian.PutUint16(h[4:6], uint16(len(body)))
	_, err := w.Write(append(h, body...))
	return err
}
func guardRequest(t *testing.T, c net.Conn, method, length string, finish bool) {
	t.Helper()
	if err := guardRecord(c, 1, []byte{0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	var params []byte
	for k, v := range map[string]string{"REQUEST_METHOD": method, "REQUEST_URI": "/test", "SERVER_PROTOCOL": "HTTP/1.1", "CONTENT_LENGTH": length, "SERVER_NAME": "local.test"} {
		params = append(params, byte(len(k)), byte(len(v)))
		params = append(params, k...)
		params = append(params, v...)
	}
	if err := guardRecord(c, 4, params); err != nil {
		t.Fatal(err)
	}
	if err := guardRecord(c, 4, nil); err != nil {
		t.Fatal(err)
	}
	if finish {
		if err := guardRecord(c, 5, nil); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := guardRecord(c, 5, []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
}
func guardResponse(t *testing.T, c net.Conn) []byte {
	t.Helper()
	var result []byte
	for {
		var h [8]byte
		if _, err := io.ReadFull(c, h[:]); err != nil {
			t.Fatal(err)
		}
		body := make([]byte, int(binary.BigEndian.Uint16(h[4:6]))+int(h[6]))
		if _, err := io.ReadFull(c, body); err != nil {
			t.Fatal(err)
		}
		if h[1] == 6 {
			result = append(result, body[:len(body)-int(h[6])]...)
		}
		if h[1] == 3 {
			return result
		}
	}
}
func guardAwait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not finish")
	}
}

func TestFastCGINormalResponse(t *testing.T) {
	l := guardStart(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("healthy")) }), time.Second, time.Second)
	c := guardDial(t, l)
	guardRequest(t, c, "GET", "0", true)
	if got := guardResponse(t, c); !bytes.Contains(got, []byte("healthy")) {
		t.Fatalf("bad response %q", got)
	}
}

func TestFastCGIStalledWriterReleasesRealSlotAndAcceptsNextRequest(t *testing.T) {
	cfg := &relayConfig{RuntimeDir: t.TempDir(), MaxSlots: 1}
	if err := os.Chmod(cfg.RuntimeDir, 0700); err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{}, 2)
	var count atomic.Int32
	l := guardStart(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { finished <- struct{}{} }()
		slot, ok := acquireSlot(cfg)
		if !ok {
			w.WriteHeader(503)
			return
		}
		defer releaseSlot(slot)
		if count.Add(1) == 1 {
			_, _ = w.Write(bytes.Repeat([]byte("x"), 128<<10))
			return
		}
		_, _ = w.Write([]byte("recovered"))
	}), time.Second, 80*time.Millisecond)
	c := guardDial(t, l)
	guardRequest(t, c, "GET", "0", true)
	// Do not read anything. The real FastCGI write must time out.
	guardAwait(t, finished)
	if slot, ok := acquireSlot(cfg); !ok {
		t.Fatal("flock still held")
	} else {
		releaseSlot(slot)
	}
	c2 := guardDial(t, l)
	guardRequest(t, c2, "GET", "0", true)
	if got := guardResponse(t, c2); !bytes.Contains(got, []byte("recovered")) {
		t.Fatalf("next request failed: %q", got)
	}
}

func TestFastCGITruncatedBodyUnblocksHandler(t *testing.T) {
	finished := make(chan struct{}, 1)
	readErr := make(chan error, 1)
	l := guardStart(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		readErr <- err
		finished <- struct{}{}
	}), 80*time.Millisecond, time.Second)
	c := guardDial(t, l)
	guardRequest(t, c, "POST", "100", false)
	guardAwait(t, finished)
	if err := <-readErr; err == nil {
		t.Fatal("truncated body unexpectedly read successfully")
	}
}

func TestFastCGIWatchdogFiresOnlyForStuckHandler(t *testing.T) {
	fired := make(chan struct{}, 1)
	block := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-block:
		default:
			close(block)
		}
	})
	stuck := fcgiWatchdog(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-block }), 40*time.Millisecond, func() { fired <- struct{}{} })
	done := make(chan struct{})
	go func() {
		defer close(done)
		stuck.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	}()
	guardAwait(t, fired)
	close(block)
	guardAwait(t, done)
}

func TestFastCGIWatchdogDisarmsAfterNormalHandler(t *testing.T) {
	fired := make(chan struct{}, 1)
	h := fcgiWatchdog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }), 40*time.Millisecond, func() { fired <- struct{}{} })
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	select {
	case <-fired:
		t.Fatal("completed request triggered watchdog")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestFastCGIWatchdogWorkerExitReleasesSlot(t *testing.T) {
	const env = "MBOX_FCGI_TEST_WATCHDOG_DIR"
	if dir := os.Getenv(env); dir != "" {
		cfg := &relayConfig{RuntimeDir: dir, MaxSlots: 1}
		lock, ok := acquireSlot(cfg)
		if !ok {
			os.Exit(24)
		}
		defer releaseSlot(lock)
		h := fcgiWatchdog(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			time.Sleep(10 * time.Second)
		}), 60*time.Millisecond, func() { os.Exit(23) })
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
		os.Exit(25)
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFastCGIWatchdogWorkerExitReleasesSlot$")
	cmd.Env = append(os.Environ(), env+"="+dir)
	out, err := cmd.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 23 {
		t.Fatalf("watchdog did not exit own process: %v, %s", err, out)
	}
	slot, ok := acquireSlot(&relayConfig{RuntimeDir: dir, MaxSlots: 1})
	if !ok {
		t.Fatal("kernel did not release exited worker's flock")
	}
	releaseSlot(slot)
}

func TestFastCGIWatchdogBoundsStalledRequestBody(t *testing.T) {
	fired := make(chan struct{}, 1)
	reader, writer := io.Pipe()
	defer writer.Close()
	handler := fcgiWatchdog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
	}), 40*time.Millisecond, func() { fired <- struct{}{} })
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/", reader))
	}()
	guardAwait(t, fired)
	_ = writer.Close()
	guardAwait(t, done)
}
