package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReleaseUDPIsIndependentOfDeveloperMode(t *testing.T) {
	t.Setenv("MBOX_DEVELOPER_MODE", "")
	oldArgs := os.Args
	os.Args = []string{"mbox-client"}
	defer func() { os.Args = oldArgs }()

	temp := t.TempDir()
	p := filepath.Join(temp, "config.json")
	base := runtimeConfig{
		Transport: runtimeTransportConfig{Listen: "127.0.0.1:10890", URL: "https://example.org/edge", EnableUDP: true},
		Crypto:    runtimeCryptoConfig{ClientID: "test-client", PSKFile: "/tmp/psk", ServerPublicKeyFile: "/tmp/pub"},
	}
	write := func(c runtimeConfig) {
		t.Helper()
		data, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(base)
	if err := loadRuntimeConfig(p); err != nil {
		t.Fatalf("authenticated UDP should work in release: %v", err)
	}
	if !enableUDPClient {
		t.Fatal("production UDP gate did not enable forwarding")
	}
	base.Transport.EnableUDP = false
	base.Transport.EnableUDPLab = true
	write(base)
	if err := loadRuntimeConfig(p); err == nil {
		t.Fatal("legacy UDP laboratory bypassed developer-mode guard")
	}
	base.Transport.EnableUDPLab = false
	base.Diagnostics.NetworkDebug = true
	write(base)
	if err := loadRuntimeConfig(p); err == nil {
		t.Fatal("production network_debug bypassed developer-mode guard")
	}
}
