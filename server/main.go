package main

import (
	"bytes"
	"crypto/rand"
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

	frameOpen  = 1
	frameData  = 2
	frameClose = 3
	frameError = 4
	framePing  = 5
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
	conn net.Conn

	writeMu sync.Mutex
	closed  atomic.Bool
}

type cachedUpResponse struct {
	record []byte
	status int
	body   []byte
}

type session struct {
	mu           sync.Mutex
	upProcessMu  sync.Mutex
	upCache      map[uint64]cachedUpResponse
	upCacheOrder []uint64

	streams map[uint32]*stream
	crypto  *cryptoState

	downFrames [][]byte
	downBytes  int
	downReady  chan struct{}

	lastSeen time.Time
	closed   bool
}

var (
	sessionsMu sync.Mutex
	sessions   = make(map[string]*session)
)

func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func getSession(id string) (*session, bool) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	s, ok := sessions[id]
	if ok {
		s.mu.Lock()
		s.lastSeen = time.Now()
		s.mu.Unlock()
	}

	return s, ok
}

func appendFrame(s *session, streamID uint32, typ byte, payload []byte) error {
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

	if s.downBytes+required > maxDownQueue {
		return fmt.Errorf("down queue overflow")
	}

	// Store complete MUX frames.
	s.downFrames = append(s.downFrames, f)
	s.downBytes += len(f)

	select {
	case s.downReady <- struct{}{}:
	default:
	}

	return nil
}

// popDownBatchLocked returns only complete MUX frames.
// Caller must hold s.mu.
//
// If the first frame alone is larger than limit, it is returned whole.
// A MUX frame must never be split between two encrypted DOWN records.
func popDownBatchLocked(s *session, limit int) []byte {
	if len(s.downFrames) == 0 {
		return nil
	}

	if limit <= 0 {
		limit = 256 * 1024
	}

	total := 0
	count := 0

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
		s.downFrames[i] = nil
	}

	s.downFrames = s.downFrames[count:]
	s.downBytes -= total

	if s.downBytes < 0 {
		panic("negative DOWN queue size")
	}

	if len(s.downFrames) == 0 {
		s.downFrames = nil

		select {
		case <-s.downReady:
		default:
		}
	}

	return data
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

	if st.closed.CompareAndSwap(false, true) {
		_ = st.conn.Close()
	}

	if notify {
		_ = appendFrame(s, id, frameClose, nil)
	}
}

