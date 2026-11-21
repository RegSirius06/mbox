package main

import (
	"bytes"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	"../common/dgram"
	"../common/noise"
	"../common/udpwire"
)

func TestOriginDatagramAssociationAndReplay(t *testing.T) {
	s, sent := serverUDPFixtures(t)
	sid := "fa123456789abcde0123456789abcdef"
	up, err := noise.NewCipherState(bytes.Repeat([]byte{0x51}, 32))
	if err != nil {
		t.Fatal(err)
	}
	s.crypto.up = up
	sessionsMu.Lock()
	if sessions[sid] != nil {
		sessionsMu.Unlock()
		t.Fatal("fixture collision")
	}
	sessions[sid] = s
	sessionsMu.Unlock()
	defer func() { sessionsMu.Lock(); delete(sessions, sid); sessionsMu.Unlock() }()
	id := uint32(25)
	if err = handleFrame(s, id, frameUDPOpen, nil); err != nil {
		t.Fatal(err)
	}
	waitUDPFlowReady(t, s, id)
	attach := dgram.Attach(up.DatagramSecret(), sid)
	start := func(request []byte, expect bool) (net.Conn, <-chan struct{}) {
		a, b := net.Pipe()
		_ = a.SetDeadline(time.Now().Add(3 * time.Second))
		done := make(chan struct{})
		go func() { serveOriginDatagram(b); close(done) }()
		if err := dgram.WriteFull(a, request); err != nil {
			a.Close()
			t.Fatal(err)
		}
		var got [32]byte
		_, err := io.ReadFull(a, got[:])
		if expect {
			if err != nil || !dgram.VerifyProof(s.crypto.down.DatagramSecret(), sid, "accept", got[:]) {
				a.Close()
				t.Fatal("attach rejected", err)
			}
		} else {
			if err == nil {
				a.Close()
				t.Fatal("unauthorized/replay attach accepted")
			}
			a.Close()
		}
		return a, done
	}
	invalid := append([]byte(nil), attach...)
	invalid[len(invalid)-1] ^= 1
	_, badDone := start(invalid, false)
	<-badDone
	c, done := start(attach, true)
	defer c.Close()
	deadline := time.Now().Add(time.Second)
	for s.datagramLane.Load() == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.datagramLane.Load() == nil {
		t.Fatal("lane not installed")
	}
	tx, _ := dgram.NewRecord(up.DatagramSecret(), sid, "up")
	rx, _ := dgram.NewRecord(s.crypto.down.DatagramSecret(), sid, "down")
	peer := netip.MustParseAddrPort("1.1.1.1:53")
	wire, err := udpwire.Marshal(udpwire.Datagram{FlowID: id, Peer: peer, Data: []byte("dns")})
	if err != nil {
		t.Fatal(err)
	}
	p, err := tx.Seal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if err = dgram.WritePacket(c, p); err != nil {
		t.Fatal(err)
	}
	if err = dgram.WritePacket(c, p); err != nil {
		t.Fatal(err)
	}
	select {
	case w := <-sent:
		dst, data, e := udpwire.UnmarshalSOCKS5(w)
		if e != nil || dst != peer || string(data) != "dns" {
			t.Fatal("wrong SOCKS UDP")
		}
	case <-time.After(time.Second):
		t.Fatal("no SOCKS UDP")
	}
	record, err := dgram.ReadPacket(c)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := rx.Open(record)
	if err != nil {
		t.Fatal(err)
	}
	d, err := udpwire.Unmarshal(raw)
	if err != nil || !bytes.Equal(d.Data, []byte("dns")) {
		t.Fatal("bad down", err)
	}
	select {
	case <-sent:
		t.Fatal("replay emitted to SOCKS")
	case <-time.After(50 * time.Millisecond):
	}
	// Unknown association and non-public target must not emit a SOCKS packet.
	for _, bad := range []udpwire.Datagram{{FlowID: id + 1, Peer: peer, Data: []byte{1}}, {FlowID: id, Peer: netip.MustParseAddrPort("127.0.0.1:53"), Data: []byte{2}}} {
		raw, _ := udpwire.Marshal(bad)
		p, _ := tx.Seal(raw)
		if e := dgram.WritePacket(c, p); e != nil {
			t.Fatal(e)
		}
	}
	select {
	case <-sent:
		t.Fatal("invalid datagram emitted to SOCKS")
	case <-time.After(50 * time.Millisecond):
	}
	c.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("origin lane leaked")
	}
	if s.datagramLane.Load() != nil {
		t.Fatal("stale lane")
	}
	_, replayDone := start(attach, false)
	<-replayDone
}

// Old epoch-0 clients still work, while newer clients can reattach with a
// monotonic epoch. Replays must not open another lane or reset AEAD counters.
func TestOriginDatagramReattachWithRekey(t *testing.T) {
	s, _ := serverUDPFixtures(t)
	sid := "ab123456789abcde0123456789abcdef"
	up, err := noise.NewCipherState(bytes.Repeat([]byte{0x71}, 32))
	if err != nil {
		t.Fatal(err)
	}
	s.crypto.up = up
	sessionsMu.Lock()
	sessions[sid] = s
	sessionsMu.Unlock()
	defer func() { sessionsMu.Lock(); delete(sessions, sid); sessionsMu.Unlock() }()
	secretUp := up.DatagramSecret()
	secretDown := s.crypto.down.DatagramSecret()
	try := func(request []byte, epoch uint32, expected bool) {
		a, b := net.Pipe()
		_ = a.SetDeadline(time.Now().Add(800 * time.Millisecond))
		done := make(chan struct{})
		go func() { serveOriginDatagram(b); close(done) }()
		if err := dgram.WriteFull(a, request); err != nil {
			t.Fatal(err)
		}
		var response [dgram.ProofSize]byte
		_, err := io.ReadFull(a, response[:])
		if expected {
			valid := epoch == 0 && dgram.VerifyProof(secretDown, sid, "accept", response[:]) ||
				epoch != 0 && dgram.VerifyAcceptEpoch(secretDown, sid, epoch, response[:])
			if err != nil || !valid {
				t.Fatalf("epoch %d failed: %v", epoch, err)
			}
		} else if err == nil {
			t.Fatalf("replay accepted at epoch %d", epoch)
		}
		a.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("lane stuck")
		}
	}
	try(dgram.Attach(secretUp, sid), 0, true)
	try(dgram.Attach(secretUp, sid), 0, false)
	try(dgram.AttachEpoch(secretUp, sid, 1), 1, true)
	try(dgram.AttachEpoch(secretUp, sid, 1), 1, false)
	try(dgram.AttachEpoch(secretUp, sid, 3), 3, true)
	try(dgram.AttachEpoch(secretUp, sid, 2), 2, false)
	if s.datagramEpoch != 3 {
		t.Fatalf("epoch=%d", s.datagramEpoch)
	}
}
