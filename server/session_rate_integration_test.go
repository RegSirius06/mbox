//go:build integration

package main

import (
	"fmt"
	"testing"
	"time"
)

// TestSessionOpenRateStateMillionExpiredIDsIsCollected is intentionally in the
// integration suite: under the race detector a million map entries make the
// normal per-commit gate unnecessarily expensive. Nightly/integration CI runs
// it with: go test -tags=integration -run MillionExpiredIDs ./...
func TestSessionOpenRateStateMillionExpiredIDsIsCollected(t *testing.T) {
	oldRates := sessionOpenRates
	sessionOpenRates = make(map[string]rateWindow, 1000000)
	defer func() { sessionOpenRates = oldRates }()

	expired := time.Now().Add(-sessionOpenRateTTL - time.Minute)
	for i := 0; i < 1000000; i++ {
		sessionOpenRates[fmt.Sprintf("id-%06d", i)] = rateWindow{start: expired, count: 1}
	}
	sessionsMu.Lock()
	pruneSessionOpenRatesLocked(time.Now())
	got := len(sessionOpenRates)
	sessionsMu.Unlock()
	if got != 0 {
		t.Fatalf("expired sessionOpenRates retained %d entries", got)
	}
}
