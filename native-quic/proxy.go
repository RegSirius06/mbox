package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/RegSirius06/mbox/common/originauth"
	"github.com/quic-go/quic-go/http3"
)

const maxBody = 8 << 20
const maxConcurrent = 16

// Only the MBox endpoint's request/response metadata can cross the gateway.
// X-MBox-Origin-* headers are always freshly generated on the VPS.
func copySafeHeaders(dst, src http.Header) {
	for k, vv := range src {
		switch strings.ToLower(k) {
		case "host", "connection", "proxy-connection", "transfer-encoding", "upgrade", "content-length",
			"keep-alive", "te", "trailer", "forwarded", "x-forwarded-for", "x-forwarded-proto", "x-real-ip",
			"x-mbox-origin-timestamp", "x-mbox-origin-nonce", "x-mbox-origin-signature":
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}
func readBounded(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	b, err := io.ReadAll(io.LimitReader(r, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxBody {
		return nil, fmt.Errorf("body too large")
	}
	return b, nil
}
func validRequest(r *http.Request, paths originPaths) bool {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		return false
	}
	if r.URL == nil || r.URL.Path == "" || !paths.allowed(r.URL.Path) {
		return false
	}
	if r.URL.EscapedPath() != r.URL.Path || strings.Contains(r.URL.Path, "//") || strings.Contains(r.URL.Path, "..") {
		return false
	}
	return true
}

// Both sides use the same bounded proxy. The VPS signs each request for the
// existing origin admission gate; the client never receives that secret.
func makeProxyHandler(target *url.URL, paths originPaths, secret []byte, client *http.Client) http.Handler {
	slots := make(chan struct{}, maxConcurrent)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !validRequest(r, paths) {
			http.NotFound(w, r)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		body, err := readBounded(r.Body)
		if err != nil {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		dest := *target
		dest.Path, dest.RawQuery = r.URL.Path, r.URL.RawQuery
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		out, err := http.NewRequestWithContext(ctx, r.Method, dest.String(), bytes.NewReader(body))
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		copySafeHeaders(out.Header, r.Header)
		if secret != nil {
			ts, nonce, sig, err := originauth.SignRequest(secret, r.Method, r.URL.Path, r.URL.RawQuery, body, time.Now())
			if err != nil {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
			out.Header.Set(originauth.TimestampHeader, ts)
			out.Header.Set(originauth.NonceHeader, nonce)
			out.Header.Set(originauth.SignatureHeader, sig)
		}
		resp, err := client.Do(out)
		if err != nil {
			http.Error(w, "transport unavailable", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		response, err := readBounded(resp.Body)
		if err != nil {
			http.Error(w, "response too large", http.StatusBadGateway)
			return
		}
		copySafeHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(response)
	})
}
func makeServerHandler(origin *url.URL, paths originPaths, secret []byte, client *http.Client) http.Handler {
	return makeProxyHandler(origin, paths, secret, client)
}
func makeClientHandler(remote *url.URL, paths originPaths, client *http.Client) http.Handler {
	return makeProxyHandler(remote, paths, nil, client)
}

func readSecret(path string) ([]byte, error) {
	if path == "" {
		return nil, errors.New("origin HMAC secret required")
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	// Match mboxctl and mbox-server: root:mbox 0640 is an intentional
	// permission model. Deny group write/execute and any other access.
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o037 != 0 {
		return nil, errors.New("origin HMAC key must be regular; group write/execute and other access forbidden")
	}
	b, err := io.ReadAll(io.LimitReader(f, 129))
	if err != nil {
		return nil, err
	}
	k, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(k) != 32 {
		return nil, errors.New("invalid origin HMAC key")
	}
	return k, nil
}
func localHTTPClient() *http.Client {
	tr := &http.Transport{Proxy: nil, MaxIdleConns: 32, MaxIdleConnsPerHost: 32, IdleConnTimeout: 30 * time.Second}
	return &http.Client{Transport: tr, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func systemAndExtraRoots(caFile string) (*x509.CertPool, error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, err
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("invalid CA PEM")
		}
	}
	return pool, nil
}
func quicHTTPClient(caFile string) (*http.Client, *http3.Transport, error) {
	return quicHTTPClientWithProtection(caFile, "", "")
}
func quicHTTPClientWithProtection(caFile, path, token string) (*http.Client, *http3.Transport, error) {
	h3, err := quicProtectedClient(caFile, path, token)
	if err != nil {
		return nil, nil, err
	}
	return &http.Client{Transport: h3, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, h3, nil
}
func serveLocalHTTP(addr string, handler http.Handler) error {
	if err := localListen(addr); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 21 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 << 10}
	return srv.Serve(ln)
}

// An app-private Unix listener avoids exposing an unauthenticated loopback
// HTTP proxy to other Android applications.
func serveLocalHTTPUnix(path string, handler http.Handler) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("invalid Unix listener path")
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return err
	}
	if !parent.IsDir() || parent.Mode().Perm()&0o077 != 0 {
		return errors.New("Unix HTTP listener requires a private owner-only directory")
	}
	if _, err := os.Lstat(path); err == nil {
		return errors.New("Unix listener already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	defer ln.Close()
	if err := os.Chmod(path, 0600); err != nil {
		return err
	}
	return (&http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 21 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 << 10}).Serve(ln)
}
