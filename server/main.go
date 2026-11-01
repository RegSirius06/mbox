package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	// Resource limits are implementation safeguards, not deployment settings.
	maxRequestBody = 2 * 1024 * 1024
	maxDownQueue   = 8 * 1024 * 1024

	frameOpen   = 1
	frameData   = 2
	frameClose  = 3
	frameError  = 4
	framePing   = 5
	frameWindow = 6

	maxDownReplayRequests = 1024
)

var (
	listen    string
	socksAddr string
)

// MUX frame layout is frozen by MBox/1:
// uint32 stream ID | byte type | uint32 payload length | payload.
// Frames are never split across encrypted DOWN records; this is what lets the
// client reorder whole records without needing a second framing layer.

type stream struct {
	conn          net.Conn
	writeQ        *byteQueue
	closed        atomic.Bool
	creditMu      sync.Mutex
	sendCredit    int
	recvAllowance int
	creditWake    chan struct{}
}

type cachedUpResponse struct {
	recordHash [32]byte
	downSeq    uint64
	status     int
}

type cachedDownResponse struct {
	counter uint64
	status  int
	body    []byte
}

type session struct {
	mu              sync.Mutex
	upProcessMu     sync.Mutex
	downCommitMu    sync.Mutex
	upCache         map[uint64]cachedUpResponse
	upCacheOrder    []uint64
	clientID        string
	streams         map[uint32]*stream
	crypto          *cryptoState
	downFrames      [][]byte
	downBytes       int
	downReady       chan struct{}
	downReplay      map[uint64]cachedDownResponse
	downReplayBytes int
	downReqSeen     map[uint64]struct{}
	downReqMax      uint64
	lastDownAck     uint64
	lastDownCounter uint64
	openWindowStart time.Time
	openWindowCount int
	done            chan struct{}
	closeOnce       sync.Once
	lastSeen        time.Time
	closed          bool
}

type rateWindow struct {
	start time.Time
	count int
}

var (
	sessionsMu        sync.Mutex
	sessions          = make(map[string]*session)
	sessionOpenRates  = make(map[string]rateWindow)
	totalStreams      atomic.Int64
	globalQueuedBytes atomic.Int64
	runtimeStatusMu   sync.Mutex
)

const runtimeStatusPath = "/run/mbox/status.json"

type runtimeStatusSnapshot struct {
	GeneratedAt    time.Time      `json:"generated_at"`
	ActiveSessions int            `json:"active_sessions"`
	ActiveStreams  int64          `json:"active_streams"`
	QueuedBytes    int64          `json:"queued_bytes"`
	Clients        map[string]int `json:"clients"`
}

func writeRuntimeStatus() {
	runtimeStatusMu.Lock()
	defer runtimeStatusMu.Unlock()

	snap := runtimeStatusSnapshot{
		GeneratedAt:   time.Now().UTC(),
		ActiveStreams: totalStreams.Load(),
		QueuedBytes:   globalQueuedBytes.Load(),
		Clients:       make(map[string]int),
	}
	sessionsMu.Lock()
	snap.ActiveSessions = len(sessions)
	for _, s := range sessions {
		if s.clientID != "" {
			snap.Clients[s.clientID]++
		}
	}
	sessionsMu.Unlock()

	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return
	}
	b = append(b, '\n')
	tmp := runtimeStatusPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o640); err != nil {
		return
	}
	_ = os.Rename(tmp, runtimeStatusPath)
}

func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func getSession(id string) (*session, bool) {
	sessionsMu.Lock()
	s, ok := sessions[id]
	if !ok {
		sessionsMu.Unlock()
		return nil, false
	}
	clientID := s.clientID
	sessionsMu.Unlock()

	// Expiry is checked on every live HTTP transaction, not only by the
	// periodic cleanup ticker. A naturally expiring paid profile therefore
	// loses its active session on the next poll/POST without waiting a minute.
	if clientID != "" {
		p, policyOK := getClientPolicy(clientID)
		if !policyOK || !clientPolicyActive(p) {
			closeSession(id)
			return nil, false
		}
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, false
	}
	s.lastSeen = time.Now()
	s.mu.Unlock()
	return s, true
}

