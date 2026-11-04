package main

import "testing"

func TestDecodeClientAuthSeparatesOpaqueHandleFromAccountID(t *testing.T) {
	in := map[string]runtimeClientConfig{
		"00112233445566778899aabbccddeeff": {PSK: "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f", InternalID: "alice"},
	}
	got, err := decodeClientAuth(in)
	if err != nil {
		t.Fatal(err)
	}
	p := got["00112233445566778899aabbccddeeff"]
	if p.AccountID != "alice" {
		t.Fatalf("account=%q", p.AccountID)
	}
}

func TestSessionLimitGroupsAliasesByAccountID(t *testing.T) {
	oldPolicies := clientPolicies
	oldLimits := runtimeLimits
	defer func() { clientPolicies = oldPolicies; runtimeLimits = oldLimits }()
	clientPolicies = map[string]clientPolicy{
		"legacy-alice":                     {PSK: make([]byte, 32), AccountID: "alice", MaxSessions: 1},
		"00112233445566778899aabbccddeeff": {PSK: make([]byte, 32), AccountID: "alice", MaxSessions: 1},
	}
	runtimeLimits.MaxSessionsGlobal = 100
	runtimeLimits.DefaultMaxSessionsPerClient = 4
	runtimeLimits.MaxSessionOpensPerMinute = 100
	serverDraining.Store(false)
	mk := func(handle string) *session {
		return &session{clientID: handle, accountID: "alice", streams: map[uint32]*stream{}, done: make(chan struct{}), downReplay: map[uint64]cachedDownResponse{}, downReqSeen: map[uint64]struct{}{}, upCache: map[uint64]cachedUpResponse{}}
	}
	sessionsMu.Lock()
	sessions = map[string]*session{}
	sessionOpenRates = map[string]rateWindow{}
	sessionsMu.Unlock()
	if !registerSession("a", mk("legacy-alice")) {
		t.Fatal("first session rejected")
	}
	if registerSession("b", mk("00112233445566778899aabbccddeeff")) {
		t.Fatal("alias bypassed account session limit")
	}
	closeAllSessions()
}
