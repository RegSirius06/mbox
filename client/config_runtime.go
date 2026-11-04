package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

type runtimeTransportConfig struct {
	Listen string `json:"listen"`
	URL    string `json:"url"`
}

type runtimeCryptoConfig struct {
	ClientID            string `json:"client_id"`
	PSKFile             string `json:"psk_file"`
	ServerPublicKeyFile string `json:"server_public_key_file"`
	DeviceID            string `json:"device_id,omitempty"`
}

type runtimeDiagnosticsConfig struct {
	NetworkDebug           bool `json:"network_debug"`
	DeterministicTransport bool `json:"deterministic_transport"`
}

type runtimeConfig struct {
	Transport   runtimeTransportConfig   `json:"transport"`
	Crypto      runtimeCryptoConfig      `json:"crypto"`
	Diagnostics runtimeDiagnosticsConfig `json:"diagnostics"`
}

var (
	clientID               string
	deviceID               string
	networkDebug           bool
	deterministicTransport bool
)

func runtimeArgEnabled(name string) bool {
	for _, arg := range os.Args[1:] {
		if arg == name {
			return true
		}
	}
	return false
}

func developerModeEnabled() bool {
	return runtimeArgEnabled("--developer-mode") || os.Getenv("MBOX_DEVELOPER_MODE") == "1"
}

func allowInsecureHTTPEnabled() bool {
	return runtimeArgEnabled("--allow-insecure-http") || os.Getenv("MBOX_ALLOW_INSECURE_HTTP") == "1"
}

// runtimeConfigPath keeps deployment details out of the binary. Android always
// passes -config explicitly; /etc/mbox/client.json is only the conventional CLI
// default for a system installation.
func runtimeConfigPath(defaultPath string) string {
	if v := os.Getenv("MBOX_CONFIG"); v != "" {
		return v
	}
	for i := 1; i+1 < len(os.Args); i++ {
		if os.Args[i] == "-config" || os.Args[i] == "--config" {
			return os.Args[i+1]
		}
	}
	return defaultPath
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validateTransportURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("invalid transport.url %q", raw)
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return fmt.Errorf("transport.url must not contain userinfo, query, or fragment")
	}
	if strings.HasSuffix(raw, "/") {
		return fmt.Errorf("transport.url must not end with /")
	}
	if u.Scheme == "http" && !loopbackHost(u.Hostname()) {
		// Security invariant: plaintext remote transport is never an accidental
		// production downgrade. It requires two explicit developer-only gates.
		if !developerModeEnabled() || !allowInsecureHTTPEnabled() {
			return fmt.Errorf("remote HTTP transport requires --developer-mode and --allow-insecure-http")
		}
	}
	return nil
}

func loadRuntimeConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var cfg runtimeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}

	if cfg.Transport.Listen == "" {
		return fmt.Errorf("transport.listen is required")
	}
	if _, _, err := net.SplitHostPort(cfg.Transport.Listen); err != nil {
		return fmt.Errorf("invalid transport.listen %q: %w", cfg.Transport.Listen, err)
	}

	if cfg.Transport.URL == "" {
		return fmt.Errorf("transport.url is required")
	}
	if err := validateTransportURL(cfg.Transport.URL); err != nil {
		return err
	}

	if cfg.Crypto.PSKFile == "" {
		return fmt.Errorf("crypto.psk_file is required")
	}
	if cfg.Crypto.ServerPublicKeyFile == "" {
		return fmt.Errorf("crypto.server_public_key_file is required")
	}
	if !validClientID(cfg.Crypto.ClientID) {
		return fmt.Errorf("Noise-only MBox requires valid crypto.client_id, got %q", cfg.Crypto.ClientID)
	}
	if (cfg.Diagnostics.NetworkDebug || cfg.Diagnostics.DeterministicTransport) && !developerModeEnabled() {
		// Security invariant: diagnostic modes can expose metadata or create
		// reproducible traffic patterns and therefore fail closed in production.
		return fmt.Errorf("diagnostic transport options require --developer-mode")
	}

	listen = cfg.Transport.Listen
	base = cfg.Transport.URL
	clientID = cfg.Crypto.ClientID
	deviceID = strings.ToLower(strings.TrimSpace(os.Getenv("MBOX_DEVICE_ID")))
	if deviceID == "" {
		deviceID = strings.ToLower(strings.TrimSpace(cfg.Crypto.DeviceID))
	}
	if deviceID != "" && !validDeviceID(deviceID) {
		return fmt.Errorf("crypto.device_id must be 32 hex characters when set")
	}
	networkDebug = cfg.Diagnostics.NetworkDebug
	deterministicTransport = cfg.Diagnostics.DeterministicTransport
	pskFile = cfg.Crypto.PSKFile
	serverPubFile = cfg.Crypto.ServerPublicKeyFile

	return loadCamouflageConfig(path)
}
