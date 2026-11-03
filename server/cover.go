package main

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	mrand "math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type coverConfig struct {
	Enabled            bool   `json:"enabled"`
	PublicURL          string `json:"public_url"`
	Path               string `json:"path"`
	StatusFile         string `json:"status_file"`
	IdleAfterMS        int    `json:"idle_after_ms"`
	StartGraceMinMS    int    `json:"start_grace_min_ms"`
	StartGraceMaxMS    int    `json:"start_grace_max_ms"`
	MaxRPS             int    `json:"max_rps"`
	MaxRequestsPerHour int    `json:"max_requests_per_hour"`
	RequestTimeoutMS   int    `json:"request_timeout_ms"`
	QuietWeight        int    `json:"quiet_weight"`
	CasualWeight       int    `json:"casual_weight"`
	ActiveWeight       int    `json:"active_weight"`
	BurstWeight        int    `json:"burst_weight"`
}

type coverStatus struct {
	Enabled          bool   `json:"enabled"`
	State            string `json:"state"`
	ActiveSessions   int    `json:"active_sessions"`
	RequestsLastHour int    `json:"requests_last_hour"`
	TotalRequests    uint64 `json:"total_requests"`
	LastRequestAt    string `json:"last_request_at,omitempty"`
	LastError        string `json:"last_error,omitempty"`
	StateSince       string `json:"state_since"`
}

type coverEngine struct {
	cfg    coverConfig
	rng    *mrand.Rand
	client *http.Client

	mu            sync.Mutex
	currentCancel context.CancelFunc
	state         string
	stateSince    time.Time
	reqTimes      []time.Time
	total         uint64
	lastAttempt   time.Time
	lastRequest   time.Time
	lastError     string
}

var globalCover *coverEngine

func normalizeCoverConfig(c coverConfig) (coverConfig, error) {
	if c.Path == "" {
		c.Path = "/api/v1/assets/"
	}
	if !strings.HasPrefix(c.Path, "/") || !strings.HasSuffix(c.Path, "/") || strings.ContainsAny(c.Path, "?#") {
		return c, fmt.Errorf("cover.path must begin and end with / and contain no ? or #")
	}
	if c.StatusFile == "" {
		c.StatusFile = "/run/mbox/cover-status.json"
	}
	if c.IdleAfterMS == 0 {
		c.IdleAfterMS = 120000
	}
	if c.StartGraceMinMS == 0 {
		c.StartGraceMinMS = 60000
	}
	if c.StartGraceMaxMS == 0 {
		c.StartGraceMaxMS = 180000
	}
	if c.MaxRPS == 0 {
		c.MaxRPS = 4
	}
	if c.MaxRequestsPerHour == 0 {
		c.MaxRequestsPerHour = 1200
	}
	if c.RequestTimeoutMS == 0 {
		c.RequestTimeoutMS = 8000
	}
	if c.QuietWeight == 0 && c.CasualWeight == 0 && c.ActiveWeight == 0 && c.BurstWeight == 0 {
		c.QuietWeight, c.CasualWeight, c.ActiveWeight, c.BurstWeight = 35, 40, 20, 5
	}
	if c.IdleAfterMS < 10000 || c.IdleAfterMS > 30*60*1000 {
		return c, fmt.Errorf("cover.idle_after_ms out of range")
	}
	if c.StartGraceMinMS < 0 || c.StartGraceMaxMS < c.StartGraceMinMS || c.StartGraceMaxMS > 60*60*1000 {
		return c, fmt.Errorf("cover start grace range invalid")
	}
	if c.MaxRPS < 1 || c.MaxRPS > 50 {
		return c, fmt.Errorf("cover.max_rps out of range 1..50")
	}
	if c.MaxRequestsPerHour < 1 || c.MaxRequestsPerHour > 100000 {
		return c, fmt.Errorf("cover.max_requests_per_hour out of range")
	}
	if c.RequestTimeoutMS < 1000 || c.RequestTimeoutMS > 60000 {
		return c, fmt.Errorf("cover.request_timeout_ms out of range")
	}
	if c.PublicURL == "" && c.Enabled {
		return c, fmt.Errorf("cover.public_url is required when cover is enabled")
	}
	for _, w := range []int{c.QuietWeight, c.CasualWeight, c.ActiveWeight, c.BurstWeight} {
		if w < 0 || w > 10000 {
			return c, fmt.Errorf("cover profile weight out of range")
		}
	}
	if c.QuietWeight+c.CasualWeight+c.ActiveWeight+c.BurstWeight <= 0 {
		return c, fmt.Errorf("cover profile weights sum to zero")
	}
	return c, nil
}

