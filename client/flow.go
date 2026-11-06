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
	localWriteTimeout        = 15 * time.Second
	maxStreamCredit          = streamInitialWindow
	maxUpQueueBytes          = 8 * 1024 * 1024
	maxUpPerStreamQueueBytes = 1024 * 1024
	maxDownPendingBytes      = 16 * 1024 * 1024
	maxClientStreams         = 128
)

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
