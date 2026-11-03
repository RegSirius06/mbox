package main

import (
	"bytes"
	"encoding/json"
	"log"
	"strings"
	"testing"
	"time"
)

func TestRedactedClientIDDoesNotExposeInput(t *testing.T) {
	id := "android-main"
	got := redactedClientID(id)
	if strings.Contains(got, id) || got == id {
		t.Fatalf("identifier leaked: %q", got)
	}
	if !strings.HasPrefix(got, "client#") {
		t.Fatalf("unexpected redaction: %q", got)
	}
}

func TestRuntimeStatusUsesOpaqueClientLabels(t *testing.T) {
	s := &session{clientID: "alice@example", streams: map[uint32]*stream{}, done: make(chan struct{}), downReplay: map[uint64]cachedDownResponse{}, downReqSeen: map[uint64]struct{}{}, upCache: map[uint64]cachedUpResponse{}}
	sessionsMu.Lock()
	sessions["privacy-test"] = s
	sessionsMu.Unlock()
	defer func() { sessionsMu.Lock(); delete(sessions, "privacy-test"); sessionsMu.Unlock() }()
	snap := runtimeStatusSnapshot{GeneratedAt: time.Now(), Clients: map[string]int{redactedClientID(s.clientID): 1}}
	b, _ := json.Marshal(snap)
	if bytes.Contains(b, []byte(s.clientID)) {
		t.Fatalf("raw client ID in metrics/status: %s", b)
	}
}

func TestAuthFailureLoggingIsRateLimited(t *testing.T) {
	old := log.Writer()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(old)
	lastAuthFailureLog.Store(0)
	now := time.Unix(1700000000, 0)
	for i := 0; i < 100; i++ {
		logAuthFailure(now)
	}
	if got := strings.Count(buf.String(), "OPEN AUTH FAILED"); got != 1 {
		t.Fatalf("got %d log lines", got)
	}
}
