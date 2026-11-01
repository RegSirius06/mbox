package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
	"time"
)

func requireLongTransport(t *testing.T) {
	t.Helper()
	if os.Getenv("MBOX_LONG_TEST") != "1" {
		t.Skip("set MBOX_LONG_TEST=1")
	}
}
func TestMultiGBUploadChunking(t *testing.T) {
	requireLongTransport(t)
	old := camouflage.UpMaxBytes
	camouflage.UpMaxBytes = 256 * 1024
	defer func() { camouflage.UpMaxBytes = old }()
	tun := newQueueTestTunnel()
	payload := bytes.Repeat([]byte{0x6d}, 60*1024)
	const target = int64(3) * 1024 * 1024 * 1024
	done := make(chan error, 1)
	go func() {
		var sent int64
		for sent < target {
			n := len(payload)
			if r := target - sent; r < int64(n) {
				n = int(r)
			}
			if err := tun.queue(1, frameData, payload[:n]); err != nil {
				done <- err
				return
			}
			sent += int64(n)
		}
		done <- nil
	}()
	var delivered int64
	deadline := time.Now().Add(20 * time.Minute)
	for delivered < target {
		if time.Now().After(deadline) {
			t.Fatal("timeout")
		}
		chunk := tun.takeUpload(camouflage.UpMaxBytes)
		if len(chunk) == 0 {
			time.Sleep(time.Millisecond)
			continue
		}
		if len(chunk) > camouflage.UpMaxBytes {
			t.Fatal("oversized")
		}
		for len(chunk) > 0 {
			if len(chunk) < 9 {
				t.Fatal("short")
			}
			n := int(binary.BigEndian.Uint32(chunk[5:9]))
			if n > len(chunk)-9 {
				t.Fatal("payload")
			}
			delivered += int64(n)
			chunk = chunk[9+n:]
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if delivered != target {
		t.Fatalf("%d", delivered)
	}
}
