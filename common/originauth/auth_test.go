package originauth

import (
	"crypto/hmac"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestSignature(t *testing.T) {
	key := []byte(strings.Repeat("K", 32))
	ts, nonce, sig, err := SignRequest(key, "POST", "/api/v1/session", "a=1", []byte("test"), time.Unix(1000, 0))
	if err != nil || ts != "1000" || len(nonce) != 32 || len(sig) != 64 {
		t.Fatalf("sign: %q %q %q %v", ts, nonce, sig, err)
	}
	decoded, err := hex.DecodeString(sig)
	if err != nil {
		t.Fatal(err)
	}
	if !hmac.Equal(decoded, Signature(key, "POST", "/api/v1/session", "a=1", ts, nonce, []byte("test"))) {
		t.Fatal("wrong signature")
	}
	if hmac.Equal(decoded, Signature(key, "POST", "/api/v1/session", "a=2", ts, nonce, []byte("test"))) {
		t.Fatal("query not authenticated")
	}
	if hmac.Equal(decoded, Signature(key, "POST", "/api/v1/session", "a=1", ts, nonce, []byte("other"))) {
		t.Fatal("body not authenticated")
	}
}
