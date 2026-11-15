package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"
)

func makeDNSLeaf(t *testing.T, ca testQUICCA, name string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: name}, DNSNames: []string{name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der, ca.cert.Raw}, PrivateKey: key}
}

func TestDNSNameValidation(t *testing.T) {
	for _, n := range []string{"node.example.com", "lupka.zalupka.fun", "a-b.example.ru"} {
		if !validQUICDNSName(n) {
			t.Fatalf("rejected valid DNS hostname: %s", n)
		}
	}
	for _, n := range []string{"", "1.2.3.4", "https://example.com", "example.com:7443", "*.example.com", "foo..com", "-foo.example", "foo-.example", "domain.", "example", "a_b.example.com"} {
		if validQUICDNSName(n) {
			t.Fatalf("accepted malformed DNS hostname: %s", n)
		}
	}
}

func TestQUICDomainIdentityWithNumericUDPEndpoint(t *testing.T) {
	ca := makeTestQUICCA(t)
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, ca.pem, 0600); err != nil {
		t.Fatal(err)
	}
	packet, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http3.Server{
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{makeDNSLeaf(t, ca, "node.example.test")}},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("domain-verified"))
		}),
	}
	go func() { _ = srv.Serve(packet) }()
	defer func() { _ = srv.Close(); _ = packet.Close() }()
	url := "https://" + packet.LocalAddr().String() + "/health" // numeric IPv4, not DNS
	for _, tc := range []struct {
		name       string
		servername string
		wantOK     bool
	}{
		{"correct-name", "node.example.test", true},
		{"wrong-name", "wrong.example.test", false},
		{"legacy-ip-validation", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, tr, err := quicHTTPClientWithProtection(caPath, "", "", tc.servername)
			if err != nil {
				t.Fatal(err)
			}
			defer tr.Close()
			client.Timeout = 4 * time.Second
			resp, err := client.Get(url)
			if !tc.wantOK {
				if err == nil {
					resp.Body.Close()
					t.Fatal("invalid TLS identity accepted")
				}
				return
			}
			if err != nil {
				t.Fatalf("domain TLS failed: %v", err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil || resp.StatusCode != http.StatusOK || string(body) != "domain-verified" {
				t.Fatalf("bad HTTP/3: code=%d body=%q err=%v", resp.StatusCode, body, err)
			}
		})
	}
}

func TestQUICRejectMalformedDNSTarget(t *testing.T) {
	for _, name := range []string{"http://host", "192.0.2.1", "*.example.com"} {
		if _, tr, err := quicHTTPClientWithProtection("", "", "", name); err == nil {
			tr.Close()
			t.Fatalf("invalid server name accepted: %s", name)
		}
	}
}

// Verify DNS leaf generation provides only DNS SAN, not accidental IP coverage.
func TestDNSLeafHasNoIPSAN(t *testing.T) {
	ca := makeTestQUICCA(t)
	leaf := makeDNSLeaf(t, ca, "node.example.test")
	parsed, err := x509.ParseCertificate(leaf.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.IPAddresses) != 0 || len(parsed.DNSNames) != 1 {
		t.Fatal("test fixture must be DNS-only")
	}
	if len(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: parsed.Raw})) == 0 {
		t.Fatal("invalid fixture")
	}
}
