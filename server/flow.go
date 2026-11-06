package main

import (
	flowctl "../common/flowctl"
	"fmt"
	"time"
)

const (
	maxFramePayload          = 64 * 1024
	streamInitialWindow      = flowctl.MaxCredit
	streamWriteQueueMaxBytes = streamInitialWindow
	upstreamWriteTimeout     = 15 * time.Second
	maxStreamCredit          = streamInitialWindow
)

func reserveGlobalQueued(n int) bool {
	if n <= 0 {
		return true
	}
	for {
		old := globalQueuedBytes.Load()
		if old+int64(n) > int64(runtimeLimits.GlobalQueuedBytes) {
			return false
		}
		if globalQueuedBytes.CompareAndSwap(old, old+int64(n)) {
			return true
		}
	}
}
func releaseGlobalQueued(n int) {
	if n <= 0 {
		return
	}
	v := globalQueuedBytes.Add(-int64(n))
	if v < 0 {
		panic("negative global queued byte count")
	}
}
func (st *stream) addSendCredit(n int) error {
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
func (st *stream) reserveSendCredit(max int, done <-chan struct{}) (int, bool) {
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
		wake := st.creditWake
		st.creditMu.Unlock()
		select {
		case <-done:
			return 0, false
		case <-wake:
		}
	}
}
func (st *stream) refundSendCredit(n int) {
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
func (st *stream) grantPeerAllowance(n int) error {
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
func (st *stream) consumePeerAllowance(n int) error {
	if n < 0 {
		return fmt.Errorf("negative DATA size")
	}
	st.creditMu.Lock()
	defer st.creditMu.Unlock()
	if n > st.recvAllowance {
		return fmt.Errorf("stream flow-control violation: data=%d allowance=%d", n, st.recvAllowance)
	}
	st.recvAllowance -= n
	return nil
}
func (st *stream) rollbackPeerAllowance(n int) {
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
func (st *stream) wakeCreditWaiters() {
	select {
	case st.creditWake <- struct{}{}:
	default:
	}
}
