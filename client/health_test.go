package main

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

func waitHealthFrame(t *testing.T, tun *tunnel) []byte {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		frame := tun.takeUpload(1024)
		if len(frame) != 0 {
			if len(frame) != 25 || binary.BigEndian.Uint32(frame[:4]) != 0 || frame[4] != framePing || binary.BigEndian.Uint32(frame[5:9]) != 16 {
				t.Fatalf("unexpected health record format: %x", frame)
			}
			return frame[9:]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("probe failed to enqueue")
	return nil
}

func TestHealthProbeNeedsMatchingAuthenticatedPong(t *testing.T) {
	tun := newQueueTestTunnel()
	result := make(chan error, 1)
	go func() { result <- tun.healthProbe(400 * time.Millisecond) }()
	nonce := waitHealthFrame(t, tun)
	wrong := bytes.Repeat([]byte{0xaa}, 16)
	if bytes.Equal(wrong, nonce) {
		wrong[0] ^= 1
	}
	tun.acceptHealthPong(0, wrong)
	tun.acceptHealthPong(9, nonce)
	select {
	case err := <-result:
		t.Fatalf("accepted incorrect pong: %v", err)
	case <-time.After(15 * time.Millisecond):
	}
	tun.acceptHealthPong(0, nonce)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestHealthProbeTimedOutWhileCoreStillAlive(t *testing.T) {
	tun := newQueueTestTunnel()
	result := make(chan error, 1)
	go func() { result <- tun.healthProbe(20 * time.Millisecond) }()
	_ = waitHealthFrame(t, tun)
	if err := <-result; err == nil {
		t.Fatal("missing authenticated reply must fail")
	}
}

func TestHealthProbeCancelledOnStop(t *testing.T) {
	tun := newQueueTestTunnel()
	result := make(chan error, 1)
	go func() { result <- tun.healthProbe(time.Minute) }()
	_ = waitHealthFrame(t, tun)
	close(tun.done)
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("stop must cancel healthcheck")
		}
	case <-time.After(time.Second):
		t.Fatal("probe not cancelled")
	}
}
