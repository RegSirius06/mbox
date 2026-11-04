package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// configureRuntimeStatusPath allows independent staging instances to keep
// lifecycle/status output separate from the production service. Invalid
// overrides are rejected before binding any network listener.
func configureRuntimeStatusPath() error {
	raw := os.Getenv("MBOX_RUNTIME_STATUS")
	if raw == "" {
		runtimeStatusPath = defaultRuntimeStatusPath
		return nil
	}
	if len(raw) > 4096 || !filepath.IsAbs(raw) || filepath.Clean(raw) != raw || !strings.HasSuffix(raw, ".json") {
		return fmt.Errorf("MBOX_RUNTIME_STATUS must be an absolute canonical .json path")
	}
	runtimeStatusPath = raw
	return nil
}

func writeRuntimeStatus() {
	runtimeStatusMu.Lock()
	defer runtimeStatusMu.Unlock()

	snap := runtimeStatusSnapshot{
		GeneratedAt:   time.Now().UTC(),
		ActiveStreams: totalStreams.Load(),
		QueuedBytes:   globalQueuedBytes.Load(),
		Clients:       make(map[string]int),
	}
	sessionsMu.Lock()
	snap.ActiveSessions = len(sessions)
	for _, s := range sessions {
		if s.clientID != "" {
			snap.Clients[redactedClientID(s.accountID)]++
		}
	}
	sessionsMu.Unlock()

	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return
	}
	b = append(b, '\n')
	tmp := runtimeStatusPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o640); err != nil {
		return
	}
	_ = os.Rename(tmp, runtimeStatusPath)
}

func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
