package wire

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestHTTPEnvelopeGoldenAndErrors(t *testing.T) {
	plain := []byte{0, 1, 2}
	env, err := EncodeHTTPEnvelope(plain)
	if err != nil || string(env) != `{"v":1,"data":"AAEC"}` {
		t.Fatalf("envelope changed: %s (%v)", env, err)
	}
	got, err := DecodeHTTPEnvelope(env)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("round trip: %v (%v)", got, err)
	}
	for _, in := range []string{``, `{"v":2,"data":"AAEC"}`, `{"v":1,"data":"#"}`} {
		if _, err := DecodeHTTPEnvelope([]byte(in)); err == nil {
			t.Fatalf("accepted bad envelope %q", in)
		}
	}
}

func TestPaddingRoundTripAndMalformed(t *testing.T) {
	for _, size := range []int{0, 1, 100, 1232} {
		plain := bytes.Repeat([]byte{0x83}, size)
		padded, err := Pad(plain, size+17)
		if err != nil {
			t.Fatal(err)
		}
		if len(padded) != size+17 || binary.BigEndian.Uint32(padded[:4]) != uint32(size) {
			t.Fatalf("padding layout changed: %d", len(padded))
		}
		out, err := Unpad(padded)
		if err != nil || !bytes.Equal(out, plain) {
			t.Fatalf("padding round trip failed: %v", err)
		}
	}
	if _, err := Pad([]byte{1, 2, 3}, 6); err == nil {
		t.Fatal("accepted undersized padding target")
	}
	for _, p := range [][]byte{nil, {0, 0, 0}, {0, 0, 0, 1}} {
		if _, err := Unpad(p); err == nil {
			t.Fatalf("accepted malformed padding %x", p)
		}
	}
}

func FuzzHTTPEnvelopeDecoder(f *testing.F) {
	f.Add([]byte(`{"v":1,"data":"AAEC"}`))
	f.Add([]byte(`{"v":1,"data":"/"}`))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = DecodeHTTPEnvelope(data) })
}
func FuzzUnpad(f *testing.F) {
	f.Add([]byte{0, 0, 0, 0})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = Unpad(data) })
}
