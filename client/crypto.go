package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	noise "../common/noise"
)

const (
	// MBox beta.2 is Noise-only. There is deliberately no legacy crypto parser.
	cryptoRecordVersion = 2
	dirUp               = 1
	dirDown             = 2
	noiseRecordHeader   = 20
	maxNoiseChunks      = 64
)

var (
	pskFile       string
	serverPubFile string
)

type cryptoState struct {
	up   *noise.CipherState
	down *noise.CipherState

	mu sync.Mutex

	upCounter uint64
	upNonce   uint64
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

func validDeviceID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

var devicePayloadPrefix = []byte{'M', 'B', 'D', '1'}

func noiseDevicePayload(id string) ([]byte, error) {
	if id == "" {
		return nil, nil
	}
	if !validDeviceID(id) {
		return nil, errors.New("invalid device_id")
	}
	raw, err := hex.DecodeString(id)
	if err != nil {
		return nil, err
	}
	payload := append([]byte(nil), devicePayloadPrefix...)
	payload = append(payload, raw...)
	return payload, nil
}

func noisePrologue(id string) []byte {
	p := []byte("MBOX3-NOISE\x00")
	p = append(p, []byte(id)...)
	return p
}

func buildOpenRequest() ([]byte, *noise.Handshake, error) {
	return buildOpenRequestForDevice(deviceID)
}

func buildLegacyOpenRequest() ([]byte, *noise.Handshake, error) {
	return buildOpenRequestForDevice("")
}

func buildOpenRequestForDevice(openDeviceID string) ([]byte, *noise.Handshake, error) {
	if !validClientID(clientID) {
		return nil, nil, fmt.Errorf("Noise-only MBox requires valid client_id")
	}
	psk, err := readHex32(pskFile)
	if err != nil {
		return nil, nil, err
	}
	serverPub, err := readHex32(serverPubFile)
	if err != nil {
		return nil, nil, err
	}
	hs, err := noise.NewInitiator(psk, serverPub, noisePrologue(clientID), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	payload, err := noiseDevicePayload(openDeviceID)
	if err != nil {
		return nil, nil, err
	}
	msg1, _, _, err := hs.WriteMessage(payload)
	if err != nil {
		return nil, nil, err
	}
	body := make([]byte, 1+len(clientID)+len(msg1))
	body[0] = byte(len(clientID))
	copy(body[1:], clientID)
	copy(body[1+len(clientID):], msg1)
	return body, hs, nil
}

func finishClientHandshake(hs *noise.Handshake, response []byte) (*cryptoState, string, error) {
	payload, send, recv, err := hs.ReadMessage(response)
	if err != nil {
		return nil, "", fmt.Errorf("Noise NKpsk0 response: %w", err)
	}
	if send == nil || recv == nil {
		return nil, "", errors.New("Noise handshake did not split")
	}
	sid := string(payload)
	if len(sid) != 32 {
		return nil, "", fmt.Errorf("invalid authenticated session id length %d", len(sid))
	}
	if _, err := hex.DecodeString(sid); err != nil {
		return nil, "", errors.New("invalid authenticated session id")
	}
	return &cryptoState{up: send, down: recv}, sid, nil
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
			n := (minChunk + maxChunk) / 2
			if !deterministicTransport {
				var err error
				n, err = randomIntInclusive(minChunk, maxChunk)
				if err != nil {
					return nil, err
				}
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

func (c *cryptoState) sealUp(sid string, plaintext []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.upCounter == ^uint64(0) {
		return nil, errors.New("Noise record counter exhausted")
	}
	c.upCounter++
	rec, next, err := sealNoiseRecord(c.up, sid, dirUp, c.upCounter, c.upNonce, plaintext)
	if err != nil {
		c.upCounter--
		return nil, err
	}
	c.upNonce = next
	return rec, nil
}

func (c *cryptoState) openDown(sid string, record []byte) (uint64, []byte, error) {
	return openNoiseRecord(c.down, sid, dirDown, record)
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

func pskFingerprint(psk []byte) [32]byte { return sha256.Sum256(psk) }
