package main

import "testing"

func TestApacheFastCGIPathCompatibility(t *testing.T) {
	cfg := &relayConfig{
		AllowedExact:    []string{"/test/health"},
		AllowedPrefixes: []string{"/test/api/v1/"},
	}
	for _, raw := range []string{
		"/relay.fcgi/test/health",
		"/test/health",
		"/relay.fcgi/test/api/v1/open",
		"/test/api/v1/open",
	} {
		c, err := canonicalPath(raw)
		if err != nil {
			t.Fatal(err)
		}
		forward, ok := stripWrapperPath(c)
		if !ok || !allowedPath(cfg, forward) {
			t.Fatalf("rejected valid path %q", raw)
		}
	}
	for _, raw := range []string{
		"/not-allowed",
		"/relay.fcgi/test/private",
		"/relay.fcgi-evil/test/health",
	} {
		forward, ok := stripWrapperPath(raw)
		if ok && allowedPath(cfg, forward) {
			t.Fatalf("accepted forbidden path %q", raw)
		}
	}
}
