package main

import (
	"net/netip"
	"testing"
	"time"

	"../common/udpwire"
)

func TestDatagramOnlyRequiresExplicitOpenFlag(t *testing.T) {
	s, upstream := serverUDPFixtures(t)
	if err := handleFrame(s, 201, frameUDPOpen, []byte{2}); err == nil {
		t.Fatal("unknown UDP-only marker accepted")
	}
	if err := handleFrame(s, 201, frameUDPOpen, []byte{1}); err != nil {
		t.Fatal(err)
	}
	waitUDPFlowReady(t, s, 201)
	if !udpFlowDatagramsOnly(s, 201) {
		t.Fatal("missing strict mode on association")
	}
	packet, err := udpwire.Marshal(udpwire.Datagram{
		FlowID: 201, Peer: netip.MustParseAddrPort("1.1.1.1:53"), Data: []byte("x"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// A reliable UDP data frame must not reach SOCKS for this flow.
	if err := handleFrame(s, 201, frameUDPData, packet); err != nil {
		t.Fatal(err)
	}
	select {
	case <-upstream:
		t.Fatal("DATAGRAM-only leaked UDP data into streams")
	case <-time.After(30 * time.Millisecond):
	}
	closeUDPFlow(s, 201, nil, false)
}
