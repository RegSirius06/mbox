package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RegSirius06/mbox/common/dgram"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

// HTTP Datagrams are associated with an explicitly negotiated CONNECT request.
// Reliable stream data carries exactly one attach/accept, never user UDP.
// The gateway never receives the Noise split secrets.
type datagramHTTPStream interface {
	io.ReadWriter
	SendDatagram([]byte) error
	ReceiveDatagram(context.Context) ([]byte, error)
	SetDeadline(time.Time) error
	CancelRead(quic.StreamErrorCode)
	CancelWrite(quic.StreamErrorCode)
}

const maxGatewayLanes = 32

func stopDgramStream(s datagramHTTPStream) { s.CancelRead(0); s.CancelWrite(0) }

// Logs only the error class, not destinations, Noise session IDs or payloads.
func datagramExitReason(err error) string {
	switch {
	case err == nil, errors.Is(err, io.EOF):
		return "EOF"
	case errors.Is(err, context.Canceled):
		return "CANCELED"
	case errors.Is(err, context.DeadlineExceeded):
		return "DEADLINE"
	default:
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return "IO_TIMEOUT"
		}
		return fmt.Sprintf("%T", err)
	}
}

func bridgeDatagrams(role string, parent context.Context, c net.Conn, s datagramHTTPStream, closeQUIC func()) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	start := time.Now()
	var once sync.Once
	report := func(source string, err error) {
		once.Do(func() {
			log.Printf("DATAGRAM code=%s_LANE_EXIT source=%s reason=%s elapsed_ms=%d", role, source, datagramExitReason(err), time.Since(start).Milliseconds())
			cancel()
		})
	}
	var wg sync.WaitGroup
	wg.Add(3)
	// A control-stream FIN/reset, local socket error or QUIC shutdown retires
	// both halves of the lane. Never attempt a second attach in this session.
	end := func() { cancel(); _ = c.Close(); stopDgramStream(s); closeQUIC() }
	go func() {
		defer wg.Done()
		var b [1]byte
		_, err := s.Read(b[:])
		report("CONTROL_STREAM", err)
	}()
	go func() {
		defer wg.Done()
		for {
			p, err := dgram.ReadPacket(c)
			if err != nil {
				report("UNIX_READ", err)
				return
			}
			if err = s.SendDatagram(p); err != nil {
				var tooLarge *quic.DatagramTooLargeError
				if errors.As(err, &tooLarge) {
					log.Printf("DATAGRAM code=%s_MTU_RETIRED", role)
				}
				report("QUIC_SEND", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for {
			p, err := s.ReceiveDatagram(ctx)
			if err != nil {
				report("QUIC_RECV", err)
				return
			}
			if len(p) < 25 || len(p) > dgram.MaxPacket {
				continue
			}
			_ = c.SetWriteDeadline(time.Now().Add(dgram.WriteTimeout))
			if err = dgram.WritePacket(c, p); err != nil {
				report("UNIX_WRITE", err)
				return
			}
		}
	}()
	<-ctx.Done()
	report("PARENT_CONTEXT", context.Cause(parent))
	end()
	wg.Wait()
}

type dgramConnKey struct{}
type dgramServerConn struct {
	conn *quic.Conn
	used atomic.Bool
}

func datagramConnContext(ctx context.Context, c *quic.Conn) context.Context {
	return context.WithValue(ctx, dgramConnKey{}, &dgramServerConn{conn: c})
}

func wrapDatagramServer(normal http.Handler, paths originPaths, originUnix string) http.Handler {
	slots := make(chan struct{}, maxGatewayLanes)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Proto != dgram.Protocol || r.URL == nil || r.URL.Path != paths.open || r.URL.EscapedPath() != r.URL.Path || r.URL.RawQuery != "" {
			normal.ServeHTTP(w, r)
			return
		}
		settings, ok := w.(http3.Settingser)
		streamer, ok2 := w.(http3.HTTPStreamer)
		if !ok || !ok2 {
			log.Print("DATAGRAM code=GATEWAY_STREAM_UNAVAILABLE")
			http.NotFound(w, r)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			log.Print("DATAGRAM code=GATEWAY_CAPACITY")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		dc, ok := r.Context().Value(dgramConnKey{}).(*dgramServerConn)
		if !ok || !dc.used.CompareAndSwap(false, true) {
			log.Print("DATAGRAM code=GATEWAY_CONNECTION_UNAVAILABLE")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		var closeOnce sync.Once
		closeQUIC := func() { closeOnce.Do(func() { _ = dc.conn.CloseWithError(0, "") }) }
		defer closeQUIC()
		ctx, cancel := context.WithTimeout(r.Context(), dgram.SetupTimeout)
		defer cancel()
		select {
		case <-ctx.Done():
			return
		case <-settings.ReceivedSettings():
		}
		if settings.Settings() == nil || !settings.Settings().EnableDatagrams {
			log.Print("DATAGRAM code=GATEWAY_SETTINGS_UNSUPPORTED")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		local, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", originUnix)
		if err != nil {
			log.Print("DATAGRAM code=GATEWAY_ORIGIN_UNAVAILABLE")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		defer local.Close()
		deadline, _ := ctx.Deadline()
		_ = local.SetDeadline(deadline)
		if http.NewResponseController(w).SetWriteDeadline(deadline) != nil {
			return
		}
		w.WriteHeader(http.StatusOK)
		stream, err := streamer.HTTPStream()
		if err != nil {
			return
		}
		defer stopDgramStream(stream)
		_ = stream.SetDeadline(deadline)
		var attach [dgram.AttachSize]byte
		if _, err = io.ReadFull(stream, attach[:]); err != nil || dgram.WriteFull(local, attach[:]) != nil {
			return
		}
		var proof [dgram.ProofSize]byte
		if _, err = io.ReadFull(local, proof[:]); err != nil || dgram.WriteFull(stream, proof[:]) != nil {
			log.Print("DATAGRAM code=GATEWAY_ORIGIN_ATTACH_REJECTED")
			return
		}
		log.Print("DATAGRAM code=GATEWAY_ATTACHED")
		if local.SetDeadline(time.Time{}) != nil || stream.SetDeadline(time.Time{}) != nil {
			return
		}
		bridgeDatagrams("GATEWAY", r.Context(), local, stream, closeQUIC)
	})
}

// The app-private listener exists only with explicit --datagrams. The new
// connection uses the exact existing TLS roots/name and protected UDP dialer.
func startClientDatagrams(path string, remote *url.URL, paths originPaths, tr *http3.Transport) (*net.UnixListener, error) {
	ln, err := dgram.ListenPrivate(path)
	if err != nil {
		return nil, err
	}
	slot := make(chan struct{}, 1)
	go func() {
		for {
			c, err := ln.AcceptUnix()
			if err != nil {
				return
			}
			select {
			case slot <- struct{}{}:
				go func() { defer func() { <-slot }(); runClientDatagram(c, remote, paths, tr) }()
			default:
				c.Close()
			}
		}
	}()
	return ln, nil
}

func runClientDatagram(local net.Conn, remote *url.URL, paths originPaths, tr *http3.Transport) {
	defer local.Close()
	ctx, cancel := context.WithTimeout(context.Background(), dgram.SetupTimeout)
	defer cancel()
	deadline, _ := ctx.Deadline()
	_ = local.SetDeadline(deadline)
	var attach [dgram.AttachSize]byte
	if _, err := io.ReadFull(local, attach[:]); err != nil {
		log.Print("DATAGRAM code=CLIENT_LOCAL_ATTACH_FAILED")
		return
	}
	tlsConfig := tr.TLSClientConfig.Clone()
	tlsConfig.NextProtos = []string{http3.NextProtoH3}
	if tlsConfig.ServerName == "" {
		tlsConfig.ServerName = remote.Hostname()
	}
	// No periodic PINGs: the lane expires on idle. The core reattaches only
	// when real UDP arrives, with fresh per-epoch authenticated AEAD keys.
	cfg := &quic.Config{EnableDatagrams: true, HandshakeIdleTimeout: 5 * time.Second, MaxIdleTimeout: 45 * time.Second}
	dial := tr.Dial
	if runtime.GOOS == "android" && dial == nil {
		log.Print("DATAGRAM code=CLIENT_PROTECTED_DIAL_UNAVAILABLE")
		return
	}
	if dial == nil {
		dial = func(ctx context.Context, addr string, t *tls.Config, c *quic.Config) (*quic.Conn, error) {
			return quic.DialAddr(ctx, addr, t, c)
		}
	}
	conn, err := dial(ctx, remote.Host, tlsConfig, cfg)
	if err != nil {
		log.Print("DATAGRAM code=CLIENT_QUIC_DIAL_FAILED")
		return
	}
	var closeOnce sync.Once
	closeQUIC := func() { closeOnce.Do(func() { _ = conn.CloseWithError(0, "") }) }
	defer closeQUIC()
	if !conn.ConnectionState().SupportsDatagrams.Remote {
		log.Print("DATAGRAM code=CLIENT_REMOTE_DATAGRAM_UNSUPPORTED")
		return
	}
	h3 := (&http3.Transport{EnableDatagrams: true, DisableCompression: true, MaxResponseHeaderBytes: 32 << 10}).NewClientConn(conn)
	select {
	case <-ctx.Done():
		return
	case <-conn.Context().Done():
		return
	case <-h3.ReceivedSettings():
	}
	settings := h3.Settings()
	if settings == nil || !settings.EnableDatagrams || !settings.EnableExtendedConnect {
		log.Print("DATAGRAM code=CLIENT_H3_SETTINGS_UNSUPPORTED")
		return
	}
	str, err := h3.OpenRequestStream(ctx)
	if err != nil {
		return
	}
	defer stopDgramStream(str)
	_ = str.SetDeadline(deadline)
	dest := *remote
	dest.Path = paths.open
	dest.RawPath = ""
	dest.RawQuery = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodConnect, dest.String(), nil)
	if err != nil {
		return
	}
	req.Proto = dgram.Protocol
	if str.SendRequestHeader(req) != nil {
		return
	}
	resp, err := str.ReadResponse()
	if err != nil {
		return
	}
	if resp.StatusCode != http.StatusOK {
		log.Printf("DATAGRAM code=CLIENT_CONNECT_REJECTED status=%d", resp.StatusCode)
		return
	}
	if dgram.WriteFull(str, attach[:]) != nil {
		return
	}
	var proof [dgram.ProofSize]byte
	if _, err = io.ReadFull(str, proof[:]); err != nil || dgram.WriteFull(local, proof[:]) != nil {
		log.Print("DATAGRAM code=CLIENT_ORIGIN_PROOF_UNAVAILABLE")
		return
	}
	log.Print("DATAGRAM code=CLIENT_ATTACHED")
	if str.SetDeadline(time.Time{}) != nil || local.SetDeadline(time.Time{}) != nil {
		return
	}
	bridgeDatagrams("CLIENT", conn.Context(), local, str, closeQUIC)
}
