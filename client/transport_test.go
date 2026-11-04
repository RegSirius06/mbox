package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	noise "../common/noise"
)

func newQueueTestTunnel() *tunnel {
	return &tunnel{streams: make(map[uint32]*localStream), upQueues: make(map[uint32][][]byte), upPerStream: make(map[uint32]int), upSpace: make(chan struct{}, 1), wake: make(chan struct{}, 1), downWake: make(chan struct{}, 1), downDeliver: make(chan []byte, 64), done: make(chan struct{}), downPending: make(map[uint64][]byte), nextDown: 1, fatalErr: make(chan error, 1)}
}
func muxIDs(t *testing.T, data []byte) []uint32 {
	var ids []uint32
	for len(data) > 0 {
		if len(data) < 9 {
			t.Fatal("short")
		}
		id := binary.BigEndian.Uint32(data[:4])
		n := int(binary.BigEndian.Uint32(data[5:9]))
		if n > len(data)-9 {
			t.Fatal("short payload")
		}
		ids = append(ids, id)
		data = data[9+n:]
	}
	return ids
}
func TestUploadIsChunkedAndRoundRobinFair(t *testing.T) {
	old := camouflage.UpMaxBytes
	camouflage.UpMaxBytes = 50000
	defer func() { camouflage.UpMaxBytes = old }()
	tun := newQueueTestTunnel()
	p := bytes.Repeat([]byte{1}, 40000)
	_ = tun.queue(1, frameData, p)
	_ = tun.queue(1, frameData, p)
	_ = tun.queue(2, frameData, p)
	a, b, c := tun.takeUpload(50000), tun.takeUpload(50000), tun.takeUpload(50000)
	if muxIDs(t, a)[0] != 1 || muxIDs(t, b)[0] != 2 || muxIDs(t, c)[0] != 1 {
		t.Fatal("not fair")
	}
}
func TestWindowCreditRejectsOverrun(t *testing.T) {
	st := &localStream{writeQ: newByteQueue(streamWriteQueueMaxBytes), creditWake: make(chan struct{}, 1)}
	_ = st.grantPeerAllowance(1024)
	if st.consumePeerAllowance(1025) == nil {
		t.Fatal("overrun accepted")
	}
}
func TestSlowStreamWriterDoesNotBlockOtherStreams(t *testing.T) {
	tun := newQueueTestTunnel()
	sl, sp := net.Pipe()
	fl, fp := net.Pipe()
	defer sl.Close()
	defer sp.Close()
	defer fl.Close()
	defer fp.Close()
	newst := func(c net.Conn) *localStream {
		return &localStream{conn: c, writeQ: newByteQueue(streamWriteQueueMaxBytes), creditWake: make(chan struct{}, 1)}
	}
	slow, fast := newst(sl), newst(fl)
	p := bytes.Repeat([]byte{4}, 32*1024)
	_ = slow.grantPeerAllowance(len(p))
	_ = fast.grantPeerAllowance(len(p))
	tun.streams[1] = slow
	tun.streams[2] = fast
	tun.startLocalWriter(1, slow)
	tun.startLocalWriter(2, fast)
	if err := tun.handleFrames(append(frame(1, frameData, p), frame(2, frameData, p)...)); err != nil {
		t.Fatal(err)
	}
	_ = fp.SetReadDeadline(time.Now().Add(time.Second))
	got := make([]byte, len(p))
	if _, err := io.ReadFull(fp, got); err != nil {
		t.Fatal(err)
	}
	close(tun.done)
	_ = sl.Close()
	_ = sp.Close()
	_ = fl.Close()
	_ = fp.Close()
	tun.streamWG.Wait()
}
func FuzzMuxFrameParser(f *testing.F) {
	f.Add(frame(1, framePing, nil))
	f.Add([]byte{1, 2, 3})
	f.Fuzz(func(t *testing.T, data []byte) { tun := newQueueTestTunnel(); _ = tun.handleFrames(data) })
}

func sealTestDown(t *testing.T, send *noise.CipherState, sid string, counter uint64, firstNonce uint64, plain []byte) []byte {
	t.Helper()
	p, err := padTransportPlaintext(plain)
	if err != nil {
		t.Fatal(err)
	}
	rec, _, err := sealNoiseRecord(send, sid, dirDown, counter, firstNonce, p)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}
