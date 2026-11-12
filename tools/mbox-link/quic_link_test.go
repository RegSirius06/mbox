package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"testing"
	"time"
)

func testCertificate(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		DNSNames: nil, IPAddresses: []net.IP{net.ParseIP("198.51.100.10")},
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestQUICProfileRoundTrip(t *testing.T) {
	psk := hex.EncodeToString(make([]byte, 32))
	ca := testCertificate(t)
	original := LinkConfig{
		Version: 1, Name: "test", ClientID: "opaque-id",
		Transport:  Transport{URL: "https://frontend.example/edge/a", QUICURL: "https://198.51.100.10:7443", QUICCAPEM: ca},
		Crypto:     LinkCrypto{PSK: psk, ServerPublicKey: psk},
		Camouflage: json.RawMessage(`{"open_path":"/api/v1/session"}`),
	}
	link, err := encodeLink(original)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeLink(link)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Transport.QUICURL != original.Transport.QUICURL || decoded.Transport.QUICCAPEM != ca {
		t.Fatal("QUIC profile fields lost during link roundtrip")
	}
	decoded.Transport.QUICURL = "https://203.0.113.20:7443"
	if err := validateLink(decoded); err == nil {
		t.Fatal("QUIC certificate accepted for wrong server IP")
	}
	decoded.Transport.QUICURL = ""
	if err := validateLink(decoded); err == nil {
		t.Fatal("QUIC certificate accepted without URL")
	}
}
