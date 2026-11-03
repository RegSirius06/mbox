package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

type runtimeTransportConfig struct {
	Listen               string `json:"listen"`
	Upstream             string `json:"upstream"`
	BasePath             string `json:"base_path"`
	OriginHMACSecretFile string `json:"origin_hmac_secret_file"`
}

type runtimeClientConfig struct {
	PSKFile     string `json:"psk_file,omitempty"`
	PSK         string `json:"psk,omitempty"`
	MaxSessions int    `json:"max_sessions,omitempty"` // 0 = use server default, -1 = unlimited
	ExpiresAt   string `json:"expires_at,omitempty"`   // RFC3339; empty = no expiry
	InternalID  string `json:"internal_id,omitempty"`
}

type clientPolicy struct {
	PSK         []byte
	MaxSessions int
	ExpiresAt   time.Time
	AccountID   string
}

type resourceLimitsConfig struct {
	MaxSessionsGlobal           int `json:"max_sessions_global"`
	DefaultMaxSessionsPerClient int `json:"default_max_sessions_per_client"`
	MaxStreamsPerSession        int `json:"max_streams_per_session"`
	MaxTotalStreams             int `json:"max_total_streams"`
	MaxStreamOpensPerSecond     int `json:"max_stream_opens_per_second"`
	MaxSessionOpensPerMinute    int `json:"max_session_opens_per_minute"`
	MaxConcurrentHandshakes     int `json:"max_concurrent_handshakes"`
	MaxHandshakeAttemptsPerSec  int `json:"max_handshake_attempts_per_second"`
	MaxDownQueueBytes           int `json:"max_down_queue_bytes"`
	MaxDownReplayBytes          int `json:"max_down_replay_bytes"`
	GlobalQueuedBytes           int `json:"global_queued_bytes"`
}

var runtimeLimits = resourceLimitsConfig{
	MaxSessionsGlobal:           128,
	DefaultMaxSessionsPerClient: 4,
	MaxStreamsPerSession:        128,
	MaxTotalStreams:             1024,
	MaxStreamOpensPerSecond:     32,
	MaxSessionOpensPerMinute:    180,
	MaxConcurrentHandshakes:     32,
	MaxHandshakeAttemptsPerSec:  64,
	MaxDownQueueBytes:           4 * 1024 * 1024,
	MaxDownReplayBytes:          8 * 1024 * 1024,
	GlobalQueuedBytes:           64 * 1024 * 1024,
}

type runtimeCryptoConfig struct {
	ServerPrivateKeyFile string                         `json:"server_private_key_file"`
	Clients              map[string]runtimeClientConfig `json:"clients"`
}

type runtimeConfig struct {
	Transport runtimeTransportConfig `json:"transport"`
	Crypto    runtimeCryptoConfig    `json:"crypto"`
	Limits    resourceLimitsConfig   `json:"limits"`
	Cover     coverConfig            `json:"cover"`
}

