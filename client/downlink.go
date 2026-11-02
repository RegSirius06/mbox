package main

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net/http"
	"time"
)

func (t *tunnel) activeStreams() int {
	t.streamsMu.Lock()
	defer t.streamsMu.Unlock()
	return len(t.streams)
}

func (t *tunnel) submitDownRecord(record []byte) error {
	c := append([]byte(nil), record...)
	select {
	case <-t.done:
		return fmt.Errorf("tunnel stopped")
	case t.downDeliver <- c:
		return nil
	}
}

func (t *tunnel) downDeliveryLoop() {
	for {
		select {
		case <-t.done:
			return
		case record := <-t.downDeliver:
			counter, padded, err := t.crypto.openDown(t.sid, record)
			if err != nil {
				t.fail(fmt.Errorf("down authentication: %w", err))
				return
			}
			plain, err := unpadTransportPlaintext(padded)
			if err != nil {
				t.fail(fmt.Errorf("down padding: %w", err))
				return
			}
			if counter < t.nextDown {
				continue
			}
			if counter-t.nextDown > 256 {
				t.fail(fmt.Errorf("down reorder window exceeded: got=%d want=%d", counter, t.nextDown))
				return
			}
			if _, exists := t.downPending[counter]; !exists {
				if t.downPendingBytes+len(plain) > maxDownPendingBytes {
					t.fail(fmt.Errorf("down pending memory budget exceeded"))
					return
				}
				t.downPending[counter] = plain
				t.downPendingBytes += len(plain)
			}
			for {
				p, ok := t.downPending[t.nextDown]
				if !ok {
					break
				}
				delete(t.downPending, t.nextDown)
				t.downPendingBytes -= len(p)
				if err := t.handleFrames(p); err != nil {
					t.fail(fmt.Errorf("down frame delivery: %w", err))
					return
				}
				t.downAck.Store(t.nextDown)
				t.nextDown++
				select {
				case t.downWake <- struct{}{}:
				default:
				}
			}
		}
	}
}

func randomDuration(minMS, maxMS int) time.Duration {
	if minMS <= 0 {
		minMS = 1
	}
	if maxMS < minMS {
		maxMS = minMS
	}
	if deterministicTransport {
		return time.Duration((minMS+maxMS)/2) * time.Millisecond
	}
	if minMS == maxMS {
		return time.Duration(minMS) * time.Millisecond
	}

	span := maxMS - minMS + 1

	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Duration((minMS+maxMS)/2) * time.Millisecond
	}

	n := binary.BigEndian.Uint64(b[:])
	ms := minMS + int(n%uint64(span))

	return time.Duration(ms) * time.Millisecond
}

func jitterDuration(base time.Duration, percent int) time.Duration {
	if percent <= 0 {
		return base
	}
	if percent > 100 {
		percent = 100
	}

	baseMS := int(base / time.Millisecond)
	delta := baseMS * percent / 100

	if delta < 1 {
		delta = 1
	}

	minMS := baseMS - delta
	maxMS := baseMS + delta

	if minMS < 1 {
		minMS = 1
	}

	return randomDuration(minMS, maxMS)
}

func (t *tunnel) downloader() {
	activeDelay := randomDuration(camouflage.Timing.ActiveMinMS, camouflage.Timing.ActiveMaxMS)
	var failureSince time.Time
	for {
		if t.activeStreams() == 0 && failureSince.IsZero() {
			timer := time.NewTimer(randomDuration(camouflage.Timing.IdleMinMS, camouflage.Timing.IdleMaxMS))
			select {
			case <-t.done:
				timer.Stop()
				return
			case <-t.downWake:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				time.Sleep(randomDuration(camouflage.Timing.WakeMinMS, camouflage.Timing.WakeMaxMS))
			case <-timer.C:
			}
		} else {
			timer := time.NewTimer(jitterDuration(activeDelay, camouflage.Timing.ActiveJitterP))
			select {
			case <-t.done:
				timer.Stop()
				return
			case <-t.downWake:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			case <-timer.C:
			}
		}
		requestURL, _, err := t.newDownRequestURL(base+camouflage.DownPath+t.sid, true)
		if err != nil {
			t.fail(err)
			return
		}
		retry := time.Duration(camouflage.Recovery.RetryMinMS) * time.Millisecond
		for {
			data, status, reqErr := t.request("GET", requestURL, nil)
			if reqErr == nil && (status == http.StatusOK || status == http.StatusNoContent) {
				failureSince = time.Time{}
				if status == http.StatusOK {
					if err := t.submitDownRecord(data); err != nil {
						t.fail(err)
						return
					}
					activeDelay = randomDuration(camouflage.Timing.ActiveMinMS, camouflage.Timing.ActiveMaxMS)
				} else if t.activeStreams() > 0 {
					baseMS := int(activeDelay / time.Millisecond)
					nextMin := baseMS + baseMS/3
					nextMax := baseMS*2 + baseMS/3
					if nextMax > camouflage.Timing.ActiveBackoffMS {
						nextMax = camouflage.Timing.ActiveBackoffMS
					}
					if nextMin > nextMax {
						nextMin = nextMax
					}
					activeDelay = randomDuration(nextMin, nextMax)
				} else {
					activeDelay = randomDuration(camouflage.Timing.ActiveMinMS, camouflage.Timing.ActiveMaxMS)
				}
				break
			}
			if reqErr == nil && (status == http.StatusGone || status == http.StatusNotFound) {
				t.fail(fmt.Errorf("session lost: download HTTP %d", status))
				return
			}
			if reqErr == nil && status >= 400 && status < 500 {
				t.fail(fmt.Errorf("download protocol HTTP %d", status))
				return
			}
			if failureSince.IsZero() {
				failureSince = time.Now()
			}
			var failure error
			if reqErr != nil {
				failure = reqErr
			} else {
				failure = fmt.Errorf("HTTP %d", status)
			}
			if time.Since(failureSince) >= time.Duration(camouflage.Recovery.FailureTimeoutMS)*time.Millisecond {
				t.fail(fmt.Errorf("download unavailable: %w", failure))
				return
			}
			timer := time.NewTimer(retry)
			select {
			case <-t.done:
				timer.Stop()
				return
			case <-t.downWake:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			case <-timer.C:
			}
			retry *= 2
			max := time.Duration(camouflage.Recovery.RetryMaxMS) * time.Millisecond
			if retry > max {
				retry = max
			}
		}
	}
}
