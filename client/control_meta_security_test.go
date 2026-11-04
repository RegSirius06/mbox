package main

import (
	"net/http"
	"net/url"
	"testing"

	controlmeta "../common/controlmeta"
)

func TestClientSignsDownAndUpMetadata(t *testing.T) {
	tun := newQueueTestTunnel()
	tun.crypto = &cryptoState{controlKey: [32]byte{1, 2, 3}}
	tun.sid = "0123456789abcdef0123456789abcdef"
	tun.downAck.Store(7)
	for _, tc := range []struct {
		raw, method string
		withMax     bool
	}{
		{"https://relay.invalid/mbox/sync/" + tun.sid, http.MethodGet, true},
		{"https://relay.invalid/mbox/events/" + tun.sid, http.MethodPost, false},
	} {
		raw, seq, err := tun.newDownRequestURL(tc.raw, tc.withMax)
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		if !controlmeta.Verify(tun.crypto.controlKey, tc.method, map[bool]string{true: "DOWN", false: "UP"}[tc.withMax], seq, 7, q.Get("max"), q.Get("cmac")) {
			t.Fatal("client control metadata not authenticated")
		}
		if controlmeta.Verify(tun.crypto.controlKey, tc.method, map[bool]string{true: "DOWN", false: "UP"}[tc.withMax], seq, 8, q.Get("max"), q.Get("cmac")) {
			t.Fatal("mutated ack verified")
		}
	}
}
