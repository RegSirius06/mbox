package main

import (
	"bytes"
	"io"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"../common/dgram"
	"../common/noise"
)

// A temporary local QUIC gateway outage must not permanently strand the
// initial authenticated DATAGRAM lane. The third attempt succeeds without
// restarting the Noise session. Once active, no new attach is attempted.
func TestDatagramRetriesBeforeAcceptOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "http.sock.dgram")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	keyUp, keyDown := bytes.Repeat([]byte{0x42}, 32), bytes.Repeat([]byte{0x43}, 32)
	up, err := noise.NewCipherState(keyUp)
	if err != nil {
		t.Fatal(err)
	}
	down, err := noise.NewCipherState(keyDown)
	if err != nil {
		t.Fatal(err)
	}
	sid := "0123456789abcdef0123456789abcdef"
	tun := &tunnel{sid: sid, crypto: &cryptoState{up: up, down: down}, done: make(chan struct{})}
	var attempts atomic.Int32
	ready := make(chan struct{})
	gatewayDone := make(chan struct{})
	go func() {
		defer close(gatewayDone)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			n := attempts.Add(1)
			var request [dgram.AttachSize]byte
			_, err = io.ReadFull(c, request[:])
			if err != nil {
				c.Close()
				continue
			}
			if !dgram.VerifyProof(up.DatagramSecret(), sid, "attach", request[32:]) {
				c.Close()
				return
			}
			if n < 3 {
				c.Close()
				continue
			}
			p := dgram.Proof(down.DatagramSecret(), sid, "accept")
			_ = dgram.WriteFull(c, p[:])
			close(ready)
			<-tun.done
			c.Close()
			return
		}
	}()
	t.Setenv("MBOX_QUIC_UNIX", filepath.Join(dir, "http.sock"))
	t.Setenv("MBOX_DATAGRAM", "1")
	t.Setenv("MBOX_HTTP_BRIDGE_ADDR", "")
	previousEnableUDPClient := enableUDPClient
	enableUDPClient = true
	t.Cleanup(func() { enableUDPClient = previousEnableUDPClient })
	tun.startDatagramLane()
	select {
	case <-ready:
	case <-time.After(6 * time.Second):
		t.Fatal("did not retry pre-accept failures")
	}
	deadline := time.Now().Add(time.Second)
	for tun.datagramLane.Load() == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if tun.datagramLane.Load() == nil {
		t.Fatal("no active lane after retry")
	}
	if attempts.Load() != 3 {
		t.Fatalf("attempts=%d", attempts.Load())
	}
	close(tun.done)
	<-gatewayDone
	time.Sleep(50 * time.Millisecond)
	if attempts.Load() != 3 {
		t.Fatal("must not reconnect after authenticated accept")
	}
}

func TestDatagramReattachUsesNewEpochWithoutNoiseReconnect(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quic.sock.dgram")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	up, err := noise.NewCipherState(bytes.Repeat([]byte{0x82}, 32))
	if err != nil {
		t.Fatal(err)
	}
	down, err := noise.NewCipherState(bytes.Repeat([]byte{0x83}, 32))
	if err != nil {
		t.Fatal(err)
	}
	sid := "0123456789abcdef0123456789abcdef"
	tun := &tunnel{sid: sid, crypto: &cryptoState{up: up, down: down}, done: make(chan struct{})}
	defer close(tun.done)
	gatewayDone := make(chan struct{})
	go func() {
		defer close(gatewayDone)
		for epoch := uint32(0); epoch < 2; epoch++ {
			c, e := ln.Accept()
			if e != nil {
				return
			}
			var request [dgram.AttachSize]byte
			if _, e = io.ReadFull(c, request[:]); e != nil {
				c.Close()
				return
			}
			if epoch == 0 {
				if !dgram.VerifyProof(up.DatagramSecret(), sid, "attach", request[32:]) {
					c.Close()
					return
				}
			} else {
				if got, ok := dgram.ParseAttachEpoch(up.DatagramSecret(), sid, request[32:]); !ok || got != epoch {
					c.Close()
					return
				}
			}
			proof := dgram.Proof(down.DatagramSecret(), sid, "accept")
			if epoch != 0 {
				proof = dgram.AcceptEpoch(down.DatagramSecret(), sid, epoch)
			}
			_ = dgram.WriteFull(c, proof[:])
			// Allow the core to install the lane, then retire it as if QUIC idled.
			time.Sleep(100 * time.Millisecond)
			c.Close()
		}
	}()
	t.Setenv("MBOX_QUIC_UNIX", filepath.Join(dir, "quic.sock"))
	t.Setenv("MBOX_DATAGRAM", "1")
	t.Setenv("MBOX_HTTP_BRIDGE_ADDR", "")
	previous := enableUDPClient
	enableUDPClient = true
	t.Cleanup(func() { enableUDPClient = previous })
	tun.startDatagramLane()
	wait := func() {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			tun.datagramMu.Lock()
			running := tun.datagramRunning
			tun.datagramMu.Unlock()
			if !running && tun.datagramLane.Load() == nil {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("lane did not retire")
	}
	wait()
	// Normally new user UDP triggers the reconnect. Skip only the cooldown in test.
	tun.datagramMu.Lock()
	tun.datagramRetryAfter = time.Time{}
	tun.datagramMu.Unlock()
	tun.startDatagramLane()
	wait()
	select {
	case <-gatewayDone:
	case <-time.After(time.Second):
		t.Fatal("second authenticated attach missing")
	}
	tun.datagramMu.Lock()
	epoch := tun.datagramEpoch
	tun.datagramMu.Unlock()
	if epoch != 2 {
		t.Fatalf("attempted epochs=%d", epoch)
	}
}
