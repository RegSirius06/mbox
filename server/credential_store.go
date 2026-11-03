package main

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	maxCredentialFileBytes = 64 * 1024
	maxCredentialFiles     = 1_000_000
)

type credentialFile struct {
	AuthHandle  string `json:"auth_handle"`
	LegacyID    string `json:"legacy_id,omitempty"`
	InternalID  string `json:"internal_id"`
	PSK         string `json:"psk"`
	MaxSessions int    `json:"max_sessions,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}

type credentialAdminRequest struct {
	Op         string `json:"op"`
	AuthHandle string `json:"auth_handle"`
}

var (
	credentialDirPath     string
	credentialReloadSock  string
	credentialAdminMu     sync.Mutex
	credentialAdminListen net.Listener
)

func validOpaqueAuthHandle(handle string) bool {
	if len(handle) != 32 {
		return false
	}
	b, err := hex.DecodeString(handle)
	return err == nil && len(b) == 16
}

func credentialFilename(handle string) (string, error) {
	if !validOpaqueAuthHandle(handle) {
		return "", fmt.Errorf("invalid opaque auth handle")
	}
	return strings.ToLower(handle) + ".json", nil
}

func decodeCredentialFile(raw []byte) (credentialFile, map[string]clientPolicy, error) {
	var c credentialFile
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, nil, fmt.Errorf("invalid credential JSON: %w", err)
	}
	if !validOpaqueAuthHandle(c.AuthHandle) {
		return c, nil, fmt.Errorf("invalid auth_handle")
	}
	c.AuthHandle = strings.ToLower(c.AuthHandle)
	if !validClientID(c.InternalID) {
		return c, nil, fmt.Errorf("invalid internal_id")
	}
	if c.LegacyID != "" && !validClientID(c.LegacyID) {
		return c, nil, fmt.Errorf("invalid legacy_id")
	}
	psk, err := hex.DecodeString(strings.TrimSpace(c.PSK))
	if err != nil || len(psk) != 32 {
		return c, nil, fmt.Errorf("psk must be 32-byte hex")
	}
	if c.MaxSessions < -1 || c.MaxSessions > 10000 {
		return c, nil, fmt.Errorf("max_sessions out of range")
	}
	p := clientPolicy{PSK: append([]byte(nil), psk...), MaxSessions: c.MaxSessions, AccountID: c.InternalID}
	if c.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, c.ExpiresAt)
		if err != nil {
			return c, nil, fmt.Errorf("expires_at: %w", err)
		}
		p.ExpiresAt = t
	}
	entries := map[string]clientPolicy{c.AuthHandle: p}
	if c.LegacyID != "" {
		if c.LegacyID == c.AuthHandle {
			return c, nil, fmt.Errorf("legacy_id collides with auth_handle")
		}
		entries[c.LegacyID] = p
	}
	return c, entries, nil
}

func readCredentialFile(path string) (credentialFile, map[string]clientPolicy, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return credentialFile{}, nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return credentialFile{}, nil, fmt.Errorf("credential must be a regular file")
	}
	if info.Size() > maxCredentialFileBytes {
		return credentialFile{}, nil, fmt.Errorf("credential file too large")
	}
	if !privateKeyModeAllowed(info.Mode().Perm()) {
		return credentialFile{}, nil, fmt.Errorf("credential permissions too open: %04o", info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return credentialFile{}, nil, err
	}
	return decodeCredentialFile(raw)
}

func loadCredentialDir(dir string) (map[string]clientPolicy, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	if len(entries) > maxCredentialFiles {
		return nil, fmt.Errorf("too many credential files")
	}
	out := make(map[string]clientPolicy, len(entries)*2)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		handle := strings.TrimSuffix(entry.Name(), ".json")
		if !validOpaqueAuthHandle(handle) {
			return nil, fmt.Errorf("invalid credential filename %q", entry.Name())
		}
		c, policies, err := readCredentialFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("credential %s: %w", entry.Name(), err)
		}
		if c.AuthHandle != strings.ToLower(handle) {
			return nil, fmt.Errorf("credential filename/auth_handle mismatch: %s", entry.Name())
		}
		for id, policy := range policies {
			if _, exists := out[id]; exists {
				return nil, fmt.Errorf("duplicate credential identity %q", id)
			}
			out[id] = policy
		}
	}
	return out, nil
}

func mergePolicies(base, extra map[string]clientPolicy) (map[string]clientPolicy, error) {
	out := make(map[string]clientPolicy, len(base)+len(extra))
	for id, p := range base {
		out[id] = p
	}
	for id, p := range extra {
		if _, exists := out[id]; exists {
			return nil, fmt.Errorf("credential identity collision %q", id)
		}
		out[id] = p
	}
	return out, nil
}

func clonePoliciesLocked() map[string]clientPolicy {
	out := make(map[string]clientPolicy, len(clientPolicies))
	for id, p := range clientPolicies {
		p.PSK = append([]byte(nil), p.PSK...)
		out[id] = p
	}
	return out
}

func cleanupRateStateForPolicies(policies map[string]clientPolicy) {
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
}

func applyCredentialEntries(handle string, entries map[string]clientPolicy, deleting bool) error {
	clientAuthMu.Lock()
	old, hadOld := clientPolicies[handle]
	if !hadOld && deleting {
		clientAuthMu.Unlock()
		return nil
	}
	accountID := ""
	if hadOld {
		accountID = old.AccountID
	}
	if !deleting {
		if p, ok := entries[handle]; !ok {
			clientAuthMu.Unlock()
			return fmt.Errorf("credential does not define requested auth_handle")
		} else if accountID == "" {
			accountID = p.AccountID
		}
	}
	next := clonePoliciesLocked()
	for id, p := range next {
		if p.AccountID == accountID || (!deleting && p.AccountID == entries[handle].AccountID) {
			delete(next, id)
		}
	}
	if !deleting {
		for id, p := range entries {
			if existing, exists := next[id]; exists && existing.AccountID != p.AccountID {
				clientAuthMu.Unlock()
				return fmt.Errorf("credential identity collision %q", id)
			}
			next[id] = p
		}
	}
	clientPolicies = next
	clientAuthMu.Unlock()
	cleanupRateStateForPolicies(next)
	// Re-evaluate every active session immediately. Rotated/revoked credentials
	// close matching sessions, while unrelated accounts retain their sessions.
	revokeUnauthorizedSessions(next)
	return nil
}

func reloadCredentialHandle(handle string) error {
	name, err := credentialFilename(handle)
	if err != nil {
		return err
	}
	c, entries, err := readCredentialFile(filepath.Join(credentialDirPath, name))
	if err != nil {
		return err
	}
	if c.AuthHandle != strings.ToLower(handle) {
		return fmt.Errorf("credential auth_handle mismatch")
	}
	return applyCredentialEntries(c.AuthHandle, entries, false)
}

func deleteCredentialHandle(handle string) error {
	if _, err := credentialFilename(handle); err != nil {
		return err
	}
	return applyCredentialEntries(strings.ToLower(handle), nil, true)
}

func startCredentialAdminSocket(path string) (net.Listener, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, fmt.Errorf("credential reload socket must be absolute")
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return nil, err
	}
	credentialAdminMu.Lock()
	credentialAdminListen = ln
	credentialAdminMu.Unlock()
	go credentialAdminLoop(ln)
	return ln, nil
}

func credentialAdminLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go handleCredentialAdminConn(conn)
	}
}

func handleCredentialAdminConn(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	dec := json.NewDecoder(io.LimitReader(conn, 4096))
	dec.DisallowUnknownFields()
	var req credentialAdminRequest
	if err := dec.Decode(&req); err != nil {
		_, _ = io.WriteString(conn, "error\n")
		return
	}
	handle := strings.ToLower(req.AuthHandle)
	var err error
	switch req.Op {
	case "reload":
		err = reloadCredentialHandle(handle)
	case "delete":
		err = deleteCredentialHandle(handle)
	default:
		err = fmt.Errorf("unsupported operation")
	}
	if err != nil {
		_, _ = io.WriteString(conn, "error\n")
		return
	}
	_, _ = io.WriteString(conn, "ok\n")
}

func stopCredentialAdminSocket() {
	credentialAdminMu.Lock()
	ln := credentialAdminListen
	credentialAdminListen = nil
	credentialAdminMu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
	if credentialReloadSock != "" {
		_ = os.Remove(credentialReloadSock)
	}
}

func sendCredentialAdminRequest(socketPath string, req credentialAdminRequest) error {
	conn, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return err
	}
	line, err := bufio.NewReader(io.LimitReader(conn, 64)).ReadString('\n')
	if err != nil {
		return err
	}
	if line != "ok\n" {
		return fmt.Errorf("credential admin rejected request")
	}
	return nil
}
