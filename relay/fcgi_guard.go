package main

import (
	"log"
	"net"
	"net/http"
	"net/http/fcgi"
	"os"
	"sync/atomic"
	"time"
)

const (
	// Longer than the existing 20s origin request budget. Apache owns the
	// front connection, so these are limits on its FastCGI socket, not on
	// the user's physical network. Do not change the origin wire protocol.
	fcgiReadIdle  = 40 * time.Second
	fcgiWriteIdle = 5 * time.Second
	fcgiHardLimit = 45 * time.Second
)

// net/http/fcgi does not set socket deadlines and cannot expose its
// underlying connection via http.ResponseController. Keep the standard
// protocol implementation and enforce deadlines at the net.Conn boundary.
type fcgiDeadlineListener struct {
	net.Listener
	readIdle, writeIdle time.Duration
}

func (l fcgiDeadlineListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &fcgiDeadlineConn{Conn: c, readIdle: l.readIdle, writeIdle: l.writeIdle}, nil
}

type fcgiDeadlineConn struct {
	net.Conn
	readIdle, writeIdle time.Duration
}

func (c *fcgiDeadlineConn) Read(p []byte) (int, error) {
	if err := c.Conn.SetReadDeadline(time.Now().Add(c.readIdle)); err != nil {
		_ = c.Conn.Close()
		return 0, err
	}
	n, err := c.Conn.Read(p)
	if err != nil {
		c.closeOnError("read", err)
	}
	return n, err
}

func (c *fcgiDeadlineConn) Write(p []byte) (int, error) {
	if err := c.Conn.SetWriteDeadline(time.Now().Add(c.writeIdle)); err != nil {
		_ = c.Conn.Close()
		return 0, err
	}
	n, err := c.Conn.Write(p)
	if err != nil {
		c.closeOnError("write", err)
	}
	return n, err
}

func (c *fcgiDeadlineConn) closeOnError(direction string, err error) {
	// Closing the socket unblocks its other direction, even when a
	// stdlib FastCGI writer holds its own record serialization mutex.
	_ = c.Conn.Close()
	if e, ok := err.(net.Error); ok && e.Timeout() {
		log.Printf("FCGI_IO_TIMEOUT direction=%s", direction)
	}
}

// A per-request watchdog bounds the complete application handler, including
// body parsing, slot admission and upstream work. No periodic polling or
// idle worker recycling. A wedged handler cannot safely release its slot
// while still executing. Terminate this worker instead; the OS releases
// its flocks and the hosting process manager can replace it.
func fcgiWatchdog(next http.Handler, limit time.Duration, expired func()) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var finished atomic.Bool
		timer := time.AfterFunc(limit, func() {
			if finished.CompareAndSwap(false, true) {
				expired()
			}
		})
		defer func() {
			finished.Store(true)
			timer.Stop()
		}()
		next.ServeHTTP(w, r)
	})
}

func serveGuardedFastCGI(handler http.Handler) error {
	// Same inherited listener as fcgi.Serve(nil, handler).
	listener, err := net.FileListener(os.Stdin)
	if err != nil {
		return err
	}
	defer listener.Close()
	return fcgi.Serve(fcgiDeadlineListener{listener, fcgiReadIdle, fcgiWriteIdle}, handler)
}

var lastSlotBusyLog atomic.Int64

func logSlotBusy() {
	now := time.Now().Unix()
	last := lastSlotBusyLog.Load()
	if now-last >= 30 && lastSlotBusyLog.CompareAndSwap(last, now) {
		log.Print("FCGI_SLOT_BUSY")
	}
}
