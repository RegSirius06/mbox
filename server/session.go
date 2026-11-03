package main

import (
	"crypto/sha256"
	"log"
	"sort"
	"time"
)

const (
	sessionOpenRateTTL        = 2 * time.Minute
	maxSessionOpenRateEntries = 65536
)

// pruneSessionOpenRatesLocked bounds pre-auth identity state. Caller must hold
// sessionsMu. Security invariant: attacker-controlled identities cannot cause
// unbounded retention after their rate window is no longer useful.
func pruneSessionOpenRatesLocked(now time.Time) {
	cutoff := now.Add(-sessionOpenRateTTL)
	for key, r := range sessionOpenRates {
		if r.start.Before(cutoff) {
			delete(sessionOpenRates, key)
		}
	}
}

func getSession(id string) (*session, bool) {
	sessionsMu.Lock()
	s, ok := sessions[id]
	if !ok {
		sessionsMu.Unlock()
		return nil, false
	}
	clientID := s.clientID
	sessionsMu.Unlock()

	// Expiry is checked on every live HTTP transaction, not only by the
	// periodic cleanup ticker. A naturally expiring paid profile therefore
	// loses its active session on the next poll/POST without waiting a minute.
	if clientID != "" {
		p, policyOK := getClientPolicy(clientID)
		if !policyOK || !clientPolicyActive(p) {
			closeSession(id)
			return nil, false
		}
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, false
	}
	s.lastSeen = time.Now()
	s.mu.Unlock()
	return s, true
}

func registerSession(id string, s *session) bool {
	if serverDraining.Load() {
		log.Printf("SESSION OPEN REJECT reason=draining")
		return false
	}
	type replacedSession struct {
		id string
		s  *session
	}
	var replaced []replacedSession

	sessionsMu.Lock()

	// A device identity is authenticated inside the Noise OPEN payload. During
	// hard network loss the previous session cannot send /close, so atomically
	// replace only sessions belonging to the same client+device. Legacy clients
	// have an empty deviceID and keep the original session-limit semantics.
	replaceIDs := make(map[string]struct{})
	if s.deviceID != "" {
		for existingID, existing := range sessions {
			if existing.clientID == s.clientID && existing.deviceID == s.deviceID {
				replaceIDs[existingID] = struct{}{}
			}
		}
	}

	effectiveGlobal := len(sessions) - len(replaceIDs)
	if effectiveGlobal >= runtimeLimits.MaxSessionsGlobal {
		sessionsMu.Unlock()
		log.Printf("SESSION OPEN REJECT reason=max_sessions_global client=%s", redactedClientID(s.clientID))
		return false
	}
	limit := runtimeLimits.DefaultMaxSessionsPerClient
	if p, ok := getClientPolicy(s.clientID); ok && p.MaxSessions != 0 {
		limit = p.MaxSessions
	}
	if limit >= 0 {
		count := 0
		for existingID, e := range sessions {
			if _, replacing := replaceIDs[existingID]; replacing {
				continue
			}
			if e.clientID == s.clientID {
				count++
			}
		}
		if count >= limit {
			sessionsMu.Unlock()
			log.Printf("SESSION OPEN REJECT reason=max_sessions_per_client client=%s active=%d limit=%d", redactedClientID(s.clientID), count, limit)
			return false
		}
	}
	key := s.clientID
	if key == "" {
		key = "legacy"
	}
	now := time.Now()
	if _, exists := sessionOpenRates[key]; !exists && len(sessionOpenRates) >= maxSessionOpenRateEntries {
		pruneSessionOpenRatesLocked(now)
		if len(sessionOpenRates) >= maxSessionOpenRateEntries {
			sessionsMu.Unlock()
			log.Printf("SESSION OPEN REJECT reason=rate_state_capacity")
			return false
		}
	}
	r := sessionOpenRates[key]
	if r.start.IsZero() || now.Sub(r.start) >= time.Minute {
		r = rateWindow{start: now}
	}
	if r.count >= runtimeLimits.MaxSessionOpensPerMinute {
		sessionsMu.Unlock()
		log.Printf("SESSION OPEN REJECT reason=session_open_rate client=%s limit=%d", redactedClientID(s.clientID), runtimeLimits.MaxSessionOpensPerMinute)
		return false
	}
	r.count++
	sessionOpenRates[key] = r

	for existingID := range replaceIDs {
		existing := sessions[existingID]
		delete(sessions, existingID)
		replaced = append(replaced, replacedSession{id: existingID, s: existing})
	}
	sessions[id] = s
	sessionsMu.Unlock()

	for _, old := range replaced {
		log.Printf("SESSION SUPERSEDE old=%s new=%s", redactedSessionID(old.id), redactedSessionID(id))
		closeSessionState(old.id, old.s)
	}
	return true
}

func revokeUnauthorizedSessions(policies map[string]clientPolicy) {
	var revoke []string
	type active struct {
		id   string
		last time.Time
	}
	byClient := map[string][]active{}
	sessionsMu.Lock()
	for id, s := range sessions {
		if s.clientID == "" {
			continue
		}
		p, ok := policies[s.clientID]
		if !ok || !clientPolicyActive(p) || sha256.Sum256(p.PSK) != s.crypto.authPSKHash {
			revoke = append(revoke, id)
			continue
		}
		s.mu.Lock()
		last := s.lastSeen
		s.mu.Unlock()
		byClient[s.clientID] = append(byClient[s.clientID], active{id, last})
	}
	for cid, list := range byClient {
		p := policies[cid]
		limit := p.MaxSessions
		if limit == 0 {
			limit = runtimeLimits.DefaultMaxSessionsPerClient
		}
		if limit < 0 || len(list) <= limit {
			continue
		}
		sort.Slice(list, func(i, j int) bool { return list[i].last.Before(list[j].last) })
		for i := 0; i < len(list)-limit; i++ {
			revoke = append(revoke, list[i].id)
		}
	}
	sessionsMu.Unlock()
	for _, id := range revoke {
		log.Printf("SESSION REVOKE %s", redactedSessionID(id))
		closeSession(id)
	}
}

func closeSession(id string) {
	sessionsMu.Lock()
	s, ok := sessions[id]
	if ok {
		delete(sessions, id)
	}
	sessionsMu.Unlock()
	if !ok {
		return
	}
	closeSessionState(id, s)
}

func closeSessionState(id string, s *session) {
	if s == nil {
		return
	}
	s.downCommitMu.Lock()
	defer s.downCommitMu.Unlock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	ids := make([]uint32, 0, len(s.streams))
	for id := range s.streams {
		ids = append(ids, id)
	}
	queued := s.downBytes
	// Replay accounting is ownership-based: release the exact reservation
	// attached to each entry, not an estimate or ciphertext length.
	for _, r := range s.downReplay {
		queued += r.reservedBytes
	}
	for i := range s.downFrames {
		s.downFrames[i] = nil
	}
	s.downFrames = nil
	s.downBytes = 0
	for seq, r := range s.downReplay {
		r.body = nil
		delete(s.downReplay, seq)
	}
	s.downReplayBytes = 0
	s.mu.Unlock()
	if queued > 0 {
		releaseGlobalQueued(queued)
	}
	s.closeOnce.Do(func() { close(s.done) })
	for _, streamID := range ids {
		closeStream(s, streamID, false)
	}
	log.Printf("SESSION CLOSE %s", redactedSessionID(id))
}
