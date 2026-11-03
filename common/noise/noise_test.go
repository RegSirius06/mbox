package noise

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type fixedReader struct{ b []byte }

func (r *fixedReader) Read(p []byte) (int, error) { n := copy(p, r.b); r.b = r.b[n:]; return n, nil }

// Vector from ethindp/noise-cpp tests/vectors/Noise_NKpsk0_25519_AESGCM_SHA256_1.json.
func TestNKpsk0OfficialVector(t *testing.T) {
	psk := mustHex(t, "54686973206973206d7920417573747269616e20706572737065637469766521")
	prologue := mustHex(t, "4a6f686e2047616c74")
	initE := mustHex(t, "893e28b9dc6ca8d611ab664754b8ceb7bac5117349a4439a6b0569da977c464a")
	respS := mustHex(t, "4a3acbfdb163dec651dfa3194dece676d437029c62a408b4c5ea9114246e4893")
	respE := mustHex(t, "bbdb4cdbd309f1a1f2e1456967fe288cadd6f712d65dc7b7793d5e63da6b375b")
	respKP, err := KeypairFromPrivate(respS)
	if err != nil {
		t.Fatal(err)
	}
	expectedRS := mustHex(t, "31e0303fd6418d2f8c0e78b91f22e8caed0fbe48656dcf4767e4834f701b8f62")
	if !bytes.Equal(respKP.Public, expectedRS) {
		t.Fatalf("responder static public mismatch: %x", respKP.Public)
	}

	i, err := NewInitiator(psk, respKP.Public, prologue, &fixedReader{b: append([]byte(nil), initE...)})
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewResponder(psk, respS, prologue, &fixedReader{b: append([]byte(nil), respE...)})
	if err != nil {
		t.Fatal(err)
	}

	p1 := mustHex(t, "4c756477696720766f6e204d69736573")
	m1, _, _, err := i.WriteMessage(p1)
	if err != nil {
		t.Fatal(err)
	}
	expected1 := mustHex(t, "ca35def5ae56cec33dc2036731ab14896bc4c75dbb07a61f879f8e3afa4c7944052cffc8d5cdfaed3f9ca0a86c406cf58d24cfc3cec1af5060fd25c4e8a5ac04")
	if !bytes.Equal(m1, expected1) {
		t.Fatalf("message1 mismatch\n got %x\nwant %x", m1, expected1)
	}
	got1, _, _, err := r.ReadMessage(m1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got1, p1) {
		t.Fatalf("payload1 mismatch")
	}

	p2 := mustHex(t, "4d757272617920526f746862617264")
	m2, rSend, rRecv, err := r.WriteMessage(p2)
	if err != nil {
		t.Fatal(err)
	}
	expected2 := mustHex(t, "95ebc60d2b1fa672c1f46a8aa265ef51bfe38e7ccb39ec5be34069f144808843afd57077a1747b71254c19bd798c87ac61a2d4808a30a1372f81b2cb4d85f6")
	if !bytes.Equal(m2, expected2) {
		t.Fatalf("message2 mismatch\n got %x\nwant %x", m2, expected2)
	}
	got2, iSend, iRecv, err := i.ReadMessage(m2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got2, p2) {
		t.Fatalf("payload2 mismatch")
	}
	wantHash := mustHex(t, "c9855b6c52aeba6b7d07097d260e058ea90e85fa810c0c3e2ec3869f3f4319c9")
	if !bytes.Equal(i.ChannelBinding(), wantHash) || !bytes.Equal(r.ChannelBinding(), wantHash) {
		t.Fatalf("handshake hash mismatch")
	}

	p3 := mustHex(t, "462e20412e20486179656b")
	c3, err := iSend.Seal(0, nil, p3)
	if err != nil {
		t.Fatal(err)
	}
	want3 := mustHex(t, "0ed5fabce6ef0ded315c4f213c51ea1e89be89f86c5dc98e50d15d")
	if !bytes.Equal(c3, want3) {
		t.Fatalf("transport 1 mismatch: %x", c3)
	}
	d3, err := rRecv.Open(0, nil, c3)
	if err != nil || !bytes.Equal(d3, p3) {
		t.Fatalf("transport 1 decrypt")
	}

	p4 := mustHex(t, "4361726c204d656e676572")
	c4, err := rSend.Seal(0, nil, p4)
	if err != nil {
		t.Fatal(err)
	}
	want4 := mustHex(t, "ba3cbc8800e6eb1b585269dca0f00fdc21f830eb1a497589215f7f")
	if !bytes.Equal(c4, want4) {
		t.Fatalf("transport 2 mismatch: %x", c4)
	}
	d4, err := iRecv.Open(0, nil, c4)
	if err != nil || !bytes.Equal(d4, p4) {
		t.Fatalf("transport 2 decrypt")
	}
}

func FuzzResponderReadMessage(f *testing.F) {
	psk := bytes.Repeat([]byte{0x11}, 32)
	priv := bytes.Repeat([]byte{0x22}, 32)
	f.Add([]byte("short"))
	f.Add(make([]byte, 48))
	f.Fuzz(func(t *testing.T, msg []byte) {
		if len(msg) > MaxMsgLen {
			t.Skip()
		}
		r, err := NewResponder(psk, priv, []byte("fuzz-prologue"), nil)
		if err != nil {
			t.Fatal(err)
		}
		_, _, _, _ = r.ReadMessage(msg)
	})
}