func activeRealSessions(idleAfter time.Duration) int {
	cutoff := time.Now().Add(-idleAfter)
	n := 0
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	for _, s := range sessions {
		s.mu.Lock()
		last := s.lastSeen
		closed := s.closed
		s.mu.Unlock()
		if !closed && last.After(cutoff) {
			n++
		}
	}
	return n
}

func seedRand() *mrand.Rand {
	var b [8]byte
	if _, err := crand.Read(b[:]); err != nil {
		return mrand.New(mrand.NewSource(time.Now().UnixNano()))
	}
	var seed int64
	for _, x := range b {
		seed = seed<<8 | int64(x)
	}
	return mrand.New(mrand.NewSource(seed))
}

func startCoverEngine(cfg coverConfig) {
	cfg, err := normalizeCoverConfig(cfg)
	if err != nil {
		log.Printf("COVER CONFIG: %v", err)
		return
	}
	e := &coverEngine{
		cfg:        cfg,
		rng:        seedRand(),
		client:     &http.Client{Timeout: time.Duration(cfg.RequestTimeoutMS) * time.Millisecond},
		state:      "disabled",
		stateSince: time.Now(),
	}
	globalCover = e
	e.writeStatus()
	if !cfg.Enabled {
		log.Printf("COVER disabled")
		return
	}
	log.Printf("COVER enabled public=%s path=%s max_rps=%d max_hour=%d", cfg.PublicURL, cfg.Path, cfg.MaxRPS, cfg.MaxRequestsPerHour)
	go e.run()
}

func (e *coverEngine) run() {
	// Privacy invariant: server-side cover is frontend background activity, not
	// a per-user signal. Its lifecycle must never depend on whether a real MBox
	// session exists, otherwise frontend observers get a negative correlation
	// oracle for real-session transitions.
	for {
		grace := e.randDuration(e.cfg.StartGraceMinMS, e.cfg.StartGraceMaxMS)
		e.setState("grace")
		if !e.waitContext(context.Background(), grace) {
			continue
		}

		ctx, cancel := context.WithCancel(context.Background())
		e.mu.Lock()
		e.currentCancel = cancel
		e.mu.Unlock()
		profile := e.chooseProfile()
		e.setState(profile)
		e.runProfile(ctx, profile)
		cancel()
		e.mu.Lock()
		e.currentCancel = nil
		e.mu.Unlock()
	}
}

func (e *coverEngine) chooseProfile() string {
	total := e.cfg.QuietWeight + e.cfg.CasualWeight + e.cfg.ActiveWeight + e.cfg.BurstWeight
	x := e.rng.Intn(total)
	if x < e.cfg.QuietWeight {
		return "quiet"
	}
	x -= e.cfg.QuietWeight
	if x < e.cfg.CasualWeight {
		return "casual"
	}
	x -= e.cfg.CasualWeight
	if x < e.cfg.ActiveWeight {
		return "active"
	}
	return "burst"
}

