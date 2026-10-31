package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	frameOpen  = 1
	frameData  = 2
	frameClose = 3
	frameError = 4
	framePing  = 5
)

var (
	listen string
	base   string
)

type localStream struct {
	conn   net.Conn
	closed atomic.Bool
}

type timingConfig struct {
	IdleMinMS     int `json:"idle_min_ms"`
	IdleMaxMS     int `json:"idle_max_ms"`
	WakeMinMS     int `json:"wake_min_ms"`
	WakeMaxMS     int `json:"wake_max_ms"`
	ActiveJitterP int `json:"active_jitter_percent"`
}

type recoveryConfig struct {
	FailureTimeoutMS int `json:"failure_timeout_ms"`
	RetryMinMS       int `json:"retry_min_ms"`
	RetryMaxMS       int `json:"retry_max_ms"`
}

type camouflageConfig struct {
	Timing                   timingConfig      `json:"timing"`
	Recovery                 recoveryConfig    `json:"recovery"`
	OpenPath                 string            `json:"open_path"`
	UpPath                   string            `json:"up_path"`
	DownPath                 string            `json:"down_path"`
	ClosePath                string            `json:"close_path"`
	ContentType              string            `json:"content_type"`
	RequestHeaders           map[string]string `json:"request_headers"`
	ResponseHeaders          map[string]string `json:"response_headers"`
	DownMaxBytes             int               `json:"down_max_bytes"`
	PaddingClasses           []int             `json:"padding_classes"`
	PaddingExtraClassPercent int               `json:"padding_extra_class_percent"`
}

var camouflage = camouflageConfig{
	Timing: timingConfig{
		IdleMinMS:     45000,
		IdleMaxMS:     90000,
		WakeMinMS:     100,
		WakeMaxMS:     220,
		ActiveJitterP: 20,
	},
	Recovery: recoveryConfig{
		FailureTimeoutMS: 30000,
		RetryMinMS:       300,
		RetryMaxMS:       5000,
	},
	OpenPath:    "/api/v1/session",
	UpPath:      "/api/v1/events/",
	DownPath:    "/api/v1/sync/",
	ClosePath:   "/api/v1/session/",
	ContentType: "application/json",
	RequestHeaders: map[string]string{
		"Accept": "application/json",
	},
	ResponseHeaders: map[string]string{
		"Cache-Control": "no-store",
	},
	DownMaxBytes: 262144,
	PaddingClasses: []int{
		512, 1024, 2048, 4096, 8192, 16384,
		32768, 65536, 131072, 262144, 524288,
	},
	PaddingExtraClassPercent: 20,
}

type tunnel struct {
	sid    string
	crypto *cryptoState

	streamsMu sync.Mutex
	streams   map[uint32]*localStream
	nextID    atomic.Uint32

	upMu  sync.Mutex
	upBuf bytes.Buffer

	wake     chan struct{}
	downWake chan struct{}
	done     chan struct{}

	downMu      sync.Mutex
	nextDown    uint64
	downPending map[uint64][]byte

	http *http.Client

	// A terminal transport/protocol failure must terminate the process. Leaving
	// the local SOCKS listener alive would make Android keep a dead VPN route.
	failOnce sync.Once
	fatalErr chan error
}

func frame(streamID uint32, typ byte, payload []byte) []byte {
	b := make([]byte, 9+len(payload))

	binary.BigEndian.PutUint32(b[0:4], streamID)
	b[4] = typ
	binary.BigEndian.PutUint32(b[5:9], uint32(len(payload)))

	copy(b[9:], payload)
	return b
}

func (t *tunnel) closeRemote() {
	if t == nil || t.sid == "" || camouflage.ClosePath == "" || t.http == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		base+camouflage.ClosePath+t.sid,
		nil,
	)
	if err != nil {
		log.Printf("SESSION CLOSE best-effort: %v", err)
		return
	}
	for k, v := range camouflage.RequestHeaders {
		req.Header.Set(k, v)
	}

	resp, err := t.http.Do(req)
	if err != nil {
		log.Printf("SESSION CLOSE best-effort: %v", err)
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		log.Printf("SESSION CLOSE %s", t.sid)
		return
	}
	log.Printf("SESSION CLOSE HTTP %d", resp.StatusCode)
}

