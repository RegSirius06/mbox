// Package wire contains identical HTTP envelope and payload padding codecs
// used by both sides of the MBox transport. It does not decide padding policy.
package wire

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
)

type httpEnvelope struct {
	Version int    `json:"v"`
	Data    string `json:"data"`
}

func EncodeHTTPEnvelope(data []byte) ([]byte, error) {
	return json.Marshal(httpEnvelope{Version: 1, Data: base64.StdEncoding.EncodeToString(data)})
}

func DecodeHTTPEnvelope(body []byte) ([]byte, error) {
	var env httpEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("invalid HTTP envelope")
	}
	if env.Version != 1 {
		return nil, fmt.Errorf("unsupported HTTP envelope version")
	}
	data, err := base64.StdEncoding.DecodeString(env.Data)
	if err != nil {
		return nil, fmt.Errorf("invalid HTTP envelope data")
	}
	return data, nil
}

// Pad preserves the 4-byte length header and cryptographically random padding.
func Pad(plain []byte, target int) ([]byte, error) {
	if target < 4+len(plain) {
		return nil, fmt.Errorf("invalid padded plaintext target")
	}
	out := make([]byte, target)
	binary.BigEndian.PutUint32(out[:4], uint32(len(plain)))
	copy(out[4:], plain)
	padding := out[4+len(plain):]
	if len(padding) > 0 {
		if _, err := rand.Read(padding); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func Unpad(padded []byte) ([]byte, error) {
	if len(padded) < 4 {
		return nil, fmt.Errorf("short padded plaintext")
	}
	n := uint64(binary.BigEndian.Uint32(padded[:4]))
	if n > uint64(len(padded)-4) {
		return nil, fmt.Errorf("invalid padded plaintext length")
	}
	return padded[4 : 4+int(n)], nil
}
