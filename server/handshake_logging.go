package main

import (
	"log"
	"time"
)

func logHandshakeLimit(now time.Time) {
	sec := now.Unix()
	for {
		prev := lastHandshakeLimitLog.Load()
		if prev >= sec {
			return
		}
		if lastHandshakeLimitLog.CompareAndSwap(prev, sec) {
			log.Printf("OPEN LIMITED code=ERR_HANDSHAKE_LIMIT")
			return
		}
	}
}

func logAuthFailure(now time.Time) {
	sec := now.Unix()
	for {
		prev := lastAuthFailureLog.Load()
		if prev >= sec {
			return
		}
		if lastAuthFailureLog.CompareAndSwap(prev, sec) {
			log.Printf("OPEN AUTH FAILED")
			return
		}
	}
}
