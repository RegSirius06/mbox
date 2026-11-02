package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAndroidBridgeRoundTripPreservesRequestAndResponse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	const token = "test-token"
	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		br := bufio.NewReader(conn)
		var magic uint32
		if err := binary.Read(br, binary.BigEndian, &magic); err != nil {
			done <- err
			return
		}
		if magic != androidBridgeRequestMagic {
			done <- io.ErrUnexpectedEOF
			return
		}
		gotToken, err := bridgeReadString16(br, 512)
		if err != nil || gotToken != token {
			done <- io.ErrUnexpectedEOF
			return
		}
		method, err := bridgeReadString16(br, 32)
		if err != nil || method != http.MethodPost {
			done <- io.ErrUnexpectedEOF
			return
		}
		var pathLen uint32
		if err := binary.Read(br, binary.BigEndian, &pathLen); err != nil {
			done <- err
			return
		}
		path := make([]byte, pathLen)
		if _, err := io.ReadFull(br, path); err != nil || string(path) != "/api/v1/session?q=1" {
			done <- io.ErrUnexpectedEOF
			return
		}
		var headerCount uint16
		if err := binary.Read(br, binary.BigEndian, &headerCount); err != nil {
			done <- err
			return
		}
		seenAccept := false
		for i := 0; i < int(headerCount); i++ {
			name, err := bridgeReadString16(br, androidBridgeMaxHeaderName)
			if err != nil {
				done <- err
				return
			}
			value, err := bridgeReadString16(br, androidBridgeMaxHeaderValue)
			if err != nil {
				done <- err
				return
			}
			if strings.EqualFold(name, "Accept") && value == "application/json" {
				seenAccept = true
			}
			if strings.EqualFold(name, "User-Agent") {
				done <- io.ErrUnexpectedEOF
				return
			}
		}
		if !seenAccept {
			done <- io.ErrUnexpectedEOF
			return
		}
		var bodyLen uint32
		if err := binary.Read(br, binary.BigEndian, &bodyLen); err != nil {
			done <- err
			return
		}
		body := make([]byte, bodyLen)
		if _, err := io.ReadFull(br, body); err != nil || string(body) != "hello" {
			done <- io.ErrUnexpectedEOF
			return
		}

		bw := bufio.NewWriter(conn)
		_ = binary.Write(bw, binary.BigEndian, androidBridgeResponseMagic)
		_ = binary.Write(bw, binary.BigEndian, uint16(200))
		_ = bridgeWriteString16(bw, "")
		_ = bridgeWriteString16(bw, "")
		_ = bridgeWriteString16(bw, "h2")
		_ = binary.Write(bw, binary.BigEndian, uint32(len("world")))
		_, _ = bw.WriteString("world")
		done <- bw.Flush()
	}()

	t.Setenv("MBOX_HTTP_BRIDGE_ADDR", ln.Addr().String())
	t.Setenv("MBOX_HTTP_BRIDGE_TOKEN", token)
	client, err := newHTTPClient("https://example.test")
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, "https://example.test/api/v1/session?q=1", bytes.NewBufferString("hello"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Go-http-client/1.1")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if string(got) != "world" || resp.StatusCode != 200 || resp.Proto != "HTTP/2.0" {
		t.Fatalf("unexpected response: status=%d proto=%q body=%q", resp.StatusCode, resp.Proto, got)
	}
	if resp.Header.Get("X-MBox-Network-Stack") != "cronet" || resp.Header.Get("X-MBox-Negotiated-Protocol") != "h2" {
		t.Fatalf("missing bridge metadata: %#v", resp.Header)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("bridge server did not complete")
	}
}

func TestAndroidBridgeRejectsCrossOrigin(t *testing.T) {
	u, _ := urlParseForTest("https://example.test")
	tr := &androidBridgeTransport{addr: "127.0.0.1:9", token: "x", origin: u}
	req, _ := http.NewRequest(http.MethodGet, "https://other.test/", nil)
	if _, err := tr.RoundTrip(req); err == nil || !strings.Contains(err.Error(), "cross-origin") {
		t.Fatalf("expected cross-origin rejection, got %v", err)
	}
}

func TestAndroidBridgeErrorCode(t *testing.T) {
	err := &androidBridgeError{Code: "ERR_TLS_HANDSHAKE", Msg: "failed"}
	if got := bridgeErrorCode(err); got != "ERR_TLS_HANDSHAKE" {
		t.Fatalf("got %q", got)
	}
}

func TestAndroidBridgeEnvValidation(t *testing.T) {
	oldAddr, hadAddr := os.LookupEnv("MBOX_HTTP_BRIDGE_ADDR")
	oldToken, hadToken := os.LookupEnv("MBOX_HTTP_BRIDGE_TOKEN")
	defer func() {
		if hadAddr {
			_ = os.Setenv("MBOX_HTTP_BRIDGE_ADDR", oldAddr)
		} else {
			_ = os.Unsetenv("MBOX_HTTP_BRIDGE_ADDR")
		}
		if hadToken {
			_ = os.Setenv("MBOX_HTTP_BRIDGE_TOKEN", oldToken)
		} else {
			_ = os.Unsetenv("MBOX_HTTP_BRIDGE_TOKEN")
		}
	}()
	_ = os.Setenv("MBOX_HTTP_BRIDGE_ADDR", "127.0.0.1:12345")
	_ = os.Unsetenv("MBOX_HTTP_BRIDGE_TOKEN")
	u, _ := urlParseForTest("https://example.test")
	if _, _, err := androidBridgeFromEnv(u); err == nil {
		t.Fatal("expected missing-token error")
	}
}

func urlParseForTest(raw string) (*url.URL, error) { return url.Parse(raw) }
