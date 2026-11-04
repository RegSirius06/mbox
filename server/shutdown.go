package main

import "time"

func closeAllSessions() {
	var ids []string
	sessionsMu.Lock()
	ids = make([]string, 0, len(sessions))
	for id := range sessions {
		ids = append(ids, id)
	}
	sessionsMu.Unlock()
	for _, id := range ids {
		closeSession(id)
	}
}

func waitForResourcesReleased(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		sessionsMu.Lock()
		n := len(sessions)
		sessionsMu.Unlock()
		if n == 0 && totalStreams.Load() == 0 && globalQueuedBytes.Load() == 0 {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}
