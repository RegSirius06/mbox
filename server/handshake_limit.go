package main

import (
	"sync"
	"time"
)

type handshakeLimiter struct {
	sem       chan struct{}
	mu        sync.Mutex
	window    time.Time
	attempts  int
	maxPerSec int
}

func newHandshakeLimiter(maxConcurrent, maxPerSec int) *handshakeLimiter {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	if maxPerSec < 1 {
		maxPerSec = 1
	}
	return &handshakeLimiter{sem: make(chan struct{}, maxConcurrent), maxPerSec: maxPerSec}
}

func (l *handshakeLimiter) acquire(now time.Time) bool {
	select {
	case l.sem <- struct{}{}:
	default:
		return false
	}

	l.mu.Lock()
	if l.window.IsZero() || now.Sub(l.window) >= time.Second {
		l.window = now
		l.attempts = 0
	}
	if l.attempts >= l.maxPerSec {
		l.mu.Unlock()
		<-l.sem
		return false
	}
	l.attempts++
	l.mu.Unlock()
	return true
}

func (l *handshakeLimiter) release() {
	select {
	case <-l.sem:
	default:
	}
}