var (
	basePath       string
	clientAuthMu   sync.RWMutex
	clientPolicies = map[string]clientPolicy{}
	coverCfg       coverConfig
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

func normalizeLimits(in resourceLimitsConfig) (resourceLimitsConfig, error) {
	out := runtimeLimits
	if in.MaxSessionsGlobal > 0 {
		out.MaxSessionsGlobal = in.MaxSessionsGlobal
	}
	if in.DefaultMaxSessionsPerClient != 0 {
		out.DefaultMaxSessionsPerClient = in.DefaultMaxSessionsPerClient
	}
	if in.MaxStreamsPerSession > 0 {
		out.MaxStreamsPerSession = in.MaxStreamsPerSession
	}
	if in.MaxTotalStreams > 0 {
		out.MaxTotalStreams = in.MaxTotalStreams
	}
	if in.MaxStreamOpensPerSecond > 0 {
		out.MaxStreamOpensPerSecond = in.MaxStreamOpensPerSecond
	}
	if in.MaxSessionOpensPerMinute > 0 {
		out.MaxSessionOpensPerMinute = in.MaxSessionOpensPerMinute
	}
	if in.MaxConcurrentHandshakes > 0 {
		out.MaxConcurrentHandshakes = in.MaxConcurrentHandshakes
	}
	if in.MaxHandshakeAttemptsPerSec > 0 {
		out.MaxHandshakeAttemptsPerSec = in.MaxHandshakeAttemptsPerSec
	}
	if in.MaxDownQueueBytes > 0 {
		out.MaxDownQueueBytes = in.MaxDownQueueBytes
	}
	if in.MaxDownReplayBytes > 0 {
		out.MaxDownReplayBytes = in.MaxDownReplayBytes
	}
	if in.GlobalQueuedBytes > 0 {
		out.GlobalQueuedBytes = in.GlobalQueuedBytes
	}
	if out.MaxSessionsGlobal < 1 || out.MaxSessionsGlobal > 100000 {
		return out, fmt.Errorf("limits.max_sessions_global out of range")
	}
	if out.DefaultMaxSessionsPerClient != -1 && (out.DefaultMaxSessionsPerClient < 1 || out.DefaultMaxSessionsPerClient > 10000) {
		return out, fmt.Errorf("limits.default_max_sessions_per_client must be -1 or 1..10000")
	}
	if out.DefaultMaxSessionsPerClient > 0 && out.DefaultMaxSessionsPerClient > out.MaxSessionsGlobal {
		return out, fmt.Errorf("limits.default_max_sessions_per_client must be <= max_sessions_global")
	}
	if out.MaxStreamsPerSession < 1 || out.MaxStreamsPerSession > 4096 {
		return out, fmt.Errorf("limits.max_streams_per_session out of range")
	}
	if out.MaxTotalStreams < out.MaxStreamsPerSession || out.MaxTotalStreams > 200000 {
		return out, fmt.Errorf("limits.max_total_streams out of range")
	}
	if out.MaxStreamOpensPerSecond < 1 || out.MaxStreamOpensPerSecond > 10000 {
		return out, fmt.Errorf("limits.max_stream_opens_per_second out of range")
	}
	if out.MaxSessionOpensPerMinute < 1 || out.MaxSessionOpensPerMinute > 100000 {
		return out, fmt.Errorf("limits.max_session_opens_per_minute out of range")
	}
	if out.MaxConcurrentHandshakes < 1 || out.MaxConcurrentHandshakes > 4096 {
		return out, fmt.Errorf("limits.max_concurrent_handshakes out of range")
	}
	if out.MaxHandshakeAttemptsPerSec < 1 || out.MaxHandshakeAttemptsPerSec > 100000 {
		return out, fmt.Errorf("limits.max_handshake_attempts_per_second out of range")
	}
	if out.MaxDownQueueBytes < 256*1024 || out.MaxDownQueueBytes > 256*1024*1024 {
		return out, fmt.Errorf("limits.max_down_queue_bytes out of range")
	}
	if out.MaxDownReplayBytes < 256*1024 || out.MaxDownReplayBytes > 256*1024*1024 {
		return out, fmt.Errorf("limits.max_down_replay_bytes out of range")
	}
	if out.GlobalQueuedBytes < 8*1024*1024 || out.GlobalQueuedBytes > 2*1024*1024*1024 {
		return out, fmt.Errorf("limits.global_queued_bytes out of range")
	}
	return out, nil
}

func privateKeyModeAllowed(mode os.FileMode) bool {
	perm := mode.Perm()
	// mboxctl intentionally installs server.key as root:mbox 0640 so the
	// unprivileged service can read it. Reject group write/execute and every
	// permission for other users; group read is the only allowed group bit.
	return perm&0o027 == 0
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
	if cfg.Transport.OriginHMACSecretFile == "" {
		return fmt.Errorf("transport.origin_hmac_secret_file is required")
	}
	originSecret, err := readHex32(cfg.Transport.OriginHMACSecretFile)
	if err != nil {
		return fmt.Errorf("transport.origin_hmac_secret_file: %w", err)
	}
	if info, statErr := os.Stat(cfg.Transport.OriginHMACSecretFile); statErr == nil && !privateKeyModeAllowed(info.Mode().Perm()) {
		return fmt.Errorf("transport.origin_hmac_secret_file permissions too open: %04o", info.Mode().Perm())
	}
	if cfg.Crypto.ServerPrivateKeyFile == "" {
		return fmt.Errorf("crypto.server_private_key_file is required")
	}
	serverKey, err := readHex32(cfg.Crypto.ServerPrivateKeyFile)
	if err != nil {
		return fmt.Errorf("crypto.server_private_key_file: %w", err)
	}
	if info, statErr := os.Stat(cfg.Crypto.ServerPrivateKeyFile); statErr == nil && !privateKeyModeAllowed(info.Mode().Perm()) {
		return fmt.Errorf("crypto.server_private_key_file permissions must be owner read/write with optional group read and no group-write/other access: %04o", info.Mode().Perm())
	}
	policies, err := decodeClientAuth(cfg.Crypto.Clients)
	if err != nil {
		return err
	}
	limits, err := normalizeLimits(cfg.Limits)
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
	serverPrivFile = cfg.Crypto.ServerPrivateKeyFile
	serverStaticPrivate = append(serverStaticPrivate[:0], serverKey...)
	originAuthSecret = append(originAuthSecret[:0], originSecret...)
	coverCfg = cov
	runtimeLimits = limits
	clientAuthMu.Lock()
	clientPolicies = policies
	clientAuthMu.Unlock()
	return loadCamouflageConfig(path)
}

func decodeClientAuth(in map[string]runtimeClientConfig) (map[string]clientPolicy, error) {
	out := make(map[string]clientPolicy, len(in))
	for id, c := range in {
		if !validClientID(id) {
			return nil, fmt.Errorf("invalid crypto.clients entry %q", id)
		}
		if (c.PSKFile == "") == (c.PSK == "") {
			return nil, fmt.Errorf("crypto.clients[%q] must set exactly one of psk or psk_file", id)
		}
		var raw []byte
		var err error
		if c.PSK != "" {
			raw, err = hex.DecodeString(strings.TrimSpace(c.PSK))
			if err != nil || len(raw) != 32 {
				return nil, fmt.Errorf("crypto.clients[%q].psk must be 32-byte hex", id)
			}
		} else {
			raw, err = readHex32(c.PSKFile)
			if err != nil {
				return nil, fmt.Errorf("crypto.clients[%q]: %w", id, err)
			}
		}
		accountID := c.InternalID
		if accountID == "" {
			accountID = id
		}
		if !validClientID(accountID) {
			return nil, fmt.Errorf("crypto.clients[%q].internal_id invalid", id)
		}
		p := clientPolicy{PSK: append([]byte(nil), raw...), MaxSessions: c.MaxSessions, AccountID: accountID}
		if c.ExpiresAt != "" {
			t, err := time.Parse(time.RFC3339, c.ExpiresAt)
			if err != nil {
				return nil, fmt.Errorf("crypto.clients[%q].expires_at: %w", id, err)
			}
			p.ExpiresAt = t
		}
		out[id] = p
	}
	return out, nil
}

func getClientPolicy(id string) (clientPolicy, bool) {
	clientAuthMu.RLock()
	defer clientAuthMu.RUnlock()
	p, ok := clientPolicies[id]
	if !ok {
		return clientPolicy{}, false
	}
	p.PSK = append([]byte(nil), p.PSK...)
	return p, true
}

func clientPolicyActive(p clientPolicy) bool {
	return p.ExpiresAt.IsZero() || time.Now().Before(p.ExpiresAt)
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
	policies, err := decodeClientAuth(cfg.Crypto.Clients)
	if err != nil {
		return err
	}
	clientAuthMu.Lock()
	clientPolicies = policies
	clientAuthMu.Unlock()
	// Drop rate-limit state for identities that no longer exist. This keeps
	// credential churn from retaining attacker/user-controlled map keys.
	sessionsMu.Lock()
	for id := range sessionOpenRates {
		if id == "legacy" {
			continue
		}
		if _, ok := policies[id]; !ok {
			delete(sessionOpenRates, id)
		}
	}
	sessionsMu.Unlock()
	revokeUnauthorizedSessions(policies)
	return nil
}
