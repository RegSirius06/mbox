package main

import (
	"fmt"
	"net/http"
	"net/url"
	"time"
)

func (t *tunnel) queue(streamID uint32, typ byte, payload []byte) error {
	if len(payload) > maxFramePayload {
		return fmt.Errorf("frame payload too large: %d", len(payload))
	}
	f := frame(streamID, typ, payload)
	if len(f) > camouflage.UpMaxBytes {
		return fmt.Errorf("frame exceeds UP record limit")
	}
	for {
		t.upMu.Lock()
		per := t.upPerStream[streamID]
		if t.upBytes+len(f) <= maxUpQueueBytes && per+len(f) <= maxUpPerStreamQueueBytes {
			if len(t.upQueues[streamID]) == 0 {
				t.upOrder = append(t.upOrder, streamID)
			}
			t.upQueues[streamID] = append(t.upQueues[streamID], f)
			t.upBytes += len(f)
			t.upPerStream[streamID] = per + len(f)
			t.upMu.Unlock()
			select {
			case t.wake <- struct{}{}:
			default:
			}
			select {
			case t.downWake <- struct{}{}:
			default:
			}
			return nil
		}
		t.upMu.Unlock()
		select {
		case <-t.done:
			return fmt.Errorf("tunnel stopped")
		case <-t.upSpace:
		}
	}
}

func (t *tunnel) takeUpload(limit int) []byte {
	if limit <= 0 {
		limit = camouflage.UpMaxBytes
	}
	t.upMu.Lock()
	defer t.upMu.Unlock()
	if t.upBytes == 0 || len(t.upOrder) == 0 {
		return nil
	}
	data := make([]byte, 0, limit)
	skipped, removed := 0, 0
	for len(t.upOrder) > 0 {
		sid := t.upOrder[0]
		t.upOrder = t.upOrder[1:]
		q := t.upQueues[sid]
		if len(q) == 0 {
			delete(t.upQueues, sid)
			delete(t.upPerStream, sid)
			continue
		}
		f := q[0]
		if len(data) > 0 && len(data)+len(f) > limit {
			t.upOrder = append(t.upOrder, sid)
			skipped++
			if skipped >= len(t.upOrder) {
				break
			}
			continue
		}
		if len(f) > limit {
			break
		}
		q[0] = nil
		q = q[1:]
		data = append(data, f...)
		t.upBytes -= len(f)
		t.upPerStream[sid] -= len(f)
		removed += len(f)
		skipped = 0
		if len(q) == 0 {
			delete(t.upQueues, sid)
			delete(t.upPerStream, sid)
		} else {
			t.upQueues[sid] = q
			t.upOrder = append(t.upOrder, sid)
		}
		if len(data) >= limit {
			break
		}
	}
	if removed > 0 {
		select {
		case t.upSpace <- struct{}{}:
		default:
		}
	}
	return data
}

func (t *tunnel) newDownRequestURL(raw string, includeMax bool) (string, uint64, error) {
	seq := t.downReqSeq.Add(1)
	u, err := url.Parse(raw)
	if err != nil {
		return "", 0, err
	}
	q := u.Query()
	q.Set("seq", fmt.Sprintf("%d", seq))
	q.Set("ack", fmt.Sprintf("%d", t.downAck.Load()))
	if includeMax {
		q.Set("max", fmt.Sprintf("%d", randomBatchLimit(camouflage.DownMaxBytes)))
	}
	u.RawQuery = q.Encode()
	return u.String(), seq, nil
}

func (t *tunnel) uploader() {
	for {
		select {
		case <-t.done:
			return
		case <-t.wake:
		}
		for {
			plain := t.takeUpload(randomBatchLimit(camouflage.UpMaxBytes))
			if len(plain) == 0 {
				break
			}
			padded, err := padTransportPlaintext(plain)
			if err != nil {
				t.fail(fmt.Errorf("pad upload: %w", err))
				return
			}
			record, err := t.crypto.sealUp(t.sid, padded)
			if err != nil {
				t.fail(fmt.Errorf("encrypt upload: %w", err))
				return
			}
			upURL, _, err := t.newDownRequestURL(base+camouflage.UpPath+t.sid, false)
			if err != nil {
				t.fail(err)
				return
			}
			var failureSince time.Time
			retry := time.Duration(camouflage.Recovery.RetryMinMS) * time.Millisecond
			for {
				resp, status, reqErr := t.request("POST", upURL, record)
				if reqErr == nil && (status == http.StatusOK || status == http.StatusNoContent) {
					if status == http.StatusOK && len(resp) > 0 {
						if err := t.submitDownRecord(resp); err != nil {
							t.fail(err)
							return
						}
					}
					break
				}
				if reqErr == nil && (status == http.StatusNotFound || status == http.StatusGone) {
					t.fail(fmt.Errorf("session lost: upload HTTP %d", status))
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
					t.fail(fmt.Errorf("upload unavailable: %w", failure))
					return
				}
				timer := time.NewTimer(retry)
				select {
				case <-t.done:
					timer.Stop()
					return
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
}
