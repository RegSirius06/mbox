package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalPathRejectsAmbiguousAndTraversalForms(t *testing.T) {
	bad := []string{
		"/api//v1", "/api/./v1", "/api/../v1", "/api/%2e%2e/v1",
		"/api/%2Fv1", "/api/%2fv1", "/api/%5cv1", "/api/%252fsecret",
		"/api/%252e%252e/secret", "/api\\v1", "/api/%zz",
	}
	for _, raw := range bad {
		if got, err := canonicalPath(raw); err == nil {
			t.Errorf("canonicalPath(%q)=%q, want rejection", raw, got)
		}
	}
}

func TestAllowedPrefixMatchesPathSegments(t *testing.T) {
	cfg := &relayConfig{AllowedPrefixes: []string{"/api/v1/events", "/assets/"}}
	for _, good := range []string{"/api/v1/events", "/api/v1/events/abc", "/assets/a.js"} {
		if !allowedPath(cfg, good) {
			t.Errorf("expected allow: %s", good)
		}
	}
	for _, bad := range []string{"/api/v1/events-evil", "/assets-evil/a.js"} {
		if allowedPath(cfg, bad) {
			t.Errorf("unexpected allow: %s", bad)
		}
	}
}

func TestPrivateRuntimeDirAndLockRejectSymlink(t *testing.T) {
	root := t.TempDir()
	runtime := filepath.Join(root, "runtime")
	if err := os.Mkdir(runtime, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ensurePrivateRuntimeDir(runtime); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(runtime, "slot-000.lock")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if f, err := secureOpenLock(link); err == nil {
		if f != nil {
			_ = f.Close()
		}
		t.Fatal("symlink lock accepted")
	}
}

func FuzzCanonicalPath(f *testing.F) {
	for _, seed := range []string{"/api/v1/events", "/api/%2e%2e/x", "/a//b", "/a/%252f/b", "/"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 64*1024 {
			t.Skip()
		}
		got, err := canonicalPath(raw)
		if err != nil {
			return
		}
		if got == "" || got[0] != '/' {
			t.Fatalf("accepted non-absolute canonical path %q from %q", got, raw)
		}
		if _, err := canonicalPath(got); err != nil {
			t.Fatalf("canonical output rejected on second pass: %q: %v", got, err)
		}
	})
}
