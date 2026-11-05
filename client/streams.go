package main

import (
	flowctl "../common/flowctl"
	"../common/udpwire"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"time"
)

func frame(streamID uint32, typ byte, payload []byte) []byte {
	b := make([]byte, 9+len(payload))

	binary.BigEndian.PutUint32(b[0:4], streamID)
	b[4] = typ
	binary.BigEndian.PutUint32(b[5:9], uint32(len(payload)))

	copy(b[9:], payload)
	return b
}

func (t *tunnel) grantPeerCredit(id uint32, st *localStream, n int) error {
	if err := st.grantPeerAllowance(n); err != nil {
		return err
	}
	if err := t.queue(id, frameWindow, flowctl.EncodeWindowCredit(n)); err != nil {
		st.rollbackPeerAllowance(n)
		return err
	}
	return nil
}

func (t *tunnel) startLocalWriter(id uint32, st *localStream) {
	t.streamWG.Add(1)
	go func() {
		defer t.streamWG.Done()
		t.localWriter(id, st)
	}()
}

func (t *tunnel) localWriter(id uint32, st *localStream) {
	for {
		payload, ok := st.writeQ.Pop(t.done)
		if !ok {
			return
		}
		_ = st.conn.SetWriteDeadline(time.Now().Add(localWriteTimeout))
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
		if err != nil {
			t.closeLocal(id, true)
			return
		}
		if err := t.grantPeerCredit(id, st, len(payload)); err != nil {
			t.closeLocal(id, false)
			return
		}
	}
}

func (t *tunnel) handleFrames(data []byte) error {
	for len(data) > 0 {
		if len(data) < 9 {
			return fmt.Errorf("short header")
		}
		id := binary.BigEndian.Uint32(data[:4])
		typ := data[4]
		n := binary.BigEndian.Uint32(data[5:9])
		data = data[9:]
		if uint64(n) > uint64(len(data)) {
			return fmt.Errorf("short payload")
		}
		if n > maxFramePayload {
			return fmt.Errorf("frame payload too large: %d", n)
		}
		payload := data[:n]
		data = data[n:]
		t.streamsMu.Lock()
		st := t.streams[id]
		uf := t.udpFlows[id]
		t.streamsMu.Unlock()
		switch typ {
		case frameUDPReady:
			if len(payload) != 0 {
				return fmt.Errorf("UDP READY must be empty")
			}
			if uf != nil && !uf.closed.Load() {
				uf.signalReady(true)
			}
		case frameUDPData:
			d, err := udpwire.Unmarshal(payload)
			if err != nil || d.FlowID != id {
				return fmt.Errorf("bad UDP return frame")
			}
			if uf == nil || uf.closed.Load() {
				continue
			}
			if !uf.readyOK.Load() {
				return fmt.Errorf("UDP response before ready")
			}
			wire, err := udpwire.MarshalHEV(d.Peer, d.Data)
			if err != nil {
				return err
			}
			if !uf.writeQ.tryPush(wire) {
				t.closeUDPFlow(id, true)
				return fmt.Errorf("UDP client write queue overflow")
			}
		case frameData:
			if st == nil || st.closed.Load() {
				continue
			}
			if err := st.consumePeerAllowance(len(payload)); err != nil {
				return err
			}
			if !st.writeQ.TryPush(payload) {
				return fmt.Errorf("local stream write queue overflow")
			}
		case frameWindow:
			if st == nil || st.closed.Load() {
				continue
			}
			credit, err := flowctl.DecodeWindowCredit(payload)
			if err != nil {
				return err
			}
			if err := st.addSendCredit(credit); err != nil {
				return err
			}
		case frameClose:
			if len(payload) != 0 {
				return fmt.Errorf("CLOSE payload must be empty")
			}
			t.closeLocal(id, false)
			t.closeUDPFlow(id, false)
		case frameError:
			log.Printf("STREAM %d server error: %s", id, string(payload))
			t.closeLocal(id, false)
			t.closeUDPFlow(id, false)
		case framePing:
		case frameOpen, frameUDPOpen:
			return fmt.Errorf("server-initiated OPEN is not supported")
		default:
			return fmt.Errorf("unknown frame type %d", typ)
		}
	}
	return nil
}

func (t *tunnel) closeLocal(id uint32, notify bool) {
	t.streamsMu.Lock()
	st := t.streams[id]
	if st != nil {
		delete(t.streams, id)
	}
	t.streamsMu.Unlock()
	if st == nil {
		return
	}
	st.writeQ.CloseAndDrain()
	st.wakeCreditWaiters()
	if st.closed.CompareAndSwap(false, true) {
		_ = st.conn.Close()
	}
	if notify {
		_ = t.queue(id, frameClose, nil)
	}
}

func (t *tunnel) handleLocal(conn net.Conn) {
	t.streamsMu.Lock()
	if len(t.streams) >= maxClientStreams {
		t.streamsMu.Unlock()
		_ = conn.Close()
		return
	}
	id := t.nextID.Add(1)
	st := &localStream{conn: conn, writeQ: flowctl.NewByteQueue(streamWriteQueueMaxBytes), creditWake: make(chan struct{}, 1)}
	t.streams[id] = st
	t.streamsMu.Unlock()
	t.startLocalWriter(id, st)
	if err := t.queue(id, frameOpen, nil); err != nil {
		t.closeLocal(id, false)
		return
	}
	if err := t.grantPeerCredit(id, st, streamInitialWindow); err != nil {
		t.closeLocal(id, true)
		return
	}
	// Fail-closed local SOCKS policy: native MBox carries only CONNECT.
	// HEV's "udp: tcp" is a UDP-over-TCP extension, not a UDP-disable
	// switch; never let an upstream SOCKS server interpret UDP/BIND here.
	if err := guardSOCKSConnectWithUDP(conn, func(p []byte) error {
		return t.forwardLocalData(id, st, p)
	}, enableUDPLabClient); err != nil {
		if errors.Is(err, errHEVForwardUDPRequested) {
			t.handoffToUDP(id, st, conn)
			return
		}
		t.closeLocal(id, true)
		return
	}
	buf := make([]byte, maxFramePayload)
	for {
		grant, ok := st.reserveSendCredit(len(buf), t.done)
		if !ok {
			return
		}
		n, err := conn.Read(buf[:grant])
		if n < grant {
			st.refundSendCredit(grant - n)
		}
		if n > 0 {
			payload := append([]byte(nil), buf[:n]...)
			if qerr := t.queue(id, frameData, payload); qerr != nil {
				t.closeLocal(id, false)
				return
			}
		}
		if err != nil {
			t.closeLocal(id, true)
			return
		}
	}
}
