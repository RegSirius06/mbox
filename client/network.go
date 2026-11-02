package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	endpointFallbackDelay        = 250 * time.Millisecond
	endpointAttemptTimeout       = 5 * time.Second
	clientMaxResponseHeaderBytes = 64 * 1024
	clientMaxHTTPBodyBytes       = 8 * 1024 * 1024
	maxEndpointCandidates        = 16
)

type dialFn func(context.Context, string, string) (net.Conn, error)

type dialResult struct {
	conn net.Conn
	ip   string
	err  error
}

func parseConnectCandidates(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	seen := make(map[string]struct{})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		ip := net.ParseIP(part)
		if ip == nil {
			return nil, fmt.Errorf("invalid endpoint IP %q", part)
		}
		canon := ip.String()
		if _, ok := seen[canon]; ok {
			continue
		}
		seen[canon] = struct{}{}
		out = append(out, canon)
		if len(out) >= maxEndpointCandidates {
			break
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no endpoint IP candidates")
	}
	return out, nil
}

func connectCandidatesFromEnv() ([]string, error) {
	if raw := strings.TrimSpace(os.Getenv("MBOX_CONNECT_ADDRS")); raw != "" {
		return parseConnectCandidates(raw)
	}
	if raw := strings.TrimSpace(os.Getenv("MBOX_CONNECT_IP")); raw != "" {
		return parseConnectCandidates(raw)
	}
	return nil, nil
}

func ipFamily(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return "unknown"
	}
	if parsed.To4() != nil {
		return "ipv4"
	}
	return "ipv6"
}

// interleaveCandidates preserves the DNS preference family of the first answer
// while alternating IPv4 and IPv6 when both families are available. This gives
// a Happy-Eyeballs-like order without throwing away the resolver's preference.
func interleaveCandidates(in []string) []string {
	if len(in) < 2 {
		return append([]string(nil), in...)
	}
	var v4, v6 []string
	for _, ip := range in {
		if net.ParseIP(ip).To4() != nil {
			v4 = append(v4, ip)
		} else {
			v6 = append(v6, ip)
		}
	}
	firstV4 := net.ParseIP(in[0]).To4() != nil
	out := make([]string, 0, len(in))
	for len(v4) > 0 || len(v6) > 0 {
		if firstV4 {
			if len(v4) > 0 {
				out = append(out, v4[0])
				v4 = v4[1:]
			}
			if len(v6) > 0 {
				out = append(out, v6[0])
				v6 = v6[1:]
			}
		} else {
			if len(v6) > 0 {
				out = append(out, v6[0])
				v6 = v6[1:]
			}
			if len(v4) > 0 {
				out = append(out, v4[0])
				v4 = v4[1:]
			}
		}
	}
	return out
}

func redactedSessionID(id string) string {
	if networkDebug {
		return id
	}
	s := sha256.Sum256([]byte(id))
	return fmt.Sprintf("sid#%x", s[:4])
}

func redactedIP(ip string) string {
	if networkDebug {
		return ip
	}
	s := sha256.Sum256([]byte(ip))
	return fmt.Sprintf("ip#%x", s[:4])
}

func logPreResolvedDNS(candidates []string) {
	v4, v6 := 0, 0
	parts := make([]string, 0, len(candidates))
	for _, ip := range candidates {
		if net.ParseIP(ip).To4() != nil {
			v4++
		} else {
			v6++
		}
		parts = append(parts, redactedIP(ip))
	}
	log.Printf("NET stage=DNS code=DNS_OK source=android count=%d ipv4=%d ipv6=%d addresses=%s", len(candidates), v4, v6, strings.Join(parts, ","))
}

