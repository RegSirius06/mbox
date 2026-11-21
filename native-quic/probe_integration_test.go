package main

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"
)

// Real HTTP/3/TLS and the unchanged beta.4.3 gateway handler. Even a
// successful probe must not forward a single request to the origin.
func TestProbeHTTP3OldGatewayDoesNotTouchOrigin(t *testing.T) {
	var originCalls atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer origin.Close()
	originURL, err := url.Parse(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	ca := makeTestQUICCA(t)
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, ca.pem, 0600); err != nil {
		t.Fatal(err)
	}
	foreign := makeTestQUICCA(t)
	for _, tc := range []struct {
		name            string
		ca              testQUICCA
		expired, wantOK bool
	}{
		{"trusted", ca, false, true},
		{"foreign-ca", foreign, false, false},
		{"expired", ca, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			packet, err := net.ListenPacket("udp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer packet.Close()
			paths := originPaths{"/open", "/up/", "/down/", "/close/"}
			srv := &http3.Server{
				TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{makeTestQUICLeaf(t, tc.ca, net.ParseIP("127.0.0.1"), tc.expired)}},
				Handler:   makeServerHandler(originURL, paths, make([]byte, 32), localHTTPClient()),
			}
			go func() { _ = srv.Serve(packet) }()
			defer srv.Close()
			client, tr, err := quicHTTPClient(caPath)
			if err != nil {
				t.Fatal(err)
			}
			defer tr.Close()
			endpoint, err := url.Parse("https://" + packet.LocalAddr().String())
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			if err := probeHTTP3(ctx, client, endpoint); (err == nil) != tc.wantOK {
				t.Fatalf("probe result %v, want success %t", err, tc.wantOK)
			}
		})
	}
	if originCalls.Load() != 0 {
		t.Fatalf("probe made %d requests to origin", originCalls.Load())
	}
}
