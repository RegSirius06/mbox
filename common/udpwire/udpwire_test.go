package udpwire

import (
	"bytes"
	"errors"
	"io"
	"net/netip"
	"testing"
)

func TestRoundTripIPv4IPv6AndEmpty(t *testing.T) {
	for _, text := range []string{"1.1.1.1:53", "[2606:4700:4700::1111]:443"} {
		p := netip.MustParseAddrPort(text)
		for _, data := range [][]byte{nil, []byte("DNS"), bytes.Repeat([]byte{42}, MaxPayload)} {
			d := Datagram{FlowID: 77, Peer: p, Data: data}
			wire, err := Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Unmarshal(wire)
			if err != nil || got.FlowID != 77 || got.Peer != p || !bytes.Equal(got.Data, data) {
				t.Fatalf("roundtrip %q: %+v %v", text, got, err)
			}
			ep, hev, err := UnmarshalHEV(mustHEV(t, p, data))
			if err != nil || ep != p || !bytes.Equal(hev, data) {
				t.Fatalf("hev %q: %v", text, err)
			}
		}
	}
}

func mustHEV(t *testing.T, p netip.AddrPort, b []byte) []byte {
	t.Helper()
	out, err := MarshalHEV(p, b)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestHEVStreamFramingAndPartialRead(t *testing.T) {
	a := netip.MustParseAddrPort("8.8.8.8:53")
	b := netip.MustParseAddrPort("[2001:4860:4860::8888]:53")
	first := mustHEV(t, a, []byte("query"))
	second := mustHEV(t, b, []byte("answer"))
	source := &slowReader{src: bytes.NewReader(append(first, second...))}
	for i, want := range []netip.AddrPort{a, b} {
		got, data, err := ReadHEV(source)
		if err != nil || got != want || len(data) == 0 {
			t.Fatalf("datagram %d: %v %v", i, got, err)
		}
	}
	_, _, err := ReadHEV(source)
	if !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
}

type slowReader struct{ src *bytes.Reader }

func (s *slowReader) Read(b []byte) (int, error) {
	if len(b) > 1 {
		b = b[:1]
	}
	return s.src.Read(b)
}

func TestMalformedRejectsWithoutPartialDelivery(t *testing.T) {
	p := netip.MustParseAddrPort("9.9.9.9:53")
	wire, _ := Marshal(Datagram{FlowID: 1, Peer: p, Data: []byte("x")})
	hev := mustHEV(t, p, []byte("x"))
	for i := 0; i < len(wire); i++ {
		if _, err := Unmarshal(wire[:i]); err == nil {
			t.Fatalf("accepted truncated MBox at %d", i)
		}
	}
	for i := 0; i < len(hev); i++ {
		if _, _, err := UnmarshalHEV(hev[:i]); err == nil {
			t.Fatalf("accepted truncated HEV at %d", i)
		}
		_, _, err := ReadHEV(bytes.NewReader(hev[:i]))
		if err == nil {
			t.Fatalf("accepted streaming truncation at %d", i)
		}
	}
	for _, bad := range [][]byte{
		append([]byte(nil), wire[1:]...),        // version missing
		append(append([]byte(nil), wire...), 0), // trailing bytes
		append([]byte(nil), wire[:5]...),        // missing IP
	} {
		if _, err := Unmarshal(bad); err == nil {
			t.Fatalf("accepted malformed %x", bad)
		}
	}
	badID := append([]byte(nil), wire...)
	copy(badID[1:5], []byte{0, 0, 0, 0})
	if _, err := Unmarshal(badID); err == nil {
		t.Fatal("accepted flow id zero")
	}
	badHEV := append([]byte(nil), hev...)
	badHEV[2] = 0xff
	if _, _, err := ReadHEV(bytes.NewReader(badHEV)); err == nil {
		t.Fatal("accepted invalid header size")
	}
	badHEV = append([]byte(nil), hev...)
	badHEV[3] = 3 // reject domains until explicit DNS-routing support
	if _, _, err := UnmarshalHEV(badHEV); err == nil {
		t.Fatal("accepted domain destination")
	}
}

func TestOversizeAndBadDestination(t *testing.T) {
	for _, s := range []string{"0.0.0.0:53", "[::]:53", "127.0.0.1:0", "[fe80::1%eth0]:53"} {
		p, err := netip.ParseAddrPort(s)
		if err != nil {
			t.Fatal(err)
		}
		if ValidPeer(p) {
			t.Fatal("bad peer accepted", s)
		}
		if _, err := Marshal(Datagram{FlowID: 1, Peer: p}); err == nil {
			t.Fatal("bad peer marshaled", s)
		}
	}
	p := netip.MustParseAddrPort("1.1.1.1:53")
	data := make([]byte, MaxPayload+1)
	if _, err := Marshal(Datagram{FlowID: 1, Peer: p, Data: data}); err == nil {
		t.Fatal("oversize MBox accepted")
	}
	if _, err := MarshalHEV(p, data); err == nil {
		t.Fatal("oversize HEV accepted")
	}
	if _, err := Marshal(Datagram{Peer: p}); err == nil {
		t.Fatal("flow ID zero accepted")
	}
}

func TestDecodeOwnershipAndNoDNS(t *testing.T) {
	p := netip.MustParseAddrPort("1.1.1.1:53")
	raw, _ := Marshal(Datagram{FlowID: 2, Peer: p, Data: []byte("secret")})
	d, err := Unmarshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] = 'X'
	if string(d.Data) != "secret" {
		t.Fatal("mutable alias to input")
	}
	h := mustHEV(t, p, []byte("abc"))
	_, b, err := UnmarshalHEV(h)
	if err != nil {
		t.Fatal(err)
	}
	h[len(h)-1] = 'X'
	if string(b) != "abc" {
		t.Fatal("mutable HEV alias")
	}
}

