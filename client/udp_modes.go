package main

import (
	"os"
	"time"
)

func udpModeFromEnv() string {
	value := os.Getenv("MBOX_UDP_MODE")
	switch value {
	case "streams", "datagrams":
		return value
	default:
		return "auto"
	}
}

// Strict mode only, never used on the AUTO path. A bounded wait for
// initial attach allows a single DNS/UDP request to work without streams.
// No periodic keepalive runs while user traffic is idle.
func (t *tunnel) offerDatagramOnly(id uint32, wire []byte) {
	if t.datagramLane.Load().Offer(id, wire) {
		return
	}
	t.startDatagramLane()
	timeout := time.NewTimer(2 * time.Second)
	tick := time.NewTicker(40 * time.Millisecond)
	defer timeout.Stop()
	defer tick.Stop()
	for {
		select {
		case <-t.done:
			return
		case <-timeout.C:
			return // drop instead of silently falling back to streams
		case <-tick.C:
			if t.datagramLane.Load().Offer(id, wire) {
				return
			}
		}
	}
}
