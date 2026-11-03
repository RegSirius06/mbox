package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
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
	originTimestampHeader = "X-MBox-Origin-Timestamp"
	originNonceHeader     = "X-MBox-Origin-Nonce"
	originSignatureHeader = "X-MBox-Origin-Signature"
	originAuthSkew        = 60 * time.Second
	originNonceTTL        = 2 * time.Minute
	originNonceMax        = 65536
)

var (
	originAuthSecret []byte
	originNonceMu    sync.Mutex
	originNonces     = make(map[string]time.Time)
)

func originAuthMessage(method, requestPath, rawQuery, ts, nonce string, body []byte) []byte {
	h := sha256.Sum256(body)
	return []byte(method + "\n" + requestPath + "\n" + rawQuery + "\n" + ts + "\n" + nonce + "\n" + hex.EncodeToString(h[:]))
}

func originSignature(secret []byte, method, requestPath, rawQuery, ts, nonce string, body []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(originAuthMessage(method, requestPath, rawQuery, ts, nonce, body))
	return mac.Sum(nil)
}

func pruneOriginNoncesLocked(now time.Time) {
	for nonce, expires := range originNonces {
		if !expires.After(now) {
			delete(originNonces, nonce)
		}
	}
}

func consumeOriginNonce(nonce string, now time.Time) bool {
	originNonceMu.Lock()
	defer originNonceMu.Unlock()
	pruneOriginNoncesLocked(now)
	if _, exists := originNonces[nonce]; exists {
		return false
	}
	if len(originNonces) >= originNonceMax {
		return false
	}
	originNonces[nonce] = now.Add(originNonceTTL)
	return true
}

func verifyOriginRequest(r *http.Request, body []byte, now time.Time) error {
	if len(originAuthSecret) != 32 {
		return fmt.Errorf("origin authentication unavailable")
	}
	tsText := r.Header.Get(originTimestampHeader)
	nonce := strings.ToLower(r.Header.Get(originNonceHeader))
	sigText := r.Header.Get(originSignatureHeader)
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
	if err != nil || len(sig) != sha256.Size {
		return fmt.Errorf("bad origin signature")
	}
	expected := originSignature(originAuthSecret, r.Method, r.URL.Path, r.URL.RawQuery, tsText, nonce, body)
	if !hmac.Equal(sig, expected) {
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
