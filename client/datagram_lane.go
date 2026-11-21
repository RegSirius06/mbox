package main

import (
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	"../common/dgram"
	"../common/udpwire"
)

// On-demand QUIC DATAGRAM: no periodic PINGs. Retire idle QUIC lanes and
// reattach on subsequent real UDP traffic with a fresh authenticated epoch.
// Epoch 0 is accepted by the older one-shot DATAGRAM origin.
func (t *tunnel) startDatagramLane() {
	if t.udpMode == "streams" || !enableUDPClient || os.Getenv("MBOX_DATAGRAM") != "1" || os.Getenv("MBOX_HTTP_BRIDGE_ADDR") != "" {
		return
	}
	path := os.Getenv("MBOX_QUIC_UNIX")
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return
	}
	if t.datagramLane.Load() != nil {
		return
	}
	t.datagramMu.Lock()
	if t.datagramRunning || time.Now().Before(t.datagramRetryAfter) || t.datagramEpoch > dgram.MaxEpoch {
		t.datagramMu.Unlock()
		return
	}
	epoch := t.datagramEpoch
	t.datagramEpoch++ // even a lost accept consumes the attempt; never reuse keys
	t.datagramRunning = true
	t.datagramMu.Unlock()
	go func() {
		attached := t.runDatagramLane(path+".dgram", epoch)
		t.datagramMu.Lock()
		t.datagramRunning = false
		// An unsupported or unreachable gateway must not cause repeated
		// handshakes for every UDP packet. A retired active lane can rejoin soon.
		pause := 60 * time.Second
		if attached {
			pause = 3 * time.Second
		}
		t.datagramRetryAfter = time.Now().Add(pause)
		t.datagramMu.Unlock()
	}()
}

func (t *tunnel) runDatagramLane(path string, epoch uint32) bool {
	// A bounded retry burst deals with local startup races. Epochs are unique
	// across bursts; a genuinely accepted attach is never replayed.
	for attempt := 0; attempt < 3; attempt++ {
		select {
		case <-t.done:
			return false
		default:
		}
		if t.tryDatagramLane(path, epoch) {
			log.Print("DATAGRAM code=STREAMS_FALLBACK")
			return true
		}
		if attempt == 2 {
			break
		}
		timer := time.NewTimer(time.Duration(1<<attempt) * time.Second)
		select {
		case <-t.done:
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
	log.Print("DATAGRAM code=STREAMS_FALLBACK")
	return false
}

// A successful attach owns a new key/counter domain. Next reconnect increments
// the epoch rather than resetting the same AES-GCM nonce sequence.
func (t *tunnel) tryDatagramLane(path string, epoch uint32) bool {
	c, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		log.Print("DATAGRAM code=LOCAL_UNAVAILABLE")
		return false
	}
	defer c.Close()
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-t.done:
			c.Close()
		case <-finished:
		}
	}()
	_ = c.SetDeadline(time.Now().Add(dgram.SetupTimeout))
	up, down := t.crypto.up.DatagramSecret(), t.crypto.down.DatagramSecret()
	request := dgram.Attach(up, t.sid)
	if epoch != 0 {
		request = dgram.AttachEpoch(up, t.sid, epoch)
	}
	if dgram.WriteFull(c, request) != nil {
		log.Print("DATAGRAM code=ATTACH_WRITE_FAILED")
		return false
	}
	var proof [dgram.ProofSize]byte
	if _, err = io.ReadFull(c, proof[:]); err != nil ||
		(epoch == 0 && !dgram.VerifyProof(down, t.sid, "accept", proof[:])) ||
		(epoch != 0 && !dgram.VerifyAcceptEpoch(down, t.sid, epoch, proof[:])) {
		log.Print("DATAGRAM code=NEGOTIATION_FAILED")
		return false
	}
	tx, err := dgram.NewRecordEpoch(up, t.sid, "up", epoch)
	if err != nil {
		return true
	}
	rx, err := dgram.NewRecordEpoch(down, t.sid, "down", epoch)
	if err != nil {
		return true
	}
	if c.SetDeadline(time.Time{}) != nil {
		return true
	}
	lane := dgram.NewLane(c, tx, rx)
	select {
	case <-t.done:
		lane.Close()
		return true
	default:
	}
	t.datagramLane.Store(lane)
	log.Print("DATAGRAM code=ACTIVE")
	lane.Run(t.done, func(raw []byte) {
		d, err := udpwire.Unmarshal(raw)
		if err != nil {
			return
		}
		_ = t.handleFrames(frame(d.FlowID, frameUDPData, raw))
	})
	t.datagramLane.CompareAndSwap(lane, nil)
	return true
}
