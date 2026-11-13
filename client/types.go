package main

import (
	flowctl "../common/flowctl"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
)

const (
	frameOpen     = 1
	frameData     = 2
	frameClose    = 3
	frameError    = 4
	framePing     = 5
	frameWindow   = 6
	frameUDPOpen  = 7
	frameUDPReady = 8
	frameUDPData  = 9
)

var (
	listen string
	base   string
)

type localStream struct {
	conn          net.Conn
	writeQ        *flowctl.ByteQueue
	closed        atomic.Bool
	creditMu      sync.Mutex
	sendCredit    int
	recvAllowance int
	creditWake    chan struct{}
	writerDone    chan struct{} // prevents old TCP writer racing the HEV UDP reply
}

type tunnel struct {
	sid    string
	crypto *cryptoState

	streamsMu sync.Mutex
	streams   map[uint32]*localStream
	udpFlows  map[uint32]*localUDPFlow
	nextID    atomic.Uint32
	streamWG  sync.WaitGroup

	upMu        sync.Mutex
	upQueues    map[uint32][][]byte
	upOrder     []uint32
	upBytes     int
	upPerStream map[uint32]int
	upSpace     chan struct{}

	wake        chan struct{}
	downWake    chan struct{}
	downDeliver chan []byte
	done        chan struct{}

	nextDown         uint64
	downPending      map[uint64][]byte
	downPendingBytes int
	downAck          atomic.Uint64
	downReqSeq       atomic.Uint64

	http *http.Client

	// A terminal transport/protocol failure must terminate the process. Leaving
	// the local SOCKS listener alive would make Android keep a dead VPN route.
	failOnce sync.Once
	fatalErr chan error

	healthMu    sync.Mutex
	healthNonce [16]byte
	healthReply chan struct{}
	healthWake  chan struct{}
}
