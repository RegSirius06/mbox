//go:build linux || android

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func TestClosedAndroidLogPipeDoesNotTerminateGoCore(t *testing.T) {
	if os.Getenv("MBOX_TEST_CLOSED_LOG_PIPE") == "1" {
		payload := make([]byte, 32*1024)
		// Child stdout is a pipe intentionally closed by its parent. Writes
		// must return EPIPE rather than killing the child with SIGPIPE/141.
		for i := 0; i < 16; i++ {
			_, err := syscall.Write(int(os.Stdout.Fd()), payload)
			if errors.Is(err, syscall.EPIPE) {
				return
			}
			if err != nil {
				t.Fatalf("unexpected write error: %v", err)
			}
		}
		t.Fatal("no EPIPE observed from closed log pipe")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestClosedAndroidLogPipeDoesNotTerminateGoCore$")
	cmd.Env = append(os.Environ(), "MBOX_TEST_CLOSED_LOG_PIPE=1")
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = pipe.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("child died on closed log pipe: %v", err)
	}
}
