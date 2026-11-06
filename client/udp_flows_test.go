package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	flowctl "../common/flowctl"
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
	st := &localStream{conn: local, writeQ: flowctl.NewByteQueue(streamWriteQueueMaxBytes), creditWake: make(chan struct{}, 1)}
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

// The old TCP greeting writer shares its socket with the HEV UDP handoff.
// Opening UDP before that writer exits could interleave the SOCKS replies.
func TestHEVHandoffWaitsForOldSOCKSWriter(t *testing.T) {
	tun := newQueueTestTunnel()
	tun.udpFlows = make(map[uint32]*localUDPFlow)
	app, local := net.Pipe()
	defer app.Close()
	defer local.Close()
	writerDone := make(chan struct{})
	st := &localStream{conn: local, writeQ: flowctl.NewByteQueue(streamWriteQueueMaxBytes), creditWake: make(chan struct{}, 1), writerDone: writerDone}
	tun.streams[1] = st
	tun.nextID.Store(1)
	finished := make(chan struct{})
	go func() { defer close(finished); tun.handoffToUDP(1, st, local) }()

	deadline := time.Now().Add(time.Second)
	for {
		raw := tun.takeUpload(1024)
		if len(raw) > 0 {
			if len(raw) != 9 || raw[4] != frameClose {
				t.Fatalf("UDP started before old writer exited: %x", raw)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("old stream CLOSE not queued")
		}
		time.Sleep(time.Millisecond)
	}
	if raw := tun.takeUpload(1024); len(raw) != 0 {
		t.Fatalf("UDP OPEN escaped writer barrier: %x", raw)
	}
	close(writerDone)
	id, _ := waitQueuedFrame(t, tun, frameUDPOpen)
	tun.closeUDPFlow(id, false)
	<-finished
	close(tun.done)
	tun.streamWG.Wait()
}

func TestFatalTransportClosesLocalUDPImmediately(t *testing.T) {
	tun := newQueueTestTunnel()
	app, local := net.Pipe()
	defer app.Close()
	f := &localUDPFlow{conn: local, ready: make(chan struct{}), writeQ: flowctl.NewByteQueue(streamWriteQueueMaxBytes)}
	tun.udpFlows = map[uint32]*localUDPFlow{7: f}
	tun.fail(errors.New("simulated downlink authentication failure"))
	if !f.closed.Load() || tun.activeStreams() != 0 {
		t.Fatal("UDP association survived fatal transport failure")
	}
	if f.writeQ.TryPush([]byte("late data")) {
		t.Fatal("accepted data after tunnel failure")
	}
	_ = app.SetReadDeadline(time.Now().Add(time.Second))
	var b [1]byte
	if _, err := app.Read(b[:]); !errors.Is(err, io.EOF) {
		t.Fatalf("local HEV socket did not close immediately: %v", err)
	}
}

type gateFirstWriteConn struct {
	net.Conn
	first   sync.Once
	entered chan struct{}
	unblock chan struct{}
	writes  atomic.Int32
}

func (c *gateFirstWriteConn) Write(p []byte) (int, error) {
	c.writes.Add(1)
	c.first.Do(func() {
		close(c.entered)
		<-c.unblock
	})
	return c.Conn.Write(p)
}

// A fast UDP response may arrive in the same DOWN batch as READY. Never
// let the background writer send that datagram before SOCKS success (10 bytes).
func TestHEVReplyPrecedesFastUDPDatagram(t *testing.T) {
	tun := newQueueTestTunnel()
	tun.udpFlows = make(map[uint32]*localUDPFlow)
	app, local := net.Pipe()
	defer app.Close()
	wrapped := &gateFirstWriteConn{Conn: local, entered: make(chan struct{}), unblock: make(chan struct{})}
	st := &localStream{conn: wrapped, writeQ: flowctl.NewByteQueue(streamWriteQueueMaxBytes), creditWake: make(chan struct{}, 1)}
	tun.streams[1] = st
	tun.nextID.Store(1)
	finished := make(chan struct{})
	go func() { defer close(finished); tun.handoffToUDP(1, st, wrapped) }()
	id, _ := waitQueuedFrame(t, tun, frameUDPOpen)
	if err := tun.handleFrames(frame(id, frameUDPReady, nil)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-wrapped.entered:
	case <-time.After(time.Second):
		t.Fatal("SOCKS success write not initiated")
	}
	peer := netip.MustParseAddrPort("1.1.1.1:53")
	response, err := udpwire.Marshal(udpwire.Datagram{FlowID: id, Peer: peer, Data: []byte("response")})
	if err != nil {
		t.Fatal(err)
	}
	if err := tun.handleFrames(frame(id, frameUDPData, response)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if n := wrapped.writes.Load(); n != 1 {
		t.Fatalf("UDP writer raced unfinished SOCKS reply: %d writes", n)
	}
	close(wrapped.unblock)
	_ = app.SetReadDeadline(time.Now().Add(time.Second))
	raw := make([]byte, 10)
	if _, err := io.ReadFull(app, raw); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, []byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("wrong SOCKS header: %x", raw)
	}
	gotPeer, gotData, err := udpwire.ReadHEV(app)
	if err != nil || gotPeer != peer || string(gotData) != "response" {
		t.Fatalf("response %v %q %v", gotPeer, gotData, err)
	}
	tun.closeUDPFlow(id, false)
	<-finished
	close(tun.done)
	tun.streamWG.Wait()
}
