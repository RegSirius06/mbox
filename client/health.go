package main

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"time"
)

// framePing is already echoed by beta.4 servers inside the authenticated
// Noise channel. Stream ID zero is reserved for this connection-level probe.
// Neither the HTTP status nor the local SOCKS listener proves session health.
const (
	healthActiveInterval = 30 * time.Second
	healthIdleInterval   = 90 * time.Second
	healthTimeout        = 10 * time.Second
)

func (t *tunnel) pokeHealth() {
	if t.healthWake == nil {
		return
	}
	select {
	case t.healthWake <- struct{}{}:
	default:
	}
}

func (t *tunnel) acceptHealthPong(id uint32, payload []byte) {
	if id != 0 || len(payload) != 16 {
		return
	}
	t.healthMu.Lock()
	defer t.healthMu.Unlock()
	if t.healthReply != nil && subtle.ConstantTimeCompare(payload, t.healthNonce[:]) == 1 {
		select {
		case t.healthReply <- struct{}{}:
		default:
		}
	}
}

func (t *tunnel) healthProbe(timeout time.Duration) error {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Errorf("health nonce: %w", err)
	}
	ack := make(chan struct{}, 1)
	t.healthMu.Lock()
	t.healthNonce = nonce
	t.healthReply = ack
	t.healthMu.Unlock()
	defer func() {
		t.healthMu.Lock()
		if t.healthReply == ack {
			t.healthReply = nil
		}
		t.healthMu.Unlock()
	}()
	if err := t.queue(0, framePing, nonce[:]); err != nil {
		return err
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ack:
		return nil
	case <-t.done:
		return errors.New("session terminated")
	case <-timer.C:
		return errors.New("remote authenticated ping timed out")
	}
}

func (t *tunnel) healthLoop() {
	for {
		if err := t.healthProbe(healthTimeout); err != nil {
			select {
			case <-t.done:
				return
			default:
				t.fail(fmt.Errorf("MBox liveness: %w", err))
				return
			}
		}
		// A bounded authenticated check also gives Android positive evidence
		// after wake-up. No public website probes or local-port shortcuts.
		log.Print("NET stage=MBOX_HEALTH code=OK")
		interval := healthIdleInterval
		if t.activeStreams() > 0 {
			interval = healthActiveInterval
		}
		timer := time.NewTimer(jitterDuration(interval, 30))
		select {
		case <-t.done:
			timer.Stop()
			return
		case <-t.healthWake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
		}
	}
}
