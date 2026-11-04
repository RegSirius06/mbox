package main

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func signedOriginRequest(t *testing.T, method, target string, body []byte, now time.Time, nonce string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, target, nil)
	ts := "1700000000"
	if !now.Equal(time.Unix(1700000000, 0)) {
		ts = fmt.Sprint(now.Unix())
	}
	r.Header.Set(originTimestampHeader, ts)
	r.Header.Set(originNonceHeader, nonce)
	r.Header.Set(originSignatureHeader, hex.EncodeToString(originSignature(originAuthSecret, method, r.URL.Path, r.URL.RawQuery, ts, nonce, body)))
	return r
}

func TestVerifyOriginRequestRejectsReplayAndTamper(t *testing.T) {
	oldSecret := originAuthSecret
	originAuthSecret = make([]byte, 32)
	defer func() { originAuthSecret = oldSecret }()
	originNonceMu.Lock()
	originNonces = make(map[string]time.Time)
	originNonceMu.Unlock()
	now := time.Unix(1700000000, 0)
	nonce := "00112233445566778899aabbccddeeff"
	r := httptest.NewRequest("POST", "https://origin.example/api/v1/sync/x?seq=1", nil)
	ts := "1700000000"
	r.Header.Set(originTimestampHeader, ts)
	r.Header.Set(originNonceHeader, nonce)
	r.Header.Set(originSignatureHeader, hex.EncodeToString(originSignature(originAuthSecret, r.Method, r.URL.Path, r.URL.RawQuery, ts, nonce, []byte("body"))))
	if err := verifyOriginRequest(r, []byte("body"), now); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	if err := verifyOriginRequest(r, []byte("body"), now); err == nil {
		t.Fatal("replayed request accepted")
	}

	nonce2 := "11112233445566778899aabbccddeeff"
	r2 := httptest.NewRequest("POST", "https://origin.example/api/v1/sync/x?seq=1", nil)
	r2.Header.Set(originTimestampHeader, ts)
	r2.Header.Set(originNonceHeader, nonce2)
	r2.Header.Set(originSignatureHeader, hex.EncodeToString(originSignature(originAuthSecret, r2.Method, r2.URL.Path, r2.URL.RawQuery, ts, nonce2, []byte("body"))))
	if err := verifyOriginRequest(r2, []byte("tampered"), now); err == nil {
		t.Fatal("tampered body accepted")
	}
}

func TestOriginNonceCacheIsBounded(t *testing.T) {
	originNonceMu.Lock()
	originNonces = make(map[string]time.Time, originNonceMax)
	now := time.Now()
	for i := 0; i < originNonceMax; i++ {
		originNonces[fmt.Sprintf("%032x", i)] = now.Add(time.Minute)
	}
	originNonceMu.Unlock()
	if consumeOriginNonce("ffffffffffffffffffffffffffffffff", now) {
		t.Fatal("nonce cache exceeded hard limit")
	}
}
