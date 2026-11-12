package main

import (
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"testing"
)

func TestAndroidQUICUnixReleaseFailClosed(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "quic.sock")
	t.Setenv("MBOX_QUIC_UNIX", socket)
	t.Setenv("MBOX_DEVELOPER_MODE", "")
	t.Setenv("MBOX_HTTP_BRIDGE_ADDR", "")
	u, _ := url.Parse("http://127.0.0.1:18451")
	if tr, ok, err := androidQUICUnixFromEnv(u); err != nil || !ok || tr == nil {
		t.Fatalf("release build must allow app-private QUIC Unix transport: %v", err)
	}

	t.Setenv("MBOX_QUIC_UNIX", "relative/quic.sock")
	if _, _, err := androidQUICUnixFromEnv(u); err == nil {
		t.Fatal("must reject relative socket path")
	}
	t.Setenv("MBOX_QUIC_UNIX", filepath.Dir(socket)+"/../quic.sock")
	if _, _, err := androidQUICUnixFromEnv(u); err == nil {
		t.Fatal("must reject non-canonical socket path")
	}
	t.Setenv("MBOX_QUIC_UNIX", socket)
	bad, _ := url.Parse("http://remote.example:18451")
	if _, _, err := androidQUICUnixFromEnv(bad); err == nil {
		t.Fatal("must reject non-loopback HTTP")
	}
	https, _ := url.Parse("https://127.0.0.1:18451")
	if _, _, err := androidQUICUnixFromEnv(https); err == nil {
		t.Fatal("must reject HTTPS bypass instead of local QUIC")
	}
	t.Setenv("MBOX_HTTP_BRIDGE_ADDR", "127.0.0.1:1234")
	if _, _, err := androidQUICUnixFromEnv(u); err == nil {
		t.Fatal("must reject ambiguous QUIC and Cronet transports")
	}
}

func TestAndroidQUICUnixReleaseRoundTrip(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "quic.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte("via-unix"))
	})}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close(); <-done })

	t.Setenv("MBOX_DEVELOPER_MODE", "")
	t.Setenv("MBOX_HTTP_BRIDGE_ADDR", "")
	t.Setenv("MBOX_QUIC_UNIX", socket)
	client, err := newHTTPClient("http://127.0.0.1:18451")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get("http://127.0.0.1:18451/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || string(body) != "via-unix" {
		t.Fatalf("expected local QUIC Unix bridge, got status=%d body=%q", resp.StatusCode, body)
	}
}
