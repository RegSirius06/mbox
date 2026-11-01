package main

import (
	"encoding/binary"
	"fmt"
	"sync"
	"time"
)

const (
	maxFramePayload          = 64 * 1024
	streamInitialWindow      = 256 * 1024
	streamWriteQueueMaxBytes = streamInitialWindow
	localWriteTimeout        = 15 * time.Second
	maxStreamCredit          = streamInitialWindow
	maxUpQueueBytes          = 8 * 1024 * 1024
	maxUpPerStreamQueueBytes = 1024 * 1024
	maxDownPendingBytes      = 16 * 1024 * 1024
	maxClientStreams         = 128
)

type byteQueue struct {
	mu              sync.Mutex
	items           [][]byte
	bytes, maxBytes int
	wake            chan struct{}
	closed          bool
}

func newByteQueue(maxBytes int) *byteQueue {
	return &byteQueue{maxBytes: maxBytes, wake: make(chan struct{}, 1)}
}
func (q *byteQueue) tryPush(p []byte) bool {
	if len(p) == 0 {
		return true
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || len(p) > q.maxBytes || q.bytes+len(p) > q.maxBytes {
		return false
	}
	q.items = append(q.items, append([]byte(nil), p...))
	q.bytes += len(p)
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return true
}
func (q *byteQueue) pop(done <-chan struct{}) ([]byte, bool) {
	for {
		q.mu.Lock()
		if len(q.items) > 0 {
			p := q.items[0]
			q.items[0] = nil
			q.items = q.items[1:]
			q.bytes -= len(p)
			if len(q.items) == 0 {
				q.items = nil
			}
			q.mu.Unlock()
			return p, true
		}
		if q.closed {
			q.mu.Unlock()
			return nil, false
		}
		w := q.wake
		q.mu.Unlock()
		select {
		case <-done:
			return nil, false
		case <-w:
		}
	}
}
func (q *byteQueue) closeAndDrain() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return 0
	}
	q.closed = true
	d := q.bytes
	for i := range q.items {
		q.items[i] = nil
	}
	q.items = nil
	q.bytes = 0
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return d
}
func encodeWindowCredit(n int) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(n))
	return b
}
func decodeWindowCredit(p []byte) (int, error) {
	if len(p) != 4 {
		return 0, fmt.Errorf("invalid WINDOW payload size %d", len(p))
	}
	n := int(binary.BigEndian.Uint32(p))
	if n <= 0 || n > maxStreamCredit {
		return 0, fmt.Errorf("invalid WINDOW credit %d", n)
	}
	return n, nil
}
func (st *localStream) addSendCredit(n int) error {
	if n <= 0 || n > maxStreamCredit {
		return fmt.Errorf("invalid send credit %d", n)
	}
	st.creditMu.Lock()
	defer st.creditMu.Unlock()
	if st.closed.Load() {
		return fmt.Errorf("stream closed")
	}
	if st.sendCredit+n > maxStreamCredit {
		return fmt.Errorf("stream credit overflow: have=%d add=%d", st.sendCredit, n)
	}
	st.sendCredit += n
	select {
	case st.creditWake <- struct{}{}:
	default:
	}
	return nil
}
func (st *localStream) reserveSendCredit(max int, done <-chan struct{}) (int, bool) {
	for {
		st.creditMu.Lock()
		if st.closed.Load() {
			st.creditMu.Unlock()
			return 0, false
		}
		if st.sendCredit > 0 {
			n := st.sendCredit
			if n > max {
				n = max
			}
			st.sendCredit -= n
			st.creditMu.Unlock()
			return n, true
		}
		w := st.creditWake
		st.creditMu.Unlock()
		select {
		case <-done:
			return 0, false
		case <-w:
		}
	}
}
func (st *localStream) refundSendCredit(n int) {
	if n <= 0 {
		return
	}
	st.creditMu.Lock()
	st.sendCredit += n
	if st.sendCredit > maxStreamCredit {
		st.sendCredit = maxStreamCredit
	}
	st.creditMu.Unlock()
	select {
	case st.creditWake <- struct{}{}:
	default:
	}
}
func (st *localStream) grantPeerAllowance(n int) error {
	if n <= 0 || n > maxStreamCredit {
		return fmt.Errorf("invalid peer allowance %d", n)
	}
	st.creditMu.Lock()
	defer st.creditMu.Unlock()
	if st.recvAllowance+n > maxStreamCredit {
		return fmt.Errorf("peer allowance overflow: have=%d add=%d", st.recvAllowance, n)
	}
	st.recvAllowance += n
	return nil
}
func (st *localStream) rollbackPeerAllowance(n int) {
	if n <= 0 {
		return
	}
	st.creditMu.Lock()
	st.recvAllowance -= n
	if st.recvAllowance < 0 {
		panic("negative peer allowance")
	}
	st.creditMu.Unlock()
}
func (st *localStream) consumePeerAllowance(n int) error {
	st.creditMu.Lock()
	defer st.creditMu.Unlock()
	if n > st.recvAllowance {
		return fmt.Errorf("stream flow-control violation: data=%d allowance=%d", n, st.recvAllowance)
	}
	st.recvAllowance -= n
	return nil
}
func (st *localStream) wakeCreditWaiters() {
	select {
	case st.creditWake <- struct{}{}:
	default:
	}
}
