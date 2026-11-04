// Package udpwire defines the bounded, versioned datagram encoding for a future
// MBox UDP transport. Encoding alone MUST NOT enable UDP in Android: the live
// SOCKS gateway remains CONNECT-only until a fully authenticated flow path,
// quotas, destination routing and physical-interface leak tests exist.
package udpwire

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/netip"
)

const (
	Version = 1
	// Fits the minimum IPv6 path MTU (1280 bytes) with an IPv6+UDP header.
	// No IP/transport fragmentation is attempted in the first UDP milestone.
	MaxPayload   = 1232
	maxWireSize  = 1 + 4 + 1 + 16 + 2 + 2 + MaxPayload
	hevMinHeader = 10
	hevMaxHeader = 22
)

var ErrMalformed = errors.New("invalid MBox UDP datagram")

// Datagram is one datagram, never a TCP byte stream. FlowID is meaningful
// ONLY inside an already-authenticated MBox Noise session and MUST NOT be
// accepted from unauthenticated HTTP query strings or headers.
type Datagram struct {
	FlowID uint32
	Peer   netip.AddrPort
	Data   []byte
}

// ValidPeer enforces explicit IP addresses and ports. No DNS resolution is
// performed in the codec, so parsing untrusted datagrams cannot make a local
// DNS query. Authorization/routing are the responsibility of the origin.
func ValidPeer(p netip.AddrPort) bool {
	a := p.Addr()
	return a.IsValid() && a.Zone() == "" && !a.IsUnspecified() && p.Port() != 0
}

func encodeAddress(p netip.AddrPort) ([]byte, error) {
	if !ValidPeer(p) {
		return nil, ErrMalformed
	}
	addr := p.Addr().Unmap()
	var out []byte
	if addr.Is4() {
		raw := addr.As4()
		out = append([]byte{1}, raw[:]...)
	} else if addr.Is6() {
		raw := addr.As16()
		out = append([]byte{4}, raw[:]...)
	} else {
		return nil, ErrMalformed
	}
	out = binary.BigEndian.AppendUint16(out, p.Port())
	return out, nil
}

func decodeAddress(raw []byte) (netip.AddrPort, int, error) {
	if len(raw) < 1 {
		return netip.AddrPort{}, 0, ErrMalformed
	}
	var addr netip.Addr
	var n int
	switch raw[0] {
	case 1:
		n = 1 + 4 + 2
		if len(raw) < n {
			return netip.AddrPort{}, 0, ErrMalformed
		}
		var v [4]byte
		copy(v[:], raw[1:5])
		addr = netip.AddrFrom4(v)
	case 4:
		n = 1 + 16 + 2
		if len(raw) < n {
			return netip.AddrPort{}, 0, ErrMalformed
		}
		var v [16]byte
		copy(v[:], raw[1:17])
		addr = netip.AddrFrom16(v)
		if addr.Is4In6() {
			return netip.AddrPort{}, 0, ErrMalformed
		}
	default:
		return netip.AddrPort{}, 0, ErrMalformed // domain and unknown types rejected
	}
	port := binary.BigEndian.Uint16(raw[n-2 : n])
	peer := netip.AddrPortFrom(addr, port)
	if !ValidPeer(peer) {
		return netip.AddrPort{}, 0, ErrMalformed
	}
	return peer, n, nil
}

// Marshal serializes a datagram to be carried INSIDE a Noise-protected MBox
// frame, not sent directly over the device's physical network.
// v1: version(1), flowID(4), SOCKS address type+IP+port, dataLen(2), data.
func Marshal(d Datagram) ([]byte, error) {
	if d.FlowID == 0 || len(d.Data) > MaxPayload {
		return nil, ErrMalformed
	}
	addr, err := encodeAddress(d.Peer)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 1+4+len(addr)+2+len(d.Data))
	out = append(out, Version)
	out = binary.BigEndian.AppendUint32(out, d.FlowID)
	out = append(out, addr...)
	out = binary.BigEndian.AppendUint16(out, uint16(len(d.Data)))
	out = append(out, d.Data...)
	return out, nil
}

func Unmarshal(raw []byte) (Datagram, error) {
	if len(raw) < 1+4+7+2 || len(raw) > maxWireSize || raw[0] != Version {
		return Datagram{}, ErrMalformed
	}
	id := binary.BigEndian.Uint32(raw[1:5])
	if id == 0 {
		return Datagram{}, ErrMalformed
	}
	peer, used, err := decodeAddress(raw[5:])
	if err != nil {
		return Datagram{}, err
	}
	tail := raw[5+used:]
	if len(tail) < 2 {
		return Datagram{}, ErrMalformed
	}
	n := int(binary.BigEndian.Uint16(tail[:2]))
	if n > MaxPayload || n != len(tail)-2 {
		return Datagram{}, ErrMalformed
	}
	return Datagram{FlowID: id, Peer: peer, Data: append([]byte(nil), tail[2:]...)}, nil
}

// MarshalHEV encodes the upstream HEV SOCKS5 UDP-in-TCP extension's datagram
// envelope: DATLEN(2), HDRLEN(1), SOCKS ATYP+ADDR+PORT, DATA. FWD UDP uses
// SOCKS command 0x05; unlike standard UDP ASSOCIATE (0x03), it does not
// require a physical UDP relay socket on Android.
func MarshalHEV(peer netip.AddrPort, payload []byte) ([]byte, error) {
	if len(payload) > MaxPayload {
		return nil, ErrMalformed
	}
	addr, err := encodeAddress(peer)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 3+len(addr)+len(payload))
	out = binary.BigEndian.AppendUint16(out, uint16(len(payload)))
	out = append(out, byte(3+len(addr)))
	out = append(out, addr...)
	out = append(out, payload...)
	return out, nil
}

func UnmarshalHEV(raw []byte) (netip.AddrPort, []byte, error) {
	if len(raw) < hevMinHeader || len(raw) > hevMaxHeader+MaxPayload {
		return netip.AddrPort{}, nil, ErrMalformed
	}
	dataLen, headerLen := int(binary.BigEndian.Uint16(raw[:2])), int(raw[2])
	if dataLen > MaxPayload || (headerLen != hevMinHeader && headerLen != hevMaxHeader) || headerLen+dataLen != len(raw) {
		return netip.AddrPort{}, nil, ErrMalformed
	}
	p, used, err := decodeAddress(raw[3:headerLen])
	if err != nil || used != headerLen-3 {
		return netip.AddrPort{}, nil, ErrMalformed
	}
	return p, append([]byte(nil), raw[headerLen:]...), nil
}

// ReadHEV reads exactly one bounded HEV datagram from a TCP stream. Truncated
// or malformed packets close the association; there is no resynchronization
// on attacker-controlled bytes. The caller MUST apply read deadlines.
func ReadHEV(r io.Reader) (netip.AddrPort, []byte, error) {
	var head [3]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return netip.AddrPort{}, nil, err
	}
	headerLen := int(head[2])
	dataLen := int(binary.BigEndian.Uint16(head[:2]))
	if dataLen > MaxPayload || (headerLen != hevMinHeader && headerLen != hevMaxHeader) {
		return netip.AddrPort{}, nil, ErrMalformed
	}
	body := make([]byte, headerLen-3+dataLen)
	if _, err := io.ReadFull(r, body); err != nil {
		return netip.AddrPort{}, nil, fmt.Errorf("truncated HEV UDP datagram: %w", err)
	}
	p, used, err := decodeAddress(body)
	if err != nil || used != headerLen-3 {
		return netip.AddrPort{}, nil, ErrMalformed
	}
	return p, body[used:], nil
}
