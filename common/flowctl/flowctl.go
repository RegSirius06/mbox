// Package flowctl contains the shared bounded byte queue and WINDOW wire codec.
// The client and origin retain their own session and credit accounting.
package flowctl

import (
	"encoding/binary"
	"fmt"
	"sync"
)

const MaxCredit = 256 * 1024

type ByteQueue struct {
	mu       sync.Mutex
	items    [][]byte
	bytes    int
	maxBytes int
	wake     chan struct{}
	closed   bool
}

func NewByteQueue(maxBytes int) *ByteQueue {
	return &ByteQueue{maxBytes: maxBytes, wake: make(chan struct{}, 1)}
}

func (q *ByteQueue) TryPush(p []byte) bool {
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

func (q *ByteQueue) Pop(done <-chan struct{}) ([]byte, bool) {
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
		wake := q.wake
		q.mu.Unlock()
		select {
		case <-done:
			return nil, false
		case <-wake:
		}
	}
}

// CloseAndDrain returns the number of bytes still held by the queue.
func (q *ByteQueue) CloseAndDrain() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return 0
	}
	q.closed = true
	n := q.bytes
	for i := range q.items {
		q.items[i] = nil
	}
	q.items = nil
	q.bytes = 0
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return n
}

func EncodeWindowCredit(n int) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(n))
	return b
}

func DecodeWindowCredit(p []byte) (int, error) {
	if len(p) != 4 {
		return 0, fmt.Errorf("invalid WINDOW payload size %d", len(p))
	}
	n := int(binary.BigEndian.Uint32(p))
	if n <= 0 || n > MaxCredit {
		return 0, fmt.Errorf("invalid WINDOW credit %d", n)
	}
	return n, nil
}
