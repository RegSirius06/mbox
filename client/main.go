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
	frameOpen   = 1
	frameData   = 2
	frameClose  = 3
	frameError  = 4
	framePing   = 5
	frameWindow = 6
)

var (
	listen string
	base   string
)

type localStream struct {
	conn          net.Conn
	writeQ        *byteQueue
	closed        atomic.Bool
	creditMu      sync.Mutex
	sendCredit    int
	recvAllowance int
	creditWake    chan struct{}
}

type timingConfig struct {
	IdleMinMS       int `json:"idle_min_ms"`
	IdleMaxMS       int `json:"idle_max_ms"`
	WakeMinMS       int `json:"wake_min_ms"`
	WakeMaxMS       int `json:"wake_max_ms"`
	ActiveJitterP   int `json:"active_jitter_percent"`
	ActiveMinMS     int `json:"active_min_ms"`
	ActiveMaxMS     int `json:"active_max_ms"`
	ActiveBackoffMS int `json:"active_backoff_max_ms"`
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
	UpMaxBytes               int               `json:"up_max_bytes"`
	PaddingClasses           []int             `json:"padding_classes"`
	PaddingExtraClassPercent int               `json:"padding_extra_class_percent"`
	PaddingRandomMaxBytes    int               `json:"padding_random_max_bytes"`
	BatchMinBytes            int               `json:"batch_min_bytes"`
	NoiseChunkMinBytes       int               `json:"noise_chunk_min_bytes"`
	NoiseChunkMaxBytes       int               `json:"noise_chunk_max_bytes"`
}

