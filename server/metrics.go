package main

import (
	"encoding/json"
	"net/http"
	"os"
	"runtime"
	"sync/atomic"
	"time"
)

type serverMetricsCounters struct {
	httpRequests           atomic.Uint64
	http5xx                atomic.Uint64
	httpLatencyNanos       atomic.Uint64
	authFailures           atomic.Uint64
	handshakeRateLimit     atomic.Uint64
	originAuthFailures     atomic.Uint64
	originAdmissionRejects atomic.Uint64
	sessionRejects         atomic.Uint64
}

var metricsCounters serverMetricsCounters

type statusCapturingWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusCapturingWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusCapturingWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(p)
}

// metricsMiddleware deliberately records only aggregate, bounded-cardinality
// values. Security invariant: observability must not introduce a new store of
// client handles, session identifiers, destination hosts, or other user data.
func metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		cw := &statusCapturingWriter{ResponseWriter: w}
		next.ServeHTTP(cw, r)
		metricsCounters.httpRequests.Add(1)
		metricsCounters.httpLatencyNanos.Add(uint64(time.Since(start)))
		if cw.status >= 500 {
			metricsCounters.http5xx.Add(1)
		}
	})
}

type safeMetricsSnapshot struct {
	GeneratedAt            time.Time `json:"generated_at"`
	ActiveSessions         int       `json:"active_sessions"`
	ActiveStreams          int64     `json:"active_streams"`
	QueuedBytes            int64     `json:"queued_bytes"`
	ReplayBytes            int64     `json:"replay_bytes"`
	SessionRateEntries     int       `json:"session_rate_entries"`
	OriginReplayEntries    int       `json:"origin_replay_entries"`
	Goroutines             int       `json:"goroutines"`
	HeapAllocBytes         uint64    `json:"heap_alloc_bytes"`
	OpenFDs                int       `json:"open_fds"`
	HTTPRequests           uint64    `json:"http_requests"`
	HTTP5xx                uint64    `json:"http_5xx"`
	HTTPLatencyNanosTotal  uint64    `json:"http_latency_nanos_total"`
	AuthFailures           uint64    `json:"auth_failures"`
	HandshakeRateLimited   uint64    `json:"handshake_rate_limited"`
	OriginAuthFailures     uint64    `json:"origin_auth_failures"`
	OriginAdmissionRejects uint64    `json:"origin_admission_rejects"`
	SessionAdmissionReject uint64    `json:"session_admission_rejects"`
}

func collectSafeMetrics() safeMetricsSnapshot {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	s := safeMetricsSnapshot{
		GeneratedAt:            time.Now().UTC(),
		ActiveStreams:          totalStreams.Load(),
		QueuedBytes:            globalQueuedBytes.Load(),
		Goroutines:             runtime.NumGoroutine(),
		HeapAllocBytes:         mem.HeapAlloc,
		OpenFDs:                openFDCount(),
		HTTPRequests:           metricsCounters.httpRequests.Load(),
		HTTP5xx:                metricsCounters.http5xx.Load(),
		HTTPLatencyNanosTotal:  metricsCounters.httpLatencyNanos.Load(),
		AuthFailures:           metricsCounters.authFailures.Load(),
		HandshakeRateLimited:   metricsCounters.handshakeRateLimit.Load(),
		OriginAuthFailures:     metricsCounters.originAuthFailures.Load(),
		OriginAdmissionRejects: metricsCounters.originAdmissionRejects.Load(),
		SessionAdmissionReject: metricsCounters.sessionRejects.Load(),
	}
	sessionsMu.Lock()
	s.ActiveSessions = len(sessions)
	s.SessionRateEntries = len(sessionOpenRates)
	for _, sess := range sessions {
		sess.mu.Lock()
		s.ReplayBytes += int64(sess.downReplayBytes)
		sess.mu.Unlock()
	}
	sessionsMu.Unlock()
	originNonceMu.Lock()
	s.OriginReplayEntries = len(originNonces)
	originNonceMu.Unlock()
	return s
}

func openFDCount() int {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return -1
	}
	return len(entries)
}

func handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(collectSafeMetrics())
}
