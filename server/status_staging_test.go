package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStagingStatusPathCanBeIsolated(t *testing.T) {
	old := runtimeStatusPath
	t.Cleanup(func() { runtimeStatusPath = old })
	custom := filepath.Join(t.TempDir(), "staging-status.json")
	t.Setenv("MBOX_RUNTIME_STATUS", custom)
	if err := configureRuntimeStatusPath(); err != nil {
		t.Fatal(err)
	}
	if runtimeStatusPath != custom {
		t.Fatalf("got %q, want %q", runtimeStatusPath, custom)
	}
	writeRuntimeStatus()
	b, err := os.ReadFile(custom)
	if err != nil {
		t.Fatalf("staging status file not created: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("staging status file empty")
	}
}

func TestInvalidStagingStatusPathFailsClosed(t *testing.T) {
	old := runtimeStatusPath
	t.Cleanup(func() { runtimeStatusPath = old })
	for _, bad := range []string{"relative.json", "/tmp/../etc/status.json", "/tmp/status.txt", " /tmp/status.json"} {
		t.Setenv("MBOX_RUNTIME_STATUS", bad)
		if err := configureRuntimeStatusPath(); err == nil {
			t.Errorf("accepted invalid path %q", bad)
		}
	}
}

func TestDefaultStatusPathUnchanged(t *testing.T) {
	old := runtimeStatusPath
	t.Cleanup(func() { runtimeStatusPath = old })
	t.Setenv("MBOX_RUNTIME_STATUS", "")
	if err := configureRuntimeStatusPath(); err != nil {
		t.Fatal(err)
	}
	if runtimeStatusPath != "/run/mbox/status.json" {
		t.Fatalf("production path changed to %q", runtimeStatusPath)
	}
}
