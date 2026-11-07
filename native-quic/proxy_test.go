package main

import (
	"bytes"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RegSirius06/mbox/common/originauth"
)

func TestConfigPaths(t *testing.T) {
	file := filepath.Join(t.TempDir(), "server.json")
	src := map[string]any{
		"transport":  map[string]any{"base_path": "/private"},
		"camouflage": map[string]any{"open_path": "/opened", "up_path": "/sent/", "down_path": "/polled/", "close_path": "/ended/"},
	}
	data, _ := json.Marshal(src)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	paths, err := readPaths(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/private/opened", "/private/sent/abc", "/private/polled/abc", "/private/ended/abc"} {
		if !paths.allowed(path) {
			t.Fatalf("expected allowed %q", path)
		}
	}
	for _, path := range []string{"/private/metrics", "/private/sent/", "/private/opened/abc", "/private/health", "/api/v1/session"} {
		if paths.allowed(path) {
			t.Fatalf("unexpected allowed %q", path)
		}
	}
}

func TestH3ProxySignsForOrigin(t *testing.T) {
	key := []byte(strings.Repeat("S", 32))
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/private/api/v1/session" || r.URL.RawQuery != "a=1" {
			t.Errorf("bad target: %q", r.URL.String())
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != "encrypted-record" {
			t.Errorf("bad body: %q", body)
		}
		sig, err := hex.DecodeString(r.Header.Get(originauth.SignatureHeader))
		if err != nil {
			t.Error(err)
		}
		expected := originauth.Signature(key, r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get(originauth.TimestampHeader), r.Header.Get(originauth.NonceHeader), body)
		if !hmac.Equal(sig, expected) {
			t.Error("origin signature invalid")
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte("reply"))
	}))
	defer backend.Close()
	u, _ := loopbackURL(backend.URL)
	paths := originPaths{open: "/private/api/v1/session", up: "/private/api/v1/events/", down: "/private/api/v1/sync/", close: "/private/api/v1/session/"}
	h := makeServerHandler(u, paths, key, localHTTPClient())
	req := httptest.NewRequest("POST", "https://fake:7443/private/api/v1/session?a=1", bytes.NewBufferString("encrypted-record"))
	req.Header.Set(originauth.SignatureHeader, "forged")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "reply" {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}
func TestH3ProxyDeniesNonMBoxPaths(t *testing.T) {
	paths := originPaths{open: "/api/v1/session", up: "/api/v1/events/", down: "/api/v1/sync/", close: "/api/v1/session/"}
	h := makeServerHandler(nil, paths, make([]byte, 32), localHTTPClient())
	for _, path := range []string{"/metrics", "/health", "/api/v1/session%2fabc", "/api/v1/events/", "/api/v1/../metrics"} {
		req := httptest.NewRequest("POST", "https://fake:7443"+path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 404 {
			t.Errorf("path=%q: status=%d", path, rec.Code)
		}
	}
}
func TestBoundedAndURLValidation(t *testing.T) {
	if _, err := readBounded(strings.NewReader(strings.Repeat("x", maxBody+1))); err == nil {
		t.Fatal("oversized body accepted")
	}
	for _, raw := range []string{"http://example.com:18449", "http://localhost:18449", "https://127.0.0.1:18449"} {
		if _, err := loopbackURL(raw); err == nil {
			t.Fatalf("accepted unsafe origin %s", raw)
		}
	}
	for _, raw := range []string{"http://quic.example:7443", "https://user@quic.example:7443", "https://quic.example:7443/path"} {
		if _, err := remoteURL(raw); err == nil {
			t.Fatalf("accepted unsafe remote %s", raw)
		}
	}
	if _, err := remoteURL("https://quic.example:7443"); err != nil {
		t.Fatal(err)
	}
}

func TestReadSecretAllowsMBoxPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "origin-hmac.key")
	key := bytes.Repeat([]byte{0xa5}, 32)
	if err := os.WriteFile(path, []byte(hex.EncodeToString(key)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		mode  os.FileMode
		allow bool
	}{
		{0600, true}, {0640, true}, {0644, false}, {0660, false}, {0641, false},
	} {
		t.Run(fmt.Sprintf("%04o", tc.mode), func(t *testing.T) {
			if err := os.Chmod(path, tc.mode); err != nil {
				t.Fatal(err)
			}
			got, err := readSecret(path)
			if tc.allow {
				if err != nil || !bytes.Equal(got, key) {
					t.Fatalf("mode %04o: key=%x err=%v", tc.mode, got, err)
				}
			} else if err == nil {
				t.Fatalf("mode %04o must fail", tc.mode)
			}
		})
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "secret-link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readSecret(link); err == nil {
		t.Fatal("symlink must fail")
	}
}
