package main

import (
	"encoding/hex"
	"testing"
	"time"
)

func TestOriginSignatureBindsMethodPathQueryAndBody(t *testing.T) {
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i + 1)
	}
	ts := "1700000000"
	nonce := hex.EncodeToString(make([]byte, 16))
	base := originSignature(secret, "POST", "/api/v1/sync/x", "seq=1&ack=0", ts, nonce, []byte("body"))
	cases := []string{
		originSignature(secret, "GET", "/api/v1/sync/x", "seq=1&ack=0", ts, nonce, []byte("body")),
		originSignature(secret, "POST", "/api/v1/sync/y", "seq=1&ack=0", ts, nonce, []byte("body")),
		originSignature(secret, "POST", "/api/v1/sync/x", "seq=2&ack=0", ts, nonce, []byte("body")),
		originSignature(secret, "POST", "/api/v1/sync/x", "seq=1&ack=0", ts, nonce, []byte("tampered")),
	}
	for _, got := range cases {
		if got == base {
			t.Fatal("signature did not bind all request components")
		}
	}
}

func TestSignOriginRequestCreatesFreshNonce(t *testing.T) {
	secret := make([]byte, 32)
	_, n1, s1, err := signOriginRequest(secret, "GET", "/health", "", nil, time.Unix(1700000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	_, n2, s2, err := signOriginRequest(secret, "GET", "/health", "", nil, time.Unix(1700000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if n1 == n2 || s1 == s2 {
		t.Fatal("origin request nonce/signature reused")
	}
}
