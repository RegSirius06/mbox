// Package noise implements the exact Noise_NKpsk0_25519_AESGCM_SHA256
// handshake used by MBox/3 and its transport CipherStates.
//
// The state-machine and HKDF construction are derived from the BSD-licensed
// github.com/flynn/noise implementation, reduced to the single protocol suite
// MBox uses and adapted to Go's standard-library crypto/ecdh.
package noise

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	ProtocolName = "Noise_NKpsk0_25519_AESGCM_SHA256"
	MaxMsgLen    = 65535
)

var (
	ErrShortMessage = errors.New("noise: message is too short")
	ErrWrongTurn    = errors.New("noise: handshake call in wrong direction")
)

type Keypair struct {
	Private []byte
	Public  []byte
}

func KeypairFromPrivate(priv []byte) (Keypair, error) {
	if len(priv) != 32 {
		return Keypair{}, fmt.Errorf("noise: X25519 private key must be 32 bytes")
	}
	k, err := ecdh.X25519().NewPrivateKey(priv)
	if err != nil {
		return Keypair{}, err
	}
	return Keypair{Private: append([]byte(nil), priv...), Public: append([]byte(nil), k.PublicKey().Bytes()...)}, nil
}

func GenerateKeypair(r io.Reader) (Keypair, error) {
	if r == nil {
		r = rand.Reader
	}
	raw := make([]byte, 32)
	if _, err := io.ReadFull(r, raw); err != nil {
		return Keypair{}, err
	}
	return KeypairFromPrivate(raw)
}

func dh(priv, pub []byte) ([]byte, error) {
	if len(priv) != 32 || len(pub) != 32 {
		return nil, errors.New("noise: invalid X25519 key length")
	}
	sk, err := ecdh.X25519().NewPrivateKey(priv)
	if err != nil {
		return nil, err
	}
	pk, err := ecdh.X25519().NewPublicKey(pub)
	if err != nil {
		return nil, err
	}
	return sk.ECDH(pk)
}