func registerSession(id string, s *session) bool {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	if len(sessions) >= runtimeLimits.MaxSessionsGlobal {
		return false
	}
	limit := runtimeLimits.DefaultMaxSessionsPerClient
	if p, ok := getClientPolicy(s.clientID); ok && p.MaxSessions != 0 {
		limit = p.MaxSessions
	}
	if limit >= 0 {
		count := 0
		for _, e := range sessions {
			if e.clientID == s.clientID {
				count++
			}
		}
		if count >= limit {
			return false
		}
	}
	key := s.clientID
	if key == "" {
		key = "legacy"
	}
	now := time.Now()
	r := sessionOpenRates[key]
	if r.start.IsZero() || now.Sub(r.start) >= time.Minute {
		r = rateWindow{start: now}
	}
	if r.count >= runtimeLimits.MaxSessionOpensPerMinute {
		return false
	}
	r.count++
	sessionOpenRates[key] = r
	sessions[id] = s
	return true
}
func revokeUnauthorizedSessions(policies map[string]clientPolicy) {
	var revoke []string
	type active struct {
		id   string
		last time.Time
	}
	byClient := map[string][]active{}
	sessionsMu.Lock()
	for id, s := range sessions {
		if s.clientID == "" {
			continue
		}
		p, ok := policies[s.clientID]
		if !ok || !clientPolicyActive(p) || sha256.Sum256(p.PSK) != s.crypto.authPSKHash {
			revoke = append(revoke, id)
			continue
		}
		s.mu.Lock()
		last := s.lastSeen
		s.mu.Unlock()
		byClient[s.clientID] = append(byClient[s.clientID], active{id, last})
	}
	for cid, list := range byClient {
		p := policies[cid]
		limit := p.MaxSessions
		if limit == 0 {
			limit = runtimeLimits.DefaultMaxSessionsPerClient
		}
		if limit < 0 || len(list) <= limit {
			continue
		}
		sort.Slice(list, func(i, j int) bool { return list[i].last.Before(list[j].last) })
		for i := 0; i < len(list)-limit; i++ {
			revoke = append(revoke, list[i].id)
		}
	}
	sessionsMu.Unlock()
	for _, id := range revoke {
		log.Printf("SESSION REVOKE %s", id)
		closeSession(id)
	}
}

func appendFrame(s *session, streamID uint32, typ byte, payload []byte) error {
	if len(payload) > maxFramePayload {
		return fmt.Errorf("frame payload too large: %d", len(payload))
	}
	required := 9 + len(payload)
	f := make([]byte, required)
	binary.BigEndian.PutUint32(f[0:4], streamID)
	f[4] = typ
	binary.BigEndian.PutUint32(f[5:9], uint32(len(payload)))
	copy(f[9:], payload)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("session closed")
	}
	if s.downBytes+required > runtimeLimits.MaxDownQueueBytes {
		return fmt.Errorf("down queue overflow")
	}
	if !reserveGlobalQueued(required) {
		return fmt.Errorf("global queue budget exhausted")
	}
	s.downFrames = append(s.downFrames, f)
	s.downBytes += required
	select {
	case s.downReady <- struct{}{}:
	default:
	}
	return nil
}

func peekDownBatchLocked(s *session, limit int) ([]byte, int, int) {
	if len(s.downFrames) == 0 {
		return nil, 0, 0
	}
	if limit <= 0 {
		limit = 256 * 1024
	}
	total, count := 0, 0
	for count < len(s.downFrames) {
		n := len(s.downFrames[count])
		if count > 0 && total+n > limit {
			break
		}
		total += n
		count++
		if total >= limit {
			break
		}
	}
	data := make([]byte, 0, total)
	for i := 0; i < count; i++ {
		data = append(data, s.downFrames[i]...)
	}
	return data, count, total
}
func consumeDownBatchLocked(s *session, count, total int) {
	if count <= 0 {
		return
	}
	for i := 0; i < count; i++ {
		s.downFrames[i] = nil
	}
	s.downFrames = s.downFrames[count:]
	s.downBytes -= total
	if s.downBytes < 0 {
		panic("negative DOWN queue size")
	}
	releaseGlobalQueued(total)
	if len(s.downFrames) == 0 {
		s.downFrames = nil
		select {
		case <-s.downReady:
		default:
		}
	}
}
func popDownBatchLocked(s *session, limit int) []byte {
	data, count, total := peekDownBatchLocked(s, limit)
	consumeDownBatchLocked(s, count, total)
	return data
}

