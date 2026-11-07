package main

import (
	originauth "../common/originauth"
	"bytes"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	originTimestampHeader    = "X-MBox-Origin-Timestamp"
	originNonceHeader        = "X-MBox-Origin-Nonce"
	originSignatureHeader    = "X-MBox-Origin-Signature"
	originAuthSkew           = 60 * time.Second
	originNonceTTL           = 2 * time.Minute
	originNonceMax           = 65536
	originNonceSweepInterval = 5 * time.Second
)

var (
	originAuthSecret     []byte
	originNonceMu        sync.Mutex
	originNonces         = make(map[string]time.Time)
	originNonceNextSweep time.Time
)

func originAuthMessage(method, requestPath, rawQuery, ts, nonce string, body []byte) []byte {
	return originauth.Message(method, requestPath, rawQuery, ts, nonce, body)
}

func originSignature(secret []byte, method, requestPath, rawQuery, ts, nonce string, body []byte) []byte {
	return originauth.Signature(secret, method, requestPath, rawQuery, ts, nonce, body)
}

func pruneOriginNoncesLocked(now time.Time) {
	for nonce, expires := range originNonces {
		if !expires.After(now) {
			delete(originNonces, nonce)
		}
	}
}

// The replay cache is swept at most once per interval. If it is full, reject
// new nonces in O(1) until the next sweep; this also bounds CPU under replay
// saturation. Old entries are also pruned by the background maintenance loop.
func consumeOriginNonce(nonce string, now time.Time) bool {
	originNonceMu.Lock()
	defer originNonceMu.Unlock()
	if originNonceNextSweep.IsZero() || !now.Before(originNonceNextSweep) {
		pruneOriginNoncesLocked(now)
		originNonceNextSweep = now.Add(originNonceSweepInterval)
	}
	if expires, exists := originNonces[nonce]; exists {
		if expires.After(now) {
			return false
		}
		delete(originNonces, nonce)
	}
	if len(originNonces) >= originNonceMax {
		return false
	}
	originNonces[nonce] = now.Add(originNonceTTL)
	return true
}

// originAuthPreflight rejects missing, duplicated, malformed and stale signed
// request metadata before buffering an attacker-controlled HTTP request body.
// Its checks do NOT authenticate the request: the body HMAC is still mandatory.
func originAuthPreflight(r *http.Request, now time.Time) error {
	if len(originAuthSecret) != 32 {
		return fmt.Errorf("origin authentication unavailable")
	}
	if len(r.Header.Values(originTimestampHeader)) != 1 || len(r.Header.Values(originNonceHeader)) != 1 || len(r.Header.Values(originSignatureHeader)) != 1 {
		return fmt.Errorf("ambiguous origin authentication headers")
	}
	tsText := r.Header.Get(originTimestampHeader)
	nonce := r.Header.Get(originNonceHeader)
	sigText := r.Header.Get(originSignatureHeader)
	// Only canonical encodings are accepted: relays always send lowercase hex.
	if len(tsText) > 20 || len(nonce) != 32 || len(sigText) != 64 || strings.ToLower(nonce) != nonce || strings.ToLower(sigText) != sigText {
		return fmt.Errorf("noncanonical origin authentication headers")
	}
	ts, err := strconv.ParseInt(tsText, 10, 64)
	if err != nil {
		return fmt.Errorf("bad origin timestamp")
	}
	requestTime := time.Unix(ts, 0)
	if now.Sub(requestTime) > originAuthSkew || requestTime.Sub(now) > originAuthSkew {
		return fmt.Errorf("stale origin request")
	}
	nonceBytes, err := hex.DecodeString(nonce)
	if err != nil || len(nonceBytes) != 16 {
		return fmt.Errorf("bad origin nonce")
	}
	sig, err := hex.DecodeString(sigText)
	if err != nil || len(sig) != 32 {
		return fmt.Errorf("bad origin signature")
	}
	return nil
}

func verifyOriginRequest(r *http.Request, body []byte, now time.Time) error {
	if err := originAuthPreflight(r, now); err != nil {
		return err
	}
	tsText := r.Header.Get(originTimestampHeader)
	nonce := r.Header.Get(originNonceHeader)
	sig, _ := hex.DecodeString(r.Header.Get(originSignatureHeader))
	expected := originSignature(originAuthSecret, r.Method, r.URL.Path, r.URL.RawQuery, tsText, nonce, body)
	if subtle.ConstantTimeCompare(sig, expected) != 1 {
		return fmt.Errorf("origin authentication failed")
	}
	// Only authenticated requests consume nonce-cache memory. This prevents an
	// unauthenticated Internet client from growing the replay structure.
	if !consumeOriginNonce(nonce, now) {
		return fmt.Errorf("replayed origin request")
	}
	return nil
}

func originAuthMiddleware(next http.Handler) http.Handler {
	return originAuthMiddlewareWithLimit(next, runtimeLimits.MaxConcurrentHTTPRequests)
}

// The semaphore is shared by all requests on the same origin listener. The
// bounded, non-blocking admission rule prevents unauthenticated request bodies
// from causing unbounded concurrent buffering (each may be maxRequestBody).
// A full queue must not spawn a goroutine waiting for capacity.
func originAuthMiddlewareWithLimit(next http.Handler, maxConcurrent int) http.Handler {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	slots := make(chan struct{}, maxConcurrent)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := originAuthPreflight(r, time.Now()); err != nil {
			metricsCounters.originAuthFailures.Add(1)
			camouflageNotFound(w)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			metricsCounters.originAdmissionRejects.Add(1)
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody+1))
		if err != nil || len(body) > maxRequestBody {
			camouflageNotFound(w)
			return
		}
		if err := verifyOriginRequest(r, body, time.Now()); err != nil {
			metricsCounters.originAuthFailures.Add(1)
			camouflageNotFound(w)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		next.ServeHTTP(w, r)
	})
}
