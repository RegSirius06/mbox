package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"

	noise "../common/noise"
)

func newTransportTestSession(t *testing.T) (*session, *noise.CipherState) {
	t.Helper()
	key := bytes.Repeat([]byte{0x42}, 32)
	send, err := noise.NewCipherState(key)
	if err != nil {
		t.Fatal(err)
	}
	recv, err := noise.NewCipherState(key)
	if err != nil {
		t.Fatal(err)
	}
	s := &session{streams: make(map[uint32]*stream), crypto: &cryptoState{down: send}, downReady: make(chan struct{}, 1), downReplay: make(map[uint64]cachedDownResponse), downReqSeen: make(map[uint64]struct{}), upCache: make(map[uint64]cachedUpResponse), done: make(chan struct{})}
	return s, recv
}
func frameForTest(id uint32, typ byte, p []byte) []byte {
	o := make([]byte, 9+len(p))
	binary.BigEndian.PutUint32(o[:4], id)
	o[4] = typ
	binary.BigEndian.PutUint32(o[5:9], uint32(len(p)))
	copy(o[9:], p)
	return o
}
func decryptTestDown(t *testing.T, recv *noise.CipherState, sid string, record []byte) (uint64, []byte) {
	t.Helper()
	counter, p, err := openNoiseRecord(recv, sid, dirDown, record)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := unpadTransportPlaintext(p)
	if err != nil {
		t.Fatal(err)
	}
	return counter, plain
}
func TestDownLostHTTPResponseReplaysExactEncryptedRecord(t *testing.T) {
	globalQueuedBytes.Store(0)
	old := camouflage.PaddingExtraClassPercent
	camouflage.PaddingExtraClassPercent = 0
	defer func() { camouflage.PaddingExtraClassPercent = old; globalQueuedBytes.Store(0) }()
	s, recv := newTransportTestSession(t)
	sid := "0123456789abcdef0123456789abcdef"
	if err := appendFrame(s, 7, framePing, []byte("once")); err != nil {
		t.Fatal(err)
	}
	r1, err := prepareDownResponse(s, sid, 100, 0, 64*1024)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := prepareDownResponse(s, sid, 100, 0, 64*1024)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(r1.body, r2.body) || r1.counter != r2.counter {
		t.Fatal("replay changed")
	}
	counter, plain := decryptTestDown(t, recv, sid, r2.body)
	if !bytes.Equal(plain, frameForTest(7, framePing, []byte("once"))) {
		t.Fatal("wrong plaintext")
	}
	if _, err := prepareDownResponse(s, sid, 101, counter, 64*1024); err != nil {
		t.Fatal(err)
	}
}
func TestConcurrentGetPostDownCommitIsStrictlyOrdered(t *testing.T) {
	globalQueuedBytes.Store(0)
	old := camouflage.PaddingExtraClassPercent
	camouflage.PaddingExtraClassPercent = 0
	defer func() { camouflage.PaddingExtraClassPercent = old; globalQueuedBytes.Store(0) }()
	s, recv := newTransportTestSession(t)
	sid := "fedcba9876543210fedcba9876543210"
	_ = appendFrame(s, 1, framePing, []byte("first"))
	_ = appendFrame(s, 2, framePing, []byte("second"))
	limit := len(frameForTest(1, framePing, []byte("first")))
	type rr struct {
		r cachedDownResponse
		e error
	}
	ch := make(chan rr, 2)
	var wg sync.WaitGroup
	for _, seq := range []uint64{201, 202} {
		wg.Add(1)
		go func(seq uint64) { defer wg.Done(); r, e := prepareDownResponse(s, sid, seq, 0, limit); ch <- rr{r, e} }(seq)
	}
	wg.Wait()
	close(ch)
	var got []cachedDownResponse
	for x := range ch {
		if x.e != nil {
			t.Fatal(x.e)
		}
		got = append(got, x.r)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].counter < got[j].counter })
	_, p1 := decryptTestDown(t, recv, sid, got[0].body)
	_, p2 := decryptTestDown(t, recv, sid, got[1].body)
	if !bytes.Equal(p1, frameForTest(1, framePing, []byte("first"))) || !bytes.Equal(p2, frameForTest(2, framePing, []byte("second"))) {
		t.Fatal("ordering broken")
	}
}
func TestUpReplayCacheDoesNotDuplicateCiphertext(t *testing.T) {
	s, _ := newTransportTestSession(t)
	big := bytes.Repeat([]byte{0x7a}, 2*1024*1024)
	s.downReplay[55] = cachedDownResponse{counter: 1, status: http.StatusOK, body: []byte("down")}
	cacheUpResponse(s, 9, big, 55, http.StatusOK)
	s.mu.Lock()
	c := s.upCache[9]
	s.mu.Unlock()
	if c.recordHash != sha256.Sum256(big) {
		t.Fatal("hash mismatch")
	}
}
func TestPerClientSessionLimitAndUnlimited(t *testing.T) {
	oldSessions := sessions
	oldPolicies := clientPolicies
	oldRates := sessionOpenRates
	sessions = map[string]*session{}
	sessionOpenRates = map[string]rateWindow{}
	clientPolicies = map[string]clientPolicy{"x": {PSK: bytes.Repeat([]byte{1}, 32), MaxSessions: 1}}
	defer func() { sessions = oldSessions; clientPolicies = oldPolicies; sessionOpenRates = oldRates }()
	s1 := &session{clientID: "x"}
	s2 := &session{clientID: "x"}
	if !registerSession("1", s1) {
		t.Fatal("first rejected")
	}
	if registerSession("2", s2) {
		t.Fatal("limit ignored")
	}
	clientPolicies["x"] = clientPolicy{PSK: bytes.Repeat([]byte{1}, 32), MaxSessions: -1}
	if !registerSession("2", s2) {
		t.Fatal("unlimited rejected")
	}
}
func TestExpiredClientPolicy(t *testing.T) {
	p := clientPolicy{ExpiresAt: time.Now().Add(-time.Second)}
	if clientPolicyActive(p) {
		t.Fatal("expired policy active")
	}
}
func FuzzDownRequestMeta(f *testing.F) {
	f.Add("1", "0")
	f.Add("x", "1")
	f.Fuzz(func(t *testing.T, seq, ack string) {
		r, err := http.NewRequest(http.MethodGet, "https://x.invalid/?seq="+seq+"&ack="+ack, nil)
		if err == nil {
			_, _, _ = parseDownRequestMeta(r)
		}
	})
}