func reserveTotalStream() bool {
	for {
		old := totalStreams.Load()
		if old >= int64(runtimeLimits.MaxTotalStreams) {
			return false
		}
		if totalStreams.CompareAndSwap(old, old+1) {
			return true
		}
	}
}
func allowStreamOpenLocked(s *session) error {
	now := time.Now()
	if s.openWindowStart.IsZero() || now.Sub(s.openWindowStart) >= time.Second {
		s.openWindowStart = now
		s.openWindowCount = 0
	}
	if s.openWindowCount >= runtimeLimits.MaxStreamOpensPerSecond {
		return fmt.Errorf("stream open rate exceeded")
	}
	if len(s.streams) >= runtimeLimits.MaxStreamsPerSession {
		return fmt.Errorf("streams/session quota exceeded")
	}
	s.openWindowCount++
	return nil
}
func grantPeerCredit(s *session, id uint32, st *stream, n int) error {
	if err := st.grantPeerAllowance(n); err != nil {
		return err
	}
	if err := appendFrame(s, id, frameWindow, encodeWindowCredit(n)); err != nil {
		st.rollbackPeerAllowance(n)
		return err
	}
	return nil
}
func closeStream(s *session, id uint32, notify bool) {
	s.mu.Lock()
	st, ok := s.streams[id]
	if ok {
		delete(s.streams, id)
	}
	s.mu.Unlock()
	if !ok {
		return
	}
	totalStreams.Add(-1)
	if d := st.writeQ.closeAndDrain(); d > 0 {
		releaseGlobalQueued(d)
	}
	st.wakeCreditWaiters()
	if st.closed.CompareAndSwap(false, true) {
		_ = st.conn.Close()
	}
	if notify {
		_ = appendFrame(s, id, frameClose, nil)
	}
}
func streamWriter(s *session, id uint32, st *stream) {
	for {
		payload, ok := st.writeQ.pop(s.done)
		if !ok {
			return
		}
		_ = st.conn.SetWriteDeadline(time.Now().Add(upstreamWriteTimeout))
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
		releaseGlobalQueued(len(payload))
		if err != nil {
			if !st.closed.Load() {
				log.Printf("TCP WRITE stream=%d: %v", id, err)
			}
			closeStream(s, id, true)
			return
		}
		if err := grantPeerCredit(s, id, st, len(payload)); err != nil {
			log.Printf("WINDOW grant stream=%d: %v", id, err)
			closeStream(s, id, false)
			return
		}
	}
}
func streamReader(s *session, id uint32, st *stream) {
	buf := make([]byte, maxFramePayload)
	for {
		grant, ok := st.reserveSendCredit(len(buf), s.done)
		if !ok {
			return
		}
		n, err := st.conn.Read(buf[:grant])
		if n < grant {
			st.refundSendCredit(grant - n)
		}
		if n > 0 {
			payload := append([]byte(nil), buf[:n]...)
			if e := appendFrame(s, id, frameData, payload); e != nil {
				log.Printf("DOWN QUEUE stream=%d: %v", id, e)
				closeStream(s, id, false)
				return
			}
		}
		if err != nil {
			if err != io.EOF && !st.closed.Load() {
				log.Printf("TCP READ stream=%d: %v", id, err)
			}
			closeStream(s, id, true)
			return
		}
	}
}
func handleFrame(s *session, streamID uint32, typ byte, payload []byte) error {
	switch typ {
	case frameOpen:
		if len(payload) != 0 {
			return fmt.Errorf("OPEN payload must be empty")
		}
		s.mu.Lock()
		_, exists := s.streams[streamID]
		if exists {
			s.mu.Unlock()
			return nil
		}
		if err := allowStreamOpenLocked(s); err != nil {
			s.mu.Unlock()
			_ = appendFrame(s, streamID, frameError, []byte(err.Error()))
			return nil
		}
		s.mu.Unlock()
		if !reserveTotalStream() {
			_ = appendFrame(s, streamID, frameError, []byte("global stream quota exceeded"))
			return nil
		}
		conn, err := net.DialTimeout("tcp", socksAddr, 5*time.Second)
		if err != nil {
			totalStreams.Add(-1)
			_ = appendFrame(s, streamID, frameError, []byte("connect upstream failed"))
			return nil
		}
		st := &stream{conn: conn, writeQ: newByteQueue(streamWriteQueueMaxBytes), creditWake: make(chan struct{}, 1)}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			totalStreams.Add(-1)
			_ = conn.Close()
			return nil
		}
		if _, dup := s.streams[streamID]; dup {
			s.mu.Unlock()
			totalStreams.Add(-1)
			_ = conn.Close()
			return nil
		}
		s.streams[streamID] = st
		s.mu.Unlock()
		go streamWriter(s, streamID, st)
		go streamReader(s, streamID, st)
		if err := grantPeerCredit(s, streamID, st, streamInitialWindow); err != nil {
			closeStream(s, streamID, false)
			return err
		}
		return nil
	case frameData:
		if len(payload) > maxFramePayload {
			return fmt.Errorf("DATA frame too large: %d", len(payload))
		}
		s.mu.Lock()
		st := s.streams[streamID]
		s.mu.Unlock()
		if st == nil || st.closed.Load() {
			return nil
		}
		if err := st.consumePeerAllowance(len(payload)); err != nil {
			return err
		}
		if !reserveGlobalQueued(len(payload)) {
			return fmt.Errorf("global queue budget exhausted")
		}
		if !st.writeQ.tryPush(payload) {
			releaseGlobalQueued(len(payload))
			return fmt.Errorf("stream write queue overflow")
		}
		return nil
	case frameWindow:
		credit, err := decodeWindowCredit(payload)
		if err != nil {
			return err
		}
		s.mu.Lock()
		st := s.streams[streamID]
		s.mu.Unlock()
		if st == nil || st.closed.Load() {
			return nil
		}
		return st.addSendCredit(credit)
	case frameClose:
		if len(payload) != 0 {
			return fmt.Errorf("CLOSE payload must be empty")
		}
		closeStream(s, streamID, false)
		return nil
	case framePing:
		return appendFrame(s, streamID, framePing, payload)
	default:
		return fmt.Errorf("unknown frame type %d", typ)
	}
}

