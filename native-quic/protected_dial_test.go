package main

import (
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestProtectPacketSocketRequiresBrokerAndAck(t *testing.T) {
	udp, err := net.ListenUDP("udp4", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	if err := protectPacketSocket(udp, "", ""); err == nil {
		t.Fatal("missing bridge must fail")
	}
	socket := filepath.Join(t.TempDir(), "protect.sock")
	broker, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer broker.Close()
	token := "0123456789abcdef0123456789abcdef"
	outcome := make(chan error, 1)
	go func() {
		conn, err := broker.AcceptUnix()
		if err != nil {
			outcome <- err
			return
		}
		defer conn.Close()
		buf := make([]byte, 128)
		control := make([]byte, 128)
		n, _, _, _, err := conn.ReadMsgUnix(buf, control)
		if err != nil {
			outcome <- err
			return
		}
		if string(buf[:n]) != token {
			outcome <- syscall.EINVAL
			return
		}
		messages, err := syscall.ParseSocketControlMessage(control)
		if err != nil {
			outcome <- err
			return
		}
		if len(messages) != 1 {
			outcome <- syscall.EINVAL
			return
		}
		fds, err := syscall.ParseUnixRights(&messages[0])
		if err != nil || len(fds) != 1 {
			outcome <- syscall.EINVAL
			return
		}
		syscall.Close(fds[0])
		_, err = conn.Write([]byte{0})
		outcome <- err
	}()
	if err := protectPacketSocket(udp, socket, token); err != nil {
		t.Fatal(err)
	}
	if err := <-outcome; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(socket); err != nil {
		t.Fatal(err)
	}
}
