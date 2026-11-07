package main

import (
	originauth "../common/originauth"
	"encoding/hex"
	"fmt"
	"os"
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
	return originauth.Message(method, requestPath, rawQuery, ts, nonce, body)
}

func originSignature(secret []byte, method, requestPath, rawQuery, ts, nonce string, body []byte) string {
	return hex.EncodeToString(originauth.Signature(secret, method, requestPath, rawQuery, ts, nonce, body))
}

func signOriginRequest(secret []byte, method, requestPath, rawQuery string, body []byte, now time.Time) (ts, nonce, signature string, err error) {
	return originauth.SignRequest(secret, method, requestPath, rawQuery, body, now)
}
