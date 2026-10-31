package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
)

type runtimeTransportConfig struct {
	Listen   string `json:"listen"`
	Upstream string `json:"upstream"`
	BasePath string `json:"base_path"`
}

type runtimeClientConfig struct {
	PSKFile string `json:"psk_file,omitempty"`
	PSK     string `json:"psk,omitempty"`
}

type runtimeCryptoConfig struct {
	// PSKFile is the optional legacy v1 PSK. Leave it empty to disable v1 OPEN.
	PSKFile              string                         `json:"psk_file"`
	ServerPrivateKeyFile string                         `json:"server_private_key_file"`
	Clients              map[string]runtimeClientConfig `json:"clients"`
}

type runtimeConfig struct {
	Transport runtimeTransportConfig `json:"transport"`
	Crypto    runtimeCryptoConfig    `json:"crypto"`
	Cover     coverConfig            `json:"cover"`
}

var (
	basePath     string
	clientAuthMu sync.RWMutex
	clientPSKs   = map[string][]byte{}
	coverCfg     coverConfig
)

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

	if cfg.Transport.Upstream == "" {
		return fmt.Errorf("transport.upstream is required")
	}
	if _, _, err := net.SplitHostPort(cfg.Transport.Upstream); err != nil {
		return fmt.Errorf("invalid transport.upstream %q: %w", cfg.Transport.Upstream, err)
	}

	if cfg.Transport.BasePath == "" || cfg.Transport.BasePath[0] != '/' {
		return fmt.Errorf("transport.base_path must begin with /")
	}
	if cfg.Transport.BasePath != "/" && strings.HasSuffix(cfg.Transport.BasePath, "/") {
		return fmt.Errorf("transport.base_path must not end with /")
	}
	if strings.ContainsAny(cfg.Transport.BasePath, "?#") {
		return fmt.Errorf("invalid transport.base_path %q", cfg.Transport.BasePath)
	}

	if cfg.Crypto.ServerPrivateKeyFile == "" {
		return fmt.Errorf("crypto.server_private_key_file is required")
	}
	clients, err := decodeClientAuth(cfg.Crypto.Clients)
	if err != nil {
		return err
	}
	cov, err := normalizeCoverConfig(cfg.Cover)
	if err != nil {
		return err
	}

	listen = cfg.Transport.Listen
	socksAddr = cfg.Transport.Upstream
	basePath = cfg.Transport.BasePath
	pskFile = cfg.Crypto.PSKFile
	serverPrivFile = cfg.Crypto.ServerPrivateKeyFile
	coverCfg = cov
	clientAuthMu.Lock()
	clientPSKs = clients
	clientAuthMu.Unlock()

	return loadCamouflageConfig(path)
}

func decodeClientAuth(in map[string]runtimeClientConfig) (map[string][]byte, error) {
	out := make(map[string][]byte, len(in))
	for id, client := range in {
		if !validClientID(id) {
			return nil, fmt.Errorf("invalid crypto.clients entry %q", id)
		}
		if (client.PSKFile == "") == (client.PSK == "") {
			return nil, fmt.Errorf("crypto.clients[%q] must set exactly one of psk or psk_file", id)
		}
		var raw []byte
		var err error
		if client.PSK != "" {
			raw, err = hex.DecodeString(strings.TrimSpace(client.PSK))
			if err != nil || len(raw) != 32 {
				return nil, fmt.Errorf("crypto.clients[%q].psk must be 32-byte hex", id)
			}
		} else {
			raw, err = readHex32(client.PSKFile)
			if err != nil {
				return nil, fmt.Errorf("crypto.clients[%q]: %w", id, err)
			}
		}
		out[id] = append([]byte(nil), raw...)
	}
	return out, nil
}

func reloadClientAuth(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cfg runtimeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}
	clients, err := decodeClientAuth(cfg.Crypto.Clients)
	if err != nil {
		return err
	}
	clientAuthMu.Lock()
	clientPSKs = clients
	clientAuthMu.Unlock()
	return nil
}