func (t *tunnel) requestRaw(method, url string, body []byte) ([]byte, int, error) {
	var r io.Reader

	if body != nil {
		r = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, url, r)
	if err != nil {
		return nil, 0, err
	}

	for k, v := range camouflage.RequestHeaders {
		req.Header.Set(k, v)
	}

	if body != nil {
		req.Header.Set("Content-Type", camouflage.ContentType)
	}

	resp, err := t.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	return data, resp.StatusCode, err
}

type httpEnvelope struct {
	Version int    `json:"v"`
	Data    string `json:"data"`
}

func encodeHTTPEnvelope(data []byte) ([]byte, error) {
	env := httpEnvelope{
		Version: 1,
		Data:    base64.StdEncoding.EncodeToString(data),
	}
	return json.Marshal(env)
}

func decodeHTTPEnvelope(body []byte) ([]byte, error) {
	var env httpEnvelope

	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("invalid HTTP envelope")
	}

	if env.Version != 1 {
		return nil, fmt.Errorf("unsupported HTTP envelope version")
	}

	data, err := base64.StdEncoding.DecodeString(env.Data)
	if err != nil {
		return nil, fmt.Errorf("invalid HTTP envelope data")
	}

	return data, nil
}

func (t *tunnel) request(method, url string, body []byte) ([]byte, int, error) {
	wireBody := body

	if body != nil {
		var err error
		wireBody, err = encodeHTTPEnvelope(body)
		if err != nil {
			return nil, 0, err
		}
	}

	data, status, err := t.requestRaw(method, url, wireBody)
	if err != nil {
		return nil, status, err
	}

	// All successful MBox responses carrying a body use the envelope.
	if status == http.StatusOK && len(data) > 0 {
		data, err = decodeHTTPEnvelope(data)
		if err != nil {
			return nil, status, err
		}
	}

	return data, status, nil
}

func newTunnel() (*tunnel, error) {
	t := &tunnel{
		streams:     make(map[uint32]*localStream),
		wake:        make(chan struct{}, 1),
		downWake:    make(chan struct{}, 1),
		done:        make(chan struct{}),
		downPending: make(map[uint64][]byte),
		nextDown:    1,
		fatalErr:    make(chan error, 1),
	}

	httpTransport := &http.Transport{
		MaxIdleConns:        8,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     90 * time.Second,
	}

	// Android builds use the pure Go resolver (CGO_ENABLED=0). Some Android
	// devices expose a loopback resolver to native processes that is not
	// reachable from a standalone Go executable. The Java wrapper therefore
	// resolves the endpoint on the current physical Network and passes the
	// numeric address through MBOX_CONNECT_IP. Dialing that address keeps the
	// original URL host intact, so HTTPS Host and TLS SNI are unchanged.
	if connectIP := strings.TrimSpace(os.Getenv("MBOX_CONNECT_IP")); connectIP != "" {
		if net.ParseIP(connectIP) == nil {
			return nil, fmt.Errorf("invalid MBOX_CONNECT_IP %q", connectIP)
		}

		baseURL, err := url.Parse(base)
		if err != nil || baseURL.Hostname() == "" {
			return nil, fmt.Errorf("invalid transport base URL %q", base)
		}

		endpointHost := baseURL.Hostname()
		dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
		httpTransport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil || !strings.EqualFold(host, endpointHost) {
				return dialer.DialContext(ctx, network, addr)
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(connectIP, port))
		}
		log.Printf("ENDPOINT resolved host=%s ip=%s", endpointHost, connectIP)
	}

	t.http = &http.Client{
		Transport: httpTransport,
		Timeout:   15 * time.Second,
	}

	openBody, ephPriv, clientPub, err := buildOpenRequest()
	if err != nil {
		return nil, err
	}

	data, status, err := t.request("POST", base+camouflage.OpenPath, openBody)
	if err != nil {
		return nil, err
	}

	if status != http.StatusOK {
		return nil, fmt.Errorf("open HTTP %d: %s", status, data)
	}

	reply := strings.TrimSpace(string(data))

	if len(reply) != 64 {
		return nil, fmt.Errorf("invalid open reply %q", reply)
	}

	t.sid = reply[:32]
	confirm := reply[32:]

	t.crypto, err = finishClientHandshake(
		ephPriv,
		clientPub,
		t.sid,
		confirm,
	)
	if err != nil {
		return nil, fmt.Errorf("crypto handshake: %w", err)
	}

	handshake := "v1"
	if clientID != "" {
		handshake = "v2"
	}
	log.Printf("SESSION %s handshake=%s record=v%d", t.sid, handshake, cryptoRecordVersion)

	go t.uploader()
	go t.downloader()

	return t, nil
}

