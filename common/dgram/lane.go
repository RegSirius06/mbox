package dgram

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const QueuePackets = 32
const PerFlowPackets = 4
const MaxReceivePerSecond = 2048

type queuedPacket struct {
	id   uint32
	data []byte
}

const SetupTimeout = 8 * time.Second
const WriteTimeout = time.Second

// Lane owns one local connection and two directional crypto contexts. Offer
// returning true transfers ownership even on a drop; never retry that packet
// through reliable streams. False means the caller can use the old path.
type Lane struct {
	conn      net.Conn
	tx, rx    *Record
	q         chan queuedPacket
	mu        sync.Mutex
	perFlow   map[uint32]int
	done      chan struct{}
	closeOnce sync.Once
}

func NewLane(c net.Conn, tx, rx *Record) *Lane {
	return &Lane{conn: c, tx: tx, rx: rx, q: make(chan queuedPacket, QueuePackets), perFlow: make(map[uint32]int), done: make(chan struct{})}
}

func (l *Lane) Close() { l.closeOnce.Do(func() { close(l.done); _ = l.conn.Close() }) }

func (l *Lane) Offer(id uint32, p []byte) bool {
	if l == nil || id == 0 || len(p) == 0 || len(p) > MaxPlain {
		return false
	}
	select {
	case <-l.done:
		return false
	default:
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	select {
	case <-l.done:
		return false
	default:
	}
	if l.perFlow[id] >= PerFlowPackets {
		return true
	}
	// Call sites transfer an immutable, newly marshaled UDP frame.
	select {
	case l.q <- queuedPacket{id, p}:
		l.perFlow[id]++
	default:
	} // congestion drops rather than blocking TCP/control
	return true
}

// Run returns only after its writer and cancellation watcher have stopped.
func (l *Lane) Run(parent <-chan struct{}, deliver func([]byte)) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		select {
		case <-parent:
			l.Close()
		case <-l.done:
		}
	}()
	go func() {
		defer wg.Done()
		defer l.Close()
		for {
			select {
			case <-l.done:
				return
			case p := <-l.q:
				l.mu.Lock()
				l.perFlow[p.id]--
				if l.perFlow[p.id] == 0 {
					delete(l.perFlow, p.id)
				}
				l.mu.Unlock()
				packet, err := l.tx.Seal(p.data)
				if err != nil {
					return
				}
				_ = l.conn.SetWriteDeadline(time.Now().Add(WriteTimeout))
				if WritePacket(l.conn, packet) != nil {
					return
				}
			}
		}
	}()
	defer func() { l.Close(); wg.Wait() }()
	window := time.Now()
	received := 0
	for {
		packet, err := ReadPacket(l.conn)
		if err != nil {
			return
		}
		now := time.Now()
		if now.Sub(window) >= time.Second {
			window = now
			received = 0
		}
		if received >= MaxReceivePerSecond {
			continue
		}
		received++
		plain, err := l.rx.Open(packet)
		if err != nil {
			continue
		}
		select {
		case <-l.done:
			return
		default:
			deliver(plain)
		}
	}
}

// Local sockets are opt-in and restricted to an issuer-owned, non-writable-by-
// others directory. Never replace an existing path (including stale sockets).
func ListenPrivate(path string) (*net.UnixListener, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("invalid datagram Unix path")
	}
	parent, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	stat, ok := parent.Sys().(*syscall.Stat_t)
	if !parent.IsDir() || !ok || int(stat.Uid) != os.Getuid() || parent.Mode().Perm()&0022 != 0 {
		return nil, errors.New("datagram Unix parent must be owner-controlled")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return nil, errors.New("datagram Unix path already exists or is inaccessible")
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	ln.SetUnlinkOnClose(true)
	if err = os.Chmod(path, 0600); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}
