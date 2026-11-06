package main

import (
	"bufio"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	flowctl "../common/flowctl"
	"../common/udpwire"
)

const (
	clientUDPReadyTimeout = 10 * time.Second
	maxClientUDPFlows     = 16
)

type localUDPFlow struct {
	conn      net.Conn
	writeQ    *flowctl.ByteQueue
	ready     chan struct{}
	readyOnce sync.Once
	readyOK   atomic.Bool
	closed    atomic.Bool
}

func (f *localUDPFlow) signalReady(ok bool) {
	if ok {
		f.readyOK.Store(true)
	}
	f.readyOnce.Do(func() { close(f.ready) })
}

func (t *tunnel) closeUDPFlow(id uint32, notify bool) {
	t.streamsMu.Lock()
	f := t.udpFlows[id]
	if f != nil {
		delete(t.udpFlows, id)
	}
	t.streamsMu.Unlock()
	if f == nil {
		return
	}
	f.closed.Store(true)
	f.signalReady(false)
	f.writeQ.CloseAndDrain()
	_ = f.conn.Close()
	if notify {
		_ = t.queue(id, frameClose, nil)
	}
}

func (t *tunnel) localUDPWriter(id uint32, f *localUDPFlow) {
	defer t.streamWG.Done()
	for {
		payload, ok := f.writeQ.Pop(t.done)
		if !ok {
			return
		}
		_ = f.conn.SetWriteDeadline(time.Now().Add(localWriteTimeout))
		for len(payload) > 0 {
			n, err := f.conn.Write(payload)
			if err != nil || n == 0 {
				t.closeUDPFlow(id, true)
				return
			}
			payload = payload[n:]
		}
	}
}

// Reuse the already authenticated SOCKS greeting for HEV FWD UDP, but never
// forward CMD 0x05 to the remote TCP SOCKS proxy. The remote TCP stream is
// closed and an independent Noise-authenticated datagram flow is opened.
func (t *tunnel) handoffToUDP(oldID uint32, st *localStream, conn net.Conn) {
	t.streamsMu.Lock()
	if t.streams[oldID] != st {
		t.streamsMu.Unlock()
		_ = conn.Close()
		return
	}
	delete(t.streams, oldID)
	if len(t.udpFlows) >= maxClientUDPFlows {
		t.streamsMu.Unlock()
		_ = conn.Close()
		_ = t.queue(oldID, frameClose, nil)
		return
	}
	id := t.nextID.Add(1)
	if id == 0 {
		t.streamsMu.Unlock()
		_ = conn.Close()
		_ = t.queue(oldID, frameClose, nil)
		return
	}
	f := &localUDPFlow{conn: conn, ready: make(chan struct{}), writeQ: flowctl.NewByteQueue(streamWriteQueueMaxBytes)}
	if t.udpFlows == nil {
		t.udpFlows = make(map[uint32]*localUDPFlow)
	}
	t.udpFlows[id] = f
	t.streamsMu.Unlock()
	st.closed.Store(true)
	st.writeQ.CloseAndDrain()
	st.wakeCreditWaiters()
	if err := t.queue(oldID, frameClose, nil); err != nil {
		t.closeUDPFlow(id, false)
		return
	}
	// The TCP stream and HEV UDP association share the local socket. A
	// previously popped SOCKS greeting response can still be in flight;
	// never interleave it with the HEV success reply or datagrams.
	if st.writerDone != nil {
		timer := time.NewTimer(clientUDPReadyTimeout)
		defer timer.Stop()
		select {
		case <-st.writerDone:
		case <-t.done:
			t.closeUDPFlow(id, false)
			return
		case <-timer.C:
			t.closeUDPFlow(id, false)
			return
		}
	}
	t.streamWG.Add(1)
	go t.localUDPWriter(id, f)
	if err := t.queue(id, frameUDPOpen, nil); err != nil {
		t.closeUDPFlow(id, false)
		return
	}
	timer := time.NewTimer(clientUDPReadyTimeout)
	defer timer.Stop()
	select {
	case <-f.ready:
	case <-t.done:
		t.closeUDPFlow(id, false)
		return
	case <-timer.C:
		t.closeUDPFlow(id, true)
		return
	}
	if !f.readyOK.Load() || f.closed.Load() {
		t.closeUDPFlow(id, false)
		return
	}
	// HEV expects an ordinary SOCKS5 success reply, then its private
	// bounded DATLEN/HDRLEN datagram stream follows over the *same* TCP fd.
	_ = conn.SetWriteDeadline(time.Now().Add(localWriteTimeout))
	err := writeHEVSuccess(conn)
	if err != nil {
		t.closeUDPFlow(id, true)
		return
	}
	reader := bufio.NewReaderSize(conn, 4096)
	for {
		select {
		case <-t.done:
			t.closeUDPFlow(id, false)
			return
		default:
		}
		if f.closed.Load() {
			return
		}
		// A deadline while waiting for the *first* byte is harmless idle.
		// Once a datagram begins, timeout must terminate the flow rather
		// than trying to resync on attacker-controlled TCP bytes.
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		if _, err := reader.Peek(1); err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			t.closeUDPFlow(id, true)
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		peer, data, err := udpwire.ReadHEV(reader)
		if err != nil {
			t.closeUDPFlow(id, true)
			return
		}
		wire, err := udpwire.Marshal(udpwire.Datagram{FlowID: id, Peer: peer, Data: data})
		if err != nil {
			t.closeUDPFlow(id, true)
			return
		}
		if err = t.queue(id, frameUDPData, wire); err != nil {
			t.closeUDPFlow(id, false)
			return
		}
	}
}

func writeHEVSuccess(conn net.Conn) error {
	remaining := []byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}
	for len(remaining) > 0 {
		n, err := conn.Write(remaining)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		remaining = remaining[n:]
	}
	return nil
}
