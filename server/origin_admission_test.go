package main

import (
	"bytes"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

type countBody struct {
	io.Reader
	reads *atomic.Int32
}

func (b *countBody) Read(p []byte) (int, error) { b.reads.Add(1); return b.Reader.Read(p) }
func (b *countBody) Close() error               { return nil }

func signedBodyRequest(body []byte, nonce string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "http://origin.invalid/api/v1/session", nil)
	ts := time.Now().Unix()
	tsText := formatOriginTimestamp(ts)
	r.Header.Set(originTimestampHeader, tsText)
	r.Header.Set(originNonceHeader, nonce)
	r.Header.Set(originSignatureHeader, hex.EncodeToString(originSignature(originAuthSecret, r.Method, r.URL.Path, r.URL.RawQuery, tsText, nonce, body)))
	r.Body = io.NopCloser(bytes.NewReader(body))
	return r
}

func formatOriginTimestamp(n int64) string { return strconv.FormatInt(n, 10) }

func TestOriginPreflightRejectsUnauthenticatedBodyWithoutReading(t *testing.T) {
	old := originAuthSecret
	originAuthSecret = make([]byte, 32)
	defer func() { originAuthSecret = old }()
	var called atomic.Int32
	handler := originAuthMiddlewareWithLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called.Add(1); w.WriteHeader(http.StatusNoContent) }), 1)
	for _, tc := range []struct {
		label  string
		mutate func(*http.Request)
	}{
		{"unsigned", func(r *http.Request) { r.Header.Del(originSignatureHeader) }},
		{"duplicated", func(r *http.Request) { r.Header.Add(originSignatureHeader, r.Header.Get(originSignatureHeader)) }},
		{"stale", func(r *http.Request) { r.Header.Set(originTimestampHeader, "1000000000") }},
		{"uppercase nonce", func(r *http.Request) { r.Header.Set(originNonceHeader, "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF") }},
		{"invalid hex", func(r *http.Request) {
			r.Header.Set(originSignatureHeader, "zz"+r.Header.Get(originSignatureHeader)[2:])
		}},
	} {
		t.Run(tc.label, func(t *testing.T) {
			r := signedBodyRequest([]byte("body"), "00112233445566778899aabbccddeeff")
			var reads atomic.Int32
			r.Body = &countBody{Reader: bytes.NewReader([]byte("body")), reads: &reads}
			tc.mutate(r)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != http.StatusNotFound || reads.Load() != 0 || called.Load() != 0 {
				t.Fatalf("rejected=%d reads=%d handlers=%d", w.Code, reads.Load(), called.Load())
			}
		})
	}
}

func TestOriginAuthLimitsSimultaneousBodyReadsAndReleasesSlots(t *testing.T) {
	old := originAuthSecret
	originAuthSecret = bytes.Repeat([]byte{0x92}, 32)
	defer func() { originAuthSecret = old }()
	originNonceMu.Lock()
	oldNonces := originNonces
	originNonces = make(map[string]time.Time)
	originNonceMu.Unlock()
	defer func() { originNonceMu.Lock(); originNonces = oldNonces; originNonceMu.Unlock() }()
	entered := make(chan struct{})
	resume := make(chan struct{})
	var active atomic.Int32
	handler := originAuthMiddlewareWithLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if active.Add(1) == 1 {
			close(entered)
			<-resume
		}
		w.WriteHeader(http.StatusNoContent)
	}), 1)
	firstDone := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, signedBodyRequest([]byte("hello"), "00112233445566778899aabbccddeeff"))
		firstDone <- w.Code
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first request did not enter handler")
	}
	second := signedBodyRequest([]byte("hello"), "10112233445566778899aabbccddeeff")
	var reads atomic.Int32
	second.Body = &countBody{Reader: bytes.NewReader([]byte("hello")), reads: &reads}
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, second)
	if w2.Code != http.StatusServiceUnavailable || reads.Load() != 0 || w2.Header().Get("Retry-After") == "" {
		t.Fatalf("over-capacity status=%d read-count=%d", w2.Code, reads.Load())
	}
	close(resume)
	if code := <-firstDone; code != http.StatusNoContent {
		t.Fatalf("first code=%d", code)
	}
	// New request is authenticated and gets a slot after the first completes.
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, signedBodyRequest([]byte("hello"), "20112233445566778899aabbccddeeff"))
	if w3.Code != http.StatusNoContent {
		t.Fatalf("after release code=%d", w3.Code)
	}
}
