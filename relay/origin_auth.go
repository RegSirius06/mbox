package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	originTimestampHeader = "X-MBox-Origin-Timestamp"
	originNonceHeader     = "X-MBox-Origin-Nonce"
	originSignatureHeader = "X-MBox-Origin-Signature"
)

func loadOriginSecret(path string) ([]byte, error) {
	if path == "" {
		return nil, fmt.Errorf("origin_hmac_secret_file is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("origin HMAC secret must be a regular owner-only file (0600)")
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	secret, err := hex.DecodeString(strings.TrimSpace(string(text)))
	if err != nil || len(secret) != 32 {
		return nil, fmt.Errorf("origin HMAC secret must contain exactly 32 bytes of hex")
	}
	return secret, nil
}

func originAuthMessage(method, requestPath, rawQuery, ts, nonce string, body []byte) []byte {
	h := sha256.Sum256(body)
	return []byte(method + "\n" + requestPath + "\n" + rawQuery + "\n" + ts + "\n" + nonce + "\n" + hex.EncodeToString(h[:]))
}

func originSignature(secret []byte, method, requestPath, rawQuery, ts, nonce string, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(originAuthMessage(method, requestPath, rawQuery, ts, nonce, body))
	return hex.EncodeToString(mac.Sum(nil))
}

func newOriginNonce() (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(nonce[:]), nil
}

func signOriginRequest(secret []byte, method, requestPath, rawQuery string, body []byte, now time.Time) (ts, nonce, signature string, err error) {
	nonce, err = newOriginNonce()
	if err != nil {
		return "", "", "", err
	}
	ts = strconv.FormatInt(now.Unix(), 10)
	signature = originSignature(secret, method, requestPath, rawQuery, ts, nonce, body)
	return ts, nonce, signature, nil
}
