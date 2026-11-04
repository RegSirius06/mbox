package main

import (
	"testing"
	"time"
)

func TestOriginNonceExpiryAndBoundedIdleSweeps(t *testing.T) {
	originNonceMu.Lock()
	oldCache, oldSweep := originNonces, originNonceNextSweep
	originNonces = make(map[string]time.Time)
	originNonceNextSweep = time.Time{}
	originNonceMu.Unlock()
	defer func() {
		originNonceMu.Lock()
		originNonces, originNonceNextSweep = oldCache, oldSweep
		originNonceMu.Unlock()
	}()
	now := time.Unix(2000000000, 0)
	nonce := "00112233445566778899aabbccddeeff"
	if !consumeOriginNonce(nonce, now) {
		t.Fatal("first nonce rejected")
	}
	if consumeOriginNonce(nonce, now.Add(time.Second)) {
		t.Fatal("replayed nonce accepted")
	}
	if !consumeOriginNonce(nonce, now.Add(originNonceTTL+time.Second)) {
		t.Fatal("expired nonce cannot be reused")
	}
	// The background maintenance path invokes this same sweep when idle;
	// no HTTP traffic is required to retire stale entries.
	originNonceMu.Lock()
	pruneOriginNoncesLocked(now.Add(2*originNonceTTL + time.Second))
	count := len(originNonces)
	originNonceMu.Unlock()
	if count != 0 {
		t.Fatalf("expired nonce leaked in cache: %d", count)
	}
}

func TestConcurrentHTTPRequestsLimitConfig(t *testing.T) {
	cfg, err := normalizeLimits(resourceLimitsConfig{})
	if err != nil || cfg.MaxConcurrentHTTPRequests != 64 {
		t.Fatalf("default=%d err=%v", cfg.MaxConcurrentHTTPRequests, err)
	}
	cfg.MaxConcurrentHTTPRequests = 4097
	if _, err = normalizeLimits(cfg); err == nil {
		t.Fatal("unsafe HTTP concurrency accepted")
	}
	cfg.MaxConcurrentHTTPRequests = 32
	if got, err := normalizeLimits(cfg); err != nil || got.MaxConcurrentHTTPRequests != 32 {
		t.Fatalf("explicit concurrency rejected: %d %v", got.MaxConcurrentHTTPRequests, err)
	}
}