func TestExpiredActiveSessionClosesOnNextRequest(t *testing.T) {
	oldSessions := sessions
	oldPolicies := clientPolicies
	sessions = map[string]*session{}
	clientPolicies = map[string]clientPolicy{
		"paid": {PSK: bytes.Repeat([]byte{2}, 32), ExpiresAt: time.Now().Add(-time.Second)},
	}
	defer func() {
		sessions = oldSessions
		clientPolicies = oldPolicies
	}()

	s := &session{
		clientID:   "paid",
		streams:    map[uint32]*stream{},
		downReplay: map[uint64]cachedDownResponse{},
		done:       make(chan struct{}),
		lastSeen:   time.Now(),
	}
	sessions["sid"] = s
	if got, ok := getSession("sid"); ok || got != nil {
		t.Fatal("expired active session remained usable")
	}
	select {
	case <-s.done:
	default:
		t.Fatal("expired active session was not closed")
	}
}

func TestUnlimitedProfileAllowsHundredConcurrentSessionsWithinGlobalCap(t *testing.T) {
	oldSessions := sessions
	oldPolicies := clientPolicies
	oldRates := sessionOpenRates
	oldLimits := runtimeLimits
	sessions = map[string]*session{}
	sessionOpenRates = map[string]rateWindow{}
	clientPolicies = map[string]clientPolicy{
		"family": {PSK: bytes.Repeat([]byte{7}, 32), MaxSessions: -1},
	}
	runtimeLimits.MaxSessionsGlobal = 128
	runtimeLimits.MaxSessionOpensPerMinute = 180
	defer func() {
		sessions = oldSessions
		clientPolicies = oldPolicies
		sessionOpenRates = oldRates
		runtimeLimits = oldLimits
	}()

	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("sid-%03d", i)
		if !registerSession(id, &session{clientID: "family"}) {
			t.Fatalf("unlimited profile rejected session %d", i+1)
		}
	}
	if got := len(sessions); got != 100 {
		t.Fatalf("sessions=%d want=100", got)
	}
}

func TestNoiseDownCounterOverflowRejected(t *testing.T) {
	key := bytes.Repeat([]byte{0x35}, 32)
	send, err := noise.NewCipherState(key)
	if err != nil {
		t.Fatal(err)
	}
	c := &cryptoState{down: send, downCounter: ^uint64(0)}
	if _, err := c.sealDown("0123456789abcdef0123456789abcdef", []byte("x")); err == nil {
		t.Fatal("counter overflow accepted")
	}
}

