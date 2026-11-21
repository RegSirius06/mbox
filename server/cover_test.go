package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalizeCoverDefaults(t *testing.T) {
	c, err := normalizeCoverConfig(coverConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Enabled {
		t.Fatal("cover must default to disabled")
	}
	if c.Path != "/api/v1/assets/" {
		t.Fatalf("unexpected path %q", c.Path)
	}
	if c.MaxRPS != 4 || c.MaxRequestsPerHour != 1200 {
		t.Fatalf("unexpected limits %+v", c)
	}
}

func TestCoverResourceHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://localhost/edge/test/api/v1/assets/a.js", nil)
	rr := httptest.NewRecorder()
	coverResourceHandler(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	if rr.Body.Len() == 0 {
		t.Fatal("empty cover response")
	}
	if rr.Header().Get("Content-Type") != "application/javascript" {
		t.Fatalf("content-type=%q", rr.Header().Get("Content-Type"))
	}
}

func TestCoverResourcePost(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://localhost/edge/test/api/v1/assets/telemetry.json", strings.NewReader(`{"event":"view"}`))
	rr := httptest.NewRecorder()
	coverResourceHandler(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestActiveRealSessions(t *testing.T) {
	sessionsMu.Lock()
	old := sessions
	sessions = map[string]*session{
		"recent": {lastSeen: time.Now()},
		"old":    {lastSeen: time.Now().Add(-10 * time.Minute)},
	}
	sessionsMu.Unlock()
	defer func() { sessionsMu.Lock(); sessions = old; sessionsMu.Unlock() }()
	if n := activeRealSessions(2 * time.Minute); n != 1 {
		t.Fatalf("active=%d", n)
	}
}

func TestCoverSuppressesRequestsForRealSessions(t *testing.T) {
	sessionsMu.Lock()
	old := sessions
	sessions = map[string]*session{"recent": {lastSeen: time.Now()}}
	sessionsMu.Unlock()
	defer func() { sessionsMu.Lock(); sessions = old; sessionsMu.Unlock() }()
	requests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()
	e := &coverEngine{
		cfg: coverConfig{PublicURL: ts.URL, Path: "/assets/", IdleAfterMS: 120000, MaxRPS: 10, MaxRequestsPerHour: 10},
		rng: seedRand(), client: ts.Client(),
	}
	if e.sendOne(context.Background()) || requests != 0 {
		t.Fatal("cover must not call frontend while a real session is active")
	}
	if e.waitContext(context.Background(), time.Second) {
		t.Fatal("cover wait must be interrupted by real session")
	}
	sessionsMu.Lock()
	delete(sessions, "recent")
	sessionsMu.Unlock()
	if !e.sendOne(context.Background()) || requests != 1 {
		t.Fatal("cover should resume after session closes")
	}
}

func TestCoverOpenCancelsCurrentProfile(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e := &coverEngine{cfg: coverConfig{Enabled: true}, currentCancel: cancel}
	old := globalCover
	globalCover = e
	defer func() { globalCover = old }()
	coverSessionOpened()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("authenticated OPEN must cancel in-flight cover profile")
	}
}

func TestCoverDoesNotForgeBrowserHeaders(t *testing.T) {
	var got http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()
	e := &coverEngine{
		cfg:    coverConfig{PublicURL: ts.URL, Path: "/assets/", MaxRPS: 10, MaxRequestsPerHour: 10},
		rng:    seedRand(),
		client: ts.Client(),
	}
	if !e.sendOne(context.Background()) {
		t.Fatal("cover request failed")
	}
	if ua := got.Get("User-Agent"); ua != "Go-http-client/1.1" && ua != "Go-http-client/2.0" {
		t.Fatalf("unexpected forged User-Agent %q", ua)
	}
	if got.Get("Accept-Language") != "" {
		t.Fatalf("unexpected browser-like Accept-Language %q", got.Get("Accept-Language"))
	}
}
