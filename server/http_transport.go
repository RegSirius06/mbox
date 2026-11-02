package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

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