func dialCandidateSet(ctx context.Context, network, port string, candidates []string, delay time.Duration, dial dialFn) (net.Conn, string, error) {
	ordered := interleaveCandidates(candidates)
	if network == "tcp4" {
		filtered := ordered[:0]
		for _, ip := range ordered {
			if net.ParseIP(ip).To4() != nil {
				filtered = append(filtered, ip)
			}
		}
		ordered = filtered
	} else if network == "tcp6" {
		filtered := ordered[:0]
		for _, ip := range ordered {
			if net.ParseIP(ip).To4() == nil {
				filtered = append(filtered, ip)
			}
		}
		ordered = filtered
	}
	if len(ordered) == 0 {
		return nil, "", fmt.Errorf("no endpoint candidates for %s", network)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan dialResult, len(ordered))
	var wg sync.WaitGroup
	for i, ip := range ordered {
		i, ip := i, ip
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i > 0 && delay > 0 {
				t := time.NewTimer(time.Duration(i) * delay)
				select {
				case <-ctx.Done():
					t.Stop()
					return
				case <-t.C:
				}
			}
			attemptCtx, attemptCancel := context.WithTimeout(ctx, endpointAttemptTimeout)
			defer attemptCancel()
			addr := net.JoinHostPort(ip, port)
			started := time.Now()
			log.Printf("NET stage=TCP code=TCP_CONNECT_START candidate=%d family=%s ip=%s", i+1, ipFamily(ip), redactedIP(ip))
			conn, err := dial(attemptCtx, network, addr)
			elapsed := time.Since(started)
			if err != nil {
				code := classifyTCPError(err)
				if networkDebug {
					log.Printf("NET stage=TCP code=%s candidate=%d family=%s ip=%s time=%s error=%q", code, i+1, ipFamily(ip), redactedIP(ip), elapsed.Round(time.Millisecond), safeErr(err))
				} else {
					log.Printf("NET stage=TCP code=%s candidate=%d family=%s ip=%s time=%s", code, i+1, ipFamily(ip), redactedIP(ip), elapsed.Round(time.Millisecond))
				}
				results <- dialResult{ip: ip, err: err}
				return
			}
			log.Printf("NET stage=TCP code=TCP_CONNECT_OK candidate=%d family=%s ip=%s time=%s", i+1, ipFamily(ip), redactedIP(ip), elapsed.Round(time.Millisecond))
			select {
			case results <- dialResult{conn: conn, ip: ip}:
			case <-ctx.Done():
				_ = conn.Close()
			}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	var errs []string
	for r := range results {
		if r.conn != nil {
			cancel()
			go func(winner net.Conn) {
				for extra := range results {
					if extra.conn != nil && extra.conn != winner {
						_ = extra.conn.Close()
					}
				}
			}(r.conn)
			return r.conn, r.ip, nil
		}
		if r.err != nil {
			errs = append(errs, classifyTCPError(r.err))
		}
	}
	if len(errs) == 0 {
		return nil, "", context.Cause(ctx)
	}
	sort.Strings(errs)
	return nil, "", fmt.Errorf("all endpoint candidates failed (%s)", strings.Join(errs, ","))
}

func classifyTCPError(err error) string {
	if err == nil {
		return "TCP_CONNECT_OK"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "ERR_TCP_TIMEOUT"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "ERR_TCP_TIMEOUT"
	}
	var op *net.OpError
	if errors.As(err, &op) {
		msg := strings.ToLower(op.Err.Error())
		if strings.Contains(msg, "refused") {
			return "ERR_TCP_REFUSED"
		}
		if strings.Contains(msg, "network is unreachable") || strings.Contains(msg, "no route") {
			return "ERR_TCP_UNREACHABLE"
		}
	}
	return "ERR_TCP_CONNECT"
}

func safeErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) > 180 {
		s = s[:180]
	}
	return s
}

