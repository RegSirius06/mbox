package main

import (
	"io"
	"log"
	"net"
	"time"

	"../common/dgram"
	"../common/udpwire"
)

const maxDatagramLanes = 32 // separate admission budget; never take HTTP slots

// Only enabled by an explicit operator-set path. Both existing MBox services
// run as user mbox; the local socket is owner-only; Noise authenticates attach.
func startOriginDatagrams(path string) (*net.UnixListener, error) {
	ln, err := dgram.ListenPrivate(path)
	if err != nil {
		return nil, err
	}
	slots := make(chan struct{}, maxDatagramLanes)
	go func() {
		for {
			c, err := ln.AcceptUnix()
			if err != nil {
				return
			}
			select {
			case slots <- struct{}{}:
				go func() { defer func() { <-slots }(); serveOriginDatagram(c) }()
			default:
				c.Close()
			}
		}
	}()
	return ln, nil
}

func serveOriginDatagram(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(dgram.SetupTimeout))
	var request [dgram.AttachSize]byte
	if _, err := io.ReadFull(c, request[:]); err != nil || !enableUDPLabServer || serverDraining.Load() {
		log.Print("DATAGRAM code=ORIGIN_ATTACH_UNAVAILABLE")
		return
	}
	sid := string(request[:32])
	if !dgram.ValidSID(sid) {
		return
	}
	s, ok := lookupSession(sid, false)
	if !ok || s.crypto == nil {
		log.Print("DATAGRAM code=ORIGIN_SESSION_UNAVAILABLE")
		return
	}
	up, down := s.crypto.up.DatagramSecret(), s.crypto.down.DatagramSecret()
	legacy := dgram.VerifyProof(up, sid, "attach", request[32:])
	var epoch uint32
	if !legacy {
		var valid bool
		epoch, valid = dgram.ParseAttachEpoch(up, sid, request[32:])
		if !valid {
			log.Print("DATAGRAM code=ORIGIN_ATTACH_AUTH_FAILED")
			return
		}
	}
	tx, err := dgram.NewRecordEpoch(down, sid, "down", epoch)
	if err != nil {
		return
	}
	rx, err := dgram.NewRecordEpoch(up, sid, "up", epoch)
	if err != nil {
		return
	}
	// Monotonic epochs reject replay. Legacy epoch 0 stays strictly one-shot.
	// Each newer epoch derives different AEAD keys before counters restart.
	s.mu.Lock()
	if s.closed || (s.datagramUsed && (legacy || epoch <= s.datagramEpoch)) {
		s.mu.Unlock()
		log.Print("DATAGRAM code=ORIGIN_ATTACH_ALREADY_USED")
		return
	}
	old := s.datagramLane.Load()
	s.datagramUsed = true
	s.datagramEpoch = epoch
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}
	proof := dgram.Proof(down, sid, "accept")
	if !legacy {
		proof = dgram.AcceptEpoch(down, sid, epoch)
	}
	if dgram.WriteFull(c, proof[:]) != nil || c.SetDeadline(time.Time{}) != nil {
		return
	}
	lane := dgram.NewLane(c, tx, rx)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		lane.Close()
		return
	}
	s.datagramLane.Store(lane)
	s.mu.Unlock()
	log.Print("DATAGRAM code=ORIGIN_ACTIVE")
	lane.Run(s.done, func(raw []byte) {
		d, err := udpwire.Unmarshal(raw)
		if err != nil {
			return
		}
		// Existing association ownership, public destination, quota, rate and
		// SOCKS routing checks are not bypassed by the new transport.
		_ = onUDPData(s, d.FlowID, raw)
	})
	s.datagramLane.CompareAndSwap(lane, nil)
	log.Print("DATAGRAM code=ORIGIN_STREAMS_FALLBACK")
}
