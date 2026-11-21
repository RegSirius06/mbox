// Package dgram provides the experimental, separately keyed, unordered UDP lane.
// The gateway sees opaque authenticated records, never Noise keys or user data.
package dgram

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
)

const (
	Protocol   = "mbox-datagram-v1"
	MaxPacket  = 1000 // conservative cap, including nonce counter and GCM tag
	MaxPlain   = MaxPacket - 8 - 16
	AttachSize = 64 // ASCII session ID (32) and HMAC (32)
	ProofSize  = 32
	MaxEpoch   = uint32(4096)      // bound authenticated reconnects per Noise session
	maxCounter = uint64(1<<32 - 1) // conservative per-key GCM message cap
)

var ErrRecord = errors.New("invalid or replayed datagram record")

func derive(seed [32]byte, sid, purpose string) [32]byte {
	h := hmac.New(sha256.New, seed[:])
	h.Write([]byte("MBOX-DATAGRAM-V1\x00" + purpose + "\x00" + sid))
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func ValidSID(sid string) bool {
	if len(sid) != 32 || strings.ToLower(sid) != sid {
		return false
	}
	_, err := hex.DecodeString(sid)
	return err == nil
}

func Proof(seed [32]byte, sid, purpose string) [ProofSize]byte {
	return derive(seed, sid, "proof/"+purpose)
}

func Attach(seed [32]byte, sid string) []byte {
	p := Proof(seed, sid, "attach")
	return append([]byte(sid), p[:]...)
}

func VerifyProof(seed [32]byte, sid, purpose string, got []byte) bool {
	p := Proof(seed, sid, purpose)
	return ValidSID(sid) && hmac.Equal(p[:], got)
}

// Reattaching a DATAGRAM lane MUST NOT reuse the AES-GCM key/counter pair.
// Epoch 0 is the legacy, one-shot format. Subsequent epochs use independent
// HMAC domains and traffic keys. Wire length remains 64 bytes for the gateway.
func epochPurpose(kind string, epoch uint32) string {
	return kind + "/epoch/" + strconv.FormatUint(uint64(epoch), 10)
}

func AttachEpoch(seed [32]byte, sid string, epoch uint32) []byte {
	if !ValidSID(sid) || epoch == 0 || epoch > MaxEpoch {
		return nil
	}
	proof := derive(seed, sid, epochPurpose("proof/attach", epoch))
	out := make([]byte, AttachSize)
	copy(out, sid)
	copy(out[32:36], "MBD2")
	binary.BigEndian.PutUint32(out[36:40], epoch)
	copy(out[40:], proof[:24])
	return out
}

func ParseAttachEpoch(seed [32]byte, sid string, got []byte) (uint32, bool) {
	if !ValidSID(sid) || len(got) != ProofSize || string(got[:4]) != "MBD2" {
		return 0, false
	}
	epoch := binary.BigEndian.Uint32(got[4:8])
	if epoch == 0 || epoch > MaxEpoch {
		return 0, false
	}
	proof := derive(seed, sid, epochPurpose("proof/attach", epoch))
	return epoch, hmac.Equal(got[8:], proof[:24])
}

func AcceptEpoch(seed [32]byte, sid string, epoch uint32) [ProofSize]byte {
	return derive(seed, sid, epochPurpose("proof/accept", epoch))
}

func VerifyAcceptEpoch(seed [32]byte, sid string, epoch uint32, got []byte) bool {
	if !ValidSID(sid) || epoch == 0 || epoch > MaxEpoch || len(got) != ProofSize {
		return false
	}
	proof := AcceptEpoch(seed, sid, epoch)
	return hmac.Equal(proof[:], got)
}

type Record struct {
	aead             cipher.AEAD
	aad              []byte
	mu               sync.Mutex
	next, high, seen uint64
}

// Pass only an exporter of the corresponding directional Noise split secret.
// Each session creates precisely one sender and receiver; counters never reset.
func NewRecord(seed [32]byte, sid, dir string) (*Record, error) {
	return NewRecordEpoch(seed, sid, dir, 0)
}

func NewRecordEpoch(seed [32]byte, sid, dir string, epoch uint32) (*Record, error) {
	if !ValidSID(sid) || (dir != "up" && dir != "down") || epoch > MaxEpoch {
		return nil, ErrRecord
	}
	purpose := "aead/" + dir
	aad := "MBOX-DATAGRAM-V1\x00" + dir + "\x00" + sid
	if epoch != 0 {
		purpose = epochPurpose(purpose, epoch)
		aad = "MBOX-DATAGRAM-V2\x00" + dir + "\x00" + sid + "\x00" + strconv.FormatUint(uint64(epoch), 10)
	}
	key := derive(seed, sid, purpose)
	b, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	a, err := cipher.NewGCM(b)
	if err != nil {
		return nil, err
	}
	return &Record{aead: a, aad: []byte(aad)}, nil
}

func (r *Record) Seal(plain []byte) ([]byte, error) {
	if len(plain) == 0 || len(plain) > MaxPlain {
		return nil, ErrRecord
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.next >= maxCounter {
		return nil, ErrRecord
	}
	r.next++
	var nonce [12]byte
	binary.BigEndian.PutUint64(nonce[4:], r.next)
	out := make([]byte, 8, 8+len(plain)+r.aead.Overhead())
	copy(out, nonce[4:])
	return r.aead.Seal(out, nonce[:], plain, r.aad), nil
}

func (r *Record) Open(packet []byte) ([]byte, error) {
	if len(packet) < 25 || len(packet) > MaxPacket {
		return nil, ErrRecord
	}
	n := binary.BigEndian.Uint64(packet[:8])
	if n == 0 || n > maxCounter {
		return nil, ErrRecord
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if n <= r.high && (r.high-n >= 64 || r.seen&(uint64(1)<<(r.high-n)) != 0) {
		return nil, ErrRecord
	}
	var nonce [12]byte
	copy(nonce[4:], packet[:8])
	plain, err := r.aead.Open(nil, nonce[:], packet[8:], r.aad)
	if err != nil {
		return nil, ErrRecord
	} // unauthenticated input cannot advance window
	if n > r.high {
		r.seen = (r.seen << (n - r.high)) | 1
		r.high = n
	} else {
		r.seen |= uint64(1) << (r.high - n)
	}
	return plain, nil
}

// The local Unix stream only frames encrypted records; no fragmentation is used.
func ReadPacket(r io.Reader) ([]byte, error) {
	var head [2]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint16(head[:]))
	if n < 25 || n > MaxPacket {
		return nil, ErrRecord
	}
	p := make([]byte, n)
	_, err := io.ReadFull(r, p)
	return p, err
}

func WriteFull(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if err != nil {
			return err
		}
		if n <= 0 || n > len(p) {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}

func WritePacket(w io.Writer, p []byte) error {
	if len(p) < 25 || len(p) > MaxPacket {
		return ErrRecord
	}
	var b [2 + MaxPacket]byte
	binary.BigEndian.PutUint16(b[:2], uint16(len(p)))
	copy(b[2:], p)
	return WriteFull(w, b[:2+len(p)])
}