func (t *tunnel) fail(err error) {
	if err == nil {
		err = fmt.Errorf("tunnel stopped")
	}
	t.failOnce.Do(func() {
		t.fatalErr <- err
		close(t.done)
	})
}

func (t *tunnel) queue(streamID uint32, typ byte, payload []byte) {
	f := frame(streamID, typ, payload)

	t.upMu.Lock()
	t.upBuf.Write(f)
	t.upMu.Unlock()

	select {
	case t.wake <- struct{}{}:
	default:
	}

	// Исходящий OPEN/DATA означает, что скоро ожидается ответ.
	// Не позволяем downloader спать на старом backoff.
	select {
	case t.downWake <- struct{}{}:
	default:
	}
}

func (t *tunnel) takeUpload() []byte {
	t.upMu.Lock()
	defer t.upMu.Unlock()

	if t.upBuf.Len() == 0 {
		return nil
	}

	data := append([]byte(nil), t.upBuf.Bytes()...)
	t.upBuf.Reset()

	return data
}

func (t *tunnel) uploader() {
	for {
		select {
		case <-t.done:
			return
		case <-t.wake:
		}
		for {
			plain := t.takeUpload()
			if len(plain) == 0 {
				break
			}
			padded, err := padTransportPlaintext(plain)
			if err != nil {
				t.fail(fmt.Errorf("pad upload: %w", err))
				return
			}

			record, err := t.crypto.sealUp(t.sid, padded)
			if err != nil {
				t.fail(fmt.Errorf("encrypt upload: %w", err))
				return
			}

			log.Printf(
				"PAD UP plain=%d padded=%d record=%d",
				len(plain),
				len(padded),
				len(record),
			)

			// The encrypted record is immutable until acknowledged. Retries therefore
			// preserve the exact ciphertext/counter while only changing their timing.
			var failureSince time.Time
			retryDelay := time.Duration(camouflage.Recovery.RetryMinMS) * time.Millisecond

			for {
				started := time.Now()
				resp, status, reqErr := t.request("POST", base+camouflage.UpPath+t.sid, record)
				log.Printf("UP HTTP=%d plain=%d wire=%d recv=%d time=%v", status, len(plain), len(record), len(resp), time.Since(started))

				if reqErr == nil && (status == http.StatusOK || status == http.StatusNoContent) {
					failureSince = time.Time{}
					retryDelay = time.Duration(camouflage.Recovery.RetryMinMS) * time.Millisecond

					if status == http.StatusOK && len(resp) > 0 {
						if err := t.acceptDownRecord(resp); err != nil {
							t.fail(fmt.Errorf("piggyback down record: %w", err))
							return
						}
					}
					break
				}

				if reqErr == nil && (status == http.StatusNotFound || status == http.StatusGone) {
					t.fail(fmt.Errorf("session lost: upload HTTP %d", status))
					return
				}

				if failureSince.IsZero() {
					failureSince = time.Now()
				}

				var failure error
				if reqErr != nil {
					failure = reqErr
					log.Printf("UPLOAD retry same record in %v: %v", retryDelay, reqErr)
				} else {
					failure = fmt.Errorf("HTTP %d", status)
					log.Printf("UPLOAD HTTP %d; retry same record in %v", status, retryDelay)
				}

				if time.Since(failureSince) >= time.Duration(camouflage.Recovery.FailureTimeoutMS)*time.Millisecond {
					t.fail(fmt.Errorf("upload transport unavailable for %v: %w", time.Since(failureSince).Round(time.Millisecond), failure))
					return
				}

				timer := time.NewTimer(retryDelay)
				select {
				case <-t.done:
					timer.Stop()
					return
				case <-timer.C:
				}

				retryDelay *= 2
				maxDelay := time.Duration(camouflage.Recovery.RetryMaxMS) * time.Millisecond
				if retryDelay > maxDelay {
					retryDelay = maxDelay
				}
			}
		}
	}
}