func streamReader(s *session, id uint32, st *stream) {
	buf := make([]byte, 64*1024)

	for {
		n, err := st.conn.Read(buf)

		if n > 0 {
			payload := append([]byte(nil), buf[:n]...)

			if e := appendFrame(s, id, frameData, payload); e != nil {
				log.Printf("DOWN QUEUE stream=%d: %v", id, e)
				closeStream(s, id, false)
				return
			}

			log.Printf("TCP->MUX stream=%d bytes=%d", id, n)
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

func handleFrame(s *session, streamID uint32, typ byte, payload []byte) {
	switch typ {

	case frameOpen:
		s.mu.Lock()
		_, exists := s.streams[streamID]
		s.mu.Unlock()

		if exists {
			return
		}

		conn, err := net.DialTimeout("tcp", socksAddr, 5*time.Second)
		if err != nil {
			_ = appendFrame(
				s,
				streamID,
				frameError,
				[]byte("connect upstream failed"),
			)
			return
		}

		st := &stream{conn: conn}

		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = conn.Close()
			return
		}

		s.streams[streamID] = st
		s.mu.Unlock()

		log.Printf("OPEN stream=%d -> %s", streamID, socksAddr)

		go streamReader(s, streamID, st)

	case frameData:
		s.mu.Lock()
		st, ok := s.streams[streamID]
		s.mu.Unlock()

		if !ok || st.closed.Load() {
			return
		}

		st.writeMu.Lock()
		_, err := st.conn.Write(payload)
		st.writeMu.Unlock()

		if err != nil {
			log.Printf("TCP WRITE stream=%d: %v", streamID, err)
			closeStream(s, streamID, true)
			return
		}

		log.Printf("MUX->TCP stream=%d bytes=%d", streamID, len(payload))

	case frameClose:
		log.Printf("CLIENT CLOSE stream=%d", streamID)
		closeStream(s, streamID, false)

	case framePing:
		_ = appendFrame(s, streamID, framePing, payload)
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

		payload := data[:n]
		data = data[n:]

		handleFrame(s, streamID, typ, payload)
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

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}

	s.closed = true

	var streams []*stream
	for _, st := range s.streams {
		streams = append(streams, st)
	}

	s.streams = nil
	s.mu.Unlock()

	for _, st := range streams {
		if st.closed.CompareAndSwap(false, true) {
			_ = st.conn.Close()
		}
	}

	log.Printf("SESSION CLOSE %s", id)
}

func cryptoRecordCounter(record []byte, direction byte) (uint64, error) {
	if len(record) < 26 || record[0] != cryptoRecordVersion || record[1] != direction {
		return 0, fmt.Errorf("invalid crypto record")
	}
	return binary.BigEndian.Uint64(record[2:10]), nil
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

func cacheUpResponse(s *session, counter uint64, record []byte, status int, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upCache[counter] = cachedUpResponse{append([]byte(nil), record...), status, append([]byte(nil), body...)}
	s.upCacheOrder = append(s.upCacheOrder, counter)
	for len(s.upCacheOrder) > 64 {
		old := s.upCacheOrder[0]
		s.upCacheOrder = s.upCacheOrder[1:]
		delete(s.upCache, old)
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
	PaddingClasses           []int             `json:"padding_classes"`
	PaddingExtraClassPercent int               `json:"padding_extra_class_percent"`
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
	PaddingClasses: []int{
		512, 1024, 2048, 4096, 8192, 16384,
		32768, 65536, 131072, 262144, 524288,
	},
	PaddingExtraClassPercent: 20,
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

	reloadCh := make(chan os.Signal, 1)
	signal.Notify(reloadCh, syscall.SIGHUP)
	go func() {
		for range reloadCh {
			if err := reloadClientAuth(configPath); err != nil {
				log.Printf("AUTH RELOAD FAILED: %v", err)
				continue
			}
			clientAuthMu.RLock()
			n := len(clientPSKs)
			clientAuthMu.RUnlock()
			log.Printf("AUTH RELOADED clients=%d", n)
		}
	}()

	base := basePath

	http.HandleFunc(base+coverCfg.Path, coverResourceHandler)

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

		handshakeVersion := "v1"
		if len(body) != 64 {
			handshakeVersion = "v2"
		}

		cryptoState, confirm, err := serverHandshake(body, id)
		if err != nil {
			log.Printf("OPEN AUTH FAILED: %v", err)
			camouflageNotFound(w)
			return
		}

		s := &session{
			streams:   make(map[uint32]*stream),
			crypto:    cryptoState,
			upCache:   make(map[uint64]cachedUpResponse),
			downReady: make(chan struct{}, 1),
			lastSeen:  time.Now(),
		}

		sessionsMu.Lock()
		sessions[id] = s
		sessionsMu.Unlock()
		notifyRealSessionOpen()

		w.Header().Set("Content-Type", camouflage.ContentType)
		w.Header().Set("Cache-Control", "no-store")

		openReply, err := encodeHTTPEnvelope([]byte(id + confirm))
		if err != nil {
			camouflageNotFound(w)
			return
		}

		w.Header().Set("Content-Type", camouflage.ContentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(openReply)))
		_, _ = w.Write(openReply)

		log.Printf("SESSION OPEN %s handshake=%s record=v%d", id, handshakeVersion, cryptoRecordVersion)
	})

	http.HandleFunc(base+camouflage.UpPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
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

		// Only one UP transaction is committed at a time. A retry can therefore
		// retrieve the exact cached HTTP response without replaying MUX bytes.
		s.upProcessMu.Lock()
		defer s.upProcessMu.Unlock()
		s.mu.Lock()
		cached, hit := s.upCache[counter]
		s.mu.Unlock()
		if hit {
			if !bytes.Equal(cached.record, body) {
				camouflageNotFound(w)
				return
			}
			log.Printf("DUPLICATE UP session=%s counter=%d cached", id, counter)
			writeCachedResponse(w, cached.status, cached.body)
			return
		}

		paddedPlaintext, cryptoErr := s.crypto.openUp(id, body)
		if cryptoErr != nil {
			log.Printf("CRYPTO UP session=%s: %v", id, cryptoErr)
			camouflageNotFound(w)
			return
		}

		plaintext, err := unpadTransportPlaintext(paddedPlaintext)
		if err != nil {
			log.Printf("PAD UP session=%s: %v", id, err)
			camouflageNotFound(w)
			return
		}

		if err := parseFrames(s, plaintext); err != nil {
			log.Printf("BAD FRAMES session=%s: %v", id, err)
			camouflageNotFound(w)
			return
		}

		s.mu.Lock()
		ready := s.downBytes > 0
		s.mu.Unlock()
		if !ready {
			timer := time.NewTimer(120 * time.Millisecond)
			select {
			case <-s.downReady:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			case <-timer.C:
			}
		}
		s.mu.Lock()
		if s.downBytes == 0 {
			s.mu.Unlock()
			cacheUpResponse(s, counter, body, http.StatusNoContent, nil)
			writeCachedResponse(w, http.StatusNoContent, nil)
			return
		}
		limit := camouflage.DownMaxBytes
		data := popDownBatchLocked(s, limit)
		if s.downBytes == 0 {
			select {
			case <-s.downReady:
			default:
			}
		}
		s.mu.Unlock()
		paddedDown, err := padTransportPlaintext(data)
		if err != nil {
			log.Printf("PAD DOWN session=%s: %v", id, err)
			camouflageNotFound(w)
			return
		}

		encrypted, err := s.crypto.sealDown(id, paddedDown)
		if err != nil {
			camouflageNotFound(w)
			return
		}
		cacheUpResponse(s, counter, body, http.StatusOK, encrypted)
		writeCachedResponse(w, http.StatusOK, encrypted)
		log.Printf("PIGGYBACK session=%s up=%d bytes=%d", id, counter, len(encrypted))
	})

	http.HandleFunc(base+camouflage.DownPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
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
			if n, err := strconv.Atoi(q); err == nil {
				if n >= 1024 && n <= 1024*1024 {
					limit = n
				}
			}
		}

		s.mu.Lock()

		if s.closed {
			s.mu.Unlock()
			w.WriteHeader(http.StatusGone)
			return
		}

		if s.downBytes == 0 {
			s.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}

		data := popDownBatchLocked(s, limit)

		s.mu.Unlock()

		paddedDown, err := padTransportPlaintext(data)
		if err != nil {
			log.Printf("PAD DOWN session=%s: %v", id, err)
			camouflageNotFound(w)
			return
		}

		encrypted, err := s.crypto.sealDown(id, paddedDown)
		if err != nil {
			camouflageNotFound(w)
			return
		}
		data = encrypted

		wireData, err := encodeHTTPEnvelope(data)
		if err != nil {
			camouflageNotFound(w)
			return
		}

		applyResponseHeaders(w)
		w.Header().Set("Content-Type", camouflage.ContentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(wireData)))

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(wireData)

		log.Printf("DOWN session=%s bytes=%d", id, len(data))
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
				s.mu.Unlock()

				if last.Before(cutoff) {
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

	log.Fatal(http.ListenAndServe(listen, nil))
}
