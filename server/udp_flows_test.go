package main

import (
	"bytes"
	"net/netip"
	"testing"
	"time"

	"../common/udpwire"
)

func newUDPFlowTestSession(t *testing.T) *session {
	t.Helper()
	s, _ := newTransportTestSession(t)
	s.udpFlows = make(map[uint32]*udpFlow)
	return s
}

func serverUDPFixtures(t *testing.T) (*session, <-chan []byte) {
	t.Helper()
	old := socksAddr
	enabled := enableUDPLabServer
	addr, sent := testSOCKSUDPProxy(t, nil)
	socksAddr = addr
	enableUDPLabServer = true
	globalQueuedBytes.Store(0)
	totalStreams.Store(0)
	s := newUDPFlowTestSession(t)
	t.Cleanup(func() {
		closeSessionState("udp-test", s)
		socksAddr = old
		enableUDPLabServer = enabled
		if x := globalQueuedBytes.Load(); x != 0 {
			t.Errorf("queued bytes leaked: %d", x)
		}
		if x := totalStreams.Load(); x != 0 {
			t.Errorf("streams leaked: %d", x)
		}
	})
	return s, sent
}

func waitUDPFlowReady(t *testing.T, s *session, id uint32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		f := s.udpFlows[id]
		ready := f != nil && f.ready.Load()
		s.mu.Unlock()
		if ready {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("UDP flow not ready")
}

func TestUDPDisabledByDefaultNoSocketOrState(t *testing.T) {
	prior := enableUDPLabServer
	enableUDPLabServer = false
	defer func() { enableUDPLabServer = prior }()
	s := newUDPFlowTestSession(t)
	if err := newUDPFlow(s, 1); err == nil {
		t.Fatal("UDP unexpectedly enabled")
	}
	if len(s.udpFlows) != 0 {
		t.Fatal("created UDP state")
	}
}

func TestUDPFlowAssociatesAndReplaysNoDirectOriginEgress(t *testing.T) {
	s, sent := serverUDPFixtures(t)
	id := uint32(25)
	if err := handleFrame(s, id, frameUDPOpen, nil); err != nil {
		t.Fatal(err)
	}
	waitUDPFlowReady(t, s, id)
	peer := netip.MustParseAddrPort("1.1.1.1:53")
	p, err := udpwire.Marshal(udpwire.Datagram{FlowID: id, Peer: peer, Data: []byte("dns")})
	if err != nil {
		t.Fatal(err)
	}
	if err := handleFrame(s, id, frameUDPData, p); err != nil {
		t.Fatal(err)
	}
	select {
	case wire := <-sent:
		dest, data, err := udpwire.UnmarshalSOCKS5(wire)
		if err != nil || dest != peer || !bytes.Equal(data, []byte("dns")) {
			t.Fatalf("incorrect SOCKS relay packet %v %q %v", dest, data, err)
		}
	case <-time.After(time.Second):
		t.Fatal("no send to SOCKS relay")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		frames := append([][]byte(nil), s.downFrames...)
		s.mu.Unlock()
		for _, f := range frames {
			if len(f) >= 9 && f[4] == frameUDPData {
				got, err := udpwire.Unmarshal(f[9:])
				if err != nil {
					t.Fatal(err)
				}
				if got.FlowID != id || got.Peer != peer || string(got.Data) != "dns" {
					t.Fatalf("bad return %+v", got)
				}
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no UDP response was queued inside MBox")
}

func TestUDPFlowRejectsTamperedIDAndEnforcesClose(t *testing.T) {
	s, _ := serverUDPFixtures(t)
	if err := handleFrame(s, 1, frameUDPOpen, []byte("bad")); err == nil {
		t.Fatal("accepted OPEN payload")
	}
	if err := handleFrame(s, 1, frameUDPOpen, nil); err != nil {
		t.Fatal(err)
	}
	waitUDPFlowReady(t, s, 1)
	p, _ := udpwire.Marshal(udpwire.Datagram{FlowID: 2, Peer: netip.MustParseAddrPort("1.1.1.1:53"), Data: []byte("x")})
	if err := handleFrame(s, 1, frameUDPData, p); err == nil {
		t.Fatal("accepted mismatched flow ID")
	}
	if err := handleFrame(s, 1, frameClose, nil); err != nil {
		t.Fatal(err)
	}
	if len(s.udpFlows) != 0 {
		t.Fatal("flow still allocated")
	}
}

func TestUDPFlowCloseDrainsQueuedGlobalBudget(t *testing.T) {
	s, _ := serverUDPFixtures(t)
	id := uint32(42)
	if err := handleFrame(s, id, frameUDPOpen, nil); err != nil {
		t.Fatal(err)
	}
	waitUDPFlowReady(t, s, id)
	d, err := udpwire.Marshal(udpwire.Datagram{FlowID: id, Peer: netip.MustParseAddrPort("8.8.8.8:53"), Data: make([]byte, 500)})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < udpPerFlowPending+10; i++ {
		_ = onUDPData(s, id, d)
	}
	closeSessionState("udp-test", s)
	deadline := time.Now().Add(time.Second)
	for globalQueuedBytes.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if x := globalQueuedBytes.Load(); x != 0 {
		t.Fatalf("global budget not released after workers exit: %d", x)
	}
}

// An asynchronous worker from a retired association must not close a newer
// association that happens to use the same numeric flow ID.
func TestUDPStaleWorkerCannotReplaceOrCloseNewFlow(t *testing.T) {
	s, _ := serverUDPFixtures(t)
	old := &udpFlow{done: make(chan struct{}), queued: make(chan udpQueuedDatagram, 1)}
	newFlow := &udpFlow{done: make(chan struct{}), queued: make(chan udpQueuedDatagram, 1)}
	const id = uint32(35)
	s.mu.Lock()
	s.udpFlows[id] = old
	s.mu.Unlock()
	totalStreams.Add(1)
	closeUDPFlow(s, id, old, true)
	s.mu.Lock()
	before := len(s.downFrames)
	s.udpFlows[id] = newFlow
	s.mu.Unlock()
	totalStreams.Add(1)

	if err := appendFrame(s, id, frameUDPData, []byte("stale"), old); err == nil {
		t.Fatal("retired worker injected a frame into replacement")
	}
	closeUDPFlow(s, id, old, true)
	s.mu.Lock()
	got := s.udpFlows[id]
	after := len(s.downFrames)
	s.mu.Unlock()
	if got != newFlow || after != before {
		t.Fatal("retired worker closed or notified replacement flow")
	}
	closeUDPFlow(s, id, newFlow, false)
}

func TestUDPPublicDestinationGuard(t *testing.T) {
	blocked := []string{
		"127.0.0.1:123", "10.0.0.1:53", "192.168.1.5:53", "172.16.0.1:53",
		"169.254.10.20:53", "100.64.1.1:53", "198.18.0.2:53",
		"192.0.2.1:53", "198.51.100.1:53", "203.0.113.1:53",
		"224.0.0.1:53", "[::1]:53", "[fe80::1]:53", "[fc00::5]:53",
		"[2001:db8::1]:53",
	}
	for _, value := range blocked {
		if publicUDPDestination(netip.MustParseAddrPort(value)) {
			t.Fatalf("accepted private/special destination %s", value)
		}
	}
	for _, value := range []string{"1.1.1.1:53", "8.8.8.8:53", "[2606:4700:4700::1111]:53"} {
		if !publicUDPDestination(netip.MustParseAddrPort(value)) {
			t.Fatalf("rejected public destination %s", value)
		}
	}
}

func TestUDPNonPublicNeverReachesSOCKSRelay(t *testing.T) {
	s, sent := serverUDPFixtures(t)
	const id uint32 = 89
	if err := handleFrame(s, id, frameUDPOpen, nil); err != nil {
		t.Fatal(err)
	}
	waitUDPFlowReady(t, s, id)
	for _, peer := range []string{"127.0.0.1:2080", "192.168.1.1:53", "100.64.0.2:53"} {
		raw, err := udpwire.Marshal(udpwire.Datagram{FlowID: id, Peer: netip.MustParseAddrPort(peer), Data: []byte("probe")})
		if err != nil {
			t.Fatal(err)
		}
		if err := handleFrame(s, id, frameUDPData, raw); err == nil {
			t.Fatalf("origin accepted private destination %s", peer)
		}
	}
	select {
	case got := <-sent:
		t.Fatalf("private packet reached SOCKS relay: %x", got)
	case <-time.After(50 * time.Millisecond):
	}
}
