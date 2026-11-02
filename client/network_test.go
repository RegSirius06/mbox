package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fakeCandidateDial(success map[string]bool) dialFn {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, _ := net.SplitHostPort(addr)
		if success[host] {
			a, b := net.Pipe()
			go func() {
				<-ctx.Done()
				_ = b.Close()
			}()
			return a, nil
		}
		return nil, fmt.Errorf("dial %s failed", host)
	}
}

func TestDialCandidatesFirstFailsSecondWorks(t *testing.T) {
	candidates := []string{"203.0.113.10", "203.0.113.11"}
	conn, ip, err := dialCandidateSet(context.Background(), "tcp", "443", candidates, 0, fakeCandidateDial(map[string]bool{"203.0.113.11": true}))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if ip != "203.0.113.11" {
		t.Fatalf("selected %s", ip)
	}
}

func TestDialCandidatesIPv6FailsIPv4Works(t *testing.T) {
	candidates := []string{"2001:db8::10", "203.0.113.11"}
	conn, ip, err := dialCandidateSet(context.Background(), "tcp", "443", candidates, 0, fakeCandidateDial(map[string]bool{"203.0.113.11": true}))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if ip != "203.0.113.11" {
		t.Fatalf("selected %s", ip)
	}
}

func TestDialCandidatesIPv4FailsIPv6Works(t *testing.T) {
	candidates := []string{"203.0.113.10", "2001:db8::11"}
	conn, ip, err := dialCandidateSet(context.Background(), "tcp", "443", candidates, 0, fakeCandidateDial(map[string]bool{"2001:db8::11": true}))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if ip != "2001:db8::11" {
		t.Fatalf("selected %s", ip)
	}
}

func TestDialCandidatesAllUnavailable(t *testing.T) {
	_, _, err := dialCandidateSet(context.Background(), "tcp", "443", []string{"203.0.113.10", "2001:db8::10"}, 0, fakeCandidateDial(nil))
	if err == nil {
		t.Fatal("all-unavailable candidates unexpectedly succeeded")
	}
}

func TestDNSCandidateOrderDoesNotPreventFallback(t *testing.T) {
	orders := [][]string{
		{"2001:db8::10", "203.0.113.11", "203.0.113.12"},
		{"203.0.113.12", "2001:db8::10", "203.0.113.11"},
	}
	for _, candidates := range orders {
		conn, ip, err := dialCandidateSet(context.Background(), "tcp", "443", candidates, 0, fakeCandidateDial(map[string]bool{"203.0.113.11": true}))
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
		if ip != "203.0.113.11" {
			t.Fatalf("order %v selected %s", candidates, ip)
		}
	}
}

func TestParseConnectCandidatesDeduplicatesAndKeepsFamilies(t *testing.T) {
	got, err := parseConnectCandidates("203.0.113.1, 2001:db8::1,203.0.113.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "203.0.113.1" || got[1] != "2001:db8::1" {
		t.Fatalf("got %v", got)
	}
}

func TestHTTP2AndALPNWithSupportedServer(t *testing.T) {
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	pool := x509.NewCertPool()
	pool.AddCert(ts.Certificate())
	tr := newHTTPTransport(u.Hostname(), []string{u.Hostname()})
	tr.TLSClientConfig = &tls.Config{RootCAs: pool, ServerName: u.Hostname()}
	client := &http.Client{Transport: tr}
	resp, err := client.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.ProtoMajor != 2 {
		t.Fatalf("HTTP proto=%s want HTTP/2", resp.Proto)
	}
	if resp.TLS == nil || resp.TLS.NegotiatedProtocol != "h2" {
		t.Fatalf("ALPN=%q want h2", resp.TLS.NegotiatedProtocol)
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirect.Close()

	t.Setenv("MBOX_CONNECT_ADDRS", "")
	t.Setenv("MBOX_CONNECT_IP", "")
	client, err := newHTTPClient(redirect.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get(redirect.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status=%d want 302", resp.StatusCode)
	}
	if targetCalls.Load() != 0 {
		t.Fatal("redirect target was contacted")
	}
}

func TestOversizedResponseBodyRejected(t *testing.T) {
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := strings.Repeat("x", 64*1024)
		for written := 0; written <= clientMaxHTTPBodyBytes; written += len(chunk) {
			_, _ = io.WriteString(w, chunk)
		}
	}))
	defer big.Close()

	tun := &tunnel{http: big.Client()}
	_, _, err := tun.requestRaw(http.MethodGet, big.URL, nil, false)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err=%v", err)
	}
}

func TestOversizedResponseHeadersRejected(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Large", strings.Repeat("a", clientMaxResponseHeaderBytes+1024))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()
	u, _ := url.Parse(ts.URL)
	client := &http.Client{Transport: newHTTPTransport(u.Hostname(), nil), Timeout: 5 * time.Second}
	_, err := client.Get(ts.URL)
	if err == nil {
		t.Fatal("oversized response headers accepted")
	}
}

func TestTruncatedResponseBodyRejected(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "short")
	}))
	defer ts.Close()
	tun := &tunnel{http: ts.Client()}
	_, _, err := tun.requestRaw(http.MethodGet, ts.URL, nil, false)
	if err == nil {
		t.Fatal("truncated response accepted")
	}
}
