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
		dest, data, err := unmarshalSOCKSUDP(wire)
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
