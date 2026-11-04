package controlmeta

import (
	"bytes"
	"strings"
	"testing"
)

func TestControlMACPreventsFrontendTampering(t *testing.T) {
	key, err := Derive(bytes.Repeat([]byte{3}, 32), bytes.Repeat([]byte{7}, 32), strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	method := "GET"
	path := "/api/v1/down/" + strings.Repeat("a", 32)
	tag := Tag(key, method, path, 17, 4, "12288")
	if !Verify(key, method, path, 17, 4, "12288", tag) {
		t.Fatal("valid metadata rejected")
	}
	cases := []struct {
		method, path string
		seq, ack     uint64
		max          string
	}{
		{"POST", path, 17, 4, "12288"},
		{"GET", path + "x", 17, 4, "12288"},
		{"GET", path, 18, 4, "12288"},
		{"GET", path, 17, 5, "12288"},
		{"GET", path, 17, 4, "12289"},
	}
	for _, tc := range cases {
		if Verify(key, tc.method, tc.path, tc.seq, tc.ack, tc.max, tag) {
			t.Fatalf("modified metadata authenticated: %+v", tc)
		}
	}
	for _, wrong := range []string{"", "!", tag + "x", tag[:len(tag)-1]} {
		if Verify(key, method, path, 17, 4, "12288", wrong) {
			t.Fatal("invalid tag accepted")
		}
	}
	other, err := Derive(bytes.Repeat([]byte{3}, 32), bytes.Repeat([]byte{8}, 32), strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	if Verify(other, method, path, 17, 4, "12288", tag) {
		t.Fatal("cross-session tag accepted")
	}
	if _, err := Derive(nil, nil, ""); err == nil {
		t.Fatal("short inputs accepted")
	}
}

// Public frontend may be /relay.fcgi/mbox/sync while the origin sees
// /mbox/sync. The logical purpose remains the same across that rewrite.
func TestControlMACSurvivesRelayPathRewrite(t *testing.T) {
	key, err := Derive(bytes.Repeat([]byte{0x19}, 32), bytes.Repeat([]byte{0x39}, 32), strings.Repeat("1", 32))
	if err != nil {
		t.Fatal(err)
	}
	signed := Tag(key, "GET", "DOWN", 99, 12, "262144")
	if !Verify(key, "GET", "DOWN", 99, 12, "262144", signed) {
		t.Fatal("path-independent purpose failed")
	}
	if Verify(key, "GET", "UP", 99, 12, "262144", signed) {
		t.Fatal("purpose confusion")
	}
}

func FuzzRejectTamperedControlMetadata(f *testing.F) {
	f.Add("GET", "DOWN", uint64(1), uint64(0), "4096")
	f.Add("POST", "UP", uint64(18446744073709551615), uint64(4), "")
	f.Fuzz(func(t *testing.T, method, purpose string, seq, ack uint64, max string) {
		if len(method)+len(purpose)+len(max) > 256 {
			t.Skip()
		}
		key := [32]byte{0x91}
		signed := Tag(key, method, purpose, seq, ack, max)
		if !Verify(key, method, purpose, seq, ack, max, signed) {
			t.Fatal("valid tag rejected")
		}
		if Verify(key, method, purpose, seq, ack^1, max, signed) {
			t.Fatal("modified ack accepted")
		}
		if Verify(key, method, purpose, seq^1, ack, max, signed) {
			t.Fatal("modified seq accepted")
		}
	})
}
