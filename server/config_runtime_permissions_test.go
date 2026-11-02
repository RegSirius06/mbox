package main

import (
	"os"
	"testing"
)

func TestPrivateKeyModeAllowed(t *testing.T) {
	allowed := []os.FileMode{0o400, 0o600, 0o440, 0o640}
	for _, mode := range allowed {
		if !privateKeyModeAllowed(mode) {
			t.Errorf("mode %04o should be allowed", mode)
		}
	}
	rejected := []os.FileMode{0o644, 0o660, 0o620, 0o604, 0o777}
	for _, mode := range rejected {
		if privateKeyModeAllowed(mode) {
			t.Errorf("mode %04o should be rejected", mode)
		}
	}
}
