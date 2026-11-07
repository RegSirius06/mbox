package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"runtime"
	"syscall"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

// The Android VPN has a full-route TUN. Before the first outgoing packet, the
// app must bind each QUIC socket to its selected physical Android Network.
// The FD crosses an authenticated app-private AF_UNIX socket (SCM_RIGHTS).
func protectedQUICDial(protectPath, token string) func(context.Context, string, *tls.Config, *quic.Config) (*quic.Conn, error) {
	return func(ctx context.Context, addr string, tlsCfg *tls.Config, cfg *quic.Config) (*quic.Conn, error) {
		dest, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			return nil, err
		}
		if runtime.GOOS == "android" && dest.IP == nil {
			return nil, errors.New("Android QUIC requires a numeric remote IP")
		}
		family := "udp6"
		if dest.IP.To4() != nil {
			family = "udp4"
		}
		packet, err := net.ListenUDP(family, nil)
		if err != nil {
			return nil, err
		}
		if err = protectPacketSocket(packet, protectPath, token); err != nil {
			packet.Close()
			return nil, err
		}
		conn, err := quic.Dial(ctx, packet, dest, tlsCfg, cfg)
		if err != nil {
			packet.Close()
			return nil, err
		}
		go func() { <-conn.Context().Done(); packet.Close() }()
		return conn, nil
	}
}

func protectPacketSocket(packet *net.UDPConn, path, token string) error {
	if path == "" || len(token) < 32 {
		return errors.New("QUIC physical-network protection is required")
	}
	raw, err := packet.SyscallConn()
	if err != nil {
		return err
	}
	fd := -1
	if err := raw.Control(func(original uintptr) {
		fd, err = syscall.Dup(int(original))
	}); err != nil {
		return err
	}
	if err != nil {
		return err
	}
	if fd < 0 {
		return errors.New("cannot duplicate QUIC UDP socket")
	}
	defer syscall.Close(fd)

	// @name is an Android abstract local socket. Its server authenticates UID
	// and the unpredictable session token before binding the supplied FD.
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return fmt.Errorf("QUIC protection bridge: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	n, _, err := conn.WriteMsgUnix([]byte(token), syscall.UnixRights(fd), nil)
	if err != nil {
		return err
	}
	if n != len(token) {
		return errors.New("incomplete QUIC protection token")
	}
	var ack [1]byte
	n, err = conn.Read(ack[:])
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("missing QUIC socket protection response")
	}
	if ack[0] != 0 {
		return errors.New("physical network rejected QUIC socket")
	}
	return nil
}

// Keep this signature separate from quicHTTPClient for existing Arch CLI.
func quicProtectedClient(caFile, protectPath, token string) (*http3.Transport, error) {
	if runtime.GOOS == "android" && protectPath == "" {
		return nil, errors.New("Android QUIC requires socket protection")
	}
	pool, err := systemAndExtraRoots(caFile)
	if err != nil {
		return nil, err
	}
	tr := &http3.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pool}}
	if protectPath != "" {
		tr.Dial = protectedQUICDial(protectPath, token)
	}
	return tr, nil
}

// Explicitly ensure no accidental direct UDP dial can bypass protection in
// Android mode, including when the remote is not a numeric IP address.
func requireNumericRemoteOnAndroid(host string) error {
	if runtime.GOOS != "android" {
		return nil
	}
	addr, _, err := net.SplitHostPort(host)
	if err != nil {
		return err
	}
	if net.ParseIP(addr) == nil {
		return errors.New("Android QUIC remote must use a numeric IP")
	}
	return nil
}