func parseFrames(s *session, data []byte) error {
	for len(data) > 0 {
		if len(data) < 9 {
			return fmt.Errorf("short frame header")
		}

		streamID := binary.BigEndian.Uint32(data[0:4])
		typ := data[4]
		n := binary.BigEndian.Uint32(data[5:9])

		data = data[9:]

		if uint64(n) > uint64(len(data)) {
			return fmt.Errorf("short frame payload")
		}
		if n > maxFramePayload {
			return fmt.Errorf("frame payload too large: %d", n)
		}
		payload := data[:n]
		data = data[n:]

		if err := handleFrame(s, streamID, typ, payload); err != nil {
			return err
		}
	}

	return nil
}

func closeSession(id string) {
	sessionsMu.Lock()
	s, ok := sessions[id]
	if ok {
		delete(sessions, id)
	}
	sessionsMu.Unlock()
	if !ok {
		return
	}
	s.downCommitMu.Lock()
	defer s.downCommitMu.Unlock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	ids := make([]uint32, 0, len(s.streams))
	for id := range s.streams {
		ids = append(ids, id)
	}
	queued := s.downBytes + s.downReplayBytes
	for i := range s.downFrames {
		s.downFrames[i] = nil
	}
	s.downFrames = nil
	s.downBytes = 0
	for seq, r := range s.downReplay {
		r.body = nil
		delete(s.downReplay, seq)
	}
	s.downReplayBytes = 0
	s.mu.Unlock()
	if queued > 0 {
		releaseGlobalQueued(queued)
	}
	s.closeOnce.Do(func() { close(s.done) })
	for _, streamID := range ids {
		closeStream(s, streamID, false)
	}
	log.Printf("SESSION CLOSE %s", id)
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

func writeCachedResponse(w http.ResponseWriter, status int, body []byte) {
	applyResponseHeaders(w)

	wireBody := body

	if len(body) > 0 {
		var err error
		wireBody, err = encodeHTTPEnvelope(body)
		if err != nil {
			camouflageNotFound(w)
			return
		}

		w.Header().Set("Content-Type", camouflage.ContentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(wireBody)))
	}

	w.WriteHeader(status)

	if len(wireBody) > 0 {
		_, _ = w.Write(wireBody)
	}
}

