package main

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTestCredential(t *testing.T, dir, handle, internal, legacy string, pskByte byte) {
	t.Helper()
	c := credentialFile{
		AuthHandle: handle,
		InternalID: internal,
		LegacyID:   legacy,
		PSK:        string(make([]byte, 0)),
	}
	psk := make([]byte, 32)
	for i := range psk {
		psk[i] = pskByte
	}
	c.PSK = ""
	const hexChars = "0123456789abcdef"
	for _, b := range psk {
		c.PSK += string([]byte{hexChars[b>>4], hexChars[b&0x0f]})
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, handle+".json")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func testSessionForPolicy(clientID, accountID string, psk []byte) *session {
	return &session{
		clientID:    clientID,
		accountID:   accountID,
		crypto:      &cryptoState{authPSKHash: sha256.Sum256(psk)},
		streams:     map[uint32]*stream{},
		downReplay:  map[uint64]cachedDownResponse{},
		downReqSeen: map[uint64]struct{}{},
		upCache:     map[uint64]cachedUpResponse{},
		downReady:   make(chan struct{}, 1),
		done:        make(chan struct{}),
		lastSeen:    time.Now(),
	}
}

func TestIncrementalCredentialRotationRevokesOnlyAffectedAccount(t *testing.T) {
	dir := t.TempDir()
	handle := "00112233445566778899aabbccddeeff"
	writeTestCredential(t, dir, handle, "alice", "alice", 0x22)

	oldPolicies := clientPolicies
	oldSessions := sessions
	oldDir := credentialDirPath
	defer func() {
		clientAuthMu.Lock()
		clientPolicies = oldPolicies
		clientAuthMu.Unlock()
		sessionsMu.Lock()
		sessions = oldSessions
		sessionsMu.Unlock()
		credentialDirPath = oldDir
	}()
	oldAlice := make([]byte, 32)
	oldBob := make([]byte, 32)
	for i := range oldAlice {
		oldAlice[i], oldBob[i] = 0x11, 0x33
	}
	clientAuthMu.Lock()
	clientPolicies = map[string]clientPolicy{
		handle:       {PSK: oldAlice, AccountID: "alice"},
		"alice":      {PSK: append([]byte(nil), oldAlice...), AccountID: "alice"},
		"bob-handle": {PSK: oldBob, AccountID: "bob"},
	}
	clientAuthMu.Unlock()
	alice := testSessionForPolicy(handle, "alice", oldAlice)
	bob := testSessionForPolicy("bob-handle", "bob", oldBob)
	sessionsMu.Lock()
	sessions = map[string]*session{"alice-session": alice, "bob-session": bob}
	sessionsMu.Unlock()
	credentialDirPath = dir

	if err := reloadCredentialHandle(handle); err != nil {
		t.Fatal(err)
	}
	sessionsMu.Lock()
	_, aliceAlive := sessions["alice-session"]
	_, bobAlive := sessions["bob-session"]
	sessionsMu.Unlock()
	if aliceAlive {
		t.Fatal("rotated account session was not revoked immediately")
	}
	if !bobAlive {
		t.Fatal("unrelated account session was interrupted")
	}
	if p, ok := getClientPolicy(handle); !ok || p.PSK[0] != 0x22 {
		t.Fatal("new credential was not loaded")
	}
}

func TestCredentialAdminSocketReloadsOneFile(t *testing.T) {
	dir := t.TempDir()
	handle := "ffeeddccbbaa99887766554433221100"
	writeTestCredential(t, dir, handle, "carol", "", 0x44)
	oldPolicies := clientPolicies
	oldDir := credentialDirPath
	oldSock := credentialReloadSock
	defer func() {
		stopCredentialAdminSocket()
		clientAuthMu.Lock()
		clientPolicies = oldPolicies
		clientAuthMu.Unlock()
		credentialDirPath, credentialReloadSock = oldDir, oldSock
	}()
	clientAuthMu.Lock()
	clientPolicies = map[string]clientPolicy{}
	clientAuthMu.Unlock()
	credentialDirPath = dir
	credentialReloadSock = filepath.Join(t.TempDir(), "credentials.sock")
	if _, err := startCredentialAdminSocket(credentialReloadSock); err != nil {
		t.Fatal(err)
	}
	if err := sendCredentialAdminRequest(credentialReloadSock, credentialAdminRequest{Op: "reload", AuthHandle: handle}); err != nil {
		t.Fatal(err)
	}
	if p, ok := getClientPolicy(handle); !ok || p.AccountID != "carol" || p.PSK[0] != 0x44 {
		t.Fatal("incremental admin reload did not apply credential")
	}
}

func TestCredentialFilenameRejectsTraversal(t *testing.T) {
	for _, bad := range []string{"../x", "alice", "00112233445566778899aabbccddeefg", "00112233445566778899aabbccddeeff/../x"} {
		if _, err := credentialFilename(bad); err == nil {
			t.Fatalf("accepted credential handle %q", bad)
		}
	}
}
