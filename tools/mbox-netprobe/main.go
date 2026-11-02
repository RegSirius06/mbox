package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type event struct {
	Target string `json:"target"`
	Stage  string `json:"stage"`
	Code   string `json:"code"`
	IP     string `json:"ip,omitempty"`
	Family string `json:"family,omitempty"`
	TimeMS int64  `json:"time_ms,omitempty"`
	SNI    string `json:"sni,omitempty"`
	TLS    string `json:"tls,omitempty"`
	ALPN   string `json:"alpn,omitempty"`
	HTTP   string `json:"http,omitempty"`
	Status int    `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

var jsonMode bool

func emit(e event) {
	if jsonMode {
		b, _ := json.Marshal(e)
		fmt.Println(string(b))
		return
	}
	fmt.Printf("%-28s %-6s %-24s", e.Target, e.Stage, e.Code)
	if e.IP != "" {
		fmt.Printf(" ip=%s", e.IP)
	}
	if e.Family != "" {
		fmt.Printf(" family=%s", e.Family)
	}
	if e.TimeMS != 0 {
		fmt.Printf(" time=%dms", e.TimeMS)
	}
	if e.SNI != "" {
		fmt.Printf(" sni=%s", e.SNI)
	}
	if e.TLS != "" {
		fmt.Printf(" tls=%s", e.TLS)
	}
	if e.ALPN != "" {
		fmt.Printf(" alpn=%s", e.ALPN)
	}
	if e.HTTP != "" {
		fmt.Printf(" http=%s", e.HTTP)
	}
	if e.Status != 0 {
		fmt.Printf(" status=%d", e.Status)
	}
	if e.Error != "" {
		fmt.Printf(" error=%q", e.Error)
	}
	fmt.Println()
}

func family(ip net.IP) string {
	if ip.To4() != nil {
		return "ipv4"
	}
	return "ipv6"
}

func tlsVersion(v uint16) string {
	switch v {
	case tls.VersionTLS13:
		return "TLS1.3"
	case tls.VersionTLS12:
		return "TLS1.2"
	default:
		return fmt.Sprintf("0x%04x", v)
	}
}

func endpointPort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
}

func probeOne(raw string, timeout time.Duration) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("invalid URL %q", raw)
	}
	host := u.Hostname()
	port := endpointPort(u)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	start := time.Now()
	answers, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	cancel()
	if err != nil {
		emit(event{Target: raw, Stage: "DNS", Code: "ERR_DNS", TimeMS: time.Since(start).Milliseconds(), Error: err.Error()})
		return nil
	}
	for _, a := range answers {
		emit(event{Target: raw, Stage: "DNS", Code: "DNS_OK", IP: a.IP.String(), Family: family(a.IP), TimeMS: time.Since(start).Milliseconds()})
	}
	if len(answers) == 0 {
		emit(event{Target: raw, Stage: "DNS", Code: "ERR_DNS_EMPTY", TimeMS: time.Since(start).Milliseconds()})
		return nil
	}

	for _, a := range answers {
		ip := a.IP.String()
		addr := net.JoinHostPort(ip, port)
		dialer := &net.Dialer{Timeout: timeout}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		start = time.Now()
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		cancel()
		if err != nil {
			emit(event{Target: raw, Stage: "TCP", Code: "ERR_TCP_CONNECT", IP: ip, Family: family(a.IP), TimeMS: time.Since(start).Milliseconds(), Error: err.Error()})
			continue
		}
		emit(event{Target: raw, Stage: "TCP", Code: "TCP_CONNECT_OK", IP: ip, Family: family(a.IP), TimeMS: time.Since(start).Milliseconds()})
		_ = conn.Close()

		if u.Scheme == "https" {
			ctx, cancel = context.WithTimeout(context.Background(), timeout)
			start = time.Now()
			rawConn, err := dialer.DialContext(ctx, "tcp", addr)
			if err != nil {
				cancel()
				emit(event{Target: raw, Stage: "TLS", Code: "ERR_TCP_CONNECT", IP: ip, SNI: host, TimeMS: time.Since(start).Milliseconds(), Error: err.Error()})
				continue
			}
			tlsConn := tls.Client(rawConn, &tls.Config{ServerName: host, NextProtos: []string{"h2", "http/1.1"}, MinVersion: tls.VersionTLS12})
			err = tlsConn.HandshakeContext(ctx)
			cancel()
			if err != nil {
				_ = rawConn.Close()
				emit(event{Target: raw, Stage: "TLS", Code: "ERR_TLS_HANDSHAKE", IP: ip, SNI: host, TimeMS: time.Since(start).Milliseconds(), Error: err.Error()})
				continue
			}
			cs := tlsConn.ConnectionState()
			emit(event{Target: raw, Stage: "TLS", Code: "TLS_OK", IP: ip, SNI: host, TimeMS: time.Since(start).Milliseconds(), TLS: tlsVersion(cs.Version), ALPN: cs.NegotiatedProtocol})
			_ = tlsConn.Close()
		}

		tr := &http.Transport{ForceAttemptHTTP2: true, TLSHandshakeTimeout: timeout, ResponseHeaderTimeout: timeout, MaxResponseHeaderBytes: 64 * 1024}
		tr.DialContext = func(ctx context.Context, network, ignored string) (net.Conn, error) {
			return (&net.Dialer{Timeout: timeout}).DialContext(ctx, network, addr)
		}
		client := &http.Client{Transport: tr, Timeout: timeout * 2, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
		req, _ := http.NewRequest(http.MethodGet, raw, nil)
		start = time.Now()
		resp, err := client.Do(req)
		if err != nil {
			emit(event{Target: raw, Stage: "HTTP", Code: "ERR_HTTP_REQUEST", IP: ip, TimeMS: time.Since(start).Milliseconds(), Error: err.Error()})
			tr.CloseIdleConnections()
			continue
		}
		alpn, tlsv := "", ""
		if resp.TLS != nil {
			alpn, tlsv = resp.TLS.NegotiatedProtocol, tlsVersion(resp.TLS.Version)
		}
		emit(event{Target: raw, Stage: "HTTP", Code: "HTTP_OK", IP: ip, TimeMS: time.Since(start).Milliseconds(), TLS: tlsv, ALPN: alpn, HTTP: resp.Proto, Status: resp.StatusCode})
		_ = resp.Body.Close()
		tr.CloseIdleConnections()
	}
	return nil
}

func main() {
	var timeout time.Duration
	flag.DurationVar(&timeout, "timeout", 5*time.Second, "timeout per network stage")
	flag.BoolVar(&jsonMode, "json", false, "emit JSONL")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintf(os.Stderr, "usage: mbox-netprobe [-json] [-timeout 5s] https://host/path [https://host2/path ...]\n")
		os.Exit(2)
	}
	for _, raw := range flag.Args() {
		if !strings.Contains(raw, "://") {
			raw = "https://" + raw
		}
		if err := probeOne(raw, timeout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