func newHTTPTransport(endpointHost string, candidates []string) *http.Transport {
	tr := &http.Transport{
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           8,
		MaxIdleConnsPerHost:    8,
		IdleConnTimeout:        90 * time.Second,
		TLSHandshakeTimeout:    10 * time.Second,
		ResponseHeaderTimeout:  15 * time.Second,
		ExpectContinueTimeout:  time.Second,
		MaxResponseHeaderBytes: clientMaxResponseHeaderBytes,
	}
	if len(candidates) == 0 {
		return tr
	}
	dialer := &net.Dialer{Timeout: endpointAttemptTimeout, KeepAlive: 30 * time.Second}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil || !strings.EqualFold(host, endpointHost) {
			return dialer.DialContext(ctx, network, addr)
		}
		conn, _, err := dialCandidateSet(ctx, network, port, candidates, endpointFallbackDelay, dialer.DialContext)
		return conn, err
	}
	return tr
}

func newHTTPClient(baseURL string) (*http.Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("invalid transport base URL %q", baseURL)
	}

	transport, bridged, err := androidBridgeFromEnv(u)
	if err != nil {
		return nil, err
	}
	if bridged {
		log.Printf("NET stage=HTTP code=HTTP_STACK stack=cronet")
		return &http.Client{
			Transport: transport,
			Timeout:   20 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}, nil
	}

	candidates, err := connectCandidatesFromEnv()
	if err != nil {
		return nil, err
	}
	if len(candidates) > 0 {
		logPreResolvedDNS(candidates)
	}
	log.Printf("NET stage=HTTP code=HTTP_STACK stack=go")
	return &http.Client{
		Transport: newHTTPTransport(u.Hostname(), candidates),
		Timeout:   15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

func withOpenTrace(req *http.Request) *http.Request {
	trace := &httptrace.ClientTrace{
		DNSStart: func(info httptrace.DNSStartInfo) {
			log.Printf("NET stage=DNS code=DNS_START host=%s", info.Host)
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			if info.Err != nil {
				log.Printf("NET stage=DNS code=ERR_DNS error=%q", safeErr(info.Err))
				return
			}
			v4, v6 := 0, 0
			for _, a := range info.Addrs {
				if a.IP.To4() != nil {
					v4++
				} else {
					v6++
				}
			}
			log.Printf("NET stage=DNS code=DNS_OK source=go count=%d ipv4=%d ipv6=%d", len(info.Addrs), v4, v6)
		},
		TLSHandshakeStart: func() {
			log.Printf("NET stage=TLS code=TLS_HANDSHAKE_START")
		},
		TLSHandshakeDone: func(cs tls.ConnectionState, err error) {
			if err != nil {
				log.Printf("NET stage=TLS code=%s error=%q", classifyTLSError(err), safeErr(err))
				return
			}
			log.Printf("NET stage=TLS code=TLS_OK version=%s alpn=%s cipher=0x%04x", tlsVersionName(cs.Version), emptyAs(cs.NegotiatedProtocol, "none"), cs.CipherSuite)
		},
		GotConn: func(info httptrace.GotConnInfo) {
			log.Printf("NET stage=HTTP code=HTTP_CONN reused=%t idle=%t", info.Reused, info.WasIdle)
		},
		GotFirstResponseByte: func() {
			log.Printf("NET stage=HTTP code=HTTP_FIRST_BYTE")
		},
	}
	return req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
}

func classifyTLSError(err error) string {
	var ua x509.UnknownAuthorityError
	var hn x509.HostnameError
	var ci x509.CertificateInvalidError
	if errors.As(err, &ua) || errors.As(err, &hn) || errors.As(err, &ci) {
		return "ERR_TLS_CERT"
	}
	return "ERR_TLS_HANDSHAKE"
}

func tlsVersionName(v uint16) string {
	switch v {
	case tls.VersionTLS13:
		return "TLS1.3"
	case tls.VersionTLS12:
		return "TLS1.2"
	case tls.VersionTLS11:
		return "TLS1.1"
	case tls.VersionTLS10:
		return "TLS1.0"
	default:
		return fmt.Sprintf("0x%04x", v)
	}
}

func emptyAs(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func responseBodyLimit() int64 {
	// The current config validation caps DOWN at 4 MiB and random padding at
	// 256 KiB. Noise chunk framing plus JSON/base64 envelope stays below 8 MiB.
	return clientMaxHTTPBodyBytes
}
