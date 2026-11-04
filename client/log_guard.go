package main

import (
	"io"
	"log"
	"os"
	"regexp"
	"strings"
)

const maxDiagnosticLine = 4096

var (
	// Both HTTPS relay paths and mbox:// links can contain reusable secrets.
	// Error values produced by net/http may re-embed them without warning.
	sensitiveURL = regexp.MustCompile(`(?i)(?:https?|mbox)://[^\s"'<>]+`)
	sensitiveKV  = regexp.MustCompile(`(?i)\b(psk|password|token|secret|authorization|private_key)\s*[:=]\s*[^\s,;]+`)
	bearerToken  = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/-]+`)
)

// redactDiagnostic guarantees that a secret-bearing URL cannot escape through
// standard Go logging even when its origin is an arbitrary wrapped error.
// It also bounds one log message and prevents forged additional log lines.
// This is defense in depth: logs from other processes require their own policy.
func redactDiagnostic(p []byte) []byte {
	// Bound *before* applying regular expressions to attacker-controlled data.
	if len(p) > maxDiagnosticLine {
		p = p[:maxDiagnosticLine]
	}
	message := strings.TrimSuffix(string(p), "\n")
	message = strings.ReplaceAll(message, "\r", `\r`)
	message = strings.ReplaceAll(message, "\n", `\n`)
	message = sensitiveURL.ReplaceAllString(message, "[URL_REDACTED]")
	message = bearerToken.ReplaceAllString(message, "Bearer [REDACTED]")
	message = sensitiveKV.ReplaceAllStringFunc(message, func(s string) string {
		sep := strings.IndexAny(s, ":=")
		if sep == -1 {
			return "[SECRET_REDACTED]"
		}
		return s[:sep+1] + "[REDACTED]"
	})
	var b strings.Builder
	b.Grow(len(message) + 1)
	for _, r := range message {
		if r < 32 && r != '\t' {
			b.WriteString("?")
		} else {
			b.WriteRune(r)
		}
	}
	b.WriteByte('\n')
	return []byte(b.String())
}

type redactedLogWriter struct{ dst io.Writer }

func (w redactedLogWriter) Write(p []byte) (int, error) {
	// log.Logger serializes writes. Preserve the input byte count expected by
	// its Writer contract, even though redaction changes the output length.
	n := len(p)
	out := redactDiagnostic(p)
	_, err := w.dst.Write(out)
	if err != nil {
		return 0, err
	}
	return n, nil
}

func init() {
	// We intentionally keep the standard library logger so existing stage
	// diagnostics remain readable; only its output is filtered.
	log.SetOutput(redactedLogWriter{dst: os.Stderr})
}