func (t *tunnel) activeStreams() int {
	t.streamsMu.Lock()
	defer t.streamsMu.Unlock()
	return len(t.streams)
}

func (t *tunnel) acceptDownRecord(record []byte) error {
	counter, paddedPlain, err := t.crypto.openDown(t.sid, record)
	if err != nil {
		return err
	}

	plain, err := unpadTransportPlaintext(paddedPlain)
	if err != nil {
		return err
	}

	t.downMu.Lock()
	if counter < t.nextDown {
		t.downMu.Unlock()
		return nil
	}
	if counter-t.nextDown > 256 {
		t.downMu.Unlock()
		return fmt.Errorf("down reorder window exceeded: got=%d want=%d", counter, t.nextDown)
	}
	if _, ok := t.downPending[counter]; !ok {
		t.downPending[counter] = plain
	}
	var ready [][]byte
	for {
		p, ok := t.downPending[t.nextDown]
		if !ok {
			break
		}
		delete(t.downPending, t.nextDown)
		ready = append(ready, p)
		t.nextDown++
	}
	t.downMu.Unlock()
	for _, p := range ready {
		if err := t.handleFrames(p); err != nil {
			return err
		}
	}
	return nil
}

func randomDuration(minMS, maxMS int) time.Duration {
	if minMS <= 0 {
		minMS = 1
	}
	if maxMS < minMS {
		maxMS = minMS
	}
	if minMS == maxMS {
		return time.Duration(minMS) * time.Millisecond
	}

	span := maxMS - minMS + 1

	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Duration((minMS+maxMS)/2) * time.Millisecond
	}

	n := binary.BigEndian.Uint64(b[:])
	ms := minMS + int(n%uint64(span))

	return time.Duration(ms) * time.Millisecond
}

func jitterDuration(base time.Duration, percent int) time.Duration {
	if percent <= 0 {
		return base
	}
	if percent > 100 {
		percent = 100
	}

	baseMS := int(base / time.Millisecond)
	delta := baseMS * percent / 100

	if delta < 1 {
		delta = 1
	}

	minMS := baseMS - delta
	maxMS := baseMS + delta

	if minMS < 1 {
		minMS = 1
	}

	return randomDuration(minMS, maxMS)
}

