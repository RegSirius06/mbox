package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	"../common/udpwire"
)

func testSOCKSUDPProxy(t *testing.T, responseOverride []byte) (string, <-chan []byte) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	relay, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close(); _ = relay.Close() })
	sent := make(chan []byte, 8)
	go func() {
		for {
			buf := make([]byte, udpUpstreamMaxPacket+1)
			n, from, err := relay.ReadFromUDP(buf)
			if err != nil {
				return
			}
			raw := append([]byte(nil), buf[:n]...)
			select {
			case sent <- raw:
			default:
			}
			_, _ = relay.WriteToUDP(raw, from)
		}
	}()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		head := make([]byte, 3)
		if _, err = io.ReadFull(conn, head); err != nil {
			return
		}
		if !bytes.Equal(head, []byte{5, 1, 0}) {
			return
		}
		_, _ = conn.Write([]byte{5, 0})
		req := make([]byte, 10)
		if _, err = io.ReadFull(conn, req); err != nil {
			return
		}
		if !bytes.Equal(req, []byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0}) {
			return
		}
		if responseOverride != nil {
			_, _ = conn.Write(responseOverride)
		} else {
			reply := []byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0}
			binary.BigEndian.PutUint16(reply[8:10], uint16(relay.LocalAddr().(*net.UDPAddr).Port))
			_, _ = conn.Write(reply)
		}
		buf := make([]byte, 1)
		for {
			if _, err = conn.Read(buf); err != nil {
				return
			}
		}
	}()
	return ln.Addr().String(), sent
}

func TestSOCKSUDPAssociateRoundtripPreservesPeerAndBoundary(t *testing.T) {
	addr, sent := testSOCKSUDPProxy(t, nil)
	assoc, err := dialSOCKSUDP(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer assoc.Close()
	for _, text := range []string{"1.1.1.1:53", "[2606:4700:4700::1111]:443"} {
		peer := netip.MustParseAddrPort(text)
		payload := []byte("private DNS payload")
		if err := assoc.Send(peer, payload); err != nil {
			t.Fatal(err)
		}
		select {
		case wire := <-sent:
			p, b, err := udpwire.UnmarshalSOCKS5(wire)
			if err != nil || p != peer || !bytes.Equal(payload, b) {
				t.Fatalf("upstream got %v %q %v", p, b, err)
			}
		case <-time.After(time.Second):
			t.Fatal("SOCKS relay did not receive")
		}
		gotPeer, got, err := assoc.Receive()
		if err != nil || gotPeer != peer || !bytes.Equal(got, payload) {
			t.Fatalf("reply %v %q %v", gotPeer, got, err)
		}
	}
}

func TestSOCKSUDPAssociateRejectsNonloopbackAndUntrustedRelay(t *testing.T) {
	for _, addr := range []string{"example.com:1080", "8.8.8.8:1080", "127.0.0.1:0", "[::1%lo]:1080"} {
		if _, err := dialSOCKSUDP(addr); !errors.Is(err, errInvalidUDPUpstream) {
			t.Fatalf("accepted %s: %v", addr, err)
		}
	}
	addr, _ := testSOCKSUDPProxy(t, []byte{5, 0, 0, 1, 8, 8, 8, 8, 0, 53})
	if _, err := dialSOCKSUDP(addr); !errors.Is(err, errInvalidUDPUpstream) {
		t.Fatalf("accepted remote relay: %v", err)
	}
}

func TestSOCKSUDPParserFailClosed(t *testing.T) {
	p := netip.MustParseAddrPort("1.1.1.1:53")
	raw, err := udpwire.MarshalSOCKS5(p, []byte("hi"))
	if err != nil {
		t.Fatal(err)
	}
	cases := [][]byte{
		raw[:3], raw[:9], append(append([]byte(nil), raw...), make([]byte, udpwire.MaxPayload)...),
		append([]byte{0, 0, 1}, raw[3:]...), append([]byte{1, 0, 0}, raw[3:]...),
		append([]byte{0, 0, 0, 3}, raw[4:]...),
	}
	for i, wire := range cases {
		if _, _, err := udpwire.UnmarshalSOCKS5(wire); err == nil {
			t.Fatalf("accepted invalid packet %d", i)
		}
	}
	if _, err := udpwire.MarshalSOCKS5(p, make([]byte, udpwire.MaxPayload+1)); err == nil {
		t.Fatal("accepted oversized")
	}
}

func TestSOCKSUDPRejectUnsolicitedReturn(t *testing.T) {
	addr, _ := testSOCKSUDPProxy(t, nil)
	a, err := dialSOCKSUDP(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	p := netip.MustParseAddrPort("1.1.1.1:53")
	if a.permittedPeer(p, false) {
		t.Fatal("unsolicited permitted")
	}
	for i := 0; i < maxUDPTrackedPeers; i++ {
		x := netip.AddrPortFrom(p.Addr(), uint16(1000+i))
		if !a.permittedPeer(x, true) {
			t.Fatal("premature full")
		}
	}
	if a.permittedPeer(p, true) {
		t.Fatal("unbounded peer map")
	}
}

// Fuzzing SOCKS5 UDP encapsulation is separate from fuzzing MBox's inner
// datagram codec. The origin must never accept malformed SOCKS relay replies.
func FuzzSOCKSUDPWire(f *testing.F) {
	f.Add([]byte{0, 0, 0, 1, 1, 1, 1, 1, 0, 53, 42})
	f.Add([]byte{0, 0, 1, 1, 1, 1, 1, 1, 0, 53, 42})
	f.Add(bytes.Repeat([]byte{0xff}, 512))
	f.Fuzz(func(t *testing.T, raw []byte) {
		p, data, err := udpwire.UnmarshalSOCKS5(raw)
		if err != nil {
			return
		}
		encoded, e := udpwire.MarshalSOCKS5(p, data)
		if e != nil || !bytes.Equal(encoded, raw) {
			t.Fatalf("accepted noncanonical SOCKS UDP: %v", e)
		}
	})
}
