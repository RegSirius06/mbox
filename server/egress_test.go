package main

import (
	noise "../common/noise"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEgressRoutingFailClosed(t *testing.T) {
	oldFile, oldDefault := egressConfigFile, socksAddr
	defer func() { egressConfigFile = oldFile; socksAddr = oldDefault }()
	d := t.TempDir()
	egressConfigFile = filepath.Join(d, "egress.json")
	socksAddr = "127.0.0.1:10001"
	if a, err := resolveSessionUpstream(""); err != nil || a != socksAddr {
		t.Fatalf("legacy %s %v", a, err)
	}
	if _, err := resolveSessionUpstream("kz"); err == nil {
		t.Fatal("unconfigured route must fail")
	}
	contents := `{"exits":[{"id":"kz","name":"Kazakhstan","upstream":"127.0.0.1:10002"},{"id":"dk","name":"Denmark","upstream":"[::1]:10003"}]}`
	if err := os.WriteFile(egressConfigFile, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"kz": "127.0.0.1:10002", "dk": "[::1]:10003"} {
		if a, err := resolveSessionUpstream(id); err != nil || a != want {
			t.Errorf("%s %s %v", id, a, err)
		}
	}
	if _, err := resolveSessionUpstream("fi"); err == nil {
		t.Fatal("unknown route leaked to default")
	}
	if err := os.WriteFile(egressConfigFile, []byte(strings.ReplaceAll(contents, "127.0.0.1:10002", "8.8.8.8:53")), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveSessionUpstream("dk"); err == nil {
		t.Fatal("invalid route table must fail closed")
	}
}
func TestNoiseRoutePayloadCompat(t *testing.T) {
	device := strings.Repeat("1", 32)
	id, route, err := parseNoiseRoutePayload(nil)
	if err != nil || id != "" || route != "" {
		t.Fatal("empty")
	}
	legacy := append([]byte("MBD1"), make([]byte, 16)...)
	id, route, err = parseNoiseRoutePayload(legacy)
	if err != nil || len(id) != 32 || route != "" {
		t.Fatal("MBD1")
	}
	routed := append([]byte("MBD2"), make([]byte, 16)...)
	routed = append(routed, byte(2), 'k', 'z')
	id, route, err = parseNoiseRoutePayload(routed)
	if err != nil || len(id) != 32 || route != "kz" {
		t.Fatal("MBD2", id, route, err)
	}
	routed[len(routed)-1] = '/'
	if _, _, err = parseNoiseRoutePayload(routed); err == nil {
		t.Fatal("invalid route")
	}
	_ = device
}

func TestRoutedNoiseHandshakeCarriesAuthenticatedExit(t *testing.T) {
	oldPolicies, oldStatic := clientPolicies, append([]byte(nil), serverStaticPrivate...)
	defer func() { clientPolicies = oldPolicies; serverStaticPrivate = oldStatic }()
	psk := bytes.Repeat([]byte{0x5a}, 32)
	secret := bytes.Repeat([]byte{0x33}, 32)
	keys, err := noise.KeypairFromPrivate(secret)
	if err != nil {
		t.Fatal(err)
	}
	serverStaticPrivate = secret
	clientPolicies = map[string]clientPolicy{"phone": {PSK: psk}}
	init, err := noise.NewInitiator(psk, keys.Public, noisePrologue("phone"), bytes.NewReader(bytes.Repeat([]byte{0x44}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	routePayload := append([]byte("MBD2"), bytes.Repeat([]byte{0xaa}, 16)...)
	routePayload = append(routePayload, 2, 'k', 'z')
	msg, _, _, err := init.WriteMessage(routePayload)
	if err != nil {
		t.Fatal(err)
	}
	body := append([]byte{5}, []byte("phone")...)
	body = append(body, msg...)
	s, reply, err := serverHandshake(body, "0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if s.egressID != "kz" {
		t.Fatal("authenticated route not preserved")
	}
	if _, _, _, err = init.ReadMessage(reply); err != nil {
		t.Fatal(err)
	}
}

// Disabling pre-device-id OPEN must not disable beta.4+ MBD1 or routed MBD2.
func TestLegacyEmptyNoiseOpenSwitch(t *testing.T) {
	old := acceptLegacyEmptyOpen
	defer func() { acceptLegacyEmptyOpen = old }()
	acceptLegacyEmptyOpen = false
	if _, _, err := parseNoiseRoutePayload(nil); err != nil {
		t.Fatal("parser must retain backwards-compatible format detection")
	}
	// The policy is enforced only after the Noise authentication succeeds.
	psk := bytes.Repeat([]byte{0x62}, 32)
	secret := bytes.Repeat([]byte{0x35}, 32)
	keys, err := noise.KeypairFromPrivate(secret)
	if err != nil {
		t.Fatal(err)
	}
	oldPolicies, oldStatic := clientPolicies, append([]byte(nil), serverStaticPrivate...)
	defer func() { clientPolicies = oldPolicies; serverStaticPrivate = oldStatic }()
	clientPolicies = map[string]clientPolicy{"phone": {PSK: psk}}
	serverStaticPrivate = secret
	for _, tc := range []struct {
		payload []byte
		allowed bool
	}{
		{nil, false},
		{append([]byte("MBD1"), make([]byte, 16)...), true},
		{append(append([]byte("MBD2"), make([]byte, 16)...), 2, 'k', 'z'), true},
	} {
		hs, err := noise.NewInitiator(psk, keys.Public, noisePrologue("phone"), bytes.NewReader(bytes.Repeat([]byte{0x41}, 32)))
		if err != nil {
			t.Fatal(err)
		}
		msg, _, _, err := hs.WriteMessage(tc.payload)
		if err != nil {
			t.Fatal(err)
		}
		body := append([]byte{5}, []byte("phone")...)
		body = append(body, msg...)
		_, _, err = serverHandshake(body, "0123456789abcdef0123456789abcdef")
		if (err == nil) != tc.allowed {
			t.Fatalf("payload %q allowed=%v err=%v", tc.payload, tc.allowed, err)
		}
	}
}