func (t *tunnel) downloader() {
	activeDelay := 250 * time.Millisecond
	var failureSince time.Time
	errorDelay := time.Duration(camouflage.Recovery.RetryMinMS) * time.Millisecond

	for {
		// Once a transport failure has been observed, keep probing on the
		// recovery backoff even if applications close all their streams.
		// Otherwise the normal 45-90 s idle camouflage delay could postpone
		// fail-fast far beyond failure_timeout_ms.
		if t.activeStreams() == 0 && failureSince.IsZero() {
			delay := randomDuration(
				camouflage.Timing.IdleMinMS,
				camouflage.Timing.IdleMaxMS,
			)

			timer := time.NewTimer(delay)

			select {
			case <-t.done:
				timer.Stop()
				return

			case <-t.downWake:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}

				time.Sleep(randomDuration(
					camouflage.Timing.WakeMinMS,
					camouflage.Timing.WakeMaxMS,
				))

			case <-timer.C:
			}

		} else {
			delay := jitterDuration(
				activeDelay,
				camouflage.Timing.ActiveJitterP,
			)

			timer := time.NewTimer(delay)

			select {
			case <-t.done:
				timer.Stop()
				return

			case <-t.downWake:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}

				time.Sleep(randomDuration(
					camouflage.Timing.WakeMinMS,
					camouflage.Timing.WakeMaxMS,
				))

			case <-timer.C:
			}
		}

		data, status, err := t.request(
			"GET",
			base+camouflage.DownPath+t.sid,
			nil,
		)

		if err != nil {
			if failureSince.IsZero() {
				failureSince = time.Now()
			}

			log.Printf("DOWNLOAD retry in %v: %v", errorDelay, err)

			if time.Since(failureSince) >= time.Duration(camouflage.Recovery.FailureTimeoutMS)*time.Millisecond {
				t.fail(fmt.Errorf("download transport unavailable for %v: %w", time.Since(failureSince).Round(time.Millisecond), err))
				return
			}

			activeDelay = errorDelay
			errorDelay *= 2
			maxDelay := time.Duration(camouflage.Recovery.RetryMaxMS) * time.Millisecond
			if errorDelay > maxDelay {
				errorDelay = maxDelay
			}
			continue
		}

		if status == http.StatusOK || status == http.StatusNoContent {
			failureSince = time.Time{}
			errorDelay = time.Duration(camouflage.Recovery.RetryMinMS) * time.Millisecond
		}

		switch status {
		case http.StatusOK:
			if err := t.acceptDownRecord(data); err != nil {
				t.fail(fmt.Errorf("download record: %w", err))
				return
			}

			activeDelay = 250 * time.Millisecond

		case http.StatusNoContent:
			if t.activeStreams() > 0 {
				activeDelay *= 2

				if activeDelay > 2*time.Second {
					activeDelay = 2 * time.Second
				}
			} else {
				activeDelay = 250 * time.Millisecond
			}

		case http.StatusGone, http.StatusNotFound:
			t.fail(fmt.Errorf("session lost: download HTTP %d", status))
			return

		default:
			if failureSince.IsZero() {
				failureSince = time.Now()
			}

			log.Printf("DOWNLOAD HTTP %d; retry in %v", status, errorDelay)

			if time.Since(failureSince) >= time.Duration(camouflage.Recovery.FailureTimeoutMS)*time.Millisecond {
				t.fail(fmt.Errorf("download transport unavailable for %v: HTTP %d", time.Since(failureSince).Round(time.Millisecond), status))
				return
			}

			activeDelay = errorDelay
			errorDelay *= 2
			maxDelay := time.Duration(camouflage.Recovery.RetryMaxMS) * time.Millisecond
			if errorDelay > maxDelay {
				errorDelay = maxDelay
			}
		}
	}
}

func (t *tunnel) handleFrames(data []byte) error {
	for len(data) > 0 {
		if len(data) < 9 {
			return fmt.Errorf("short header")
		}

		streamID := binary.BigEndian.Uint32(data[0:4])
		typ := data[4]
		n := binary.BigEndian.Uint32(data[5:9])

		data = data[9:]

		if uint64(n) > uint64(len(data)) {
			return fmt.Errorf("short payload")
		}

		payload := data[:n]
		data = data[n:]

		t.streamsMu.Lock()
		st := t.streams[streamID]
		t.streamsMu.Unlock()

		switch typ {
		case frameData:
			if st != nil && !st.closed.Load() {
				if _, err := st.conn.Write(payload); err != nil {
					t.closeLocal(streamID, false)
				}
			}

		case frameClose:
			t.closeLocal(streamID, false)

		case frameError:
			log.Printf(
				"STREAM %d server error: %s",
				streamID,
				string(payload),
			)
			t.closeLocal(streamID, false)

		case framePing:
			// reserved
		}
	}

	return nil
}

func (t *tunnel) closeLocal(id uint32, notify bool) {
	t.streamsMu.Lock()

	st := t.streams[id]

	if st != nil {
		delete(t.streams, id)
	}

	t.streamsMu.Unlock()

	if st == nil {
		return
	}

	if st.closed.CompareAndSwap(false, true) {
		_ = st.conn.Close()
	}

	if notify {
		t.queue(id, frameClose, nil)
	}
}

func (t *tunnel) handleLocal(conn net.Conn) {
	id := t.nextID.Add(1)

	st := &localStream{conn: conn}

	t.streamsMu.Lock()
	t.streams[id] = st
	t.streamsMu.Unlock()

	log.Printf("STREAM %d OPEN", id)

	// OPEN и первые DATA могут попасть в один HTTP batch.
	t.queue(id, frameOpen, nil)

	buf := make([]byte, 64*1024)

	for {
		n, err := conn.Read(buf)

		if n > 0 {
			payload := append([]byte(nil), buf[:n]...)
			t.queue(id, frameData, payload)
		}

		if err != nil {
			if err != io.EOF && !st.closed.Load() {
				log.Printf("STREAM %d local read: %v", id, err)
			}

			t.closeLocal(id, true)
			log.Printf("STREAM %d CLOSE", id)
			return
		}
	}
}

