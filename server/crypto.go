package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	controlmeta "../common/controlmeta"
	noise "../common/noise"
)

const (
	cryptoRecordVersion = 2
	dirUp               = 1
	dirDown             = 2
	noiseRecordHeader   = 20
	maxNoiseChunks      = 64
)

var (
	serverPrivFile      string
	serverStaticPrivate []byte
)

type cryptoState struct {
	controlKey [32]byte
	up         *noise.CipherState // receive: client -> server
	down       *noise.CipherState // send: server -> client

	clientID    string
	accountID   string
	deviceID    string
	authPSKHash [32]byte

	mu sync.Mutex

	lastUpCounter uint64
	downCounter   uint64
	downNonce     uint64
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

func validClientID(id string) bool {
	if len(id) < 1 || len(id) > 63 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

var devicePayloadPrefix = []byte{'M', 'B', 'D', '1'}

func parseNoiseOpenPayload(payload []byte) (string, error) {
	if len(payload) == 0 {
		return "", nil
	}
	if len(payload) != len(devicePayloadPrefix)+16 || !bytes.Equal(payload[:len(devicePayloadPrefix)], devicePayloadPrefix) {
		return "", errors.New("invalid Noise OPEN payload")
	}
	return hex.EncodeToString(payload[len(devicePayloadPrefix):]), nil
}

func noisePrologue(id string) []byte {
	p := []byte("MBOX3-NOISE\x00")
	p = append(p, []byte(id)...)
	return p
}

func serverHandshake(openBody []byte, sid string) (*cryptoState, []byte, error) {
	// Noise-only OPEN: [idLen:1][clientID][Noise NKpsk0 message1].
	if len(openBody) < 1+1+32+16 {
		return nil, nil, errors.New("open authentication failed")
	}
	idLen := int(openBody[0])
	if idLen < 1 || idLen > 63 || len(openBody) <= 1+idLen {
		return nil, nil, errors.New("open authentication failed")
	}
	clientID := string(openBody[1 : 1+idLen])
	if !validClientID(clientID) {
		return nil, nil, errors.New("open authentication failed")
	}
	policy, ok := getClientPolicy(clientID)
	if !ok || !clientPolicyActive(policy) || len(policy.PSK) != 32 {
		return nil, nil, errors.New("open authentication failed")
	}

	if len(serverStaticPrivate) != 32 {
		return nil, nil, errors.New("server static key unavailable")
	}
	hs, err := noise.NewResponder(policy.PSK, serverStaticPrivate, noisePrologue(clientID), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	payload, _, _, err := hs.ReadMessage(openBody[1+idLen:])
	if err != nil {
		return nil, nil, errors.New("open authentication failed")
	}
	deviceID, err := parseNoiseOpenPayload(payload)
	if err != nil {
		return nil, nil, errors.New("open authentication failed")
	}
	msg2, send, recv, err := hs.WriteMessage([]byte(sid))
	if err != nil {
		return nil, nil, err
	}
	if send == nil || recv == nil {
		return nil, nil, errors.New("Noise handshake did not split")
	}
	key, err := controlmeta.Derive(policy.PSK, hs.ChannelBinding(), sid)
	if err != nil {
		return nil, nil, err
	}
	return &cryptoState{controlKey: key, up: recv, down: send, clientID: clientID, accountID: policy.AccountID, deviceID: deviceID, authPSKHash: sha256.Sum256(policy.PSK)}, msg2, nil
}

func cryptoAAD(sid string, header []byte, chunkIndex uint16, chunkLen uint16) []byte {
	a := append([]byte(nil), header...)
	var meta [4]byte
	binary.BigEndian.PutUint16(meta[0:2], chunkIndex)
	binary.BigEndian.PutUint16(meta[2:4], chunkLen)
	a = append(a, meta[:]...)
	a = append(a, 0)
	a = append(a, []byte(sid)...)
	return a
}

func randomIntInclusive(min, max int) (int, error) {
	if max <= min {
		return min, nil
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	return min + int(binary.BigEndian.Uint64(b[:])%uint64(max-min+1)), nil
}

func splitNoisePlaintext(plain []byte) ([][]byte, error) {
	minChunk := camouflage.NoiseChunkMinBytes
	maxChunk := camouflage.NoiseChunkMaxBytes
	if minChunk < 1024 {
		minChunk = 1024
	}
	if maxChunk <= 0 || maxChunk > noise.MaxMsgLen-16 {
		maxChunk = noise.MaxMsgLen - 16
	}
	if minChunk > maxChunk {
		minChunk = maxChunk
	}
	var chunks [][]byte
	for off := 0; off < len(plain); {
		remain := len(plain) - off
		size := remain
		if remain > maxChunk {
			n, err := randomIntInclusive(minChunk, maxChunk)
			if err != nil {
				return nil, err
			}
			size = n
			if remain-size > 0 && remain-size < minChunk {
				size = remain - minChunk
			}
			if size <= 0 || size > maxChunk {
				size = maxChunk
			}
		}
		chunks = append(chunks, plain[off:off+size])
		off += size
		if len(chunks) > maxNoiseChunks {
			return nil, fmt.Errorf("too many Noise chunks")
		}
	}
	if len(chunks) == 0 {
		chunks = append(chunks, []byte{})
	}
	return chunks, nil
}

func sealNoiseRecord(c *noise.CipherState, sid string, direction byte, bundleCounter, firstNonce uint64, plain []byte) ([]byte, uint64, error) {
	chunks, err := splitNoisePlaintext(plain)
	if err != nil {
		return nil, 0, err
	}
	if firstNonce > ^uint64(0)-uint64(len(chunks)) {
		return nil, 0, errors.New("Noise nonce exhausted")
	}
	header := make([]byte, noiseRecordHeader)
	header[0] = cryptoRecordVersion
	header[1] = direction
	binary.BigEndian.PutUint64(header[2:10], bundleCounter)
	binary.BigEndian.PutUint64(header[10:18], firstNonce)
	binary.BigEndian.PutUint16(header[18:20], uint16(len(chunks)))
	out := append([]byte(nil), header...)
	for i, p := range chunks {
		clen := len(p) + c.Overhead()
		if clen > noise.MaxMsgLen {
			return nil, 0, errors.New("Noise chunk too large")
		}
		aad := cryptoAAD(sid, header, uint16(i), uint16(clen))
		ct, err := c.Seal(firstNonce+uint64(i), aad, p)
		if err != nil {
			return nil, 0, err
		}
		var lb [2]byte
		binary.BigEndian.PutUint16(lb[:], uint16(len(ct)))
		out = append(out, lb[:]...)
		out = append(out, ct...)
	}
	return out, firstNonce + uint64(len(chunks)), nil
}

func openNoiseRecord(c *noise.CipherState, sid string, direction byte, record []byte) (uint64, []byte, error) {
	if len(record) < noiseRecordHeader {
		return 0, nil, errors.New("short Noise record")
	}
	header := record[:noiseRecordHeader]
	if header[0] != cryptoRecordVersion || header[1] != direction {
		return 0, nil, errors.New("wrong Noise record type")
	}
	counter := binary.BigEndian.Uint64(header[2:10])
	if counter == 0 {
		return 0, nil, errors.New("zero record counter")
	}
	firstNonce := binary.BigEndian.Uint64(header[10:18])
	count := int(binary.BigEndian.Uint16(header[18:20]))
	if count < 1 || count > maxNoiseChunks {
		return 0, nil, errors.New("invalid Noise chunk count")
	}
	if firstNonce > ^uint64(0)-uint64(count) {
		return 0, nil, errors.New("Noise nonce range overflow")
	}
	off := noiseRecordHeader
	var plain []byte
	for i := 0; i < count; i++ {
		if off+2 > len(record) {
			return 0, nil, errors.New("truncated Noise chunk length")
		}
		clen := int(binary.BigEndian.Uint16(record[off : off+2]))
		off += 2
		if clen < 16 || clen > noise.MaxMsgLen || off+clen > len(record) {
			return 0, nil, errors.New("invalid Noise chunk length")
		}
		aad := cryptoAAD(sid, header, uint16(i), uint16(clen))
		p, err := c.Open(firstNonce+uint64(i), aad, record[off:off+clen])
		if err != nil {
			return 0, nil, errors.New("Noise transport authentication failed")
		}
		plain = append(plain, p...)
		off += clen
	}
	if off != len(record) {
		return 0, nil, errors.New("trailing bytes after Noise record")
	}
	return counter, plain, nil
}

func (c *cryptoState) openUp(sid string, record []byte) ([]byte, error) {
	counter, plain, err := openNoiseRecord(c.up, sid, dirUp, record)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if counter <= c.lastUpCounter {
		return nil, errors.New("replayed/out-of-order upload record")
	}
	if counter != c.lastUpCounter+1 {
		return nil, errors.New("upload record gap")
	}
	c.lastUpCounter = counter
	return plain, nil
}

func (c *cryptoState) sealDown(sid string, plaintext []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.downCounter == ^uint64(0) {
		return nil, errors.New("Noise record counter exhausted")
	}
	c.downCounter++
	rec, next, err := sealNoiseRecord(c.down, sid, dirDown, c.downCounter, c.downNonce, plaintext)
	if err != nil {
		c.downCounter--
		return nil, err
	}
	c.downNonce = next
	return rec, nil
}

func cryptoRecordCounter(record []byte, direction byte) (uint64, error) {
	if len(record) < noiseRecordHeader || record[0] != cryptoRecordVersion || record[1] != direction {
		return 0, errors.New("wrong Noise record")
	}
	n := binary.BigEndian.Uint64(record[2:10])
	if n == 0 {
		return 0, errors.New("zero record counter")
	}
	return n, nil
}

func noiseRecordSizeUpperBound(plain int) int {
	if plain < 0 {
		return 0
	}
	minChunk := camouflage.NoiseChunkMinBytes
	if minChunk < 1024 {
		minChunk = 1024
	}
	chunks := (plain + minChunk - 1) / minChunk
	if chunks < 1 {
		chunks = 1
	}
	if chunks > maxNoiseChunks {
		chunks = maxNoiseChunks
	}
	return noiseRecordHeader + plain + chunks*(2+16)
}

func redactedClientID(id string) string {
	s := sha256.Sum256([]byte(id))
	return fmt.Sprintf("client#%x", s[:6])
}

func redactedSessionID(id string) string {
	s := sha256.Sum256([]byte(id))
	return fmt.Sprintf("sid#%x", s[:4])
}