func (e *coverEngine) runProfile(ctx context.Context, profile string) {
	switch profile {
	case "quiet":
		e.waitContext(ctx, e.randDuration(60_000, 20*60_000))
	case "casual":
		pages := e.randInt(1, 5)
		for i := 0; i < pages; i++ {
			if !e.page(ctx, e.randInt(4, 14), 120, 900) {
				return
			}
			if !e.waitContext(ctx, e.randDuration(5_000, 120_000)) {
				return
			}
		}
	case "active":
		pages := e.randInt(3, 12)
		for i := 0; i < pages; i++ {
			if !e.page(ctx, e.randInt(8, 30), 60, 500) {
				return
			}
			if !e.waitContext(ctx, e.randDuration(2_000, 45_000)) {
				return
			}
		}
	case "burst":
		_ = e.page(ctx, e.randInt(30, 120), 50, 350)
	}
}

func (e *coverEngine) page(ctx context.Context, count, minDelayMS, maxDelayMS int) bool {
	for i := 0; i < count; i++ {
		if ctx.Err() != nil {
			return false
		}
		if !e.sendOne(ctx) {
			if ctx.Err() != nil {
				return false
			}
		}
		if !e.waitContext(ctx, e.randDuration(minDelayMS, maxDelayMS)) {
			return false
		}
	}
	return true
}

func (e *coverEngine) sendOne(ctx context.Context) bool {
	if !e.allowRequest() {
		return false
	}
	ext := e.randomResourceType()
	var token [8]byte
	_, _ = crand.Read(token[:])
	method := http.MethodGet
	var body io.Reader
	if e.rng.Intn(100) < 10 {
		method = http.MethodPost
		ext = "json"
		var pad [96]byte
		_, _ = crand.Read(pad[:])
		payload := fmt.Sprintf(`{"event":"view","id":"%s","ts":%d,"data":"%s"}`, hex.EncodeToString(token[:]), time.Now().UnixMilli(), hex.EncodeToString(pad[:]))
		body = strings.NewReader(payload)
	}
	path := strings.TrimRight(e.cfg.PublicURL, "/") + e.cfg.Path + hex.EncodeToString(token[:]) + "." + ext
	req, err := http.NewRequestWithContext(ctx, method, path, body)
	if err != nil {
		e.noteError(err)
		return false
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	started := time.Now()
	resp, err := e.client.Do(req)
	if err != nil {
		e.noteError(err)
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 256*1024))
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		e.noteError(fmt.Errorf("HTTP %d", resp.StatusCode))
		return false
	}
	e.noteRequest(started)
	return true
}

func (e *coverEngine) allowRequest() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-time.Hour)
	j := 0
	for _, t := range e.reqTimes {
		if t.After(cutoff) {
			e.reqTimes[j] = t
			j++
		}
	}
	e.reqTimes = e.reqTimes[:j]
	if len(e.reqTimes) >= e.cfg.MaxRequestsPerHour {
		return false
	}
	if !e.lastAttempt.IsZero() {
		minGap := time.Second / time.Duration(e.cfg.MaxRPS)
		if time.Since(e.lastAttempt) < minGap {
			return false
		}
	}
	e.lastAttempt = now
	e.reqTimes = append(e.reqTimes, now)
	return true
}

func (e *coverEngine) noteRequest(t time.Time) {
	e.mu.Lock()
	_ = t
	e.total++
	e.lastRequest = time.Now()
	e.lastError = ""
	e.mu.Unlock()
	e.writeStatus()
}
func (e *coverEngine) noteError(err error) {
	e.mu.Lock()
	e.lastError = err.Error()
	e.mu.Unlock()
	e.writeStatus()
}
func (e *coverEngine) setState(s string) {
	e.mu.Lock()
	if e.state != s {
		e.state = s
		e.stateSince = time.Now()
		log.Printf("COVER state=%s", s)
	}
	e.mu.Unlock()
	e.writeStatus()
}

