package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// The Go core can use the app-private local QUIC gateway instead of the
// Chromium HTTPS bridge. No physical socket is opened by this transport.
func androidQUICUnixFromEnv(origin *url.URL) (http.RoundTripper, bool, error) {
	path := os.Getenv("MBOX_QUIC_UNIX")
	if path == "" {
		return nil, false, nil
	}
	if !developerModeEnabled() || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, false, errors.New("Android QUIC local socket requires developer mode and absolute path")
	}
	if os.Getenv("MBOX_HTTP_BRIDGE_ADDR") != "" || origin == nil || origin.Scheme != "http" || !loopbackHost(origin.Hostname()) {
		return nil, false, errors.New("Android QUIC Unix transport requires loopback HTTP and no Cronet bridge")
	}
	return &http.Transport{Proxy: nil, DisableCompression: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", path)
		},
		MaxIdleConnsPerHost: 8, IdleConnTimeout: 15 * time.Second,
	}, true, nil
}
