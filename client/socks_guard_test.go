package main

import (
	"bytes"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// A deliberately fragmented client. The gate must reject UDP/BIND even if
// request headers arrive one byte at a time, without forwarding those bytes.
type guardTestConn struct {
	r     *bytes.Reader
	w     bytes.Buffer
	chunk int
}

func newGuardTestConn(data []byte, chunk int) *guardTestConn {
	return &guardTestConn{r: bytes.NewReader(data), chunk: chunk}
}
func (c *guardTestConn) Read(p []byte) (int, error) {
	if c.chunk > 0 && len(p) > c.chunk {
		p = p[:c.chunk]
	}
	return c.r.Read(p)
}
func (c *guardTestConn) Write(p []byte) (int, error)      { return c.w.Write(p) }
func (c *guardTestConn) Close() error                     { return nil }
func (c *guardTestConn) LocalAddr() net.Addr              { return nil }
func (c *guardTestConn) RemoteAddr() net.Addr             { return nil }
func (c *guardTestConn) SetDeadline(time.Time) error      { return nil }
func (c *guardTestConn) SetReadDeadline(time.Time) error  { return nil }
func (c *guardTestConn) SetWriteDeadline(time.Time) error { return nil }

func TestSOCKSConnectGateForwardsVerbatim(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  []byte
	}{
		{"ipv4", []byte{5, 1, 0, 1, 8, 8, 8, 8, 0, 53}},
		{"ipv6", []byte{5, 1, 0, 4, 0x20, 1, 0xdb, 8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 0xbb}},
		{"domain", append([]byte{5, 1, 0, 3, 11}, append([]byte("example.com"), 1, 0xbb)...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			intro := []byte{5, 2, 0, 2}
			all := append(append([]byte{}, intro...), tc.req...)
			for _, chunk := range []int{1, 2, 32} {
				conn := newGuardTestConn(all, chunk)
				var forwarded []byte
				err := guardSOCKSConnect(conn, func(b []byte) error {
					forwarded = append(forwarded, b...)
					return nil
				})
				if err != nil || !bytes.Equal(forwarded, all) {
					t.Fatalf("chunk %d err=%v got=%x want=%x", chunk, err, forwarded, all)
				}
			}
		})
	}
}

func TestSOCKSConnectGateRejectsBINDAndUDP(t *testing.T) {
	for _, cmd := range []byte{2, 3, 5, 0x80} {
		for _, chunk := range []int{1, 16} {
			intro := []byte{5, 1, 0}
			request := []byte{5, cmd, 0, 1, 127, 0, 0, 1, 0, 53}
			conn := newGuardTestConn(append(append([]byte{}, intro...), request...), chunk)
			var forwarded []byte
			err := guardSOCKSConnect(conn, func(b []byte) error {
				forwarded = append(forwarded, b...)
				return nil
			})
			if !errors.Is(err, errSOCKSCommandRejected) {
				t.Fatalf("cmd=%x chunk=%d: %v", cmd, chunk, err)
			}
			if !bytes.Equal(forwarded, intro) {
				t.Fatalf("forbidden SOCKS command left local device: %x", forwarded)
			}
			if !bytes.Equal(conn.w.Bytes(), []byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0}) {
				t.Fatalf("wrong failure response: %x", conn.w.Bytes())
			}
		}
	}
}

func TestSOCKSConnectGateRejectsBadPrelude(t *testing.T) {
	for _, data := range [][]byte{
		{4, 1, 0},                      // SOCKS4 rejected.
		{5, 0},                         // No supported methods.
		{5, 1, 2},                      // No no-auth method.
		{5, 1, 0, 5, 1, 1, 1},          // Bad reserved byte.
		{5, 1, 0, 5, 1, 0, 3, 0, 1, 2}, // Empty domain.
		{5, 1, 0, 5, 1, 0, 0x80},       // Invalid ATYP.
		{5, 1, 0, 5, 1, 0, 1, 127},     // Incomplete address.
	} {
		conn := newGuardTestConn(data, 1)
		var forwarded []byte
		err := guardSOCKSConnect(conn, func(b []byte) error {
			forwarded = append(forwarded, b...)
			return nil
		})
		if err == nil {
			t.Fatalf("accepted malformed prelude %x", data)
		}
		// Only a valid, no-auth hello may leave this endpoint.
		if len(forwarded) > 0 && !bytes.Equal(forwarded, []byte{5, 1, 0}) {
			t.Fatalf("malformed data escaped: %x", forwarded)
		}
	}
}

func TestSOCKSConnectGatePropagatesForwardFailure(t *testing.T) {
	conn := newGuardTestConn([]byte{5, 1, 0}, 1)
	want := io.ErrClosedPipe
	if err := guardSOCKSConnect(conn, func([]byte) error { return want }); !errors.Is(err, want) {
		t.Fatalf("missing forward error: %v", err)
	}
}

func FuzzSOCKSConnectGate(f *testing.F) {
	f.Add([]byte{5, 1, 0, 5, 1, 0, 1, 127, 0, 0, 1, 0, 53})
	f.Add([]byte{5, 1, 0, 5, 3, 0, 1, 127, 0, 0, 1, 0, 53})
	f.Add([]byte{5, 1, 0, 5, 2, 0, 1})
	f.Fuzz(func(t *testing.T, data []byte) {
		conn := newGuardTestConn(data, 1)
		var forwarded [][]byte
		_ = guardSOCKSConnect(conn, func(b []byte) error {
			forwarded = append(forwarded, append([]byte(nil), b...))
			return nil
		})
		if len(forwarded) > 2 {
			t.Fatal("forwarded more than greeting + CONNECT")
		}
		if len(forwarded) == 2 && (len(forwarded[1]) < 4 || forwarded[1][1] != 1) {
			t.Fatal("forwarded forbidden SOCKS command")
		}
	})
}
