// Package originauth is the shared REG.RU / native-QUIC to origin HMAC signer.
// The origin verifies the signature and a one-time nonce independently.
package originauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"
)

const (
	TimestampHeader = "X-MBox-Origin-Timestamp"
	NonceHeader     = "X-MBox-Origin-Nonce"
	SignatureHeader = "X-MBox-Origin-Signature"
)

func Message(method, path, query, timestamp, nonce string, body []byte) []byte {
	h := sha256.Sum256(body)
	return []byte(method + "\n" + path + "\n" + query + "\n" + timestamp + "\n" + nonce + "\n" + hex.EncodeToString(h[:]))
}

func Signature(secret []byte, method, path, query, timestamp, nonce string, body []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(Message(method, path, query, timestamp, nonce, body))
	return mac.Sum(nil)
}

func SignRequest(secret []byte, method, path, query string, body []byte, now time.Time) (timestamp, nonce, signature string, err error) {
	var rnd [16]byte
	if _, err = rand.Read(rnd[:]); err != nil {
		return "", "", "", err
	}
	nonce = hex.EncodeToString(rnd[:])
	timestamp = strconv.FormatInt(now.Unix(), 10)
	signature = hex.EncodeToString(Signature(secret, method, path, query, timestamp, nonce, body))
	return timestamp, nonce, signature, nil
}
