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
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

type relayConfig struct {
	Target          string   `json:"target"`
	AllowedExact    []string `json:"allowed_exact"`
	AllowedPrefixes []string `json:"allowed_prefixes"`
	MaxSlots        int      `json:"max_slots"`
	MaxBodyBytes    int64    `json:"max_body_bytes"`
	RuntimeDir      string   `json:"runtime_dir"`
	// Deprecated and intentionally rejected. Predictable shared /tmp prefixes
	// are not an acceptable lock ownership boundary.
	SlotLockPrefix string `json:"slot_lock_prefix,omitempty"`
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
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("target must be a clean https URL")
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
	if cfg.RuntimeDir == "" {
		if cfg.SlotLockPrefix != "" {
			return nil, fmt.Errorf("slot_lock_prefix is no longer supported; configure private runtime_dir")
		}
		return nil, fmt.Errorf("runtime_dir is required")
	}
	if err := ensurePrivateRuntimeDir(cfg.RuntimeDir); err != nil {
		return nil, fmt.Errorf("runtime_dir: %w", err)
	}
	for _, candidate := range append(append([]string{}, cfg.AllowedExact...), cfg.AllowedPrefixes...) {
		canonical, err := canonicalPath(candidate)
		if err != nil || canonical != candidate {
			return nil, fmt.Errorf("invalid allowed path %q", candidate)
		}
	}
	return &cfg, nil
}

func ensurePrivateRuntimeDir(dir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("must be absolute")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("must be a real directory, not a symlink")
	}
	if info.Mode().Perm() != 0o700 {
		return fmt.Errorf("permissions must be 0700, got %04o", info.Mode().Perm())
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Geteuid() {
		return fmt.Errorf("directory owner uid=%d does not match process uid=%d", stat.Uid, os.Geteuid())
	}
	return nil
}

func secureOpenLock(path string) (*os.File, error) {
	// Security invariant: a shared-host peer must not redirect relay lock files
	// through a symlink or substitute a non-regular file.
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	if f == nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("failed to wrap lock fd")
	}
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		_ = f.Close()
		return nil, err
	}
	if st.Mode&syscall.S_IFMT != syscall.S_IFREG || int(st.Uid) != os.Geteuid() || st.Mode&0o077 != 0 {
		_ = f.Close()
		return nil, fmt.Errorf("unsafe lock owner/type/mode")
	}
	return f, nil
}

func acquireSlot(cfg *relayConfig) (*os.File, bool) {
	for i := 0; i < cfg.MaxSlots; i++ {
		lockPath := filepath.Join(cfg.RuntimeDir, fmt.Sprintf("slot-%03d.lock", i))
		f, err := secureOpenLock(lockPath)
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

func isHex(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

func containsResidualEscape(s string) bool {
	for i := 0; i+2 < len(s); i++ {
		if s[i] == '%' && isHex(s[i+1]) && isHex(s[i+2]) {
			return true
		}
	}
	return false
}

func canonicalPath(raw string) (string, error) {
	if raw == "" {
		return "/", nil
	}
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") || strings.Contains(raw, "\\") {
		return "", fmt.Errorf("encoded slash/backslash is not allowed")
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "", fmt.Errorf("malformed path escape")
	}
	if !utf8.ValidString(decoded) || !strings.HasPrefix(decoded, "/") || strings.ContainsRune(decoded, 0) || strings.Contains(decoded, "\\") {
		return "", fmt.Errorf("invalid path")
	}
	// Reject double-encoding rather than letting a later proxy/webserver apply a
	// second interpretation (%252f -> %2f -> '/').
	if containsResidualEscape(decoded) {
		return "", fmt.Errorf("double-encoded path is not allowed")
	}
	if strings.Contains(decoded, "//") {
		return "", fmt.Errorf("duplicate slash is not allowed")
	}
	trailing := strings.HasSuffix(decoded, "/") && decoded != "/"
	probe := decoded
	if trailing {
		probe = strings.TrimSuffix(probe, "/")
	}
	if path.Clean(probe) != probe {
		return "", fmt.Errorf("dot/traversal segment is not allowed")
	}
	return decoded, nil
}

func stripWrapperPath(canonical string) (string, bool) {
	if canonical == "/relay.fcgi" || canonical == "/relay.fcgi/" {
		return "/", true
	}
	if strings.HasPrefix(canonical, "/relay.fcgi/") {
		return canonical[len("/relay.fcgi"):], true
	}
	return "", false
}

func allowedPath(cfg *relayConfig, candidate string) bool {
	for _, exact := range cfg.AllowedExact {
		if candidate == exact {
			return true
		}
	}
	for _, prefix := range cfg.AllowedPrefixes {
		if strings.HasSuffix(prefix, "/") {
			if strings.HasPrefix(candidate, prefix) {
				return true
			}
			continue
		}
		if candidate == prefix || strings.HasPrefix(candidate, prefix+"/") {
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
		oldDirector(r)
		r.Host = target.Host
		r.Header.Del("Connection")
		r.Header.Del("Proxy-Connection")
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("upstream error")
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		canonical, err := canonicalPath(r.URL.EscapedPath())
		if err != nil {
			http.NotFound(w, r)
			return
		}
		forwardPath, ok := stripWrapperPath(canonical)
		if !ok || !allowedPath(cfg, forwardPath) {
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

		slot, ok := acquireSlot(cfg)
		if !ok {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "Service Busy", http.StatusServiceUnavailable)
			return
		}
		defer releaseSlot(slot)
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		r.URL.Path = forwardPath
		r.URL.RawPath = ""
		proxy.ServeHTTP(w, r.WithContext(ctx))
	})

	if err := fcgi.Serve(nil, handler); err != nil {
		log.Fatal(err)
	}
}
