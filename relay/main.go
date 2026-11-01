package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/fcgi"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type relayConfig struct {
	Target          string   `json:"target"`
	AllowedExact    []string `json:"allowed_exact"`
	AllowedPrefixes []string `json:"allowed_prefixes"`
	MaxSlots        int      `json:"max_slots"`
	MaxBodyBytes    int64    `json:"max_body_bytes"`
	SlotLockPrefix  string   `json:"slot_lock_prefix"`
}

func configPath() (string, error) {
	if p := os.Getenv("MBOX_RELAY_CONFIG"); p != "" {
		return p, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), "relay.json"), nil
}

func loadConfig() (*relayConfig, error) {
	p, err := configPath()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}

	var cfg relayConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	u, err := url.Parse(cfg.Target)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("target must be an https URL")
	}
	if len(cfg.AllowedExact) == 0 && len(cfg.AllowedPrefixes) == 0 {
		return nil, fmt.Errorf("at least one allowed path is required")
	}
	if cfg.MaxSlots < 1 || cfg.MaxSlots > 128 {
		return nil, fmt.Errorf("max_slots out of range")
	}
	if cfg.MaxBodyBytes < 1024 || cfg.MaxBodyBytes > 16*1024*1024 {
		return nil, fmt.Errorf("max_body_bytes out of range")
	}
	if cfg.SlotLockPrefix == "" {
		return nil, fmt.Errorf("slot_lock_prefix is required")
	}
	for _, p := range append(append([]string{}, cfg.AllowedExact...), cfg.AllowedPrefixes...) {
		if p == "" || p[0] != '/' || strings.ContainsAny(p, "?#") {
			return nil, fmt.Errorf("invalid allowed path %q", p)
		}
	}
	return &cfg, nil
}

func acquireSlot(cfg *relayConfig) (*os.File, bool) {
	for i := 0; i < cfg.MaxSlots; i++ {
		path := fmt.Sprintf("%s-%02d.lock", cfg.SlotLockPrefix, i)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			continue
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			return f, true
		}
		_ = f.Close()
	}
	return nil, false
}

func releaseSlot(f *os.File) {
	if f == nil {
		return
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	_ = f.Close()
}

func allowedPath(cfg *relayConfig, path string) bool {
	for _, exact := range cfg.AllowedExact {
		if path == exact {
			return true
		}
	}
	for _, prefix := range cfg.AllowedPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("CONFIG: %v", err)
	}

	target, _ := url.Parse(cfg.Target)

	// FastCGI requests are short, complete HTTP transactions. Connection reuse
	// is still valuable on the relay->origin leg, so keep a small HTTP/2 pool.
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 60 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          cfg.MaxSlots * 2,
		MaxIdleConnsPerHost:   cfg.MaxSlots,
		MaxConnsPerHost:       cfg.MaxSlots,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ExpectContinueTimeout: time.Second,
		DisableCompression:    true,
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = transport
	proxy.FlushInterval = -1

	oldDirector := proxy.Director
	proxy.Director = func(r *http.Request) {
		// REG.RU invokes the wrapper as /relay.fcgi/<original path>. Remove only
		// the wrapper prefix; the configured MBox path is forwarded unchanged.
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/relay.fcgi")
		if r.URL.Path == "" {
			r.URL.Path = "/"
		}
		oldDirector(r)
		r.Host = target.Host
		r.Header.Del("Connection")
		r.Header.Del("Proxy-Connection")
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("upstream error: %v", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/relay.fcgi")
		if !allowedPath(cfg, path) {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxBodyBytes)
		}

		// The shared host starts multiple FastCGI processes. Flock provides one
		// account-wide concurrency limit instead of a limit per process.
		slot, ok := acquireSlot(cfg)
		if !ok {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "Service Busy", http.StatusServiceUnavailable)
			return
		}
		defer releaseSlot(slot)
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		proxy.ServeHTTP(w, r.WithContext(ctx))
	})

	if err := fcgi.Serve(nil, handler); err != nil {
		log.Fatal(err)
	}
}
