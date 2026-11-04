//go:build integration
// +build integration

package main

import (
	"io"
	"net"
	"runtime"
	"testing"
	"time"
)

func TestSessionStreamLifecycleReturnsResourcesNearBaseline(t *testing.T) {
	oldSocks := socksAddr
	oldLimits := runtimeLimits
	oldSessions := sessions
	defer func() {
		socksAddr = oldSocks
		runtimeLimits = oldLimits
		sessionsMu.Lock()
		sessions = oldSessions
		sessionsMu.Unlock()
	}()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_, _ = io.Copy(io.Discard, c)
			}()
		}
	}()
	socksAddr = ln.Addr().String()
	runtimeLimits.MaxStreamsPerSession = 128
	runtimeLimits.MaxTotalStreams = 1024
	runtimeLimits.MaxStreamOpensPerSecond = 10000
	runtimeLimits.GlobalQueuedBytes = 64 << 20
	globalQueuedBytes.Store(0)
	totalStreams.Store(0)
	baseFD := openFDCount()
	baseG := runtime.NumGoroutine()

	for cycle := 0; cycle < 200; cycle++ {
		s := &session{
			streams:     make(map[uint32]*stream),
			downReady:   make(chan struct{}, 1),
			downReplay:  make(map[uint64]cachedDownResponse),
			downReqSeen: make(map[uint64]struct{}),
			upCache:     make(map[uint64]cachedUpResponse),
			done:        make(chan struct{}),
			lastSeen:    time.Now(),
		}
		id := randomID()
		sessionsMu.Lock()
		sessions[id] = s
		sessionsMu.Unlock()
		for streamID := uint32(1); streamID <= 8; streamID++ {
			if err := handleFrame(s, streamID, frameOpen, nil); err != nil {
				t.Fatalf("cycle=%d stream=%d open: %v", cycle, streamID, err)
			}
		}
		closeSession(id)
	}
	if !waitForResourcesReleased(5 * time.Second) {
		t.Fatalf("resources remain: streams=%d queued=%d", totalStreams.Load(), globalQueuedBytes.Load())
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		if runtime.NumGoroutine() <= baseG+8 && (baseFD < 0 || openFDCount() <= baseFD+8) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("lifecycle leak suspected: goroutines baseline=%d now=%d fds baseline=%d now=%d", baseG, runtime.NumGoroutine(), baseFD, openFDCount())
}
