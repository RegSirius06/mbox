package main

import (
	"net/url"
	"os"
	"testing"
)

func TestAndroidQUICUnixFailClosed(t *testing.T) {
	t.Setenv("MBOX_QUIC_UNIX", "/data/user/0/io.mbox.android/cache/quic.sock")
	t.Setenv("MBOX_DEVELOPER_MODE", "")
	u, _ := url.Parse("http://127.0.0.1:18451")
	if _, _, err := androidQUICUnixFromEnv(u); err == nil {
		t.Fatal("must reject without developer gate")
	}
	t.Setenv("MBOX_DEVELOPER_MODE", "1")
	bad, _ := url.Parse("http://remote.example:18451")
	if _, _, err := androidQUICUnixFromEnv(bad); err == nil {
		t.Fatal("must reject nonloopback HTTP")
	}
	t.Setenv("MBOX_HTTP_BRIDGE_ADDR", "127.0.0.1:1234")
	if _, _, err := androidQUICUnixFromEnv(u); err == nil {
		t.Fatal("must reject ambiguous transport")
	}
	os.Unsetenv("MBOX_HTTP_BRIDGE_ADDR")
	if tr, ok, err := androidQUICUnixFromEnv(u); err != nil || !ok || tr == nil {
		t.Fatalf("expected private Unix transport: %v", err)
	}
}
