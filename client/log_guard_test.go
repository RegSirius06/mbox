package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRedactedDiagnosticSuppressesProvisioningAndRelaySecrets(t *testing.T) {
	samples := []struct {
		text      string
		forbidden []string
	}{
		{`Post "https://example.fun/edge/0123456789abcdef/api/v1/session": timeout`, []string{"/edge/", "0123456789abcdef", "example.fun"}},
		{`mbox://my-secret-provisioning#profile`, []string{"my-secret", "provisioning"}},
		{`Authorization: Bearer hunter2`, []string{"hunter2"}},
		{`Proxy-Authorization: Basic dXNlcjpwYXNz`, []string{"dXNlcjpwYXNz"}},
		{`psk=CAFEBABE password:abcd token=xyz private_key=qwerty`, []string{"CAFEBABE", "abcd", "xyz", "qwerty"}},
	}
	for _, tt := range samples {
		result := string(redactDiagnostic([]byte(tt.text + "\n")))
		for _, secret := range tt.forbidden {
			if strings.Contains(result, secret) {
				t.Fatalf("secret %q was logged: %q", secret, result)
			}
		}
	}
}

func TestRedactedDiagnosticCapsAndEscapesUntrustedMessages(t *testing.T) {
	input := "HTTP ERROR\nFORGED: success\r" + strings.Repeat("X", maxDiagnosticLine*2)
	result := string(redactDiagnostic([]byte(input)))
	if strings.Count(result, "\n") != 1 {
		t.Fatalf("multiple log lines escaped: %q", result[:70])
	}
	if !strings.Contains(result, `\nFORGED`) || !strings.Contains(result, `\r`) {
		t.Fatal("control character escaped into log")
	}
	if len(result) > maxDiagnosticLine*2 {
		t.Fatalf("unbounded log line: %d", len(result))
	}
}

func TestRedactedWriterReturnsOriginalByteCount(t *testing.T) {
	var b bytes.Buffer
	w := redactedLogWriter{dst: &b}
	input := []byte("https://example.fun/path-with-private-token\n")
	n, err := w.Write(input)
	if err != nil || n != len(input) {
		t.Fatalf("write (%d,%v)", n, err)
	}
	if strings.Contains(b.String(), "path-with-private-token") {
		t.Fatal("unredacted URL")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken stream") }
func TestRedactedWriterPropagatesWriteError(t *testing.T) {
	w := redactedLogWriter{dst: failingWriter{}}
	if _, err := w.Write([]byte("hello\n")); err == nil {
		t.Fatal("ignored output error")
	}
}
