package main

import (
	"os"
	"testing"
)

func withArgsEnv(t *testing.T, args []string, env map[string]string, fn func()) {
	t.Helper()
	oldArgs := os.Args
	os.Args = append([]string{"mbox-client"}, args...)
	t.Cleanup(func() { os.Args = oldArgs })
	for k, v := range env {
		old, ok := os.LookupEnv(k)
		if v == "" {
			_ = os.Unsetenv(k)
		} else {
			_ = os.Setenv(k, v)
		}
		kk, vv, existed := k, old, ok
		t.Cleanup(func() {
			if existed {
				_ = os.Setenv(kk, vv)
			} else {
				_ = os.Unsetenv(kk)
			}
		})
	}
	fn()
}

func TestValidateTransportURLRejectsRemoteHTTPByDefault(t *testing.T) {
	withArgsEnv(t, nil, map[string]string{"MBOX_DEVELOPER_MODE": "", "MBOX_ALLOW_INSECURE_HTTP": ""}, func() {
		if err := validateTransportURL("http://example.com/api"); err == nil {
			t.Fatal("remote plaintext HTTP accepted without developer override")
		}
		if err := validateTransportURL("http://127.0.0.1:8080/api"); err != nil {
			t.Fatalf("loopback HTTP should remain available for local plumbing: %v", err)
		}
	})
}

func TestValidateTransportURLRequiresBothUnsafeGates(t *testing.T) {
	withArgsEnv(t, []string{"--developer-mode"}, map[string]string{"MBOX_ALLOW_INSECURE_HTTP": ""}, func() {
		if err := validateTransportURL("http://example.com/api"); err == nil {
			t.Fatal("developer mode alone must not permit remote HTTP")
		}
	})
	withArgsEnv(t, []string{"--developer-mode", "--allow-insecure-http"}, nil, func() {
		if err := validateTransportURL("http://example.com/api"); err != nil {
			t.Fatalf("explicit developer+insecure override rejected: %v", err)
		}
	})
}

func TestRuntimeConfigPathAcceptsFlagsInAnyOrder(t *testing.T) {
	withArgsEnv(t, []string{"--developer-mode", "-config", "/tmp/client.json", "--allow-insecure-http"}, map[string]string{"MBOX_CONFIG": ""}, func() {
		if got := runtimeConfigPath("/default"); got != "/tmp/client.json" {
			t.Fatalf("got %q", got)
		}
	})
}
