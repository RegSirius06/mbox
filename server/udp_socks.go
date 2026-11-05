package main

// A SOCKS5 UDP ASSOCIATION is the only permitted UDP egress on the origin.
// Never dial the datagram's target directly: the SOCKS5 endpoint owns the
// routing decision (e.g. via sing-box outbound chains).

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"../common/udpwire"
)

const (
	udpUpstreamHandshakeTimeout = 5 * time.Second
	udpUpstreamWriteTimeout     = 250 * time.Millisecond
	udpUpstreamReadTimeout      = 5 * time.Second
	udpUpstreamMaxPacket        = 3 + 1 + 16 + 2 + udpwire.MaxPayload
	maxUDPTrackedPeers          = 64
	udpTrackedPeerTTL           = 2 * time.Minute
)

var errInvalidUDPUpstream = errors.New("invalid SOCKS5 UDP upstream")

type socksUDPAssociation struct {
	control   net.Conn
	udp       *net.UDPConn
	allowedMu sync.Mutex
	allowed   map[netip.AddrPort]time.Time
	closeOnce sync.Once
}

// dialSOCKSUDP only accepts literal loopback IPs. In particular, user-supplied
// DNS names and public SOCKS servers are prohibited, preventing an accidental
// origin UDP bypass or a new DNS-resolution path.
func dialSOCKSUDP(upstream string) (*socksUDPAssociation, error) {
	host, port, err := net.SplitHostPort(upstream)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errInvalidUDPUpstream, err)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || !addr.IsLoopback() || addr.Zone() != "" {
		return nil, errInvalidUDPUpstream
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return nil, errInvalidUDPUpstream
	}
	ctrl, err := net.DialTimeout("tcp", upstream, udpUpstreamHandshakeTimeout)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = ctrl.Close()
		}
	}()
	if err = ctrl.SetDeadline(time.Now().Add(udpUpstreamHandshakeTimeout)); err != nil {
		return nil, err
	}
	if err = writeUDPControl(ctrl, []byte{5, 1, 0}); err != nil {
		return nil, err
	}
	var method [2]byte
	if _, err = io.ReadFull(ctrl, method[:]); err != nil {
		return nil, err
	}
	if method != [2]byte{5, 0} {
		return nil, fmt.Errorf("SOCKS UDP upstream no-auth rejected")
	}
	// SOCKS5 UDP ASSOCIATE with zero client address. The relay must be local.
	if err = writeUDPControl(ctrl, []byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return nil, err
	}
	var header [4]byte
	if _, err = io.ReadFull(ctrl, header[:]); err != nil {
		return nil, err
	}
	if header[0] != 5 || header[1] != 0 || header[2] != 0 {
		return nil, fmt.Errorf("SOCKS UDP association rejected")
	}
	var relayIP netip.Addr
	switch header[3] {
	case 1:
		var b [4]byte
		if _, err = io.ReadFull(ctrl, b[:]); err != nil {
			return nil, err
		}
		relayIP = netip.AddrFrom4(b)
	case 4:
		var b [16]byte
		if _, err = io.ReadFull(ctrl, b[:]); err != nil {
			return nil, err
		}
		relayIP = netip.AddrFrom16(b).Unmap()
	default:
		// Do not resolve domain name returned by an untrusted SOCKS endpoint.
		return nil, errInvalidUDPUpstream
	}
	var bp [2]byte
	if _, err = io.ReadFull(ctrl, bp[:]); err != nil {
		return nil, err
	}
	relayPort := binary.BigEndian.Uint16(bp[:])
	if relayPort == 0 {
		return nil, errInvalidUDPUpstream
	}
	if relayIP.IsUnspecified() {
		relayIP = addr
	}
	// Some servers advertise a wildcard, but never allow non-loopback relay.
	if !relayIP.IsLoopback() || relayIP.Zone() != "" {
		return nil, errInvalidUDPUpstream
	}
	relay := net.UDPAddrFromAddrPort(netip.AddrPortFrom(relayIP, relayPort))
	udp, err := net.DialUDP("udp", nil, relay)
	if err != nil {
		return nil, err
	}
	_ = ctrl.SetDeadline(time.Time{})
	ok = true
	return &socksUDPAssociation{control: ctrl, udp: udp, allowed: make(map[netip.AddrPort]time.Time)}, nil
}

func socksUDPAddress(p netip.AddrPort) ([]byte, error) {
	if !udpwire.ValidPeer(p) {
		return nil, udpwire.ErrMalformed
	}
	addr := p.Addr().Unmap()
	b := make([]byte, 0, 19)
	if addr.Is4() {
		a := addr.As4()
		b = append(b, 1)
		b = append(b, a[:]...)
	} else if addr.Is6() {
		a := addr.As16()
		b = append(b, 4)
		b = append(b, a[:]...)
	} else {
		return nil, udpwire.ErrMalformed
	}
	return binary.BigEndian.AppendUint16(b, p.Port()), nil
}

