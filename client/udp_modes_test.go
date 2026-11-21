package main

import "testing"

func TestUDPModeEnvironment(t *testing.T) {
	for _, tc := range []struct{ raw, expected string }{
		{"", "auto"}, {"auto", "auto"}, {"streams", "streams"},
		{"datagrams", "datagrams"}, {"INVALID", "auto"},
	} {
		t.Setenv("MBOX_UDP_MODE", tc.raw)
		if got := udpModeFromEnv(); got != tc.expected {
			t.Fatalf("%q: %q, want %q", tc.raw, got, tc.expected)
		}
	}
}
