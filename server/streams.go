package main

import (
	flowctl "../common/flowctl"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"time"
)

// UDP replies are emitted only while the producing association still owns its
// ID. The ownership check shares the same session lock as frame enqueueing.
func appendFrame(s *session, streamID uint32, typ byte, payload []byte, expected ...*udpFlow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return appendFrameLocked(s, streamID, typ, payload, expected...)
}

// appendFrameLocked requires s.mu. Close paths use it to enqueue CLOSE before
// retiring a flow ID, preventing a stale worker from notifying a new owner.
func appendFrameLocked(s *session, streamID uint32, typ byte, payload []byte, expected ...*udpFlow) error {
	if len(expected) > 0 && (len(expected) != 1 || expected[0] == nil || s.udpFlows[streamID] != expected[0] || expected[0].closed.Load()) {
		return fmt.Errorf("stale UDP flow")
	}
	if len(payload) > maxFramePayload {
		return fmt.Errorf("frame payload too large: %d", len(payload))
	}
	required := 9 + len(payload)
	if s.closed {
		return fmt.Errorf("session closed")
	}
	if s.downBytes+required > runtimeLimits.MaxDownQueueBytes {
		return fmt.Errorf("down queue overflow")
	}
	if !reserveGlobalQueued(required) {
		return fmt.Errorf("global queue budget exhausted")
	}
	f := make([]byte, required)
	binary.BigEndian.PutUint32(f[0:4], streamID)
	f[4] = typ
	binary.BigEndian.PutUint32(f[5:9], uint32(len(payload)))
	copy(f[9:], payload)
	s.downFrames = append(s.downFrames, f)
	s.downBytes += required
	select {
	case s.downReady <- struct{}{}:
	default:
	}
	return nil
}

func reserveTotalStream() bool {
	for {
		old := totalStreams.Load()
		if old >= int64(runtimeLimits.MaxTotalStreams) {
			return false
		}
		if totalStreams.CompareAndSwap(old, old+1) {
			return true
		}
	}
}

func allowStreamOpenLocked(s *session) error {
	now := time.Now()
	if s.openWindowStart.IsZero() || now.Sub(s.openWindowStart) >= time.Second {
		s.openWindowStart = now
		s.openWindowCount = 0
	}
	if s.openWindowCount >= runtimeLimits.MaxStreamOpensPerSecond {
		return fmt.Errorf("stream open rate exceeded")
	}
	if len(s.streams)+len(s.udpFlows) >= runtimeLimits.MaxStreamsPerSession {
		return fmt.Errorf("streams/session quota exceeded")
	}
	s.openWindowCount++
	return nil
}

func grantPeerCredit(s *session, id uint32, st *stream, n int) error {
	if err := st.grantPeerAllowance(n); err != nil {
		return err
	}
	if err := appendFrame(s, id, frameWindow, flowctl.EncodeWindowCredit(n)); err != nil {
		st.rollbackPeerAllowance(n)
		return err
	}
	return nil
}

func closeStream(s *session, id uint32, notify bool) {
	s.mu.Lock()
	st, ok := s.streams[id]
	if ok {
		delete(s.streams, id)
	}
	s.mu.Unlock()
	if !ok {
		return
	}
	totalStreams.Add(-1)
	if d := st.writeQ.CloseAndDrain(); d > 0 {
		releaseGlobalQueued(d)
	}
	st.wakeCreditWaiters()
	if st.closed.CompareAndSwap(false, true) {
		_ = st.conn.Close()
	}
	if notify {
		_ = appendFrame(s, id, frameClose, nil)
	}
}

func streamWriter(s *session, id uint32, st *stream) {
	for {
		payload, ok := st.writeQ.Pop(s.done)
		if !ok {
			return
		}
		_ = st.conn.SetWriteDeadline(time.Now().Add(upstreamWriteTimeout))
		written := 0
		var err error
		for written < len(payload) {
			var n int
			n, err = st.conn.Write(payload[written:])
			written += n
			if err != nil || n == 0 {
				if err == nil {
					err = io.ErrUnexpectedEOF
				}
				break
			}
		}
		releaseGlobalQueued(len(payload))
		if err != nil {
			if !st.closed.Load() {
				log.Printf("TCP WRITE stream=%d: %v", id, err)
			}
			closeStream(s, id, true)
			return
		}
		if err := grantPeerCredit(s, id, st, len(payload)); err != nil {
			log.Printf("WINDOW grant stream=%d: %v", id, err)
			closeStream(s, id, false)
			return
		}
	}
}

func streamReader(s *session, id uint32, st *stream) {
	buf := make([]byte, maxFramePayload)
	for {
		grant, ok := st.reserveSendCredit(len(buf), s.done)
		if !ok {
			return
		}
		n, err := st.conn.Read(buf[:grant])
		if n < grant {
			st.refundSendCredit(grant - n)
		}
		if n > 0 {
			payload := append([]byte(nil), buf[:n]...)
			if e := appendFrame(s, id, frameData, payload); e != nil {
				log.Printf("DOWN QUEUE stream=%d: %v", id, e)
				closeStream(s, id, false)
				return
			}
		}
		if err != nil {
			if err != io.EOF && !st.closed.Load() {
				log.Printf("TCP READ stream=%d: %v", id, err)
			}
			closeStream(s, id, true)
			return
		}
	}
}

