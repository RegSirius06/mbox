// Package controlmeta authenticates HTTP control metadata with a session-scoped
// key derived from the shared Noise PSK and the authenticated handshake
// transcript. The HTTP frontend never receives that key.
package controlmeta

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
)

const domain = "MBOX-HTTP-CONTROL-V1\x00"

// Derive binds the control authentication key to one successfully negotiated
// Noise session; binding is the Noise channel binding after handshake split.
func Derive(psk, binding []byte, sid string) ([32]byte, error) {
	var key [32]byte
	if len(psk) != 32 || len(binding) != 32 || len(sid) != 32 {
		return key, errors.New("invalid control key derivation inputs")
	}
	mac := hmac.New(sha256.New, psk)
	_, _ = mac.Write([]byte(domain + "KEY\x00"))
	_, _ = mac.Write(binding)
	_, _ = mac.Write([]byte(sid))
	copy(key[:], mac.Sum(nil))
	return key, nil
}

func tagBytes(key [32]byte, method, purpose string, seq, ack uint64, max string) []byte {
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write([]byte(domain + "REQUEST\x00"))
	// Use a logical endpoint purpose rather than the URL pathname: the shared
	// hosting relay removes /relay.fcgi from the path when forwarding.
	// Length-prefix variable fields to prevent ambiguous concatenations.
	for _, field := range []string{method, purpose, max} {
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(field)))
		_, _ = mac.Write(size[:])
		_, _ = mac.Write([]byte(field))
	}
	var counters [16]byte
	binary.BigEndian.PutUint64(counters[:8], seq)
	binary.BigEndian.PutUint64(counters[8:], ack)
	_, _ = mac.Write(counters[:])
	return mac.Sum(nil)
}

func Tag(key [32]byte, method, purpose string, seq, ack uint64, max string) string {
	return base64.RawURLEncoding.EncodeToString(tagBytes(key, method, purpose, seq, ack, max))
}

func Verify(key [32]byte, method, purpose string, seq, ack uint64, max, encoded string) bool {
	if len(encoded) != base64.RawURLEncoding.EncodedLen(sha256.Size) {
		return false
	}
	supplied, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(supplied) != sha256.Size {
		return false
	}
	return hmac.Equal(supplied, tagBytes(key, method, purpose, seq, ack, max))
}