var camouflage = camouflageConfig{
	Timing: timingConfig{
		IdleMinMS:       45000,
		IdleMaxMS:       90000,
		WakeMinMS:       100,
		WakeMaxMS:       220,
		ActiveJitterP:   35,
		ActiveMinMS:     120,
		ActiveMaxMS:     420,
		ActiveBackoffMS: 2200,
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
	UpMaxBytes:   262144,
	PaddingClasses: []int{
		512, 1024, 2048, 4096, 8192, 16384,
		32768, 65536, 131072, 262144, 524288,
	},
	PaddingExtraClassPercent: 0,
	PaddingRandomMaxBytes:    16384,
	BatchMinBytes:            73728,
	NoiseChunkMinBytes:       16384,
	NoiseChunkMaxBytes:       61440,
}

type tunnel struct {
	sid    string
	crypto *cryptoState

	streamsMu sync.Mutex
	streams   map[uint32]*localStream
	nextID    atomic.Uint32

	upMu        sync.Mutex
	upQueues    map[uint32][][]byte
	upOrder     []uint32
	upBytes     int
	upPerStream map[uint32]int
	upSpace     chan struct{}

	wake        chan struct{}
	downWake    chan struct{}
	downDeliver chan []byte
	done        chan struct{}

	nextDown         uint64
	downPending      map[uint64][]byte
	downPendingBytes int
	downAck          atomic.Uint64
	downReqSeq       atomic.Uint64

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
		streams: make(map[uint32]*localStream), upQueues: make(map[uint32][][]byte), upPerStream: make(map[uint32]int), upSpace: make(chan struct{}, 1), wake: make(chan struct{}, 1), downWake: make(chan struct{}, 1), downDeliver: make(chan []byte, 64), done: make(chan struct{}), downPending: make(map[uint64][]byte), nextDown: 1, fatalErr: make(chan error, 1),
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

	openBody, noiseHS, err := buildOpenRequest()
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

	t.crypto, t.sid, err = finishClientHandshake(noiseHS, data)
	if err != nil {
		return nil, fmt.Errorf("Noise handshake: %w", err)
	}

	log.Printf("SESSION %s handshake=Noise_NKpsk0_25519_AESGCM_SHA256 record=v%d", t.sid, cryptoRecordVersion)

	go t.downDeliveryLoop()
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

func (t *tunnel) queue(streamID uint32, typ byte, payload []byte) error {
	if len(payload) > maxFramePayload {
		return fmt.Errorf("frame payload too large: %d", len(payload))
	}
	f := frame(streamID, typ, payload)
	if len(f) > camouflage.UpMaxBytes {
		return fmt.Errorf("frame exceeds UP record limit")
	}
	for {
		t.upMu.Lock()
		per := t.upPerStream[streamID]
		if t.upBytes+len(f) <= maxUpQueueBytes && per+len(f) <= maxUpPerStreamQueueBytes {
			if len(t.upQueues[streamID]) == 0 {
				t.upOrder = append(t.upOrder, streamID)
			}
			t.upQueues[streamID] = append(t.upQueues[streamID], f)
			t.upBytes += len(f)
			t.upPerStream[streamID] = per + len(f)
			t.upMu.Unlock()
			select {
			case t.wake <- struct{}{}:
			default:
			}
			select {
			case t.downWake <- struct{}{}:
			default:
			}
			return nil
		}
		t.upMu.Unlock()
		select {
		case <-t.done:
			return fmt.Errorf("tunnel stopped")
		case <-t.upSpace:
		}
	}
}
func (t *tunnel) takeUpload(limit int) []byte {
	if limit <= 0 {
		limit = camouflage.UpMaxBytes
	}
	t.upMu.Lock()
	defer t.upMu.Unlock()
	if t.upBytes == 0 || len(t.upOrder) == 0 {
		return nil
	}
	data := make([]byte, 0, limit)
	skipped, removed := 0, 0
	for len(t.upOrder) > 0 {
		sid := t.upOrder[0]
		t.upOrder = t.upOrder[1:]
		q := t.upQueues[sid]
		if len(q) == 0 {
			delete(t.upQueues, sid)
			delete(t.upPerStream, sid)
			continue
		}
		f := q[0]
		if len(data) > 0 && len(data)+len(f) > limit {
			t.upOrder = append(t.upOrder, sid)
			skipped++
			if skipped >= len(t.upOrder) {
				break
			}
			continue
		}
		if len(f) > limit {
			break
		}
		q[0] = nil
		q = q[1:]
		data = append(data, f...)
		t.upBytes -= len(f)
		t.upPerStream[sid] -= len(f)
		removed += len(f)
		skipped = 0
		if len(q) == 0 {
			delete(t.upQueues, sid)
			delete(t.upPerStream, sid)
		} else {
			t.upQueues[sid] = q
			t.upOrder = append(t.upOrder, sid)
		}
		if len(data) >= limit {
			break
		}
	}
	if removed > 0 {
		select {
		case t.upSpace <- struct{}{}:
		default:
		}
	}
	return data
}
func (t *tunnel) newDownRequestURL(raw string, includeMax bool) (string, uint64, error) {
	seq := t.downReqSeq.Add(1)
	u, err := url.Parse(raw)
	if err != nil {
		return "", 0, err
	}
	q := u.Query()
	q.Set("seq", fmt.Sprintf("%d", seq))
	q.Set("ack", fmt.Sprintf("%d", t.downAck.Load()))
	if includeMax {
		q.Set("max", fmt.Sprintf("%d", randomBatchLimit(camouflage.DownMaxBytes)))
	}
	u.RawQuery = q.Encode()
	return u.String(), seq, nil
}

func (t *tunnel) uploader() {
	for {
		select {
		case <-t.done:
			return
		case <-t.wake:
		}
		for {
			plain := t.takeUpload(randomBatchLimit(camouflage.UpMaxBytes))
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
			upURL, _, err := t.newDownRequestURL(base+camouflage.UpPath+t.sid, false)
			if err != nil {
				t.fail(err)
				return
			}
			var failureSince time.Time
			retry := time.Duration(camouflage.Recovery.RetryMinMS) * time.Millisecond
			for {
				resp, status, reqErr := t.request("POST", upURL, record)
				if reqErr == nil && (status == http.StatusOK || status == http.StatusNoContent) {
					if status == http.StatusOK && len(resp) > 0 {
						if err := t.submitDownRecord(resp); err != nil {
							t.fail(err)
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
				} else {
					failure = fmt.Errorf("HTTP %d", status)
				}
				if time.Since(failureSince) >= time.Duration(camouflage.Recovery.FailureTimeoutMS)*time.Millisecond {
					t.fail(fmt.Errorf("upload unavailable: %w", failure))
					return
				}
				timer := time.NewTimer(retry)
				select {
				case <-t.done:
					timer.Stop()
					return
				case <-timer.C:
				}
				retry *= 2
				max := time.Duration(camouflage.Recovery.RetryMaxMS) * time.Millisecond
				if retry > max {
					retry = max
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

func (t *tunnel) submitDownRecord(record []byte) error {
	c := append([]byte(nil), record...)
	select {
	case <-t.done:
		return fmt.Errorf("tunnel stopped")
	case t.downDeliver <- c:
		return nil
	}
}
func (t *tunnel) downDeliveryLoop() {
	for {
		select {
		case <-t.done:
			return
		case record := <-t.downDeliver:
			counter, padded, err := t.crypto.openDown(t.sid, record)
			if err != nil {
				t.fail(fmt.Errorf("down authentication: %w", err))
				return
			}
			plain, err := unpadTransportPlaintext(padded)
			if err != nil {
				t.fail(fmt.Errorf("down padding: %w", err))
				return
			}
			if counter < t.nextDown {
				continue
			}
			if counter-t.nextDown > 256 {
				t.fail(fmt.Errorf("down reorder window exceeded: got=%d want=%d", counter, t.nextDown))
				return
			}
			if _, exists := t.downPending[counter]; !exists {
				if t.downPendingBytes+len(plain) > maxDownPendingBytes {
					t.fail(fmt.Errorf("down pending memory budget exceeded"))
					return
				}
				t.downPending[counter] = plain
				t.downPendingBytes += len(plain)
			}
			for {
				p, ok := t.downPending[t.nextDown]
				if !ok {
					break
				}
				delete(t.downPending, t.nextDown)
				t.downPendingBytes -= len(p)
				if err := t.handleFrames(p); err != nil {
					t.fail(fmt.Errorf("down frame delivery: %w", err))
					return
				}
				t.downAck.Store(t.nextDown)
				t.nextDown++
				select {
				case t.downWake <- struct{}{}:
				default:
				}
			}
		}
	}
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
	activeDelay := randomDuration(camouflage.Timing.ActiveMinMS, camouflage.Timing.ActiveMaxMS)
	var failureSince time.Time
	for {
		if t.activeStreams() == 0 && failureSince.IsZero() {
			timer := time.NewTimer(randomDuration(camouflage.Timing.IdleMinMS, camouflage.Timing.IdleMaxMS))
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
				time.Sleep(randomDuration(camouflage.Timing.WakeMinMS, camouflage.Timing.WakeMaxMS))
			case <-timer.C:
			}
		} else {
			timer := time.NewTimer(jitterDuration(activeDelay, camouflage.Timing.ActiveJitterP))
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
			case <-timer.C:
			}
		}
		requestURL, _, err := t.newDownRequestURL(base+camouflage.DownPath+t.sid, true)
		if err != nil {
			t.fail(err)
			return
		}
		retry := time.Duration(camouflage.Recovery.RetryMinMS) * time.Millisecond
		for {
			data, status, reqErr := t.request("GET", requestURL, nil)
			if reqErr == nil && (status == http.StatusOK || status == http.StatusNoContent) {
				failureSince = time.Time{}
				if status == http.StatusOK {
					if err := t.submitDownRecord(data); err != nil {
						t.fail(err)
						return
					}
					activeDelay = randomDuration(camouflage.Timing.ActiveMinMS, camouflage.Timing.ActiveMaxMS)
				} else if t.activeStreams() > 0 {
					baseMS := int(activeDelay / time.Millisecond)
					nextMin := baseMS + baseMS/3
					nextMax := baseMS*2 + baseMS/3
					if nextMax > camouflage.Timing.ActiveBackoffMS {
						nextMax = camouflage.Timing.ActiveBackoffMS
					}
					if nextMin > nextMax {
						nextMin = nextMax
					}
					activeDelay = randomDuration(nextMin, nextMax)
				} else {
					activeDelay = randomDuration(camouflage.Timing.ActiveMinMS, camouflage.Timing.ActiveMaxMS)
				}
				break
			}
			if reqErr == nil && (status == http.StatusGone || status == http.StatusNotFound) {
				t.fail(fmt.Errorf("session lost: download HTTP %d", status))
				return
			}
			if reqErr == nil && status >= 400 && status < 500 {
				t.fail(fmt.Errorf("download protocol HTTP %d", status))
				return
			}
			if failureSince.IsZero() {
				failureSince = time.Now()
			}
			var failure error
			if reqErr != nil {
				failure = reqErr
			} else {
				failure = fmt.Errorf("HTTP %d", status)
			}
			if time.Since(failureSince) >= time.Duration(camouflage.Recovery.FailureTimeoutMS)*time.Millisecond {
				t.fail(fmt.Errorf("download unavailable: %w", failure))
				return
			}
			timer := time.NewTimer(retry)
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
			case <-timer.C:
			}
			retry *= 2
			max := time.Duration(camouflage.Recovery.RetryMaxMS) * time.Millisecond
			if retry > max {
				retry = max
			}
		}
	}
}

func (t *tunnel) grantPeerCredit(id uint32, st *localStream, n int) error {
	if err := st.grantPeerAllowance(n); err != nil {
		return err
	}
	if err := t.queue(id, frameWindow, encodeWindowCredit(n)); err != nil {
		st.rollbackPeerAllowance(n)
		return err
	}
	return nil
}
func (t *tunnel) localWriter(id uint32, st *localStream) {
	for {
		payload, ok := st.writeQ.pop(t.done)
		if !ok {
			return
		}
		_ = st.conn.SetWriteDeadline(time.Now().Add(localWriteTimeout))
		written := 0
		var err error
		for written < len(payload) {
			var n int
			n, err = st.conn.Write(payload[written:])
			written += n
			if err != nil || n == 0 {
				if err == nil {
					err = io.ErrUnexpectedEOF
				}
				break
			}
		}
		if err != nil {
			t.closeLocal(id, true)
			return
		}
		if err := t.grantPeerCredit(id, st, len(payload)); err != nil {
			t.closeLocal(id, false)
			return
		}
	}
}
func (t *tunnel) handleFrames(data []byte) error {
	for len(data) > 0 {
		if len(data) < 9 {
			return fmt.Errorf("short header")
		}
		id := binary.BigEndian.Uint32(data[:4])
		typ := data[4]
		n := binary.BigEndian.Uint32(data[5:9])
		data = data[9:]
		if uint64(n) > uint64(len(data)) {
			return fmt.Errorf("short payload")
		}
		if n > maxFramePayload {
			return fmt.Errorf("frame payload too large: %d", n)
		}
		payload := data[:n]
		data = data[n:]
		t.streamsMu.Lock()
		st := t.streams[id]
		t.streamsMu.Unlock()
		switch typ {
		case frameData:
			if st == nil || st.closed.Load() {
				continue
			}
			if err := st.consumePeerAllowance(len(payload)); err != nil {
				return err
			}
			if !st.writeQ.tryPush(payload) {
				return fmt.Errorf("local stream write queue overflow")
			}
		case frameWindow:
			if st == nil || st.closed.Load() {
				continue
			}
			credit, err := decodeWindowCredit(payload)
			if err != nil {
				return err
			}
			if err := st.addSendCredit(credit); err != nil {
				return err
			}
		case frameClose:
			if len(payload) != 0 {
				return fmt.Errorf("CLOSE payload must be empty")
			}
			t.closeLocal(id, false)
		case frameError:
			log.Printf("STREAM %d server error: %s", id, string(payload))
			t.closeLocal(id, false)
		case framePing:
		case frameOpen:
			return fmt.Errorf("server-initiated OPEN is not supported")
		default:
			return fmt.Errorf("unknown frame type %d", typ)
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
	st.writeQ.closeAndDrain()
	st.wakeCreditWaiters()
	if st.closed.CompareAndSwap(false, true) {
		_ = st.conn.Close()
	}
	if notify {
		_ = t.queue(id, frameClose, nil)
	}
}
func (t *tunnel) handleLocal(conn net.Conn) {
	t.streamsMu.Lock()
	if len(t.streams) >= maxClientStreams {
		t.streamsMu.Unlock()
		_ = conn.Close()
		return
	}
	id := t.nextID.Add(1)
	st := &localStream{conn: conn, writeQ: newByteQueue(streamWriteQueueMaxBytes), creditWake: make(chan struct{}, 1)}
	t.streams[id] = st
	t.streamsMu.Unlock()
	go t.localWriter(id, st)
	if err := t.queue(id, frameOpen, nil); err != nil {
		t.closeLocal(id, false)
		return
	}
	if err := t.grantPeerCredit(id, st, streamInitialWindow); err != nil {
		t.closeLocal(id, true)
		return
	}
	buf := make([]byte, maxFramePayload)
	for {
		grant, ok := st.reserveSendCredit(len(buf), t.done)
		if !ok {
			return
		}
		n, err := conn.Read(buf[:grant])
		if n < grant {
			st.refundSendCredit(grant - n)
		}
		if n > 0 {
			payload := append([]byte(nil), buf[:n]...)
			if qerr := t.queue(id, frameData, payload); qerr != nil {
				t.closeLocal(id, false)
				return
			}
		}
		if err != nil {
			t.closeLocal(id, true)
			return
		}
	}
}

func paddedPlaintextSize(real int) (int, error) {
	needed := real + 4
	if camouflage.PaddingRandomMaxBytes > 0 {
		extra, err := randomIntInclusive(0, camouflage.PaddingRandomMaxBytes)
		if err != nil {
			return 0, err
		}
		return needed + extra, nil
	}

	// Compatibility mode for explicitly configured legacy padding classes.
	for i, class := range camouflage.PaddingClasses {
		if class < needed {
			continue
		}
		target := class
		if camouflage.PaddingExtraClassPercent > 0 && i+1 < len(camouflage.PaddingClasses) {
			var b [1]byte
			if _, err := rand.Read(b[:]); err != nil {
				return 0, err
			}
			if int(b[0])*100/256 < camouflage.PaddingExtraClassPercent {
				target = camouflage.PaddingClasses[i+1]
			}
		}
		return target, nil
	}
	return 0, fmt.Errorf("plaintext too large for padding policy: %d", real)
}

func randomBatchLimit(max int) int {
	min := camouflage.BatchMinBytes
	if min < maxFramePayload+9 {
		min = maxFramePayload + 9
	}
	if min <= 0 || min >= max {
		return max
	}
	n, err := randomIntInclusive(min, max)
	if err != nil {
		return max
	}
	return n
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
		// Old beta profiles predate the stochastic active-poll fields.  They
		// remain valid; missing fields inherit beta.2 defaults.
		if camouflage.Timing.ActiveMinMS == 0 {
			camouflage.Timing.ActiveMinMS = 120
		}
		if camouflage.Timing.ActiveMaxMS == 0 {
			camouflage.Timing.ActiveMaxMS = 420
		}
		if camouflage.Timing.ActiveBackoffMS == 0 {
			camouflage.Timing.ActiveBackoffMS = 2200
		}
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
	if cfg.Camouflage.UpMaxBytes > 0 {
		camouflage.UpMaxBytes = cfg.Camouflage.UpMaxBytes
	}
	if len(cfg.Camouflage.PaddingClasses) > 0 {
		camouflage.PaddingClasses = cfg.Camouflage.PaddingClasses
	}
	if _, ok := raw.Camouflage["padding_extra_class_percent"]; ok {
		camouflage.PaddingExtraClassPercent = cfg.Camouflage.PaddingExtraClassPercent
	}
	if _, ok := raw.Camouflage["padding_random_max_bytes"]; ok {
		camouflage.PaddingRandomMaxBytes = cfg.Camouflage.PaddingRandomMaxBytes
	}
	if cfg.Camouflage.BatchMinBytes > 0 {
		camouflage.BatchMinBytes = cfg.Camouflage.BatchMinBytes
	}
	if cfg.Camouflage.NoiseChunkMinBytes > 0 {
		camouflage.NoiseChunkMinBytes = cfg.Camouflage.NoiseChunkMinBytes
	}
	if cfg.Camouflage.NoiseChunkMaxBytes > 0 {
		camouflage.NoiseChunkMaxBytes = cfg.Camouflage.NoiseChunkMaxBytes
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
	if camouflage.UpMaxBytes < maxFramePayload+9 || camouflage.UpMaxBytes > 1024*1024 {
		return fmt.Errorf("up_max_bytes out of range: %d", camouflage.UpMaxBytes)
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

	if camouflage.PaddingRandomMaxBytes == 0 && camouflage.UpMaxBytes+4 > camouflage.PaddingClasses[len(camouflage.PaddingClasses)-1] {
		return fmt.Errorf("up_max_bytes exceeds largest padding class")
	}

	if camouflage.Timing.ActiveMinMS < 20 || camouflage.Timing.ActiveMinMS > camouflage.Timing.ActiveMaxMS || camouflage.Timing.ActiveMaxMS > 5000 {
		return fmt.Errorf("invalid active polling range")
	}
	if camouflage.Timing.ActiveBackoffMS < camouflage.Timing.ActiveMaxMS || camouflage.Timing.ActiveBackoffMS > 30000 {
		return fmt.Errorf("invalid active_backoff_max_ms")
	}

	if camouflage.PaddingExtraClassPercent < 0 ||
		camouflage.PaddingExtraClassPercent > 100 {
		return fmt.Errorf(
			"padding_extra_class_percent out of range: %d",
			camouflage.PaddingExtraClassPercent,
		)
	}
	if camouflage.PaddingRandomMaxBytes < 0 || camouflage.PaddingRandomMaxBytes > 256*1024 {
		return fmt.Errorf("padding_random_max_bytes out of range: %d", camouflage.PaddingRandomMaxBytes)
	}
	if camouflage.BatchMinBytes < maxFramePayload+9 || camouflage.BatchMinBytes > camouflage.UpMaxBytes || camouflage.BatchMinBytes > camouflage.DownMaxBytes {
		return fmt.Errorf("batch_min_bytes out of range: %d", camouflage.BatchMinBytes)
	}
	if camouflage.NoiseChunkMinBytes < 1024 || camouflage.NoiseChunkMinBytes > camouflage.NoiseChunkMaxBytes {
		return fmt.Errorf("noise_chunk_min_bytes out of range: %d", camouflage.NoiseChunkMinBytes)
	}
	if camouflage.NoiseChunkMaxBytes > 65519 {
		return fmt.Errorf("noise_chunk_max_bytes exceeds Noise transport limit: %d", camouflage.NoiseChunkMaxBytes)
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
