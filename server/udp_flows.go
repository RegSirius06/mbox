package main

// UDP associations are fully session-owned and only exist behind an explicit
// lab gate. No UDP user traffic may egress directly from the origin: all
// packets go to a loopback SOCKS5 UDP relay, which applies outbound routing.

import (
	"fmt"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"../common/udpwire"
)

const (
	udpSessionIdle        = 2 * time.Minute
	udpPerFlowPending     = 32
	maxUDPFlowsPerSession = 16
	udpMaxDataPerSecond   = 128
)

type udpQueuedDatagram struct {
	d        udpwire.Datagram
	reserved int
}

type udpFlow struct {
	assoc      *socksUDPAssociation
	queued     chan udpQueuedDatagram
	done       chan struct{}
	closed     atomic.Bool
	ready      atomic.Bool
	lastActive atomic.Int64
	enqueueMu  sync.Mutex
	rateMu     sync.Mutex
	window     time.Time
	packets    int
}

func (f *udpFlow) touch() { f.lastActive.Store(time.Now().UnixNano()) }

func (f *udpFlow) allowPacket() bool {
	f.rateMu.Lock()
	defer f.rateMu.Unlock()
	now := time.Now()
	if now.Sub(f.window) >= time.Second {
		f.window = now
		f.packets = 0
	}
	if f.packets >= udpMaxDataPerSecond {
		return false
	}
	f.packets++
	return true
}

func closeUDPFlow(s *session, id uint32, expected *udpFlow, notify bool) {
	s.mu.Lock()
	f := s.udpFlows[id]
	if f == nil || (expected != nil && f != expected) {
		s.mu.Unlock()
		return
	}
	if notify {
		_ = appendFrameLocked(s, id, frameClose, nil, f)
	}
	delete(s.udpFlows, id)
	s.mu.Unlock()
	totalStreams.Add(-1)
	f.enqueueMu.Lock()
	f.closed.Store(true)
	close(f.done)
	f.enqueueMu.Unlock()
	if f.assoc != nil {
		f.assoc.Close()
	}
	// The sender owns in-flight reservations; the closer drains the rest.
	for {
		select {
		case item := <-f.queued:
			releaseGlobalQueued(item.reserved)
		default:
			return
		}
	}
}

func newUDPFlow(s *session, id uint32) error {
	if !enableUDPLabServer {
		return fmt.Errorf("UDP is disabled")
	}
	if id == 0 {
		return fmt.Errorf("zero UDP flow id")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session closed")
	}
	if _, dup := s.streams[id]; dup {
		s.mu.Unlock()
		return fmt.Errorf("UDP/TCP ID collision")
	}
	if _, dup := s.udpFlows[id]; dup {
		s.mu.Unlock()
		return fmt.Errorf("duplicate UDP flow")
	}
	if len(s.udpFlows) >= maxUDPFlowsPerSession {
		s.mu.Unlock()
		return fmt.Errorf("UDP flow quota exceeded")
	}
	if err := allowStreamOpenLocked(s); err != nil {
		s.mu.Unlock()
		return err
	}
	if !reserveTotalStream() {
		s.mu.Unlock()
		return fmt.Errorf("global stream quota exceeded")
	}
	f := &udpFlow{queued: make(chan udpQueuedDatagram, udpPerFlowPending), done: make(chan struct{})}
	f.touch()
	if s.udpFlows == nil {
		s.udpFlows = make(map[uint32]*udpFlow)
	}
	s.udpFlows[id] = f
	s.mu.Unlock()
	go startUDPFlow(s, id, f)
	return nil
}

func startUDPFlow(s *session, id uint32, f *udpFlow) {
	assoc, err := dialSOCKSUDP(socksAddr)
	if err != nil {
		_ = appendFrame(s, id, frameError, []byte("UDP upstream unavailable"), f)
		closeUDPFlow(s, id, f, false)
		return
	}
	s.mu.Lock()
	if s.closed || f.closed.Load() {
		s.mu.Unlock()
		assoc.Close()
		return
	}
	f.assoc = assoc
	f.ready.Store(true)
	s.mu.Unlock()
	if err := appendFrame(s, id, frameUDPReady, nil, f); err != nil {
		closeUDPFlow(s, id, f, false)
		return
	}
	go udpFlowSender(s, id, f)
	go udpFlowReceiver(s, id, f)
	go udpFlowMonitorControl(s, id, f)
}

