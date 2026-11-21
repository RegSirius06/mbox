package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RegSirius06/mbox/common/dgram"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

// Real loopback HTTP/3 and RFC 9221 frames, with an opaque fixture at the
// origin Unix boundary. The actual server-side association is tested separately.
func TestDatagramHTTP3RoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		enabled, origin, wrongTLS bool
	}{
		{"enabled", true, true, false}, {"old-gateway", false, false, false}, {"missing-origin", true, false, false}, {"wrong-tls", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "mbox-h3-dgram-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			path := filepath.Join(dir, "origin.sock")
			ca := makeTestQUICCA(t)
			trusted := ca
			if tc.wrongTLS {
				trusted = makeTestQUICCA(t)
			}
			caPath := filepath.Join(dir, "ca.pem")
			if err = os.WriteFile(caPath, trusted.pem, 0600); err != nil {
				t.Fatal(err)
			}
			up, down := [32]byte{1}, [32]byte{2}
			sid := "0123456789abcdef0123456789abcdef"
			attach := dgram.Attach(up, sid)
			ack := dgram.Proof(down, sid, "accept")
			originDone := make(chan error, 1)
			if tc.origin {
				ln, err := dgram.ListenPrivate(path)
				if err != nil {
					t.Fatal(err)
				}
				defer ln.Close()
				go func() {
					c, e := ln.Accept()
					if e != nil {
						originDone <- e
						return
					}
					defer c.Close()
					_ = c.SetDeadline(time.Now().Add(10 * time.Second))
					var a [dgram.AttachSize]byte
					if _, e = io.ReadFull(c, a[:]); e != nil {
						originDone <- e
						return
					}
					if !bytes.Equal(a[:], attach) {
						originDone <- io.ErrUnexpectedEOF
						return
					}
					if e = dgram.WriteFull(c, ack[:]); e != nil {
						originDone <- e
						return
					}
					p, e := dgram.ReadPacket(c)
					if e != nil {
						originDone <- e
						return
					}
					rx, _ := dgram.NewRecord(up, sid, "up")
					plain, e := rx.Open(p)
					if e != nil {
						originDone <- e
						return
					}
					tx, _ := dgram.NewRecord(down, sid, "down")
					out, e := tx.Seal(plain)
					if e == nil {
						e = dgram.WritePacket(c, out)
					}
					originDone <- e
					// Keep the lane open until peer closes; don't race the datagram send
					// against teardown of the dedicated connection.
					var stop [1]byte
					_, _ = c.Read(stop[:])
				}()
			}
			packet, err := net.ListenPacket("udp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer packet.Close()
			paths := originPaths{open: "/session", up: "/up/", down: "/down/", close: "/close/"}
			h := http.Handler(http.NotFoundHandler())
			if tc.enabled {
				h = wrapDatagramServer(h, paths, path)
			}
			srv := &http3.Server{TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{makeTestQUICLeaf(t, ca, net.ParseIP("127.0.0.1"), false)}}, Handler: h, EnableDatagrams: tc.enabled, IdleTimeout: 30 * time.Second}
			if tc.enabled {
				srv.ConnContext = datagramConnContext
			}
			go func() { _ = srv.Serve(packet) }()
			defer srv.Close()
			_, tr, err := quicHTTPClient(caPath)
			if err != nil {
				t.Fatal(err)
			}
			defer tr.Close()
			remote, _ := url.Parse("https://" + packet.LocalAddr().String())
			local, gateway := net.Pipe()
			defer local.Close()
			defer gateway.Close()
			_ = local.SetDeadline(time.Now().Add(12 * time.Second))
			done := make(chan struct{})
			go func() { runClientDatagram(gateway, remote, paths, tr); close(done) }()
			if err = dgram.WriteFull(local, attach); err != nil {
				t.Fatal(err)
			}
			var gotAck [32]byte
			_, err = io.ReadFull(local, gotAck[:])
			if !tc.enabled || !tc.origin || tc.wrongTLS {
				if err == nil {
					t.Fatal("unexpected negotiation")
				}
			} else {
				if err != nil || gotAck != ack {
					t.Fatal("bad authenticated negotiation", err)
				}
				tx, _ := dgram.NewRecord(up, sid, "up")
				payload := []byte("opaque UDP fixture")
				p, _ := tx.Seal(payload)
				if err = dgram.WritePacket(local, p); err != nil {
					t.Fatal(err)
				}
				got, err := dgram.ReadPacket(local)
				if err != nil {
					t.Fatal(err)
				}
				rx, _ := dgram.NewRecord(down, sid, "down")
				plain, err := rx.Open(got)
				if err != nil || !bytes.Equal(plain, payload) {
					t.Fatal("bad authenticated down datagram", err)
				}
				select {
				case e := <-originDone:
					if e != nil {
						t.Fatal(e)
					}
				case <-time.After(time.Second):
					t.Fatal("origin fixture did not finish")
				}
			}
			local.Close()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("gateway lane leaked")
			}
		})
	}
}