func hkdf(outputs int, ck, ikm []byte) (o1, o2, o3 []byte) {
	tempMAC := hmac.New(sha256.New, ck)
	_, _ = tempMAC.Write(ikm)
	tempKey := tempMAC.Sum(nil)

	m1 := hmac.New(sha256.New, tempKey)
	_, _ = m1.Write([]byte{1})
	o1 = m1.Sum(nil)
	if outputs == 1 {
		return
	}
	m2 := hmac.New(sha256.New, tempKey)
	_, _ = m2.Write(o1)
	_, _ = m2.Write([]byte{2})
	o2 = m2.Sum(nil)
	if outputs == 2 {
		return
	}
	m3 := hmac.New(sha256.New, tempKey)
	_, _ = m3.Write(o2)
	_, _ = m3.Write([]byte{3})
	o3 = m3.Sum(nil)
	return
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("noise: AESGCM key must be 32 bytes")
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

func nonce(n uint64) []byte {
	var out [12]byte
	binary.BigEndian.PutUint64(out[4:], n)
	return out[:]
}

type symmetricState struct {
	ck   []byte
	h    []byte
	key  [32]byte
	aead cipher.AEAD
	n    uint64
	hasK bool
}

func newSymmetric(prologue, responderStatic []byte) *symmetricState {
	p := []byte(ProtocolName)
	var h []byte
	if len(p) <= sha256.Size {
		h = make([]byte, sha256.Size)
		copy(h, p)
	} else {
		sum := sha256.Sum256(p)
		h = append([]byte(nil), sum[:]...)
	}
	s := &symmetricState{h: h, ck: append([]byte(nil), h...)}
	s.mixHash(prologue)
	s.mixHash(responderStatic) // NK responder static pre-message.
	return s
}

func (s *symmetricState) mixHash(data []byte) {
	h := sha256.New()
	_, _ = h.Write(s.h)
	_, _ = h.Write(data)
	s.h = h.Sum(nil)
}

func (s *symmetricState) mixKey(input []byte) error {
	ck, k, _ := hkdf(2, s.ck, input)
	s.ck = ck
	copy(s.key[:], k)
	a, err := newGCM(s.key[:])
	if err != nil {
		return err
	}
	s.aead = a
	s.n = 0
	s.hasK = true
	return nil
}

func (s *symmetricState) mixKeyAndHash(input []byte) error {
	ck, tempH, k := hkdf(3, s.ck, input)
	s.ck = ck
	s.mixHash(tempH)
	copy(s.key[:], k)
	a, err := newGCM(s.key[:])
	if err != nil {
		return err
	}
	s.aead = a
	s.n = 0
	s.hasK = true
	return nil
}

func (s *symmetricState) encryptAndHash(out, plain []byte) ([]byte, error) {
	if !s.hasK {
		s.mixHash(plain)
		return append(out, plain...), nil
	}
	if s.n == ^uint64(0) {
		return nil, errors.New("noise: handshake nonce exhausted")
	}
	start := len(out)
	out = s.aead.Seal(out, nonce(s.n), plain, s.h)
	s.n++
	s.mixHash(out[start:])
	return out, nil
}

func (s *symmetricState) decryptAndHash(out, ct []byte) ([]byte, error) {
	if !s.hasK {
		s.mixHash(ct)
		return append(out, ct...), nil
	}
	if s.n == ^uint64(0) {
		return nil, errors.New("noise: handshake nonce exhausted")
	}
	plain, err := s.aead.Open(out, nonce(s.n), ct, s.h)
	if err != nil {
		return nil, err
	}
	s.n++
	s.mixHash(ct)
	return plain, nil
}

func (s *symmetricState) split() (*CipherState, *CipherState, error) {
	k1, k2, _ := hkdf(2, s.ck, nil)
	c1, err := NewCipherState(k1)
	if err != nil {
		return nil, nil, err
	}
	c2, err := NewCipherState(k2)
	if err != nil {
		return nil, nil, err
	}
	return c1, c2, nil
}

type CipherState struct {
	key  [32]byte
	aead cipher.AEAD
}

func NewCipherState(key []byte) (*CipherState, error) {
	if len(key) != 32 {
		return nil, errors.New("noise: transport key must be 32 bytes")
	}
	a, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	c := &CipherState{aead: a}
	copy(c.key[:], key)
	return c, nil
}

func (c *CipherState) Overhead() int { return c.aead.Overhead() }
func (c *CipherState) Seal(n uint64, ad, plain []byte) ([]byte, error) {
	if len(plain)+c.aead.Overhead() > MaxMsgLen {
		return nil, fmt.Errorf("noise: transport message too large: %d", len(plain)+c.aead.Overhead())
	}
	return c.aead.Seal(nil, nonce(n), plain, ad), nil
}
func (c *CipherState) Open(n uint64, ad, ct []byte) ([]byte, error) {
	if len(ct) > MaxMsgLen || len(ct) < c.aead.Overhead() {
		return nil, errors.New("noise: invalid transport ciphertext length")
	}
	return c.aead.Open(nil, nonce(n), ct, ad)
}

type Handshake struct {
	initiator bool
	ss        *symmetricState
	s         Keypair
	e         Keypair
	rs        []byte
	re        []byte
	psk       []byte
	rng       io.Reader
	step      int
}

func NewInitiator(psk, responderStatic, prologue []byte, rng io.Reader) (*Handshake, error) {
	if len(psk) != 32 || len(responderStatic) != 32 {
		return nil, errors.New("noise: NKpsk0 requires 32-byte PSK and responder static public key")
	}
	if rng == nil {
		rng = rand.Reader
	}
	return &Handshake{initiator: true, ss: newSymmetric(prologue, responderStatic), rs: append([]byte(nil), responderStatic...), psk: append([]byte(nil), psk...), rng: rng}, nil
}

func NewResponder(psk, responderStaticPriv, prologue []byte, rng io.Reader) (*Handshake, error) {
	if len(psk) != 32 || len(responderStaticPriv) != 32 {
		return nil, errors.New("noise: NKpsk0 requires 32-byte PSK and responder static private key")
	}
	kp, err := KeypairFromPrivate(responderStaticPriv)
	if err != nil {
		return nil, err
	}
	if rng == nil {
		rng = rand.Reader
	}
	return &Handshake{initiator: false, ss: newSymmetric(prologue, kp.Public), s: kp, psk: append([]byte(nil), psk...), rng: rng}, nil
}

func (h *Handshake) WriteMessage(payload []byte) ([]byte, *CipherState, *CipherState, error) {
	if len(payload) > MaxMsgLen {
		return nil, nil, nil, errors.New("noise: handshake payload too large")
	}
	if h.initiator {
		if h.step != 0 {
			return nil, nil, nil, ErrWrongTurn
		}
		if err := h.ss.mixKeyAndHash(h.psk); err != nil { // psk0
			return nil, nil, nil, err
		}
		e, err := GenerateKeypair(h.rng)
		if err != nil {
			return nil, nil, nil, err
		}
		h.e = e
		out := append([]byte(nil), e.Public...)
		h.ss.mixHash(e.Public)
		if err := h.ss.mixKey(e.Public); err != nil { // PSK modifier mixes every e.
			return nil, nil, nil, err
		}
		es, err := dh(e.Private, h.rs)
		if err != nil {
			return nil, nil, nil, err
		}
		if err := h.ss.mixKey(es); err != nil {
			return nil, nil, nil, err
		}
		out, err = h.ss.encryptAndHash(out, payload)
		if err != nil {
			return nil, nil, nil, err
		}
		h.step = 1
		return out, nil, nil, nil
	}

	if h.step != 1 || len(h.re) != 32 {
		return nil, nil, nil, ErrWrongTurn
	}
	e, err := GenerateKeypair(h.rng)
	if err != nil {
		return nil, nil, nil, err
	}
	h.e = e
	out := append([]byte(nil), e.Public...)
	h.ss.mixHash(e.Public)
	if err := h.ss.mixKey(e.Public); err != nil {
		return nil, nil, nil, err
	}
	ee, err := dh(e.Private, h.re)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := h.ss.mixKey(ee); err != nil {
		return nil, nil, nil, err
	}
	out, err = h.ss.encryptAndHash(out, payload)
	if err != nil {
		return nil, nil, nil, err
	}
	c1, c2, err := h.ss.split()
	if err != nil {
		return nil, nil, nil, err
	}
	h.step = 2
	// Canonical NK direction: c1 initiator->responder, c2 responder->initiator.
	return out, c2, c1, nil // responder send, responder receive
}

func (h *Handshake) ReadMessage(message []byte) ([]byte, *CipherState, *CipherState, error) {
	if h.initiator {
		if h.step != 1 {
			return nil, nil, nil, ErrWrongTurn
		}
		if len(message) < 32+16 {
			return nil, nil, nil, ErrShortMessage
		}
		h.re = append([]byte(nil), message[:32]...)
		h.ss.mixHash(h.re)
		if err := h.ss.mixKey(h.re); err != nil {
			return nil, nil, nil, err
		}
		ee, err := dh(h.e.Private, h.re)
		if err != nil {
			return nil, nil, nil, err
		}
		if err := h.ss.mixKey(ee); err != nil {
			return nil, nil, nil, err
		}
		payload, err := h.ss.decryptAndHash(nil, message[32:])
		if err != nil {
			return nil, nil, nil, err
		}
		c1, c2, err := h.ss.split()
		if err != nil {
			return nil, nil, nil, err
		}
		h.step = 2
		return payload, c1, c2, nil // initiator send, initiator receive
	}

	if h.step != 0 {
		return nil, nil, nil, ErrWrongTurn
	}
	if len(message) < 32+16 {
		return nil, nil, nil, ErrShortMessage
	}
	if err := h.ss.mixKeyAndHash(h.psk); err != nil { // psk0
		return nil, nil, nil, err
	}
	h.re = append([]byte(nil), message[:32]...)
	h.ss.mixHash(h.re)
	if err := h.ss.mixKey(h.re); err != nil {
		return nil, nil, nil, err
	}
	es, err := dh(h.s.Private, h.re)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := h.ss.mixKey(es); err != nil {
		return nil, nil, nil, err
	}
	payload, err := h.ss.decryptAndHash(nil, message[32:])
	if err != nil {
		return nil, nil, nil, err
	}
	h.step = 1
	return payload, nil, nil, nil
}

func (h *Handshake) ChannelBinding() []byte { return append([]byte(nil), h.ss.h...) }
