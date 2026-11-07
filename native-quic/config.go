package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

// originPaths is the same minimal set that MBox's HTTP handlers register.
// Never grant a public QUIC client access to arbitrary origin endpoints.
type originPaths struct{ open, up, down, close string }

func readPaths(path string) (originPaths, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return originPaths{}, err
	}
	var cfg struct {
		Transport struct {
			BasePath string `json:"base_path"`
		} `json:"transport"`
		Camouflage struct {
			OpenPath  string `json:"open_path"`
			UpPath    string `json:"up_path"`
			DownPath  string `json:"down_path"`
			ClosePath string `json:"close_path"`
		} `json:"camouflage"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return originPaths{}, err
	}
	c := cfg.Camouflage
	base := cfg.Transport.BasePath
	if base != "" && (!strings.HasPrefix(base, "/") || strings.HasSuffix(base, "/") || strings.Contains(base, "..")) {
		return originPaths{}, fmt.Errorf("invalid base_path")
	}
	defaults := []string{"/api/v1/session", "/api/v1/events/", "/api/v1/sync/", "/api/v1/session/"}
	fields := []string{c.OpenPath, c.UpPath, c.DownPath, c.ClosePath}
	for i, v := range fields {
		if v == "" {
			fields[i] = defaults[i]
		}
	}
	for _, v := range fields {
		if !strings.HasPrefix(v, "/") || strings.Contains(v, "..") || strings.Contains(v, "//") {
			return originPaths{}, fmt.Errorf("invalid camouflage path")
		}
	}
	if fields[0] == fields[3] || !strings.HasSuffix(fields[1], "/") || !strings.HasSuffix(fields[2], "/") || !strings.HasSuffix(fields[3], "/") {
		return originPaths{}, fmt.Errorf("invalid open/up/down/close paths")
	}
	return originPaths{base + fields[0], base + fields[1], base + fields[2], base + fields[3]}, nil
}
func (p originPaths) allowed(path string) bool {
	if path == p.open {
		return true
	}
	for _, prefix := range []string{p.up, p.down, p.close} {
		if strings.HasPrefix(path, prefix) && len(path) > len(prefix) {
			return true
		}
	}
	return false
}

func loopbackURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("origin must be a loopback http://host:port URL")
	}
	host, _, err := net.SplitHostPort(u.Host)
	if err != nil {
		return nil, fmt.Errorf("origin must include port: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("origin must be a loopback IP, not a hostname")
	}
	return u, nil
}
func remoteURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("remote must be an https://host:port URL")
	}
	if _, _, err := net.SplitHostPort(u.Host); err != nil {
		return nil, fmt.Errorf("remote must include QUIC port: %w", err)
	}
	return u, nil
}
func localListen(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("local listener must bind a numeric loopback IP")
	}
	return nil
}