func cacheUpResponse(s *session, counter uint64, record []byte, downSeq uint64, status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upCache[counter] = cachedUpResponse{recordHash: sha256.Sum256(record), downSeq: downSeq, status: status}
	s.upCacheOrder = append(s.upCacheOrder, counter)
	for len(s.upCacheOrder) > 64 {
		old := s.upCacheOrder[0]
		s.upCacheOrder = s.upCacheOrder[1:]
		delete(s.upCache, old)
	}
}
func replayCachedUpResponse(s *session, c cachedUpResponse) (int, []byte) {
	if c.status != http.StatusOK {
		return c.status, nil
	}
	s.mu.Lock()
	d, ok := s.downReplay[c.downSeq]
	s.mu.Unlock()
	if !ok {
		return http.StatusNoContent, nil
	}
	return d.status, append([]byte(nil), d.body...)
}

func parseDownRequestMeta(r *http.Request) (uint64, uint64, error) {
	seqText := r.URL.Query().Get("seq")
	ackText := r.URL.Query().Get("ack")
	if seqText == "" || ackText == "" {
		return 0, 0, fmt.Errorf("missing DOWN sequence metadata")
	}
	seq, err := strconv.ParseUint(seqText, 10, 64)
	if err != nil || seq == 0 {
		return 0, 0, fmt.Errorf("invalid DOWN request sequence")
	}
	ack, err := strconv.ParseUint(ackText, 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid DOWN ACK")
	}
	return seq, ack, nil
}
func cloneDownResponse(r cachedDownResponse) cachedDownResponse {
	return cachedDownResponse{counter: r.counter, status: r.status, body: append([]byte(nil), r.body...)}
}
func applyDownAckLocked(s *session, ack uint64) error {
	if ack < s.lastDownAck {
		return nil
	}
	if ack > s.lastDownCounter {
		return fmt.Errorf("DOWN ACK beyond committed counter: ack=%d committed=%d", ack, s.lastDownCounter)
	}
	if ack == s.lastDownAck {
		return nil
	}
	for seq, r := range s.downReplay {
		if r.counter != 0 && r.counter <= ack {
			if len(r.body) > 0 {
				s.downReplayBytes -= len(r.body)
				releaseGlobalQueued(len(r.body))
			}
			delete(s.downReplay, seq)
		}
	}
	if s.downReplayBytes < 0 {
		panic("negative DOWN replay bytes")
	}
	s.lastDownAck = ack
	return nil
}
func rememberDownRequestLocked(s *session, seq uint64) {
	s.downReqSeen[seq] = struct{}{}
	if seq > s.downReqMax {
		s.downReqMax = seq
	}
	if s.downReqMax <= maxDownReplayRequests {
		return
	}
	floor := s.downReqMax - maxDownReplayRequests
	for old := range s.downReqSeen {
		if old >= floor {
			continue
		}
		if r, ok := s.downReplay[old]; ok && r.counter != 0 {
			continue
		}
		delete(s.downReqSeen, old)
		if r, ok := s.downReplay[old]; ok {
			if len(r.body) > 0 {
				s.downReplayBytes -= len(r.body)
				releaseGlobalQueued(len(r.body))
			}
			delete(s.downReplay, old)
		}
	}
}
func prepareDownResponse(s *session, sid string, seq, ack uint64, limit int) (cachedDownResponse, error) {
	s.downCommitMu.Lock()
	defer s.downCommitMu.Unlock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return cachedDownResponse{}, fmt.Errorf("session closed")
	}
	if c, ok := s.downReplay[seq]; ok {
		r := cloneDownResponse(c)
		s.mu.Unlock()
		return r, nil
	}
	if _, seen := s.downReqSeen[seq]; seen {
		s.mu.Unlock()
		return cachedDownResponse{status: http.StatusNoContent}, nil
	}
	if err := applyDownAckLocked(s, ack); err != nil {
		s.mu.Unlock()
		return cachedDownResponse{}, err
	}
	data, count, total := peekDownBatchLocked(s, limit)
	if count == 0 {
		r := cachedDownResponse{status: http.StatusNoContent}
		s.downReplay[seq] = r
		rememberDownRequestLocked(s, seq)
		s.mu.Unlock()
		return r, nil
	}
	s.mu.Unlock()
	padded, err := padTransportPlaintext(data)
	if err != nil {
		return cachedDownResponse{}, err
	}
	pred := noiseRecordSizeUpperBound(len(padded))
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return cachedDownResponse{}, fmt.Errorf("session closed")
	}
	if s.downReplayBytes+pred > runtimeLimits.MaxDownReplayBytes {
		r := cachedDownResponse{status: http.StatusNoContent}
		s.downReplay[seq] = r
		rememberDownRequestLocked(s, seq)
		s.mu.Unlock()
		return r, nil
	}
	s.mu.Unlock()
	if !reserveGlobalQueued(pred) {
		s.mu.Lock()
		r := cachedDownResponse{status: http.StatusNoContent}
		s.downReplay[seq] = r
		rememberDownRequestLocked(s, seq)
		s.mu.Unlock()
		return r, nil
	}
	record, err := s.crypto.sealDown(sid, padded)
	if err != nil {
		releaseGlobalQueued(pred)
		return cachedDownResponse{}, err
	}
	counter, err := cryptoRecordCounter(record, dirDown)
	if err != nil {
		releaseGlobalQueued(pred)
		return cachedDownResponse{}, err
	}
	s.mu.Lock()
	consumeDownBatchLocked(s, count, total)
	r := cachedDownResponse{counter: counter, status: http.StatusOK, body: append([]byte(nil), record...)}
	s.downReplay[seq] = r
	s.downReplayBytes += len(record)
	s.lastDownCounter = counter
	rememberDownRequestLocked(s, seq)
	s.mu.Unlock()
	return cloneDownResponse(r), nil
}
func waitForDownData(s *session, d time.Duration) {
	s.mu.Lock()
	ready := s.downBytes > 0 || s.closed
	s.mu.Unlock()
	if ready || d <= 0 {
		return
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-s.downReady:
	case <-s.done:
	case <-timer.C:
	}
}

