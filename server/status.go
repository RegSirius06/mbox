package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"time"
)

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
			snap.Clients[redactedClientID(s.clientID)]++
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
