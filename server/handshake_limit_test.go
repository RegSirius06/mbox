package main

import (
	"testing"
	"time"
)

func TestHandshakeLimiterConcurrency(t *testing.T) {
	l := newHandshakeLimiter(2, 100)
	now := time.Unix(10, 0)
	if !l.acquire(now) || !l.acquire(now) {
		t.Fatal("initial slots rejected")
	}
	if l.acquire(now) {
		t.Fatal("concurrency limit ignored")
	}
	l.release()
	if !l.acquire(now) {
		t.Fatal("released slot not reusable")
	}
	l.release()
	l.release()
}

func TestHandshakeLimiterRate(t *testing.T) {
	l := newHandshakeLimiter(4, 2)
	now := time.Unix(20, 0)
	for i := 0; i < 2; i++ {
		if !l.acquire(now) {
			t.Fatalf("attempt %d unexpectedly rejected", i+1)
		}
		l.release()
	}
	if l.acquire(now) {
		t.Fatal("rate limit ignored")
	}
	if !l.acquire(now.Add(time.Second)) {
		t.Fatal("rate window did not reset")
	}
	l.release()
}

func TestHandshakeLimiterFloodIsBounded(t *testing.T) {
	l := newHandshakeLimiter(8, 16)
	now := time.Unix(30, 0)
	accepted := 0
	for i := 0; i < 10000; i++ {
		if l.acquire(now) {
			accepted++
			l.release()
		}
	}
	if accepted != 16 {
		t.Fatalf("accepted=%d want=16", accepted)
	}
}