func TestDownDeliveryReordersAndDeduplicates(t *testing.T) {
	old := camouflage.PaddingExtraClassPercent
	camouflage.PaddingExtraClassPercent = 0
	defer func() { camouflage.PaddingExtraClassPercent = old }()
	key := bytes.Repeat([]byte{3}, 32)
	send, err := noise.NewCipherState(key)
	if err != nil {
		t.Fatal(err)
	}
	recv, err := noise.NewCipherState(key)
	if err != nil {
		t.Fatal(err)
	}
	tun := newQueueTestTunnel()
	tun.sid = "00112233445566778899aabbccddeeff"
	tun.crypto = &cryptoState{down: recv}
	done := make(chan struct{})
	go func() { defer close(done); tun.downDeliveryLoop() }()
	r1 := sealTestDown(t, send, tun.sid, 1, 0, frame(1, framePing, []byte("1")))
	r2 := sealTestDown(t, send, tun.sid, 2, 1, frame(1, framePing, []byte("2")))
	_ = tun.submitDownRecord(r2)
	_ = tun.submitDownRecord(r1)
	_ = tun.submitDownRecord(r1)
	deadline := time.Now().Add(time.Second)
	for tun.downAck.Load() != 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if tun.downAck.Load() != 2 {
		t.Fatal("ack did not advance")
	}
	close(tun.done)
	<-done
}

