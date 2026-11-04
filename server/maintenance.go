package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func warnFileDescriptorLimit() {
	var rlim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rlim); err == nil {
		needed := uint64(runtimeLimits.MaxTotalStreams*2 + runtimeLimits.MaxSessionsGlobal*4 + 1024)
		if rlim.Cur < needed {
			log.Printf("WARNING: RLIMIT_NOFILE=%d, recommended >=%d", rlim.Cur, needed)
		}
	}
}

func startAuthReloadLoop(configPath string) {
	reloadCh := make(chan os.Signal, 1)
	signal.Notify(reloadCh, syscall.SIGHUP)
	go func() {
		for range reloadCh {
			if err := reloadClientAuth(configPath); err != nil {
				log.Printf("AUTH RELOAD FAILED: %v", err)
				continue
			}
			clientAuthMu.RLock()
			n := len(clientPolicies)
			clientAuthMu.RUnlock()
			log.Printf("AUTH RELOADED clients=%d", n)
		}
	}()
}

func startRuntimeStatusLoop() {
	writeRuntimeStatus()
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for range t.C {
			writeRuntimeStatus()
		}
	}()
}

func startSessionExpiryLoop() {
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for range t.C {
			now := time.Now()
			cutoff := now.Add(-5 * time.Minute)
			// Authentication nonce retention is bounded in both active and idle
			// periods. Retaining old entries indefinitely after an idle spell
			// would create a false steady-state heap baseline.
			originNonceMu.Lock()
			pruneOriginNoncesLocked(now)
			originNonceNextSweep = now.Add(originNonceSweepInterval)
			originNonceMu.Unlock()
			var expired []string

			sessionsMu.Lock()
			pruneSessionOpenRatesLocked(time.Now())
			for id, s := range sessions {
				s.mu.Lock()
				last := s.lastSeen
				clientID := s.clientID
				s.mu.Unlock()
				policyExpired := false
				if clientID != "" {
					if p, ok := getClientPolicy(clientID); !ok || !clientPolicyActive(p) {
						policyExpired = true
					}
				}
				if last.Before(cutoff) || policyExpired {
					expired = append(expired, id)
				}
			}
			sessionsMu.Unlock()

			for _, id := range expired {
				log.Printf("SESSION EXPIRE %s", redactedSessionID(id))
				closeSession(id)
			}
		}
	}()
}