func parseSOCKSUDPAddress(raw []byte) (netip.AddrPort, int, error) {
	if len(raw) < 1 {
		return netip.AddrPort{}, 0, udpwire.ErrMalformed
	}
	var n int
	var ip netip.Addr
	switch raw[0] {
	case 1:
		n = 7
		if len(raw) < n {
			return netip.AddrPort{}, 0, udpwire.ErrMalformed
		}
		var b [4]byte
		copy(b[:], raw[1:5])
		ip = netip.AddrFrom4(b)
	case 4:
		n = 19
		if len(raw) < n {
			return netip.AddrPort{}, 0, udpwire.ErrMalformed
		}
		var b [16]byte
		copy(b[:], raw[1:17])
		ip = netip.AddrFrom16(b)
		if ip.Is4In6() {
			return netip.AddrPort{}, 0, udpwire.ErrMalformed
		}
	default:
		return netip.AddrPort{}, 0, udpwire.ErrMalformed
	}
	p := netip.AddrPortFrom(ip, binary.BigEndian.Uint16(raw[n-2:n]))
	if !udpwire.ValidPeer(p) {
		return netip.AddrPort{}, 0, udpwire.ErrMalformed
	}
	return p, n, nil
}

func marshalSOCKSUDP(p netip.AddrPort, data []byte) ([]byte, error) {
	if len(data) > udpwire.MaxPayload {
		return nil, udpwire.ErrMalformed
	}
	a, err := socksUDPAddress(p)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 3+len(a)+len(data))
	out = append(out, 0, 0, 0)
	out = append(out, a...)
	return append(out, data...), nil
}

func unmarshalSOCKSUDP(raw []byte) (netip.AddrPort, []byte, error) {
	if len(raw) < 3+7 || len(raw) > udpUpstreamMaxPacket || raw[0] != 0 || raw[1] != 0 || raw[2] != 0 {
		return netip.AddrPort{}, nil, udpwire.ErrMalformed
	}
	p, n, err := parseSOCKSUDPAddress(raw[3:])
	if err != nil {
		return netip.AddrPort{}, nil, err
	}
	data := raw[3+n:]
	if len(data) > udpwire.MaxPayload {
		return netip.AddrPort{}, nil, udpwire.ErrMalformed
	}
	return p, append([]byte(nil), data...), nil
}

func (a *socksUDPAssociation) permittedPeer(p netip.AddrPort, register bool) bool {
	a.allowedMu.Lock()
	defer a.allowedMu.Unlock()
	now := time.Now()
	if register {
		// Bound the peer map, even if the client chooses new destinations.
		if _, ok := a.allowed[p]; !ok && len(a.allowed) >= maxUDPTrackedPeers {
			for key, exp := range a.allowed {
				if now.After(exp) {
					delete(a.allowed, key)
				}
			}
			if len(a.allowed) >= maxUDPTrackedPeers {
				return false
			}
		}
		a.allowed[p] = now.Add(udpTrackedPeerTTL)
		return true
	}
	exp, ok := a.allowed[p]
	return ok && !now.After(exp)
}

// Send emits one SOCKS-wrapped datagram to loopback only. The outbound target
// is encoded in the packet, not passed to net.DialUDP.
func (a *socksUDPAssociation) Send(p netip.AddrPort, data []byte) error {
	packet, err := marshalSOCKSUDP(p, data)
	if err != nil {
		return err
	}
	if !a.permittedPeer(p, true) {
		return fmt.Errorf("udp flow destination quota exceeded")
	}
	_ = a.udp.SetWriteDeadline(time.Now().Add(udpUpstreamWriteTimeout))
	n, err := a.udp.Write(packet)
	if err != nil {
		return err
	}
	if n != len(packet) {
		return io.ErrShortWrite
	}
	return nil
}

func (a *socksUDPAssociation) Receive() (netip.AddrPort, []byte, error) {
	buf := make([]byte, udpUpstreamMaxPacket+1)
	_ = a.udp.SetReadDeadline(time.Now().Add(udpUpstreamReadTimeout))
	n, err := a.udp.Read(buf)
	if err != nil {
		return netip.AddrPort{}, nil, err
	}
	if n > udpUpstreamMaxPacket {
		return netip.AddrPort{}, nil, udpwire.ErrMalformed
	}
	p, data, err := unmarshalSOCKSUDP(buf[:n])
	if err != nil {
		return netip.AddrPort{}, nil, err
	}
	if !a.permittedPeer(p, false) {
		return netip.AddrPort{}, nil, udpwire.ErrMalformed
	}
	return p, data, nil
}

func (a *socksUDPAssociation) Close() {
	a.closeOnce.Do(func() { _ = a.udp.Close(); _ = a.control.Close() })
}

// A SOCKS control message must be written completely; a partial handshake
// must never be interpreted as a valid association.
func writeUDPControl(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
