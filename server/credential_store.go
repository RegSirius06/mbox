package main

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	maxCredentialFileBytes      = 64 * 1024
	maxCredentialFiles          = 1_000_000
	maxCredentialAdminConns     = 16
	maxCredentialAdminLineBytes = 4096
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
	// Serialize an entire credential mutation, including session revocation.
	// A newer reload must not race an older reload's post-commit cleanup.
	credentialUpdateMu sync.Mutex
	policyIDsByAccount map[string]map[string]struct{}
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
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, nil, fmt.Errorf("invalid credential JSON: %w", err)
	}
	// A credential is exactly one JSON document. Silently accepting a second
	// object after the first would make validation and operator review disagree.
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return c, nil, fmt.Errorf("credential has trailing JSON data")
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
	// Check the *opened file*, not an Lstat of a pathname which can be swapped
	// before ReadFile. O_NOFOLLOW rejects symlink substitution; O_NONBLOCK
	// avoids hanging when an attacker substitutes a FIFO for a credential.
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return credentialFile{}, nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return credentialFile{}, nil, err
	}
	if !info.Mode().IsRegular() {
		return credentialFile{}, nil, fmt.Errorf("credential must be a regular file")
	}
	if info.Size() > maxCredentialFileBytes {
		return credentialFile{}, nil, fmt.Errorf("credential file too large")
	}
	if !privateKeyModeAllowed(info.Mode().Perm()) {
		return credentialFile{}, nil, fmt.Errorf("credential permissions too open: %04o", info.Mode().Perm())
	}
	// Bound the bytes actually consumed too: a file can grow after Stat.
	raw, err := io.ReadAll(io.LimitReader(f, maxCredentialFileBytes+1))
	if err != nil {
		return credentialFile{}, nil, err
	}
	if len(raw) > maxCredentialFileBytes {
		return credentialFile{}, nil, fmt.Errorf("credential file too large")
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

// buildPolicyAccountIndex is called for a full configuration reload. Incremental
// changes maintain the index in place, so updating one account does not copy
// or traverse every unrelated user's PSK. Caller holds clientAuthMu.
func buildPolicyAccountIndex(policies map[string]clientPolicy) map[string]map[string]struct{} {
	index := make(map[string]map[string]struct{})
	for id, policy := range policies {
		ids := index[policy.AccountID]
		if ids == nil {
			ids = make(map[string]struct{})
			index[policy.AccountID] = ids
		}
		ids[id] = struct{}{}
	}
	return index
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

// applyCredentialEntries changes only the identities belonging to the old and
// new accounts. Identity conflicts are checked before any mutation, making a
// rejected credential transaction atomic. The in-place map is always accessed
// under clientAuthMu; getClientPolicy returns private PSK copies.
func applyCredentialEntries(handle string, entries map[string]clientPolicy, deleting bool) error {
	credentialUpdateMu.Lock()
	defer credentialUpdateMu.Unlock()

	clientAuthMu.Lock()
	old, hadOld := clientPolicies[handle]
	if !hadOld && deleting {
		clientAuthMu.Unlock()
		return nil
	}
	if !deleting {
		if !validOpaqueAuthHandle(handle) {
			clientAuthMu.Unlock()
			return fmt.Errorf("invalid auth_handle")
		}
		if _, ok := entries[handle]; !ok {
			clientAuthMu.Unlock()
			return fmt.Errorf("credential does not define requested auth_handle")
		}
	}

	// Unit tests and in-memory bootstrap may initialize clientPolicies without
	// using loadRuntimeConfig. Rebuild once in that case, not per update.
	if policyIDsByAccount == nil {
		policyIDsByAccount = buildPolicyAccountIndex(clientPolicies)
	}
	affected := make(map[string]struct{}, 2)
	if hadOld {
		affected[old.AccountID] = struct{}{}
	}
	if !deleting {
		newAccount := entries[handle].AccountID
		affected[newAccount] = struct{}{}
		for id, p := range entries {
			if p.AccountID != newAccount {
				clientAuthMu.Unlock()
				return fmt.Errorf("credential aliases must share an account")
			}
			if existing, exists := clientPolicies[id]; exists {
				if _, replacing := affected[existing.AccountID]; !replacing {
					clientAuthMu.Unlock()
					return fmt.Errorf("credential identity collision %q", id)
				}
			}
		}
	}

	removed := make(map[string]struct{})
	for account := range affected {
		for id := range policyIDsByAccount[account] {
			delete(clientPolicies, id)
			removed[id] = struct{}{}
		}
		delete(policyIDsByAccount, account)
	}
	if !deleting {
		for id, p := range entries {
			// Never retain a slice owned by the JSON decoder/caller. Other
			// accounts' secrets are deliberately left untouched.
			p.PSK = append([]byte(nil), p.PSK...)
			clientPolicies[id] = p
			if policyIDsByAccount[p.AccountID] == nil {
				policyIDsByAccount[p.AccountID] = make(map[string]struct{})
			}
			policyIDsByAccount[p.AccountID][id] = struct{}{}
			delete(removed, id)
		}
	}
	// The map pointer stays stable. Only affected identities are removed from
	// the rate limiter; full sweeps are reserved for full configuration reloads.
	policies := clientPolicies
	clientAuthMu.Unlock()

	sessionsMu.Lock()
	for id := range removed {
		delete(sessionOpenRates, id)
	}
	sessionsMu.Unlock()
	revokeUnauthorizedSessionsForAccounts(policies, affected)
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
	// The configured admin socket path must never delete an unrelated file
	// or a symlink during startup/upgrade. A stale socket can be replaced.
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("credential admin path exists and is not a socket")
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
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
	credentialAdminLoopWithHandler(ln, handleCredentialAdminConn)
}

func credentialAdminLoopWithHandler(ln net.Listener, handler func(net.Conn)) {
	// An untrusted local peer that can connect to the admin socket must not
	// create unlimited goroutines with idle or partial JSON requests.
	permits := make(chan struct{}, maxCredentialAdminConns)
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		select {
		case permits <- struct{}{}:
			go func() {
				defer func() { <-permits }()
				handler(conn) // production handler closes the socket
			}()
		default:
			_ = conn.Close()
		}
	}
}

func handleCredentialAdminConn(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	// Admin protocol is one JSON object terminated by a newline. Parse the
	// complete bounded line; do not accept a valid prefix followed by a
	// second JSON value or arbitrarily large whitespace/unknown fields.
	line, err := bufio.NewReader(io.LimitReader(conn, maxCredentialAdminLineBytes+1)).ReadBytes('\n')
	if err != nil || len(line) > maxCredentialAdminLineBytes {
		_, _ = io.WriteString(conn, "error\n")
		return
	}
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	var req credentialAdminRequest
	if err := dec.Decode(&req); err != nil {
		_, _ = io.WriteString(conn, "error\n")
		return
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		_, _ = io.WriteString(conn, "error\n")
		return
	}
	handle := strings.ToLower(req.AuthHandle)
	err = nil
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
		// Never unlink an operator file/symlink that appeared at the path
		// after our listener was started.
		if info, err := os.Lstat(credentialReloadSock); err == nil && info.Mode()&os.ModeSocket != 0 {
			_ = os.Remove(credentialReloadSock)
		}
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
