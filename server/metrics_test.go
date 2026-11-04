package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSafeMetricsContainsNoClientLabels(t *testing.T) {
	oldSessions := sessions
	sessionsMu.Lock()
	sessions = map[string]*session{
		"secret-session": {
			clientID:   "secret-client-handle",
			accountID:  "alice@example.invalid",
			streams:    map[uint32]*stream{},
			downReplay: map[uint64]cachedDownResponse{},
		},
	}
	sessionsMu.Unlock()
	defer func() {
		sessionsMu.Lock()
		sessions = oldSessions
		sessionsMu.Unlock()
	}()
	b, err := json.Marshal(collectSafeMetrics())
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, forbidden := range []string{"secret-session", "secret-client-handle", "alice@example.invalid"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("metrics leaked %q: %s", forbidden, text)
		}
	}
}

func TestMetricsMiddlewareCounts5xxWithoutRequestMetadata(t *testing.T) {
	beforeReq := metricsCounters.httpRequests.Load()
	before5xx := metricsCounters.http5xx.Load()
	h := metricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	r := httptest.NewRequest(http.MethodGet, "http://example.invalid/private?client=secret", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if metricsCounters.httpRequests.Load() != beforeReq+1 || metricsCounters.http5xx.Load() != before5xx+1 {
		t.Fatal("aggregate metrics not updated")
	}
	b, _ := json.Marshal(collectSafeMetrics())
	if strings.Contains(string(b), "secret") {
		t.Fatalf("request metadata leaked into metrics: %s", b)
	}
}