func TestFullDuplexSingleStream(t *testing.T) {
	tun := newQueueTestTunnel()
	app, tunnelSide := net.Pipe()
	defer app.Close()
	handleDone := make(chan struct{})
	go func() {
		defer close(handleDone)
		tun.handleLocal(tunnelSide)
	}()

	deadline := time.Now().Add(time.Second)
	var st *localStream
	for time.Now().Before(deadline) {
		tun.streamsMu.Lock()
		st = tun.streams[1]
		tun.streamsMu.Unlock()
		if st != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if st == nil {
		t.Fatal("stream did not open")
	}

	// The remote/server side grants credit for client->server DATA.
	if err := tun.handleFrames(frame(1, frameWindow, encodeWindowCredit(streamInitialWindow))); err != nil {
		t.Fatal(err)
	}

	// The local endpoint is SOCKS5, not an arbitrary raw TCP stream.
	// Pipelined greeting and CONNECT prove byte-for-byte beta.2.1 framing
	// is preserved before the full-duplex DATA portion starts.
	socksPrelude := []byte{0x05, 0x01, 0x00, 0x05, 0x01, 0x00, 0x01, 127, 0, 0, 1, 0, 80}
	upPayload := append(append([]byte{}, socksPrelude...), bytes.Repeat([]byte{0x11}, 24*1024)...)
	downPayload := bytes.Repeat([]byte{0x22}, 24*1024)
	upWriteDone := make(chan error, 1)
	go func() {
		_, err := app.Write(upPayload)
		upWriteDone <- err
	}()

	// Simultaneously deliver server->client DATA. The initial peer allowance was
	// advertised by handleLocal before application DATA can flow.
	if err := tun.handleFrames(frame(1, frameData, downPayload)); err != nil {
		t.Fatal(err)
	}
	_ = app.SetReadDeadline(time.Now().Add(time.Second))
	gotDown := make([]byte, len(downPayload))
	if _, err := io.ReadFull(app, gotDown); err != nil {
		t.Fatalf("DOWN direction failed: %v", err)
	}
	if !bytes.Equal(gotDown, downPayload) {
		t.Fatal("DOWN payload mismatch")
	}
	if err := <-upWriteDone; err != nil {
		t.Fatalf("UP local write failed: %v", err)
	}

	var gotUp []byte
	deadline = time.Now().Add(time.Second)
	for len(gotUp) < len(upPayload) && time.Now().Before(deadline) {
		chunk := tun.takeUpload(camouflage.UpMaxBytes)
		if len(chunk) == 0 {
			time.Sleep(time.Millisecond)
			continue
		}
		for len(chunk) > 0 {
			if len(chunk) < 9 {
				t.Fatal("short UP frame")
			}
			id := binary.BigEndian.Uint32(chunk[:4])
			typ := chunk[4]
			n := int(binary.BigEndian.Uint32(chunk[5:9]))
			if n > len(chunk)-9 {
				t.Fatal("short UP payload")
			}
			if id == 1 && typ == frameData {
				gotUp = append(gotUp, chunk[9:9+n]...)
			}
			chunk = chunk[9+n:]
		}
	}
	if !bytes.Equal(gotUp, upPayload) {
		t.Fatalf("UP payload mismatch: got=%d want=%d", len(gotUp), len(upPayload))
	}
	close(tun.done)
	_ = app.Close()
	<-handleDone
	tun.streamWG.Wait()
}

func TestUploader504RetriesExactNoiseRecordAndSequence(t *testing.T) {
	oldBase := base
	oldCamo := camouflage
	defer func() { base = oldBase; camouflage = oldCamo }()
	camouflage.Recovery.RetryMinMS = 1
	camouflage.Recovery.RetryMaxMS = 2
	camouflage.Recovery.FailureTimeoutMS = 2000
	camouflage.UpPath = "/up/"
	camouflage.BatchMinBytes = maxFramePayload + 9
	camouflage.PaddingRandomMaxBytes = 32
	camouflage.NoiseChunkMinBytes = 4096
	camouflage.NoiseChunkMaxBytes = 8192

	tun := newQueueTestTunnel()
	tun.sid = "0123456789abcdef0123456789abcdef"
	key := bytes.Repeat([]byte{0x31}, 32)
	send, err := noise.NewCipherState(key)
	if err != nil {
		t.Fatal(err)
	}
	tun.crypto = &cryptoState{up: send}

	type seenReq struct {
		url  string
		body []byte
	}
	seen := make(chan seenReq, 2)
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen <- seenReq{url: r.URL.String(), body: b}
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()
	base = ts.URL
	tun.http = ts.Client()

	workerDone := make(chan struct{})
	go func() { defer close(workerDone); tun.uploader() }()
	if err := tun.queue(1, framePing, []byte("retry-me")); err != nil {
		t.Fatal(err)
	}
	var a, b seenReq
	select {
	case a = <-seen:
	case <-time.After(2 * time.Second):
		t.Fatal("first upload not observed")
	}
	select {
	case b = <-seen:
	case <-time.After(2 * time.Second):
		t.Fatal("retry upload not observed")
	}
	close(tun.done)
	<-workerDone
	if a.url != b.url {
		t.Fatalf("retry URL changed: %q != %q", a.url, b.url)
	}
	if !bytes.Equal(a.body, b.body) {
		t.Fatal("retry body/ciphertext changed")
	}
	wire, err := decodeHTTPEnvelope(a.body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cryptoRecordCounter(wire, dirUp); err != nil {
		t.Fatalf("not a Noise UP record: %v", err)
	}
}

func TestDownloader502RetriesExactSequenceURL(t *testing.T) {
	oldBase := base
	oldCamo := camouflage
	defer func() { base = oldBase; camouflage = oldCamo }()
	camouflage.DownPath = "/down/"
	camouflage.Timing.IdleMinMS = 1
	camouflage.Timing.IdleMaxMS = 1
	camouflage.Timing.WakeMinMS = 1
	camouflage.Timing.WakeMaxMS = 1
	camouflage.Timing.ActiveMinMS = 1
	camouflage.Timing.ActiveMaxMS = 1
	camouflage.Timing.ActiveBackoffMS = 2
	camouflage.Timing.ActiveJitterP = 0
	camouflage.Recovery.RetryMinMS = 1
	camouflage.Recovery.RetryMaxMS = 2
	camouflage.Recovery.FailureTimeoutMS = 2000

	tun := newQueueTestTunnel()
	tun.sid = "fedcba9876543210fedcba9876543210"
	tun.crypto = &cryptoState{}
	seen := make(chan string, 2)
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.URL.String()
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()
	base = ts.URL
	tun.http = ts.Client()
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); tun.downloader() }()
	var a, b string
	select {
	case a = <-seen:
	case <-time.After(2 * time.Second):
		t.Fatal("first poll not observed")
	}
	select {
	case b = <-seen:
	case <-time.After(2 * time.Second):
		t.Fatal("retry poll not observed")
	}
	close(tun.done)
	<-workerDone
	if a != b {
		t.Fatalf("retry DOWN URL changed: %q != %q", a, b)
	}
}
