package main

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCredentialOpenRefusesSymlinkAndSpecialFiles(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "credential.json")
	c := credentialFile{
		AuthHandle: "00112233445566778899aabbccddeeff",
		InternalID: "testuser",
		PSK:        strings.Repeat("ab", 32),
	}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, got, err := readCredentialFile(real); err != nil || got[c.AuthHandle].AccountID != "testuser" {
		t.Fatalf("regular credential must be read: %v", err)
	}
	link := filepath.Join(dir, "symlink.json")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readCredentialFile(link); err == nil {
		t.Fatal("credential symlink was followed")
	}
	fifo := filepath.Join(dir, "fifo.json")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readCredentialFile(fifo); err == nil {
		t.Fatal("credential FIFO was accepted")
	}
}

func TestCredentialOpenBoundsSizeAndFileMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credential.json")
	if err := os.WriteFile(path, []byte(strings.Repeat(" ", maxCredentialFileBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readCredentialFile(path); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("oversized credential not rejected by size: %v", err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readCredentialFile(path); err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("public credential must be rejected: %v", err)
	}
}

func adminRequestRoundTrip(t *testing.T, payload string) string {
	t.Helper()
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleCredentialAdminConn(server)
	}()
	defer func() { client.Close(); <-done }()
	_ = client.SetDeadline(time.Now().Add(time.Second))
	go func() { _, _ = io.WriteString(client, payload) }()
	reply := make([]byte, 16)
	n, err := client.Read(reply)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	return string(reply[:n])
}

func TestCredentialAdminRejectsTrailingJSONAndOversizedLines(t *testing.T) {
	base := `{"op":"delete","auth_handle":"00112233445566778899aabbccddeeff"}`
	if got := adminRequestRoundTrip(t, base+"\n"); got != "ok\n" {
		t.Fatalf("valid command rejected: %q", got)
	}
	for _, bad := range []string{
		base + ` {}` + "\n",
		base + ` nonsense` + "\n",
		base + ` {"op":"delete"}` + "\n",
		base + strings.Repeat(" ", maxCredentialAdminLineBytes) + "\n",
		`{"op":"delete","auth_handle":"00112233445566778899aabbccddeeff","extra":"unknown"}` + "\n",
	} {
		if got := adminRequestRoundTrip(t, bad); got != "error\n" {
			t.Fatalf("invalid admin command accepted (%d bytes): %q", len(bad), got)
		}
	}
}
