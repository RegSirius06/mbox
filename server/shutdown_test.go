package main

import (
	"testing"
	"time"
)

func TestRegisterSessionRejectsDrain(t *testing.T) {
	old := serverDraining.Load()
	serverDraining.Store(true)
	defer serverDraining.Store(old)
	s := &session{clientID: "test", streams: map[uint32]*stream{}, done: make(chan struct{}), downReplay: map[uint64]cachedDownResponse{}, downReqSeen: map[uint64]struct{}{}, upCache: map[uint64]cachedUpResponse{}}
	if registerSession("drain-test", s) {
		t.Fatal("session admitted while draining")
	}
}

func TestCloseAllSessionsReleasesReplayBudget(t *testing.T) {
	oldLimit := runtimeLimits.GlobalQueuedBytes
	runtimeLimits.GlobalQueuedBytes = 1 << 20
	defer func() { runtimeLimits.GlobalQueuedBytes = oldLimit }()
	globalQueuedBytes.Store(0)
	if !reserveGlobalQueued(1234) {
		t.Fatal("reservation failed")
	}
	s := &session{clientID: "", streams: map[uint32]*stream{}, done: make(chan struct{}), downReplay: map[uint64]cachedDownResponse{1: {body: []byte("x"), reservedBytes: 1234}}, downReplayBytes: 1, downReqSeen: map[uint64]struct{}{}, upCache: map[uint64]cachedUpResponse{}}
	sessionsMu.Lock()
	sessions["shutdown-test"] = s
	sessionsMu.Unlock()
	closeAllSessions()
	if !waitForResourcesReleased(time.Second) {
		t.Fatal("resources did not return to baseline")
	}
	if got := globalQueuedBytes.Load(); got != 0 {
		t.Fatalf("queued=%d", got)
	}
}
