package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

const (
	// cryptoRecordVersion is the encrypted record format. It is independent
	// from the OPEN handshake generation (legacy v1 or client-authenticated v2).
	cryptoRecordVersion = 1
	dirUp               = 1
	dirDown             = 2
)

var (
	pskFile       string
	serverPubFile string
)

type cryptoState struct {
	upAEAD   cipher.AEAD
	downAEAD cipher.AEAD

	mu sync.Mutex

	upCounter uint64
}

func readHex32(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil {
		return nil, err
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("%s: expected 32 bytes, got %d", path, len(raw))
	}
	return raw, nil
}

func hkdfExtract(salt, ikm []byte) []byte {
	h := hmac.New(sha256.New, salt)
	h.Write(ikm)
	return h.Sum(nil)
}

func hkdfExpand(prk, info []byte, length int) []byte {
	var out, previous []byte
	for counter := byte(1); len(out) < length; counter++ {
		h := hmac.New(sha256.New, prk)
		h.Write(previous)
		h.Write(info)
		h.Write([]byte{counter})
		previous = h.Sum(nil)
		out = append(out, previous...)
	}
	return out[:length]
}

func deriveKeys(shared, psk []byte, sid string, clientPub []byte) ([]byte, []byte, []byte) {
	prk := hkdfExtract(psk, shared)

	info := []byte("MBOX1-KEYS\x00")
	info = append(info, []byte(sid)...)
	info = append(info, 0)
	info = append(info, clientPub...)

	okm := hkdfExpand(prk, info, 96)
	return okm[:32], okm[32:64], okm[64:96]
}

func deriveKeysV2(shared, psk []byte, sid, clientID string, clientPub []byte) ([]byte, []byte, []byte) {
	prk := hkdfExtract(psk, shared)

	info := []byte("MBOX2-KEYS\x00")
	info = append(info, []byte(sid)...)
	info = append(info, 0)
	info = append(info, []byte(clientID)...)
	info = append(info, 0)
	info = append(info, clientPub...)

	okm := hkdfExpand(prk, info, 96)
	return okm[:32], okm[32:64], okm[64:96]
}

func validClientID(id string) bool {
	if len(id) < 1 || len(id) > 63 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !((c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func buildOpenRequest() ([]byte, *ecdh.PrivateKey, []byte, error) {
	psk, err := readHex32(pskFile)
	if err != nil {
		return nil, nil, nil, err
	}

	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, nil, err
	}

	pub := priv.PublicKey().Bytes()

	h := hmac.New(sha256.New, psk)

	var body []byte

	if clientID != "" {
		if !validClientID(clientID) {
			return nil, nil, nil, fmt.Errorf("invalid client_id %q", clientID)
		}

		h.Write([]byte("MBOX2-OPEN\x00"))
		h.Write([]byte(clientID))
		h.Write([]byte{0})
		h.Write(pub)

		body = append(body, byte(len(clientID)))
		body = append(body, []byte(clientID)...)
		body = append(body, pub...)
		body = append(body, h.Sum(nil)...)
	} else {
		h.Write([]byte("MBOX1-OPEN\x00"))
		h.Write(pub)

		body = append([]byte(nil), pub...)
		body = append(body, h.Sum(nil)...)
	}

	return body, priv, pub, nil
}

func finishClientHandshake(priv *ecdh.PrivateKey, clientPub []byte, sid, confirmHex string) (*cryptoState, error) {
	psk, err := readHex32(pskFile)
	if err != nil {
		return nil, err
	}

	serverPubRaw, err := readHex32(serverPubFile)
	if err != nil {
		return nil, err
	}

	serverPub, err := ecdh.X25519().NewPublicKey(serverPubRaw)
	if err != nil {
		return nil, err
	}

	shared, err := priv.ECDH(serverPub)
	if err != nil {
		return nil, err
	}

	var upKey, downKey, confirmKey []byte

	if clientID != "" {
		upKey, downKey, confirmKey = deriveKeysV2(
			shared, psk, sid, clientID, clientPub,
		)
	} else {
		upKey, downKey, confirmKey = deriveKeys(
			shared, psk, sid, clientPub,
		)
	}

	h := hmac.New(sha256.New, confirmKey)

	if clientID != "" {
		h.Write([]byte("MBOX2-ACCEPT\x00"))
		h.Write([]byte(sid))
		h.Write([]byte{0})
		h.Write([]byte(clientID))
		h.Write([]byte{0})
		h.Write(clientPub)
	} else {
		h.Write([]byte("MBOX1-ACCEPT\x00"))
		h.Write([]byte(sid))
		h.Write(clientPub)
	}

	expected := h.Sum(nil)[:16]

	got, err := hex.DecodeString(confirmHex)
	if err != nil || len(got) != 16 {
		return nil, errors.New("bad server confirmation")
	}
	if !hmac.Equal(got, expected) {
		return nil, errors.New("server authentication failed")
	}

	upAEAD, err := newAEAD(upKey)
	if err != nil {
		return nil, err
	}
	downAEAD, err := newAEAD(downKey)
	if err != nil {
		return nil, err
	}

	return &cryptoState{
		upAEAD:   upAEAD,
		downAEAD: downAEAD,
	}, nil
}

func nonce(direction byte, counter uint64) []byte {
	n := make([]byte, 12)
	n[3] = direction
	binary.BigEndian.PutUint64(n[4:], counter)
	return n
}

func cryptoAAD(sid string, header []byte) []byte {
	a := append([]byte(nil), header...)
	a = append(a, 0)
	a = append(a, []byte(sid)...)
	return a
}

func (c *cryptoState) sealUp(sid string, plaintext []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.upCounter++

	header := make([]byte, 10)
	header[0] = cryptoRecordVersion
	header[1] = dirUp
	binary.BigEndian.PutUint64(header[2:], c.upCounter)

	ct := c.upAEAD.Seal(
		nil,
		nonce(dirUp, c.upCounter),
		plaintext,
		cryptoAAD(sid, header),
	)

	return append(header, ct...), nil
}

func (c *cryptoState) openDown(sid string, record []byte) (uint64, []byte, error) {
	if len(record) < 26 {
		return 0, nil, errors.New("short crypto record")
	}
	if record[0] != cryptoRecordVersion || record[1] != dirDown {
		return 0, nil, errors.New("wrong crypto record type")
	}
	counter := binary.BigEndian.Uint64(record[2:10])
	plaintext, err := c.downAEAD.Open(nil, nonce(dirDown, counter), record[10:], cryptoAAD(sid, record[:10]))
	if err != nil {
		return 0, nil, errors.New("down authentication failed")
	}
	return counter, plaintext, nil
}
