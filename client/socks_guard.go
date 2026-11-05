package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

const socksHandshakeTimeout = 30 * time.Second

var errSOCKSCommandRejected = errors.New("SOCKS5 command rejected by MBox policy")
var errHEVForwardUDPRequested = errors.New("HEV UDP-in-TCP requested via SOCKS5 command 0x05")

// guardSOCKSConnect is a local (pre-Noise) security boundary. The MBox
// transport carries a raw SOCKS5 byte stream to the remote SOCKS server;
// without this gate, UDP ASSOCIATE or BIND might be interpreted by that
// server even though MBox has no authenticated native datagram transport.
// Only an RFC 1928 CONNECT request may cross this boundary. The original
// greeting/request bytes are forwarded unchanged for beta.2.1 compatibility.
func guardSOCKSConnect(conn net.Conn, forward func([]byte) error) error {
	return guardSOCKSConnectWithUDP(conn, forward, false)
}

// The lab-only extension never forwards HEV FWD UDP to an upstream SOCKS
// server. It consumes the request and hands the same local TCP socket to the
// dedicated authenticated MBox UDP path.
func guardSOCKSConnectWithUDP(conn net.Conn, forward func([]byte) error, udpLab bool) error {
	if err := conn.SetReadDeadline(time.Now().Add(socksHandshakeTimeout)); err != nil {
		return err
	}
	defer conn.SetReadDeadline(time.Time{})

	var helloHeader [2]byte
	if _, err := io.ReadFull(conn, helloHeader[:]); err != nil {
		return err
	}
	if helloHeader[0] != 0x05 || helloHeader[1] == 0 {
		return fmt.Errorf("invalid local SOCKS5 greeting")
	}
	methods := make([]byte, int(helloHeader[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return err
	}
	noAuth := false
	for _, method := range methods {
		if method == 0x00 {
			noAuth = true
		}
	}
	if !noAuth {
		_ = conn.SetWriteDeadline(time.Now().Add(localWriteTimeout))
		_, _ = conn.Write([]byte{0x05, 0xff})
		return errSOCKSCommandRejected
	}
	greeting := append(helloHeader[:], methods...)
	if err := forward(greeting); err != nil {
		return err
	}

	var reqHeader [4]byte
	if _, err := io.ReadFull(conn, reqHeader[:]); err != nil {
		return err
	}
	if reqHeader[0] != 0x05 || reqHeader[2] != 0x00 {
		return fmt.Errorf("invalid local SOCKS5 request header")
	}
	hevUDP := udpLab && reqHeader[1] == 0x05
	if reqHeader[1] != 0x01 && !hevUDP { // Never forward UDP to TCP upstream.
		writeSOCKSFailure(conn, 0x07) // Command not supported.
		return errSOCKSCommandRejected
	}

	var addrBytes int
	switch reqHeader[3] {
	case 0x01: // IPv4 address + port
		addrBytes = 4 + 2
	case 0x04: // IPv6 address + port
		addrBytes = 16 + 2
	case 0x03: // Domain length + domain + port
		var domainLen [1]byte
		if _, err := io.ReadFull(conn, domainLen[:]); err != nil {
			return err
		}
		if domainLen[0] == 0 {
			return fmt.Errorf("zero length SOCKS domain")
		}
		addrBytes = int(domainLen[0]) + 2
		remaining := make([]byte, addrBytes)
		if _, err := io.ReadFull(conn, remaining); err != nil {
			return err
		}
		request := make([]byte, 0, len(reqHeader)+1+addrBytes)
		request = append(request, reqHeader[:]...)
		request = append(request, domainLen[0])
		request = append(request, remaining...)
		if hevUDP {
			return errHEVForwardUDPRequested
		}
		return forward(request)
	default:
		writeSOCKSFailure(conn, 0x08) // Address type not supported.
		return errSOCKSCommandRejected
	}

	remaining := make([]byte, addrBytes)
	if _, err := io.ReadFull(conn, remaining); err != nil {
		return err
	}
	request := append(reqHeader[:], remaining...)
	if hevUDP {
		return errHEVForwardUDPRequested
	}
	return forward(request)
}

func writeSOCKSFailure(conn net.Conn, code byte) {
	_ = conn.SetWriteDeadline(time.Now().Add(localWriteTimeout))
	// RFC 1928: VER REP RSV ATYP BND.ADDR BND.PORT. No datagram endpoint.
	_, _ = conn.Write([]byte{0x05, code, 0, 0x01, 0, 0, 0, 0, 0, 0})
}

func (t *tunnel) forwardLocalData(id uint32, st *localStream, p []byte) error {
	for len(p) > 0 {
		max := len(p)
		if max > maxFramePayload {
			max = maxFramePayload
		}
		grant, ok := st.reserveSendCredit(max, t.done)
		if !ok {
			return io.ErrClosedPipe
		}
		if err := t.queue(id, frameData, p[:grant]); err != nil {
			st.refundSendCredit(grant)
			return err
		}
		p = p[grant:]
	}
	return nil
}
