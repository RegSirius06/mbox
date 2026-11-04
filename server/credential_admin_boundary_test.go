package main

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCredentialJSONRejectsTrailingObjects(t *testing.T) {
	valid := credentialFile{
		AuthHandle: "00112233445566778899aabbccddeeff",
		InternalID: "operator",
		PSK:        strings.Repeat("ab", 32),
	}
	raw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := decodeCredentialFile(append(append([]byte(nil), raw...), []byte(" \n\t")...)); err != nil {
		t.Fatalf("single valid credential rejected: %v", err)
	}
	for _, suffix := range []string{" {}", " null", " garbage", " []"} {
		if _, _, err := decodeCredentialFile(append(append([]byte(nil), raw...), []byte(suffix)...)); err == nil {
			t.Fatalf("trailing data %q accepted", suffix)
		}
	}
}

func TestCredentialSocketDoesNotDeleteUnrelatedFiles(t *testing.T) {
	root := t.TempDir()
	plain := filepath.Join(root, "admin.sock")
	if err := os.WriteFile(plain, []byte("operator-file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := startCredentialAdminSocket(plain); err == nil {
		t.Fatal("ordinary file overwritten")
	}
	data, err := os.ReadFile(plain)
	if err != nil || string(data) != "operator-file" {
		t.Fatalf("existing file damaged: %q, %v", data, err)
	}
	link := filepath.Join(root, "admin-link.sock")
	if err := os.Symlink(plain, link); err != nil {
		t.Fatal(err)
	}
	if _, err := startCredentialAdminSocket(link); err == nil {
		t.Fatal("symlink overwritten")
	}
	target, err := os.Readlink(link)
	if err != nil || target != plain {
		t.Fatalf("symlink damaged: %q, %v", target, err)
	}
}

func TestCredentialAdminConnectionQuota(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	blocked := make(chan struct{})
	var active atomic.Int64
	var peak atomic.Int64
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		credentialAdminLoopWithHandler(ln, func(conn net.Conn) {
			defer conn.Close()
			n := active.Add(1)
			for {
				seen := peak.Load()
				if n <= seen || peak.CompareAndSwap(seen, n) {
					break
				}
			}
			<-blocked
			active.Add(-1)
		})
	}()
	var conns []net.Conn
	defer func() {
		close(blocked)
		for _, conn := range conns {
			conn.Close()
		}
		ln.Close()
		wg.Wait()
	}()
	for i := 0; i < maxCredentialAdminConns+64; i++ {
		conn, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second)
		if err == nil {
			conns = append(conns, conn)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && active.Load() < maxCredentialAdminConns {
		time.Sleep(time.Millisecond * 10)
	}
	if got := peak.Load(); got > maxCredentialAdminConns {
		t.Fatalf("admin connection goroutines exceeded quota: %d", got)
	}
	if got := active.Load(); got == 0 {
		t.Fatal("admin connections never started")
	}
	// Some excess peers should be disconnected rather than held indefinitely.
	disconnected := 0
	for _, conn := range conns {
		conn.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
		var one [1]byte
		_, e := conn.Read(one[:])
		if errors.Is(e, io.EOF) {
			disconnected++
		}
	}
	if disconnected == 0 {
		t.Fatal("connection overflow was not rejected")
	}
}