func udpFlowSender(s *session, id uint32, f *udpFlow) {
	for {
		select {
		case <-f.done:
			return
		case item := <-f.queued:
			if !f.closed.Load() {
				if err := f.assoc.Send(item.d.Peer, item.d.Data); err != nil {
					releaseGlobalQueued(item.reserved)
					closeUDPFlow(s, id, f, true)
					return
				}
				f.touch()
			}
			releaseGlobalQueued(item.reserved)
		}
	}
}

func udpFlowReceiver(s *session, id uint32, f *udpFlow) {
	for {
		select {
		case <-f.done:
			return
		default:
		}
		p, data, err := f.assoc.Receive()
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				if time.Since(time.Unix(0, f.lastActive.Load())) < udpSessionIdle {
					continue
				}
			}
			closeUDPFlow(s, id, f, true)
			return
		}
		if f.closed.Load() {
			return
		}
		f.touch()
		out, err := udpwire.Marshal(udpwire.Datagram{FlowID: id, Peer: p, Data: data})
		if err != nil {
			closeUDPFlow(s, id, f, true)
			return
		}
		if err := appendFrame(s, id, frameUDPData, out, f); err != nil {
			closeUDPFlow(s, id, f, true)
			return
		}
	}
}

// An experimental UDP association may only target public unicast IPs.
// In particular, do not make the origin's loopback, LAN, CGNAT or benchmark
// networks reachable via sing-box rules that deliberately use direct routing.
// This is an egress guard, not a replacement for checking sing-box policy.
var udpNonPublic = [...]netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"), // CGNAT / Android mapdns
	netip.MustParsePrefix("198.18.0.0/15"), // network benchmarking
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func publicUDPDestination(peer netip.AddrPort) bool {
	if !udpwire.ValidPeer(peer) {
		return false
	}
	addr := peer.Addr()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.Is4In6() {
		return false
	}
	for _, prefix := range udpNonPublic {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

func onUDPData(s *session, id uint32, raw []byte) error {
	d, err := udpwire.Unmarshal(raw)
	if err != nil || d.FlowID != id {
		return fmt.Errorf("invalid UDP datagram or flow id")
	}
	if !publicUDPDestination(d.Peer) {
		return fmt.Errorf("UDP destination is not a public unicast address")
	}
	s.mu.Lock()
	f := s.udpFlows[id]
	s.mu.Unlock()
	if f == nil || f.closed.Load() {
		return nil
	}
	if !f.ready.Load() {
		return fmt.Errorf("UDP flow not ready")
	}
	if !f.allowPacket() {
		closeUDPFlow(s, id, f, true)
		return nil
	}
	// Reservation is owned until the sender consumes it or close drains it.
	reserved := len(raw) + 9
	if !reserveGlobalQueued(reserved) {
		return fmt.Errorf("global queue budget exhausted")
	}
	f.enqueueMu.Lock()
	if f.closed.Load() {
		f.enqueueMu.Unlock()
		releaseGlobalQueued(reserved)
		return nil
	}
	select {
	case f.queued <- udpQueuedDatagram{d: d, reserved: reserved}:
		f.enqueueMu.Unlock()
		return nil
	default:
		f.enqueueMu.Unlock()
		releaseGlobalQueued(reserved)
		closeUDPFlow(s, id, f, true)
		return nil
	}
}

// RFC 1928 binds the UDP association's lifetime to the TCP control socket.
// If the upstream proxy closes that socket, never keep a stale relay active.
func udpFlowMonitorControl(s *session, id uint32, f *udpFlow) {
	var b [1]byte
	// A SOCKS UDP ASSOCIATE control connection sends no further application
	// bytes after the reply. Any bytes/EOF/error end the association.
	_, _ = f.assoc.control.Read(b[:])
	closeUDPFlow(s, id, f, true)
}
