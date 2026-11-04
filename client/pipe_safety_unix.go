//go:build linux || android

package main

import (
	"os/signal"
	"syscall"
)

func init() {
	// Android closes the merged stdout/stderr pipe while replacing a stopped
	// tunnel process during handover. The Go runtime would otherwise allow a
	// write to its standard fds to terminate the process with SIGPIPE (141).
	// Security invariant: losing a diagnostic consumer must not kill an
	// otherwise healthy transport. The write receives EPIPE instead.
	signal.Ignore(syscall.SIGPIPE)
}