func (e *coverEngine) writeStatus() {
	if e.cfg.StatusFile == "" {
		return
	}
	idleAfter := time.Duration(e.cfg.IdleAfterMS) * time.Millisecond
	e.mu.Lock()
	cutoff := time.Now().Add(-time.Hour)
	n := 0
	for _, t := range e.reqTimes {
		if t.After(cutoff) {
			n++
		}
	}
	s := coverStatus{Enabled: e.cfg.Enabled, State: e.state, ActiveSessions: activeRealSessions(idleAfter), RequestsLastHour: n, TotalRequests: e.total, LastError: e.lastError, StateSince: e.stateSince.Format(time.RFC3339)}
	if !e.lastRequest.IsZero() {
		s.LastRequestAt = e.lastRequest.Format(time.RFC3339)
	}
	e.mu.Unlock()
	b, _ := json.MarshalIndent(s, "", "  ")
	_ = os.MkdirAll(filepath.Dir(e.cfg.StatusFile), 0755)
	tmp := e.cfg.StatusFile + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0644); err == nil {
		_ = os.Rename(tmp, e.cfg.StatusFile)
	}
}

func (e *coverEngine) waitContext(ctx context.Context, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return false
		}
		step := time.Second
		if rem := time.Until(deadline); rem < step {
			step = rem
		}
		timer := time.NewTimer(step)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
	return true
}
func (e *coverEngine) randDuration(minMS, maxMS int) time.Duration {
	if maxMS <= minMS {
		return time.Duration(minMS) * time.Millisecond
	}
	return time.Duration(minMS+e.rng.Intn(maxMS-minMS+1)) * time.Millisecond
}
func (e *coverEngine) randInt(a, b int) int {
	if b <= a {
		return a
	}
	return a + e.rng.Intn(b-a+1)
}
func (e *coverEngine) randomResourceType() string {
	x := e.rng.Intn(100)
	switch {
	case x < 18:
		return "html"
	case x < 35:
		return "css"
	case x < 62:
		return "js"
	case x < 72:
		return "json"
	case x < 84:
		return "svg"
	default:
		return "webp"
	}
}

func coverResourceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		_, _ = io.Copy(io.Discard, http.MaxBytesReader(w, r.Body, 16*1024))
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		camouflageNotFound(w)
		return
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(r.URL.Path), "."))
	min, max := 512, 4096
	ctype := "application/octet-stream"
	switch ext {
	case "html":
		min, max, ctype = 2048, 16384, "text/html; charset=utf-8"
	case "css":
		min, max, ctype = 1024, 24576, "text/css; charset=utf-8"
	case "js":
		min, max, ctype = 2048, 32768, "application/javascript"
	case "json":
		min, max, ctype = 512, 4096, "application/json"
	case "svg":
		min, max, ctype = 1024, 8192, "image/svg+xml"
	case "webp":
		min, max, ctype = 4096, 32768, "image/webp"
	}
	var rb [2]byte
	_, _ = crand.Read(rb[:])
	n := min + int(rb[0])*(max-min+1)/256
	body := make([]byte, n)
	_, _ = crand.Read(body)
	if ext == "html" {
		prefix := []byte("<!doctype html><html><head><meta charset=\"utf-8\"></head><body>")
		copy(body, prefix)
	}
	if ext == "css" {
		prefix := []byte("/* asset */\n:root{--x:1}\n")
		copy(body, prefix)
	}
	if ext == "js" {
		prefix := []byte("/* asset */\n(()=>{const x=1;})();\n")
		copy(body, prefix)
	}
	if ext == "json" {
		body = []byte(fmt.Sprintf("{\"ok\":true,\"ts\":%d,\"pad\":\"%s\"}", time.Now().Unix(), hex.EncodeToString(body[:minInt(len(body)/4, 512)])))
	}
	if ext == "svg" {
		body = []byte(fmt.Sprintf("<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"64\" height=\"64\"><rect width=\"64\" height=\"64\" fill=\"#%06x\"/></svg>", eRandColor()))
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func eRandColor() int {
	var b [3]byte
	_, _ = crand.Read(b[:])
	return int(b[0])<<16 | int(b[1])<<8 | int(b[2])
}
