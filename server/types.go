package main

import (
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// Resource limits are implementation safeguards, not deployment settings.
	maxRequestBody = 2 * 1024 * 1024
	maxDownQueue   = 8 * 1024 * 1024

	frameOpen   = 1
	frameData   = 2
	frameClose  = 3
	frameError  = 4
	framePing   = 5
	frameWindow = 6

	maxDownReplayRequests = 1024
)

var (
	listen    string
	socksAddr string
)

type stream struct {
	conn          net.Conn
	writeQ        *byteQueue
	closed        atomic.Bool
	creditMu      sync.Mutex
	sendCredit    int
	recvAllowance int
	creditWake    chan struct{}
}

type cachedUpResponse struct {
	recordHash [32]byte
	downSeq    uint64
	status     int
}

type cachedDownResponse struct {
	counter uint64
	status  int
	body    []byte
	// reservedBytes is the exact global queue-budget reservation owned by
	// this replay entry. Security invariant: every successful reservation has
	// exactly one owner and is released exactly once, independent of body size.
	reservedBytes int
}

type session struct {
	mu              sync.Mutex
	upProcessMu     sync.Mutex
	downCommitMu    sync.Mutex
	upCache         map[uint64]cachedUpResponse
	upCacheOrder    []uint64
	clientID        string // opaque wire auth handle (legacy clients may still send account ID)
	accountID       string // backend-only account grouping identity
	deviceID        string
	streams         map[uint32]*stream
	crypto          *cryptoState
	downFrames      [][]byte
	downBytes       int
	downReady       chan struct{}
	downReplay      map[uint64]cachedDownResponse
	downReplayBytes int
	downReqSeen     map[uint64]struct{}
	downReqMax      uint64
	lastDownAck     uint64
	lastDownCounter uint64
	openWindowStart time.Time
	openWindowCount int
	done            chan struct{}
	closeOnce       sync.Once
	lastSeen        time.Time
	closed          bool
}

type rateWindow struct {
	start time.Time
	count int
}

var (
	sessionsMu            sync.Mutex
	sessions              = make(map[string]*session)
	sessionOpenRates      = make(map[string]rateWindow)
	totalStreams          atomic.Int64
	globalQueuedBytes     atomic.Int64
	runtimeStatusMu       sync.Mutex
	preAuthLimiter        *handshakeLimiter
	lastHandshakeLimitLog atomic.Int64
	lastAuthFailureLog    atomic.Int64
	serverDraining        atomic.Bool
	serverReady           atomic.Bool
)

const defaultRuntimeStatusPath = "/run/mbox/status.json"

// Production preserves its historic status path. Staging instances override it
// explicitly, so two server processes never write the same status file.
var runtimeStatusPath = defaultRuntimeStatusPath

type runtimeStatusSnapshot struct {
	GeneratedAt    time.Time      `json:"generated_at"`
	ActiveSessions int            `json:"active_sessions"`
	ActiveStreams  int64          `json:"active_streams"`
	QueuedBytes    int64          `json:"queued_bytes"`
	Clients        map[string]int `json:"clients"`
}