type tooLargeTestStream struct {
	sends atomic.Int32
	done  chan struct{}
	once  sync.Once
}

func (s *tooLargeTestStream) Read([]byte) (int, error) {
	<-s.done
	return 0, io.EOF
}
func (s *tooLargeTestStream) Write(p []byte) (int, error) { return len(p), nil }
func (s *tooLargeTestStream) SendDatagram(p []byte) error {
	s.sends.Add(1)
	if len(p) > 100 {
		return &quic.DatagramTooLargeError{MaxDatagramPayloadSize: 100}
	}
	return nil
}
func (s *tooLargeTestStream) ReceiveDatagram(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.done:
		return nil, io.EOF
	}
}
func (s *tooLargeTestStream) SetDeadline(time.Time) error { return nil }
func (s *tooLargeTestStream) CancelRead(quic.StreamErrorCode) {
	s.once.Do(func() { close(s.done) })
}
func (s *tooLargeTestStream) CancelWrite(quic.StreamErrorCode) {
	s.once.Do(func() { close(s.done) })
}

func TestDatagramTooLargeRetiresLaneForStreamFallback(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	stream := &tooLargeTestStream{done: make(chan struct{})}
	var closed atomic.Bool
	done := make(chan struct{})
	go func() {
		bridgeDatagrams("TEST", context.Background(), local, stream, func() { closed.Store(true) })
		close(done)
	}()
	// Supply a real authenticated record that fits the local framing cap
	// but exceeds the mock QUIC transport's current payload limit.
	tx, err := dgram.NewRecord([32]byte{1}, "0123456789abcdef0123456789abcdef", "up")
	if err != nil {
		t.Fatal(err)
	}
	packet, err := tx.Seal(bytes.Repeat([]byte{1}, 128))
	if err != nil {
		t.Fatal(err)
	}
	if len(packet) <= 100 || len(packet) > dgram.MaxPacket {
		t.Fatalf("invalid MTU test fixture length: %d", len(packet))
	}
	_ = peer.SetWriteDeadline(time.Now().Add(time.Second))
	if err := dgram.WritePacket(peer, packet); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("oversized DATAGRAM did not retire lane")
	}
	if !closed.Load() {
		t.Fatal("QUIC connection was not closed")
	}
	if got := stream.sends.Load(); got != 1 {
		t.Fatalf("SendDatagram calls = %d, want exactly 1", got)
	}
}

func TestDatagramExitReasonIsBoundedAndSecretFree(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, "EOF"},
		{io.EOF, "EOF"},
		{context.Canceled, "CANCELED"},
		{context.DeadlineExceeded, "DEADLINE"},
	} {
		if got := datagramExitReason(tc.err); got != tc.want {
			t.Fatalf("datagramExitReason(%v) = %q; want %q", tc.err, got, tc.want)
		}
	}
}
