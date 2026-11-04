package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	controlmeta "../common/controlmeta"
)

func TestSignedControlAckCannotBeRaisedByFrontend(t *testing.T) {
	const sid = "0123456789abcdef0123456789abcdef"
	var key [32]byte
	copy(key[:], bytes.Repeat([]byte{7}, 32))
	s := &session{
		crypto:          &cryptoState{controlKey: key},
		streams:         map[uint32]*stream{},
		downReplay:      map[uint64]cachedDownResponse{1: {counter: 4, status: http.StatusOK, body: []byte("ciphertext")}},
		downReqSeen:     map[uint64]struct{}{},
		lastDownCounter: 4,
		downReplayBytes: len("ciphertext"),
		lastSeen:        time.Now().Add(-time.Minute),
		done:            make(chan struct{}),
	}
	sessionsMu.Lock()
	oldSessions := sessions
	sessions = map[string]*session{sid: s}
	sessionsMu.Unlock()
	oldRequire, oldBase, oldCamouflage := requireAuthenticatedControl, basePath, camouflage
	requireAuthenticatedControl = true
	basePath = "/mbox"
	camouflage.DownPath = "/sync/"
	defer func() {
		requireAuthenticatedControl, basePath, camouflage = oldRequire, oldBase, oldCamouflage
		sessionsMu.Lock()
		sessions = oldSessions
		sessionsMu.Unlock()
	}()
	path := basePath + camouflage.DownPath + sid
	tag := controlmeta.Tag(key, http.MethodGet, "DOWN", 17, 0, "4096")
	signed := fmt.Sprintf("https://relay.invalid%s?seq=17&ack=0&max=4096&cmac=%s", path, tag)

	forged := fmt.Sprintf("https://relay.invalid%s?seq=17&ack=4&max=4096&cmac=%s", path, tag)
	w := httptest.NewRecorder()
	handleDown(w, httptest.NewRequest(http.MethodGet, forged, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("forged ack status=%d", w.Code)
	}
	s.mu.Lock()
	if s.lastSeen.After(time.Now().Add(-30 * time.Second)) {
		t.Fatal("unauthenticated poll refreshed session TTL")
	}
	if s.lastDownAck != 0 || len(s.downReplay) != 1 {
		t.Fatal("forged ack altered replay state")
	}
	s.mu.Unlock()

	w = httptest.NewRecorder()
	handleDown(w, httptest.NewRequest(http.MethodGet, signed, nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("signed down status=%d", w.Code)
	}
	s.mu.Lock()
	if s.lastSeen.Before(time.Now().Add(-10 * time.Second)) {
		t.Fatal("authenticated poll failed to refresh TTL")
	}
	s.mu.Unlock()

	w = httptest.NewRecorder()
	handleDown(w, httptest.NewRequest(http.MethodGet, "https://relay.invalid"+path+"?seq=19&ack=4", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("strict mode accepted missing MAC: %d", w.Code)
	}
}

func TestSignedCloseCannotBeForged(t *testing.T) {
	const sid = "0123456789abcdef0123456789abcdef"
	var key [32]byte
	copy(key[:], bytes.Repeat([]byte{5}, 32))
	s := &session{crypto: &cryptoState{controlKey: key}, streams: map[uint32]*stream{}, downReplay: map[uint64]cachedDownResponse{}, done: make(chan struct{})}
	sessionsMu.Lock()
	oldSessions := sessions
	sessions = map[string]*session{sid: s}
	sessionsMu.Unlock()
	oldRequire, oldBase, oldCamouflage := requireAuthenticatedControl, basePath, camouflage
	requireAuthenticatedControl = true
	basePath = "/mbox"
	camouflage.ClosePath = "/close/"
	defer func() {
		requireAuthenticatedControl, basePath, camouflage = oldRequire, oldBase, oldCamouflage
		sessionsMu.Lock()
		sessions = oldSessions
		sessionsMu.Unlock()
	}()
	path := basePath + camouflage.ClosePath + sid
	for _, url := range []string{
		"https://relay.invalid" + path,
		"https://relay.invalid" + path + "?cmac=garbage",
	} {
		w := httptest.NewRecorder()
		handleClose(w, httptest.NewRequest(http.MethodPost, url, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("forged close accepted %d", w.Code)
		}
		sessionsMu.Lock()
		_, alive := sessions[sid]
		sessionsMu.Unlock()
		if !alive {
			t.Fatal("forged close removed live session")
		}
	}
	signed := "https://relay.invalid" + path + "?cmac=" + controlmeta.Tag(key, http.MethodPost, "CLOSE", 0, 0, "")
	w := httptest.NewRecorder()
	handleClose(w, httptest.NewRequest(http.MethodPost, signed, nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("signed close status %d", w.Code)
	}
	sessionsMu.Lock()
	_, alive := sessions[sid]
	sessionsMu.Unlock()
	if alive {
		t.Fatal("signed close failed")
	}
}

func TestControlMACTransitionModeRejectsInvalidPresentSignature(t *testing.T) {
	var key [32]byte
	s := &session{crypto: &cryptoState{controlKey: key}}
	old := requireAuthenticatedControl
	requireAuthenticatedControl = false
	defer func() { requireAuthenticatedControl = old }()
	for _, raw := range []string{
		"https://relay.invalid/sync?seq=1&ack=0",
	} {
		req := httptest.NewRequest(http.MethodGet, raw, nil)
		if !verifyHTTPControlMeta(s, req, "DOWN", 1, 0) {
			t.Fatal("transition rejected legacy request")
		}
	}
	req := httptest.NewRequest(http.MethodGet, "https://relay.invalid/sync?seq=1&ack=0&cmac=fake", nil)
	if verifyHTTPControlMeta(s, req, "DOWN", 1, 0) {
		t.Fatal("transition silently downgraded invalid MAC")
	}
}
