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
}

type runtimeConfig struct {
	Transport runtimeTransportConfig `json:"transport"`
	Crypto    runtimeCryptoConfig    `json:"crypto"`
}

var clientID string

// runtimeConfigPath keeps deployment details out of the binary. Android always
// passes -config explicitly; /etc/mbox/client.json is only the conventional CLI
// default for a system installation.
func runtimeConfigPath(defaultPath string) string {
	if v := os.Getenv("MBOX_CONFIG"); v != "" {
		return v
	}
	if len(os.Args) >= 3 && os.Args[1] == "-config" {
		return os.Args[2]
	}
	return defaultPath
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
	u, err := url.Parse(cfg.Transport.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("invalid transport.url %q", cfg.Transport.URL)
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return fmt.Errorf("transport.url must not contain userinfo, query, or fragment")
	}
	if strings.HasSuffix(cfg.Transport.URL, "/") {
		return fmt.Errorf("transport.url must not end with /")
	}

	if cfg.Crypto.PSKFile == "" {
		return fmt.Errorf("crypto.psk_file is required")
	}
	if cfg.Crypto.ServerPublicKeyFile == "" {
		return fmt.Errorf("crypto.server_public_key_file is required")
	}
	if cfg.Crypto.ClientID != "" && !validClientID(cfg.Crypto.ClientID) {
		return fmt.Errorf("invalid crypto.client_id %q", cfg.Crypto.ClientID)
	}

	listen = cfg.Transport.Listen
	base = cfg.Transport.URL
	clientID = cfg.Crypto.ClientID
	pskFile = cfg.Crypto.PSKFile
	serverPubFile = cfg.Crypto.ServerPublicKeyFile

	return loadCamouflageConfig(path)
}