func TestGlobalQueuedBudgetReturnsToZeroAfterDownAck(t *testing.T) {
	globalQueuedBytes.Store(0)
	old := camouflage.PaddingExtraClassPercent
	camouflage.PaddingExtraClassPercent = 0
	defer func() { camouflage.PaddingExtraClassPercent = old; globalQueuedBytes.Store(0) }()

	s, _ := newTransportTestSession(t)
	sid := "11111111111111111111111111111111"
	if err := appendFrame(s, 1, framePing, bytes.Repeat([]byte("x"), 4093)); err != nil {
		t.Fatal(err)
	}
	r, err := prepareDownResponse(s, sid, 1, 0, 64*1024)
	if err != nil {
		t.Fatal(err)
	}
	if r.reservedBytes != len(r.body) {
		t.Fatalf("reservation=%d body=%d", r.reservedBytes, len(r.body))
	}
	if got := globalQueuedBytes.Load(); got != int64(len(r.body)) {
		t.Fatalf("queued=%d want=%d", got, len(r.body))
	}
	if _, err := prepareDownResponse(s, sid, 2, r.counter, 64*1024); err != nil {
		t.Fatal(err)
	}
	if got := globalQueuedBytes.Load(); got != 0 {
		t.Fatalf("globalQueuedBytes=%d after ACK, want 0", got)
	}
}

func TestGlobalQueuedBudgetReturnsToZeroOnSessionClose(t *testing.T) {
	globalQueuedBytes.Store(0)
	oldSessions := sessions
	sessions = map[string]*session{}
	defer func() { sessions = oldSessions; globalQueuedBytes.Store(0) }()

	s, _ := newTransportTestSession(t)
	s.clientID = "budget-test"
	s.lastSeen = time.Now()
	sessions["sid"] = s
	if err := appendFrame(s, 1, framePing, bytes.Repeat([]byte("z"), 7777)); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareDownResponse(s, "22222222222222222222222222222222", 1, 0, 64*1024); err != nil {
		t.Fatal(err)
	}
	closeSession("sid")
	if got := globalQueuedBytes.Load(); got != 0 {
		t.Fatalf("globalQueuedBytes=%d after close, want 0", got)
	}
}

func TestGlobalQueuedBudgetHundredThousandDownAckCycles(t *testing.T) {
	globalQueuedBytes.Store(0)
	old := camouflage.PaddingExtraClassPercent
	camouflage.PaddingExtraClassPercent = 0
	defer func() { camouflage.PaddingExtraClassPercent = old; globalQueuedBytes.Store(0) }()

	s, _ := newTransportTestSession(t)
	sid := "33333333333333333333333333333333"
	var ack uint64
	for i := 0; i < 100000; i++ {
		if err := appendFrame(s, 1, framePing, []byte{byte(i), byte(i >> 8)}); err != nil {
			t.Fatal(err)
		}
		r, err := prepareDownResponse(s, sid, uint64(i+1), ack, 64*1024)
		if err != nil {
			t.Fatal(err)
		}
		ack = r.counter
		// ACK immediately using a distinct request sequence outside the loop's
		// normal sequence space, then discard the no-content replay entry.
		if err := applyDownAckForTest(s, ack); err != nil {
			t.Fatal(err)
		}
		if got := globalQueuedBytes.Load(); got != 0 {
			t.Fatalf("cycle=%d globalQueuedBytes=%d", i, got)
		}
	}
}

func applyDownAckForTest(s *session, ack uint64) error {
	s.downCommitMu.Lock()
	defer s.downCommitMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	return applyDownAckLocked(s, ack)
}

func TestSessionOpenRateStateCapacityIsBounded(t *testing.T) {
	oldRates := sessionOpenRates
	oldSessions := sessions
	oldPolicies := clientPolicies
	sessionOpenRates = make(map[string]rateWindow, maxSessionOpenRateEntries)
	sessions = map[string]*session{}
	clientPolicies = map[string]clientPolicy{"new": {PSK: bytes.Repeat([]byte{9}, 32), MaxSessions: -1}}
	defer func() { sessionOpenRates = oldRates; sessions = oldSessions; clientPolicies = oldPolicies }()

	now := time.Now()
	for i := 0; i < maxSessionOpenRateEntries; i++ {
		sessionOpenRates[fmt.Sprintf("live-%05d", i)] = rateWindow{start: now, count: 1}
	}
	if registerSession("new-session", &session{clientID: "new"}) {
		t.Fatal("new rate identity admitted after bounded map reached capacity")
	}
	if got := len(sessionOpenRates); got != maxSessionOpenRateEntries {
		t.Fatalf("rate map size=%d want=%d", got, maxSessionOpenRateEntries)
	}
}
