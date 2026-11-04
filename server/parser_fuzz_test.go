package main

import (
	"bytes"
	"testing"

	noise "../common/noise"
)

func FuzzHTTPEnvelopeParser(f *testing.F) {
	f.Add([]byte(`{"v":1,"data":"aGVsbG8="}`))
	f.Add([]byte(`{"v":2,"data":""}`))
	f.Add([]byte("not-json"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxRequestBody {
			t.Skip()
		}
		_, _ = decodeHTTPEnvelope(data)
	})
}

func FuzzNoiseOpenPayloadParser(f *testing.F) {
	f.Add([]byte{})
	f.Add(append(append([]byte(nil), devicePayloadPrefix...), make([]byte, 16)...))
	f.Add([]byte("MBD1-short"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = parseNoiseOpenPayload(data)
	})
}

func FuzzFrameWireParser(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0, 0, 0, 1, framePing, 0, 0, 0, 0})
	f.Add([]byte{0, 0, 0, 1, frameData, 0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 2*maxRequestBody {
			t.Skip()
		}
		_ = walkFrames(data, func(streamID uint32, typ byte, payload []byte) error {
			if len(payload) > maxFramePayload {
				t.Fatalf("parser emitted oversized payload: %d", len(payload))
			}
			return nil
		})
	})
}

func FuzzNoiseRecordParser(f *testing.F) {
	key := bytes.Repeat([]byte{0x42}, 32)
	seedCipher, err := noise.NewCipherState(key)
	if err != nil {
		f.Fatal(err)
	}
	oldCamouflage := camouflage
	camouflage.NoiseChunkMinBytes = 1024
	camouflage.NoiseChunkMaxBytes = 4096
	seed, _, err := sealNoiseRecord(seedCipher, "fuzz-session", dirUp, 1, 0, []byte("hello"))
	camouflage = oldCamouflage
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	f.Add([]byte("short"))
	f.Fuzz(func(t *testing.T, record []byte) {
		if len(record) > 4*maxRequestBody {
			t.Skip()
		}
		c, err := noise.NewCipherState(key)
		if err != nil {
			t.Fatal(err)
		}
		_, _, _ = openNoiseRecord(c, "fuzz-session", dirUp, record)
	})
}
