package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
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

// Exercise the actual QUIC/HTTP3 TLS stack on localhost, not a mocked HTTPS client.
// IP SAN validation, certificate dates and CA isolation must remain enabled.
type testQUICCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func makeTestQUICCA(t *testing.T) testQUICCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "MBox integration test CA"},
		NotBefore:    now.Add(-time.Hour), NotAfter: now.Add(24 * 365 * time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return testQUICCA{parsed, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

func makeTestQUICLeaf(t *testing.T, ca testQUICCA, ip net.IP, expired bool) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	start, end := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	if expired {
		start, end = time.Now().Add(-72*time.Hour), time.Now().Add(-48*time.Hour)
	}
	serialMax := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialMax)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "MBox QUIC test"},
		NotBefore: start, NotAfter: end,
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{ip},
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der, ca.cert.Raw}, PrivateKey: key}
}

func runLocalQUIC(t *testing.T, leaf tls.Certificate, caFile string, shouldSucceed bool) {
	t.Helper()
	packet, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http3.Server{
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{leaf}},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("verified"))
		}),
	}
	go func() { _ = server.Serve(packet) }()
	defer func() { _ = server.Close(); _ = packet.Close() }()

	client, transport, err := quicHTTPClient(caFile)
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	client.Timeout = 4 * time.Second
	resp, err := client.Get(fmt.Sprintf("https://%s/health", packet.LocalAddr().String()))
	if shouldSucceed {
		if err != nil {
			t.Fatalf("valid QUIC certificate rejected: %v", err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != http.StatusOK || string(body) != "verified" {
			t.Fatalf("unexpected QUIC response: status=%d body=%q error=%v", resp.StatusCode, body, err)
		}
	} else {
		if err == nil {
			resp.Body.Close()
			t.Fatal("invalid QUIC certificate accepted")
		}
	}
}

func TestQUICIPCertificateValidationAndLeafRenewal(t *testing.T) {
	trusted := makeTestQUICCA(t)
	foreign := makeTestQUICCA(t)
	pemPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(pemPath, trusted.pem, 0600); err != nil {
		t.Fatal(err)
	}
	loopback := net.ParseIP("127.0.0.1")
	cases := []struct {
		name    string
		ca      testQUICCA
		ip      net.IP
		expired bool
		ok      bool
	}{
		{"valid", trusted, loopback, false, true},
		{"expired", trusted, loopback, true, false},
		{"foreign-ca", foreign, loopback, false, false},
		{"wrong-ip-san", trusted, net.ParseIP("127.0.0.2"), false, false},
		{"renewed-leaf-same-ca", trusted, loopback, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runLocalQUIC(t, makeTestQUICLeaf(t, tc.ca, tc.ip, tc.expired), pemPath, tc.ok)
		})
	}
}
