package main

import (
	"crypto/sha256"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func registerHTTPHandlers(mux *http.ServeMux) {
	mux.HandleFunc(basePath+coverCfg.Path, coverResourceHandler)
	mux.HandleFunc(basePath+"/health", handleHealth)
	mux.HandleFunc(basePath+camouflage.OpenPath, handleOpen)
	mux.HandleFunc(basePath+camouflage.UpPath, handleUp)
	mux.HandleFunc(basePath+camouflage.DownPath, handleDown)
	mux.HandleFunc(basePath+camouflage.ClosePath, handleClose)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func handleOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		camouflageNotFound(w)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil {
		camouflageNotFound(w)
		return
	}
	body, err = decodeHTTPEnvelope(body)
	if err != nil {
		camouflageNotFound(w)
		return
	}

	now := time.Now()
	if preAuthLimiter == nil || !preAuthLimiter.acquire(now) {
		logHandshakeLimit(now)
		camouflageNotFound(w)
		return
	}
	defer preAuthLimiter.release()

	id := randomID()
	cryptoState, noiseReply, err := serverHandshake(body, id)
	if err != nil {
		log.Printf("OPEN AUTH FAILED: %v", err)
		camouflageNotFound(w)
		return
	}

	s := &session{clientID: cryptoState.clientID, deviceID: cryptoState.deviceID, streams: make(map[uint32]*stream), crypto: cryptoState, upCache: make(map[uint64]cachedUpResponse), downReady: make(chan struct{}, 1), downReplay: make(map[uint64]cachedDownResponse), downReqSeen: make(map[uint64]struct{}), done: make(chan struct{}), lastSeen: time.Now()}
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
	w.Header().Set("Content-Length", strconv.Itoa(len(openReply)))
	_, _ = w.Write(openReply)

	log.Printf("SESSION OPEN %s handshake=Noise_NKpsk0_25519_AESGCM_SHA256 record=v%d", redactedSessionID(id), cryptoRecordVersion)
}

func handleUp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		camouflageNotFound(w)
		return
	}
	seq, ack, err := parseDownRequestMeta(r)
	if err != nil {
		camouflageNotFound(w)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, basePath+camouflage.UpPath)
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
}

func handleDown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		camouflageNotFound(w)
		return
	}
	seq, ack, err := parseDownRequestMeta(r)
	if err != nil {
		camouflageNotFound(w)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, basePath+camouflage.DownPath)
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
}

func handleClose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		camouflageNotFound(w)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, basePath+camouflage.ClosePath)
	closeSession(id)
	w.WriteHeader(http.StatusNoContent)
}
