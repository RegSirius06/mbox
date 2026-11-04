package main

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	controlmeta "../common/controlmeta"
	noise "../common/noise"
)

func TestServerNoiseOnlyHandshakeInteroperates(t *testing.T) {
	oldPolicies, oldPriv, oldStatic := clientPolicies, serverPrivFile, append([]byte(nil), serverStaticPrivate...)
	defer func() { clientPolicies, serverPrivFile, serverStaticPrivate = oldPolicies, oldPriv, oldStatic }()

	psk := bytes.Repeat([]byte{0x5a}, 32)
	staticPriv := bytes.Repeat([]byte{0x33}, 32)
	kp, err := noise.KeypairFromPrivate(staticPriv)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "server.key")
	if err := os.WriteFile(path, []byte(hex.EncodeToString(staticPriv)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	serverPrivFile = path
	serverStaticPrivate = append([]byte(nil), staticPriv...)
	clientPolicies = map[string]clientPolicy{"phone": {PSK: psk}}

	init, err := noise.NewInitiator(psk, kp.Public, noisePrologue("phone"), bytes.NewReader(bytes.Repeat([]byte{0x44}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	msg1, _, _, err := init.WriteMessage(nil)
	if err != nil {
		t.Fatal(err)
	}
	body := append([]byte{5}, []byte("phone")...)
	body = append(body, msg1...)
	sid := "0123456789abcdef0123456789abcdef"
	serverState, msg2, err := serverHandshake(body, sid)
	if err != nil {
		t.Fatal(err)
	}
	payload, clientSend, clientRecv, err := init.ReadMessage(msg2)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != sid {
		t.Fatalf("authenticated sid=%q", payload)
	}
	key, err := controlmeta.Derive(psk, init.ChannelBinding(), sid)
	if err != nil {
		t.Fatal(err)
	}
	if serverState.controlKey != key {
		t.Fatal("client/server Noise channel binding produced different HTTP control keys")
	}

	c1, err := clientSend.Seal(0, nil, []byte("up"))
	if err != nil {
		t.Fatal(err)
	}
	p1, err := serverState.up.Open(0, nil, c1)
	if err != nil || string(p1) != "up" {
		t.Fatal("UP Noise key mismatch")
	}
	c2, err := serverState.down.Seal(0, nil, []byte("down"))
	if err != nil {
		t.Fatal(err)
	}
	p2, err := clientRecv.Open(0, nil, c2)
	if err != nil || string(p2) != "down" {
		t.Fatal("DOWN Noise key mismatch")
	}
}

func TestServerRejectsLegacyOpenFraming(t *testing.T) {
	if _, _, err := serverHandshake(make([]byte, 64), "0123456789abcdef0123456789abcdef"); err == nil {
		t.Fatal("legacy OPEN unexpectedly accepted")
	}
}

func TestServerNoiseHandshakeRejectsExpiredPolicy(t *testing.T) {
	oldPolicies, oldPriv, oldStatic := clientPolicies, serverPrivFile, append([]byte(nil), serverStaticPrivate...)
	defer func() { clientPolicies, serverPrivFile, serverStaticPrivate = oldPolicies, oldPriv, oldStatic }()

	psk := bytes.Repeat([]byte{0x6b}, 32)
	staticPriv := bytes.Repeat([]byte{0x23}, 32)
	kp, err := noise.KeypairFromPrivate(staticPriv)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "server.key")
	if err := os.WriteFile(path, []byte(hex.EncodeToString(staticPriv)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	serverPrivFile = path
	serverStaticPrivate = append([]byte(nil), staticPriv...)
	clientPolicies = map[string]clientPolicy{
		"expired": {PSK: psk, ExpiresAt: time.Now().Add(-time.Minute)},
	}
	init, err := noise.NewInitiator(psk, kp.Public, noisePrologue("expired"), bytes.NewReader(bytes.Repeat([]byte{0x42}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	msg1, _, _, err := init.WriteMessage(nil)
	if err != nil {
		t.Fatal(err)
	}
	body := append([]byte{7}, []byte("expired")...)
	body = append(body, msg1...)
	if _, _, err := serverHandshake(body, "0123456789abcdef0123456789abcdef"); err == nil {
		t.Fatal("expired policy unexpectedly completed Noise handshake")
	}
}

func TestServerNoiseHandshakeAcceptsAuthenticatedDeviceID(t *testing.T) {
	oldPolicies, oldPriv, oldStatic := clientPolicies, serverPrivFile, append([]byte(nil), serverStaticPrivate...)
	defer func() { clientPolicies, serverPrivFile, serverStaticPrivate = oldPolicies, oldPriv, oldStatic }()

	psk := bytes.Repeat([]byte{0x2a}, 32)
	staticPriv := bytes.Repeat([]byte{0x11}, 32)
	kp, err := noise.KeypairFromPrivate(staticPriv)
	if err != nil {
		t.Fatal(err)
	}
	serverStaticPrivate = append([]byte(nil), staticPriv...)
	clientPolicies = map[string]clientPolicy{"phone": {PSK: psk}}

	init, err := noise.NewInitiator(psk, kp.Public, noisePrologue("phone"), bytes.NewReader(bytes.Repeat([]byte{0x22}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	deviceRaw, _ := hex.DecodeString("00112233445566778899aabbccddeeff")
	payload := append([]byte(nil), devicePayloadPrefix...)
	payload = append(payload, deviceRaw...)
	msg1, _, _, err := init.WriteMessage(payload)
	if err != nil {
		t.Fatal(err)
	}
	body := append([]byte{5}, []byte("phone")...)
	body = append(body, msg1...)
	state, _, err := serverHandshake(body, "0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if state.deviceID != "00112233445566778899aabbccddeeff" {
		t.Fatalf("deviceID=%q", state.deviceID)
	}
}