type camouflageConfig struct {
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

	return nil
}

func applyResponseHeaders(w http.ResponseWriter) {
	for k, v := range camouflage.ResponseHeaders {
		w.Header().Set(k, v)
	}
}

func camouflageNotFound(w http.ResponseWriter) {
	applyResponseHeaders(w)
	w.WriteHeader(http.StatusNotFound)
}

func main() {
	configPath := runtimeConfigPath("/etc/mbox/server.json")
	if err := loadRuntimeConfig(configPath); err != nil {
		log.Fatalf("CONFIG: %v", err)
	}
	var rlim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rlim); err == nil {
		needed := uint64(runtimeLimits.MaxTotalStreams*2 + runtimeLimits.MaxSessionsGlobal*4 + 1024)
		if rlim.Cur < needed {
			log.Printf("WARNING: RLIMIT_NOFILE=%d, recommended >=%d", rlim.Cur, needed)
		}
	}

	reloadCh := make(chan os.Signal, 1)
	signal.Notify(reloadCh, syscall.SIGHUP)
	go func() {
		for range reloadCh {
			if err := reloadClientAuth(configPath); err != nil {
				log.Printf("AUTH RELOAD FAILED: %v", err)
				continue
			}
			clientAuthMu.RLock()
			n := len(clientPolicies)
			clientAuthMu.RUnlock()
			log.Printf("AUTH RELOADED clients=%d", n)
		}
	}()

	base := basePath

	http.HandleFunc(base+coverCfg.Path, coverResourceHandler)
	http.HandleFunc(base+"/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	http.HandleFunc(base+camouflage.OpenPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			camouflageNotFound(w)
			return
		}

		body, err := io.ReadAll(
			http.MaxBytesReader(w, r.Body, 4096),
		)
		if err != nil {
			camouflageNotFound(w)
			return
		}

		body, err = decodeHTTPEnvelope(body)
		if err != nil {
			camouflageNotFound(w)
			return
		}

		id := randomID()

		cryptoState, noiseReply, err := serverHandshake(body, id)
		if err != nil {
			log.Printf("OPEN AUTH FAILED: %v", err)
			camouflageNotFound(w)
			return
		}

		s := &session{clientID: cryptoState.clientID, streams: make(map[uint32]*stream), crypto: cryptoState, upCache: make(map[uint64]cachedUpResponse), downReady: make(chan struct{}, 1), downReplay: make(map[uint64]cachedDownResponse), downReqSeen: make(map[uint64]struct{}), done: make(chan struct{}), lastSeen: time.Now()}
		openReply, err := encodeHTTPEnvelope(noiseReply)
		if err != nil {
			camouflageNotFound(w)
			return
		}
		if !registerSession(id, s) {
			applyResponseHeaders(w)
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		notifyRealSessionOpen()
		w.Header().Set("Content-Type", camouflage.ContentType)
		w.Header().Set("Cache-Control", "no-store")

		w.Header().Set("Content-Type", camouflage.ContentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(openReply)))
		_, _ = w.Write(openReply)

		log.Printf("SESSION OPEN %s handshake=Noise_NKpsk0_25519_AESGCM_SHA256 record=v%d", id, cryptoRecordVersion)
	})

	http.HandleFunc(base+camouflage.UpPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			camouflageNotFound(w)
			return
		}
		seq, ack, err := parseDownRequestMeta(r)
		if err != nil {
			camouflageNotFound(w)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, base+camouflage.UpPath)
		s, ok := getSession(id)
		if !ok {
			camouflageNotFound(w)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBody))
		if err != nil {
			camouflageNotFound(w)
			return
		}
		body, err = decodeHTTPEnvelope(body)
		if err != nil {
			camouflageNotFound(w)
			return
		}
		counter, err := cryptoRecordCounter(body, dirUp)
		if err != nil {
			camouflageNotFound(w)
			return
		}
		s.upProcessMu.Lock()
		defer s.upProcessMu.Unlock()
		s.mu.Lock()
		cached, hit := s.upCache[counter]
		s.mu.Unlock()
		if hit {
			if cached.recordHash != sha256.Sum256(body) {
				camouflageNotFound(w)
				return
			}
			status, cbody := replayCachedUpResponse(s, cached)
			writeCachedResponse(w, status, cbody)
			return
		}
		padded, err := s.crypto.openUp(id, body)
		if err != nil {
			camouflageNotFound(w)
			return
		}
		plain, err := unpadTransportPlaintext(padded)
		if err != nil {
			closeSession(id)
			w.WriteHeader(http.StatusGone)
			return
		}
		if err := parseFrames(s, plain); err != nil {
			closeSession(id)
			w.WriteHeader(http.StatusGone)
			return
		}
		waitForDownData(s, 120*time.Millisecond)
		resp, err := prepareDownResponse(s, id, seq, ack, randomBatchLimit(camouflage.DownMaxBytes))
		if err != nil {
			closeSession(id)
			w.WriteHeader(http.StatusGone)
			return
		}
		cacheUpResponse(s, counter, body, seq, resp.status)
		writeCachedResponse(w, resp.status, resp.body)
	})

	http.HandleFunc(base+camouflage.DownPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			camouflageNotFound(w)
			return
		}
		seq, ack, err := parseDownRequestMeta(r)
		if err != nil {
			camouflageNotFound(w)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, base+camouflage.DownPath)
		s, ok := getSession(id)
		if !ok {
			camouflageNotFound(w)
			return
		}
		limit := camouflage.DownMaxBytes
		if q := r.URL.Query().Get("max"); q != "" {
			if n, e := strconv.Atoi(q); e == nil && n >= 1024 && n <= 1024*1024 {
				limit = n
			}
		}
		resp, err := prepareDownResponse(s, id, seq, ack, limit)
		if err != nil {
			closeSession(id)
			w.WriteHeader(http.StatusGone)
			return
		}
		writeCachedResponse(w, resp.status, resp.body)
	})

	http.HandleFunc(base+camouflage.ClosePath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			camouflageNotFound(w)
			return
		}

		id := strings.TrimPrefix(r.URL.Path, base+camouflage.ClosePath)
		closeSession(id)

		w.WriteHeader(http.StatusNoContent)
	})

	writeRuntimeStatus()
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for range t.C {
			writeRuntimeStatus()
		}
	}()

	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()

		for range t.C {
			cutoff := time.Now().Add(-5 * time.Minute)

			var expired []string

			sessionsMu.Lock()

			for id, s := range sessions {
				s.mu.Lock()
				last := s.lastSeen
				clientID := s.clientID
				s.mu.Unlock()
				policyExpired := false
				if clientID != "" {
					if p, ok := getClientPolicy(clientID); !ok || !clientPolicyActive(p) {
						policyExpired = true
					}
				}
				if last.Before(cutoff) || policyExpired {
					expired = append(expired, id)
				}
			}

			sessionsMu.Unlock()

			for _, id := range expired {
				log.Printf("SESSION EXPIRE %s", id)
				closeSession(id)
			}
		}
	}()

	startCoverEngine(coverCfg)

	log.Printf(
		"MBox server listening on %s -> %s",
		listen,
		socksAddr,
	)

	srv := &http.Server{Addr: listen, Handler: nil, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 64 * 1024}
	log.Fatal(srv.ListenAndServe())
}
