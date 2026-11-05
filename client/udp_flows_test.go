package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	"../common/udpwire"
)

func TestHEVCommandOnlyExitsViaDedicatedLabFlow(t *testing.T) {
	hello := []byte{5, 1, 0}
	request := []byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0}
	for _, lab := range []bool{false, true} {
		conn := newGuardTestConn(append(append([]byte(nil), hello...), request...), 1)
		forwarded := make([]byte, 0)
		err := guardSOCKSConnectWithUDP(conn, func(p []byte) error { forwarded = append(forwarded, p...); return nil }, lab)
		if !bytes.Equal(forwarded, hello) {
			t.Fatalf("forwarded HEV request to upstream: %x", forwarded)
		}
		if lab && !errors.Is(err, errHEVForwardUDPRequested) {
			t.Fatalf("lab handoff failed: %v", err)
		}
		if !lab && !errors.Is(err, errSOCKSCommandRejected) {
			t.Fatalf("fail-closed rejected: %v", err)
		}
	}
}

func waitQueuedFrame(t *testing.T, tun *tunnel, typ byte) (uint32, []byte) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		raw := tun.takeUpload(1024 * 1024)
		for len(raw) >= 9 {
			id := binary.BigEndian.Uint32(raw[:4])
			n := int(binary.BigEndian.Uint32(raw[5:9]))
			if len(raw) < 9+n {
				t.Fatal("truncated queue")
			}
			if raw[4] == typ {
				return id, append([]byte(nil), raw[9:9+n]...)
			}
			raw = raw[9+n:]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("frame type %d missing", typ)
	return 0, nil
}

func TestHEVUDPFlowRoundTripThroughEncryptedFrameQueue(t *testing.T) {
	tun := newQueueTestTunnel()
	tun.udpFlows = make(map[uint32]*localUDPFlow)
	app, local := net.Pipe()
	defer app.Close()
	defer local.Close()
	st := &localStream{conn: local, writeQ: newByteQueue(streamWriteQueueMaxBytes), creditWake: make(chan struct{}, 1)}
	tun.streams[1] = st
	tun.nextID.Store(1)
	done := make(chan struct{})
	go func() { defer close(done); tun.handoffToUDP(1, st, local) }()
	id, _ := waitQueuedFrame(t, tun, frameUDPOpen)
	if id == 0 || id == 1 {
		t.Fatalf("invalid new UDP ID %d", id)
	}
	if err := tun.handleFrames(frame(id, frameUDPReady, nil)); err != nil {
		t.Fatal(err)
	}
	sockReply := make([]byte, 10)
	_ = app.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(app, sockReply); err != nil {
		t.Fatalf("HEV success: %v", err)
	}
	if !bytes.Equal(sockReply, []byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("bad HEV success: %x", sockReply)
	}
	peer := netip.MustParseAddrPort("9.9.9.9:53")
	hev, _ := udpwire.MarshalHEV(peer, []byte("dns-query"))
	if _, err := app.Write(hev); err != nil {
		t.Fatal(err)
	}
	dataID, up := waitQueuedFrame(t, tun, frameUDPData)
	got, err := udpwire.Unmarshal(up)
	if err != nil || dataID != id || got.FlowID != id || got.Peer != peer || string(got.Data) != "dns-query" {
		t.Fatalf("wrong uploaded datagram %+v %v", got, err)
	}
	down, _ := udpwire.Marshal(udpwire.Datagram{FlowID: id, Peer: peer, Data: []byte("reply")})
	if err := tun.handleFrames(frame(id, frameUDPData, down)); err != nil {
		t.Fatal(err)
	}
	_ = app.SetReadDeadline(time.Now().Add(time.Second))
	gotPeer, gotData, err := udpwire.ReadHEV(app)
	if err != nil || gotPeer != peer || string(gotData) != "reply" {
		t.Fatalf("bad HEV reply %v %q %v", gotPeer, gotData, err)
	}
	tun.closeUDPFlow(id, false)
	<-done
	close(tun.done)
	tun.streamWG.Wait()
	if tun.activeStreams() != 0 {
		t.Fatal("flow still active")
	}
}

func TestHEVUDPReturnRejectsCrossFlowAndUnopenedTraffic(t *testing.T) {
	tun := newQueueTestTunnel()
	dat, _ := udpwire.Marshal(udpwire.Datagram{FlowID: 42, Peer: netip.MustParseAddrPort("1.1.1.1:53"), Data: []byte("x")})
	if err := tun.handleFrames(frame(41, frameUDPData, dat)); err == nil {
		t.Fatal("accepted cross-flow injection")
	}
	if err := tun.handleFrames(frame(42, frameUDPData, dat)); err != nil {
		t.Fatal("unknown flow should be safely ignored")
	}
}
