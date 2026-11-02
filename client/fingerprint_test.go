package main

import (
	"testing"
	"time"
)

func TestDeterministicTransportDisablesCamouflageTimingJitter(t *testing.T) {
	old := deterministicTransport
	deterministicTransport = true
	defer func() { deterministicTransport = old }()

	if got := randomDuration(100, 300); got != 200*time.Millisecond {
		t.Fatalf("randomDuration=%v want 200ms", got)
	}
	if got := jitterDuration(200*time.Millisecond, 35); got != 200*time.Millisecond {
		t.Fatalf("jitterDuration=%v want 200ms", got)
	}
}

func TestDeterministicTransportStabilizesBatchAndPadding(t *testing.T) {
	oldDet := deterministicTransport
	oldBatch := camouflage.BatchMinBytes
	oldPad := camouflage.PaddingRandomMaxBytes
	deterministicTransport = true
	camouflage.BatchMinBytes = 100000
	camouflage.PaddingRandomMaxBytes = 1000
	defer func() {
		deterministicTransport = oldDet
		camouflage.BatchMinBytes = oldBatch
		camouflage.PaddingRandomMaxBytes = oldPad
	}()

	if got := randomBatchLimit(200000); got != 150000 {
		t.Fatalf("batch=%d want 150000", got)
	}
	got, err := paddedPlaintextSize(100)
	if err != nil {
		t.Fatal(err)
	}
	if got != 604 { // 100 bytes + 4-byte length + fixed half of 1000 bytes.
		t.Fatalf("padded size=%d want 604", got)
	}
}

func TestDeterministicTransportStabilizesNoiseChunking(t *testing.T) {
	oldDet := deterministicTransport
	oldMin, oldMax := camouflage.NoiseChunkMinBytes, camouflage.NoiseChunkMaxBytes
	deterministicTransport = true
	camouflage.NoiseChunkMinBytes = 4096
	camouflage.NoiseChunkMaxBytes = 8192
	defer func() {
		deterministicTransport = oldDet
		camouflage.NoiseChunkMinBytes, camouflage.NoiseChunkMaxBytes = oldMin, oldMax
	}()

	plain := make([]byte, 20*1024)
	chunks, err := splitNoisePlaintext(plain)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	if len(chunks[0]) != 6144 {
		t.Fatalf("first chunk=%d want 6144", len(chunks[0]))
	}
}