func handleFrame(s *session, streamID uint32, typ byte, payload []byte) error {
	switch typ {
	case frameOpen:
		if len(payload) != 0 {
			return fmt.Errorf("OPEN payload must be empty")
		}
		s.mu.Lock()
		_, exists := s.streams[streamID]
		if _, udpExists := s.udpFlows[streamID]; udpExists {
			s.mu.Unlock()
			return fmt.Errorf("TCP/UDP ID collision")
		}
		if exists {
			s.mu.Unlock()
			return nil
		}
		if err := allowStreamOpenLocked(s); err != nil {
			s.mu.Unlock()
			_ = appendFrame(s, streamID, frameError, []byte(err.Error()))
			return nil
		}
		s.mu.Unlock()
		if !reserveTotalStream() {
			_ = appendFrame(s, streamID, frameError, []byte("global stream quota exceeded"))
			return nil
		}
		conn, err := net.DialTimeout("tcp", socksAddr, 5*time.Second)
		if err != nil {
			totalStreams.Add(-1)
			_ = appendFrame(s, streamID, frameError, []byte("connect upstream failed"))
			return nil
		}
		st := &stream{conn: conn, writeQ: flowctl.NewByteQueue(streamWriteQueueMaxBytes), creditWake: make(chan struct{}, 1)}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			totalStreams.Add(-1)
			_ = conn.Close()
			return nil
		}
		if _, dup := s.streams[streamID]; dup {
			s.mu.Unlock()
			totalStreams.Add(-1)
			_ = conn.Close()
			return nil
		}
		s.streams[streamID] = st
		s.mu.Unlock()
		go streamWriter(s, streamID, st)
		go streamReader(s, streamID, st)
		if err := grantPeerCredit(s, streamID, st, streamInitialWindow); err != nil {
			closeStream(s, streamID, false)
			return err
		}
		return nil
	case frameData:
		if len(payload) > maxFramePayload {
			return fmt.Errorf("DATA frame too large: %d", len(payload))
		}
		s.mu.Lock()
		st := s.streams[streamID]
		s.mu.Unlock()
		if st == nil || st.closed.Load() {
			return nil
		}
		if err := st.consumePeerAllowance(len(payload)); err != nil {
			return err
		}
		if !reserveGlobalQueued(len(payload)) {
			return fmt.Errorf("global queue budget exhausted")
		}
		if !st.writeQ.TryPush(payload) {
			releaseGlobalQueued(len(payload))
			return fmt.Errorf("stream write queue overflow")
		}
		return nil
	case frameWindow:
		credit, err := flowctl.DecodeWindowCredit(payload)
		if err != nil {
			return err
		}
		s.mu.Lock()
		st := s.streams[streamID]
		s.mu.Unlock()
		if st == nil || st.closed.Load() {
			return nil
		}
		return st.addSendCredit(credit)
	case frameUDPOpen:
		if len(payload) != 0 {
			return fmt.Errorf("UDP OPEN payload must be empty")
		}
		if err := newUDPFlow(s, streamID); err != nil {
			_ = appendFrame(s, streamID, frameError, []byte("UDP open rejected"))
		}
		return nil
	case frameUDPData:
		if !enableUDPLabServer {
			return fmt.Errorf("UDP disabled")
		}
		return onUDPData(s, streamID, payload)
	case frameClose:
		if len(payload) != 0 {
			return fmt.Errorf("CLOSE payload must be empty")
		}
		closeStream(s, streamID, false)
		closeUDPFlow(s, streamID, nil, false)
		return nil
	case framePing:
		return appendFrame(s, streamID, framePing, payload)
	default:
		return fmt.Errorf("unknown frame type %d", typ)
	}
}

func walkFrames(data []byte, fn func(streamID uint32, typ byte, payload []byte) error) error {
	// Parser invariant: lengths are validated before slicing and a frame can
	// never request allocation based solely on an untrusted length field.
	for len(data) > 0 {
		if len(data) < 9 {
			return fmt.Errorf("short frame header")
		}

		streamID := binary.BigEndian.Uint32(data[0:4])
		typ := data[4]
		n := binary.BigEndian.Uint32(data[5:9])
		data = data[9:]

		if n > maxFramePayload {
			return fmt.Errorf("frame payload too large: %d", n)
		}
		if uint64(n) > uint64(len(data)) {
			return fmt.Errorf("short frame payload")
		}
		payload := data[:n]
		data = data[n:]
		if err := fn(streamID, typ, payload); err != nil {
			return err
		}
	}
	return nil
}

func parseFrames(s *session, data []byte) error {
	return walkFrames(data, func(streamID uint32, typ byte, payload []byte) error {
		return handleFrame(s, streamID, typ, payload)
	})
}