func FuzzMBoxDatagram(f *testing.F) {
	f.Add([]byte{1, 0, 0, 0, 1, 1, 1, 1, 1, 1, 0, 53, 0, 0})
	f.Add(bytes.Repeat([]byte{255}, 2048))
	f.Fuzz(func(t *testing.T, raw []byte) {
		d, err := Unmarshal(raw)
		if err != nil {
			return
		}
		wire, e := Marshal(d)
		if e != nil || !bytes.Equal(wire, raw) {
			t.Fatalf("accepted noncanonical representation: %v", e)
		}
	})
}

func FuzzHEVDatagram(f *testing.F) {
	f.Add([]byte{0, 0, 10, 1, 1, 1, 1, 1, 0, 53})
	f.Add(bytes.Repeat([]byte{255}, 2048))
	f.Fuzz(func(t *testing.T, raw []byte) {
		p, b, err := UnmarshalHEV(raw)
		if err != nil {
			return
		}
		wire, e := MarshalHEV(p, b)
		if e != nil || !bytes.Equal(wire, raw) {
			t.Fatalf("accepted noncanonical representation: %v", e)
		}
	})
}

// These bytes must always traverse the MBox authenticated record boundary.
// The tests exercise the codec pair independently, never create UDP sockets.
func TestHEVEncryptedFrameEnvelopeRoundTrip(t *testing.T) {
	peer := netip.MustParseAddrPort("[2606:4700:4700::1111]:53")
	hev := mustHEV(t, peer, []byte{0x11, 0x22, 0x33})
	resolved, packet, err := UnmarshalHEV(hev)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Marshal(Datagram{FlowID: 0x11223344, Peer: resolved, Data: packet})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Unmarshal(encoded)
	if err != nil || decoded.FlowID != 0x11223344 {
		t.Fatalf("MBox frame: %v", err)
	}
	back, err := MarshalHEV(decoded.Peer, decoded.Data)
	if err != nil || !bytes.Equal(back, hev) {
		t.Fatalf("HEV roundtrip: %v", err)
	}
}

func TestStreamingLengthCapBeforeBodyRead(t *testing.T) {
	// Declare a 65535-byte datagram, but supply only its three-byte header:
	// must reject immediately instead of trying to read or allocate 64K.
	_, _, err := ReadHEV(bytes.NewReader([]byte{0xff, 0xff, 10}))
	if !errors.Is(err, ErrMalformed) {
		t.Fatalf("missing pre-read cap: %v", err)
	}
}
