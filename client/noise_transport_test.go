package main

import (
	"bytes"
	"encoding/binary"
	"testing"

	noise "../common/noise"
)

func TestNoiseTransportRecordMultiChunkRoundTrip(t *testing.T) {
	oldMin, oldMax := camouflage.NoiseChunkMinBytes, camouflage.NoiseChunkMaxBytes
	camouflage.NoiseChunkMinBytes, camouflage.NoiseChunkMaxBytes = 16*1024, 48*1024
	defer func() { camouflage.NoiseChunkMinBytes, camouflage.NoiseChunkMaxBytes = oldMin, oldMax }()

	key := bytes.Repeat([]byte{0x6c}, 32)
	send, err := noise.NewCipherState(key)
	if err != nil {
		t.Fatal(err)
	}
	recv, err := noise.NewCipherState(key)
	if err != nil {
		t.Fatal(err)
	}
	plain := bytes.Repeat([]byte("noise-mbox-"), 23000) // > 200 KiB
	sid := "0123456789abcdef0123456789abcdef"
	rec, next, err := sealNoiseRecord(send, sid, dirUp, 1, 0, plain)
	if err != nil {
		t.Fatal(err)
	}
	chunks := int(binary.BigEndian.Uint16(rec[18:20]))
	if chunks < 4 {
		t.Fatalf("expected multiple Noise chunks, got %d", chunks)
	}
	if next != uint64(chunks) {
		t.Fatalf("next nonce=%d chunks=%d", next, chunks)
	}
	counter, got, err := openNoiseRecord(recv, sid, dirUp, rec)
	if err != nil {
		t.Fatal(err)
	}
	if counter != 1 || !bytes.Equal(got, plain) {
		t.Fatal("Noise record roundtrip mismatch")
	}
}

func TestNoiseTransportHeaderTamperFails(t *testing.T) {
	key := bytes.Repeat([]byte{0x71}, 32)
	send, _ := noise.NewCipherState(key)
	recv, _ := noise.NewCipherState(key)
	sid := "fedcba9876543210fedcba9876543210"
	rec, _, err := sealNoiseRecord(send, sid, dirDown, 7, 3, []byte("authenticated"))
	if err != nil {
		t.Fatal(err)
	}
	rec[9] ^= 1 // bundle counter is authenticated as AAD.
	if _, _, err := openNoiseRecord(recv, sid, dirDown, rec); err == nil {
		t.Fatal("tampered Noise header accepted")
	}
}

func FuzzNoiseTransportRecord(f *testing.F) {
	f.Add([]byte{})
	f.Add(make([]byte, noiseRecordHeader))
	key := bytes.Repeat([]byte{0x55}, 32)
	f.Fuzz(func(t *testing.T, record []byte) {
		recv, err := noise.NewCipherState(key)
		if err != nil {
			t.Fatal(err)
		}
		_, _, _ = openNoiseRecord(recv, "0123456789abcdef0123456789abcdef", dirDown, record)
	})
}