func paddedPlaintextSize(real int) (int, error) {
	needed := real + 4

	for i, class := range camouflage.PaddingClasses {
		if class < needed {
			continue
		}

		target := class

		if camouflage.PaddingExtraClassPercent > 0 &&
			i+1 < len(camouflage.PaddingClasses) {

			var b [1]byte
			if _, err := rand.Read(b[:]); err != nil {
				return 0, err
			}

			if int(b[0])*100/256 <
				camouflage.PaddingExtraClassPercent {
				target = camouflage.PaddingClasses[i+1]
			}
		}

		return target, nil
	}

	return 0, fmt.Errorf(
		"plaintext too large for padding classes: %d",
		real,
	)
}

func padTransportPlaintext(plain []byte) ([]byte, error) {
	target, err := paddedPlaintextSize(len(plain))
	if err != nil {
		return nil, err
	}

	out := make([]byte, target)

	binary.BigEndian.PutUint32(
		out[:4],
		uint32(len(plain)),
	)

	copy(out[4:], plain)

	padding := out[4+len(plain):]

	if len(padding) > 0 {
		if _, err := rand.Read(padding); err != nil {
			return nil, err
		}
	}

	return out, nil
}

func unpadTransportPlaintext(padded []byte) ([]byte, error) {
	if len(padded) < 4 {
		return nil, fmt.Errorf("short padded plaintext")
	}

	n := uint64(
		binary.BigEndian.Uint32(padded[:4]),
	)

	if n > uint64(len(padded)-4) {
		return nil, fmt.Errorf(
			"invalid padded plaintext length",
		)
	}

	return padded[4 : 4+int(n)], nil
}

func loadCamouflageConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var cfg struct {
		Camouflage camouflageConfig `json:"camouflage"`
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}

	var raw struct {
		Camouflage map[string]json.RawMessage `json:"camouflage"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if _, ok := raw.Camouflage["timing"]; ok {
		camouflage.Timing = cfg.Camouflage.Timing
	}
	if _, ok := raw.Camouflage["recovery"]; ok {
		camouflage.Recovery = cfg.Camouflage.Recovery
	}

	if cfg.Camouflage.OpenPath != "" {
		camouflage.OpenPath = cfg.Camouflage.OpenPath
	}
	if cfg.Camouflage.UpPath != "" {
		camouflage.UpPath = cfg.Camouflage.UpPath
	}
	if cfg.Camouflage.DownPath != "" {
		camouflage.DownPath = cfg.Camouflage.DownPath
	}
	if cfg.Camouflage.ClosePath != "" {
		camouflage.ClosePath = cfg.Camouflage.ClosePath
	}
	if cfg.Camouflage.ContentType != "" {
		camouflage.ContentType = cfg.Camouflage.ContentType
	}
	if cfg.Camouflage.RequestHeaders != nil {
		camouflage.RequestHeaders = cfg.Camouflage.RequestHeaders
	}
	if cfg.Camouflage.ResponseHeaders != nil {
		camouflage.ResponseHeaders = cfg.Camouflage.ResponseHeaders
	}
	if cfg.Camouflage.DownMaxBytes > 0 {
		camouflage.DownMaxBytes = cfg.Camouflage.DownMaxBytes
	}
	if len(cfg.Camouflage.PaddingClasses) > 0 {
		camouflage.PaddingClasses = cfg.Camouflage.PaddingClasses
	}
	if _, ok := raw.Camouflage["padding_extra_class_percent"]; ok {
		camouflage.PaddingExtraClassPercent = cfg.Camouflage.PaddingExtraClassPercent
	}

	paths := []string{
		camouflage.OpenPath,
		camouflage.UpPath,
		camouflage.DownPath,
		camouflage.ClosePath,
	}

	for _, p := range paths {
		if p == "" || p[0] != '/' {
			return fmt.Errorf("camouflage path must begin with /: %q", p)
		}
		if strings.ContainsAny(p, "?#") {
			return fmt.Errorf("invalid camouflage path: %q", p)
		}
	}

	if camouflage.OpenPath == camouflage.UpPath ||
		camouflage.OpenPath == camouflage.DownPath ||
		camouflage.UpPath == camouflage.DownPath {
		return fmt.Errorf("camouflage paths must be distinct")
	}

	if camouflage.DownMaxBytes < 1024 || camouflage.DownMaxBytes > 4*1024*1024 {
		return fmt.Errorf("down_max_bytes out of range: %d", camouflage.DownMaxBytes)
	}

	if len(camouflage.PaddingClasses) == 0 {
		return fmt.Errorf("padding_classes must not be empty")
	}

	prev := 0
	for _, n := range camouflage.PaddingClasses {
		if n < 64 || n > 4*1024*1024 {
			return fmt.Errorf("invalid padding class: %d", n)
		}
		if n <= prev {
			return fmt.Errorf("padding_classes must be strictly increasing")
		}
		prev = n
	}

	if camouflage.PaddingExtraClassPercent < 0 ||
		camouflage.PaddingExtraClassPercent > 100 {
		return fmt.Errorf(
			"padding_extra_class_percent out of range: %d",
			camouflage.PaddingExtraClassPercent,
		)
	}

	if camouflage.Timing.IdleMinMS < 1000 ||
		camouflage.Timing.IdleMaxMS < camouflage.Timing.IdleMinMS {
		return fmt.Errorf(
			"invalid timing idle range: %d..%d ms",
			camouflage.Timing.IdleMinMS,
			camouflage.Timing.IdleMaxMS,
		)
	}

	if camouflage.Timing.WakeMinMS < 0 ||
		camouflage.Timing.WakeMaxMS < camouflage.Timing.WakeMinMS ||
		camouflage.Timing.WakeMaxMS > 5000 {
		return fmt.Errorf(
			"invalid timing wake range: %d..%d ms",
			camouflage.Timing.WakeMinMS,
			camouflage.Timing.WakeMaxMS,
		)
	}

	if camouflage.Timing.ActiveJitterP < 0 ||
		camouflage.Timing.ActiveJitterP > 100 {
		return fmt.Errorf(
			"active_jitter_percent out of range: %d",
			camouflage.Timing.ActiveJitterP,
		)
	}

	if camouflage.Recovery.FailureTimeoutMS < 5000 ||
		camouflage.Recovery.FailureTimeoutMS > 300000 {
		return fmt.Errorf(
			"recovery.failure_timeout_ms out of range: %d",
			camouflage.Recovery.FailureTimeoutMS,
		)
	}
	if camouflage.Recovery.RetryMinMS < 100 ||
		camouflage.Recovery.RetryMaxMS < camouflage.Recovery.RetryMinMS ||
		camouflage.Recovery.RetryMaxMS > 30000 {
		return fmt.Errorf(
			"invalid recovery retry range: %d..%d ms",
			camouflage.Recovery.RetryMinMS,
			camouflage.Recovery.RetryMaxMS,
		)
	}

	return nil
}

func main() {
	if err := loadRuntimeConfig(runtimeConfigPath("/etc/mbox/client.json")); err != nil {
		log.Fatalf("CONFIG: %v", err)
	}

	t, err := newTunnel()
	if err != nil {
		log.Fatal(err)
	}

	ln, err := net.Listen("tcp", listen)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf(
		"MBox client SOCKS listening on %s session=%s",
		listen,
		t.sid,
	)

	shutdown := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		select {
		case err := <-t.fatalErr:
			log.Printf("FATAL: %v", err)
			_ = ln.Close()
		case sig := <-sigCh:
			log.Printf("SHUTDOWN signal=%s", sig)
			t.closeRemote()
			close(shutdown)
			_ = ln.Close()
		}
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-t.done:
				return
			case <-shutdown:
				return
			default:
				log.Printf("ACCEPT: %v", err)
				continue
			}
		}

		go t.handleLocal(conn)
	}
}
