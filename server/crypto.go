package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
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
	// cryptoRecordVersion is the AEAD record format, not the OPEN handshake version.
	cryptoRecordVersion = 1
	dirUp               = 1
	dirDown             = 2
)

var (
	pskFile        string
	serverPrivFile string
)

type cryptoState struct {
	upAEAD   cipher.AEAD
	downAEAD cipher.AEAD

	mu sync.Mutex

	lastUpCounter uint64
	downCounter   uint64
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

func serverHandshake(openBody []byte, sid string) (*cryptoState, string, error) {
	var (
		clientPubRaw []byte
		clientMAC    []byte
		clientID     string
		authV2       bool
		pskPath      string
	)

	if len(openBody) == 64 {
		// Legacy MBox/1 OPEN. Production can disable it by omitting crypto.psk_file.
		if pskFile == "" {
			return nil, "", errors.New("open authentication failed")
		}
		clientPubRaw = openBody[:32]
		clientMAC = openBody[32:]
		pskPath = pskFile
	} else {
		// MBox authenticated-client OPEN v2:
		// [1-byte id length][id][32-byte X25519 pub][32-byte HMAC]
		if len(openBody) < 66 {
			return nil, "", fmt.Errorf("bad v2 open size %d", len(openBody))
		}

		idLen := int(openBody[0])
		if idLen < 1 || idLen > 63 || len(openBody) != 1+idLen+32+32 {
			return nil, "", errors.New("bad v2 open framing")
		}

		clientID = string(openBody[1 : 1+idLen])
		if !validClientID(clientID) {
			return nil, "", errors.New("invalid client id")
		}

		clientPubRaw = openBody[1+idLen : 1+idLen+32]
		clientMAC = openBody[1+idLen+32:]
		authV2 = true
	}

	var psk []byte
	var err error
	if authV2 {
		clientAuthMu.RLock()
		stored, ok := clientPSKs[clientID]
		if ok {
			psk = append([]byte(nil), stored...)
		}
		clientAuthMu.RUnlock()
		if len(psk) != 32 {
			return nil, "", errors.New("open authentication failed")
		}
	} else {
		psk, err = readHex32(pskPath)
		if err != nil {
			return nil, "", err
		}
	}

	m := hmac.New(sha256.New, psk)

	if authV2 {
		m.Write([]byte("MBOX2-OPEN\x00"))
		m.Write([]byte(clientID))
		m.Write([]byte{0})
		m.Write(clientPubRaw)
	} else {
		m.Write([]byte("MBOX1-OPEN\x00"))
		m.Write(clientPubRaw)
	}

	if !hmac.Equal(clientMAC, m.Sum(nil)) {
		return nil, "", errors.New("open authentication failed")
	}

	privRaw, err := readHex32(serverPrivFile)
	if err != nil {
		return nil, "", err
	}

	curve := ecdh.X25519()

	serverPriv, err := curve.NewPrivateKey(privRaw)
	if err != nil {
		return nil, "", err
	}

	clientPub, err := curve.NewPublicKey(clientPubRaw)
	if err != nil {
		return nil, "", err
	}

	shared, err := serverPriv.ECDH(clientPub)
	if err != nil {
		return nil, "", err
	}

	var upKey, downKey, confirmKey []byte

	if authV2 {
		upKey, downKey, confirmKey = deriveKeysV2(
			shared, psk, sid, clientID, clientPubRaw,
		)
	} else {
		upKey, downKey, confirmKey = deriveKeys(
			shared, psk, sid, clientPubRaw,
		)
	}

	upAEAD, err := newAEAD(upKey)
	if err != nil {
		return nil, "", err
	}
	downAEAD, err := newAEAD(downKey)
	if err != nil {
		return nil, "", err
	}

	c := &cryptoState{
		upAEAD:   upAEAD,
		downAEAD: downAEAD,
	}

	h := hmac.New(sha256.New, confirmKey)

	if authV2 {
		h.Write([]byte("MBOX2-ACCEPT\x00"))
		h.Write([]byte(sid))
		h.Write([]byte{0})
		h.Write([]byte(clientID))
		h.Write([]byte{0})
		h.Write(clientPubRaw)
	} else {
		h.Write([]byte("MBOX1-ACCEPT\x00"))
		h.Write([]byte(sid))
		h.Write(clientPubRaw)
	}

	return c, hex.EncodeToString(h.Sum(nil)[:16]), nil
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

func (c *cryptoState) openUp(sid string, record []byte) ([]byte, error) {
	if len(record) < 26 {
		return nil, errors.New("short crypto record")
	}
	if record[0] != cryptoRecordVersion || record[1] != dirUp {
		return nil, errors.New("wrong crypto record type")
	}

	counter := binary.BigEndian.Uint64(record[2:10])

	c.mu.Lock()
	defer c.mu.Unlock()

	if counter <= c.lastUpCounter {
		return nil, errors.New("replayed/out-of-order upload record")
	}

	plaintext, err := c.upAEAD.Open(
		nil,
		nonce(dirUp, counter),
		record[10:],
		cryptoAAD(sid, record[:10]),
	)
	if err != nil {
		return nil, errors.New("upload authentication failed")
	}

	c.lastUpCounter = counter

	return plaintext, nil
}

func (c *cryptoState) sealDown(sid string, plaintext []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.downCounter++

	header := make([]byte, 10)
	header[0] = cryptoRecordVersion
	header[1] = dirDown
	binary.BigEndian.PutUint64(header[2:], c.downCounter)

	ct := c.downAEAD.Seal(
		nil,
		nonce(dirDown, c.downCounter),
		plaintext,
		cryptoAAD(sid, header),
	)

	return append(header, ct...), nil
}
