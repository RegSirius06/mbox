package dgram

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

const sidA = "0123456789abcdef0123456789abcdef"
const sidB = "1123456789abcdef0123456789abcdef"

func seed(n byte) (s [32]byte) { s[0] = n; return }
func record(t *testing.T, s [32]byte, sid, dir string) *Record {
	t.Helper()
	r, e := NewRecord(s, sid, dir)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func seal(t *testing.T, r *Record, b []byte) []byte {
	t.Helper()
	p, e := r.Seal(b)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func wantOpen(t *testing.T, r *Record, p, b []byte) {
	t.Helper()
	a, e := r.Open(p)
	if e != nil || !bytes.Equal(a, b) {
		t.Fatalf("open mismatch: %v", e)
	}
}
func wantReject(t *testing.T, r *Record, p []byte) {
	t.Helper()
	if _, e := r.Open(p); e == nil {
		t.Fatal("accepted invalid packet")
	}
}

func TestRecordLossReorderReplay(t *testing.T) {
	tx, rx := record(t, seed(1), sidA, "up"), record(t, seed(1), sidA, "up")
	a, b, c := seal(t, tx, []byte{1}), seal(t, tx, []byte{2}), seal(t, tx, []byte{3})
	wantOpen(t, rx, c, []byte{3})
	wantOpen(t, rx, a, []byte{1})
	wantReject(t, rx, c)
	// loss and delayed delivery do not stall the receiver.
	for i := 0; i < 65; i++ {
		c = seal(t, tx, []byte{9})
	}
	wantOpen(t, rx, c, []byte{9})
	wantReject(t, rx, b)
}
func TestRecordTamperDoesNotAdvance(t *testing.T) {
	tx, rx := record(t, seed(1), sidA, "up"), record(t, seed(1), sidA, "up")
	a := seal(t, tx, []byte{1})
	b := append([]byte(nil), a...)
	binary.BigEndian.PutUint64(b[:8], 1000)
	wantReject(t, rx, b)
	b = append([]byte(nil), a...)
	b[len(b)-1] ^= 1
	wantReject(t, rx, b)
	wantOpen(t, rx, a, []byte{1})
}
func TestRecordDomainAndMTU(t *testing.T) {
	tx := record(t, seed(1), sidA, "up")
	plain := make([]byte, MaxPlain)
	p := seal(t, tx, plain)
	if len(p) != MaxPacket {
		t.Fatal(len(p))
	}
	wantOpen(t, record(t, seed(1), sidA, "up"), p, plain)
	wantReject(t, record(t, seed(1), sidA, "down"), p)
	wantReject(t, record(t, seed(2), sidA, "up"), p)
	wantReject(t, record(t, seed(1), sidB, "up"), p)
	for _, n := range []int{0, MaxPlain + 1} {
		if _, err := tx.Seal(make([]byte, n)); err == nil {
			t.Fatal("invalid MTU")
		}
	}
	tx.next = maxCounter - 1
	_ = seal(t, tx, []byte{1})
	if _, e := tx.Seal([]byte{1}); e == nil {
		t.Fatal("counter exhausted")
	}
}
func TestProofDomain(t *testing.T) {
	a := Attach(seed(1), sidA)
	if len(a) != AttachSize || !VerifyProof(seed(1), sidA, "attach", a[32:]) {
		t.Fatal("attach")
	}
	if VerifyProof(seed(1), sidA, "accept", a[32:]) || VerifyProof(seed(1), sidB, "attach", a[32:]) || VerifyProof(seed(2), sidA, "attach", a[32:]) {
		t.Fatal("proof crossover")
	}
}
func TestConcurrentSealUniqueCounters(t *testing.T) {
	r := record(t, seed(1), sidA, "up")
	var wg sync.WaitGroup
	out := make(chan []byte, 64)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, e := r.Seal([]byte{1})
			if e != nil {
				t.Error(e)
				return
			}
			out <- p
		}()
	}
	wg.Wait()
	close(out)
	seen := map[uint64]bool{}
	for p := range out {
		n := binary.BigEndian.Uint64(p[:8])
		if seen[n] {
			t.Fatal("nonce reused")
		}
		seen[n] = true
	}
	if len(seen) != 64 {
		t.Fatal(len(seen))
	}
}
func TestFramingBounds(t *testing.T) {
	var b bytes.Buffer
	p := make([]byte, MaxPacket)
	if e := WritePacket(&b, p); e != nil {
		t.Fatal(e)
	}
	got, e := ReadPacket(&b)
	if e != nil || !bytes.Equal(got, p) {
		t.Fatal(e)
	}
	for _, bad := range [][]byte{{0, 0}, {0xff, 0xff}, {0, 25, 1}} {
		if _, e := ReadPacket(bytes.NewReader(bad)); e == nil {
			t.Fatal("malformed frame")
		}
	}
	if WritePacket(io.Discard, make([]byte, MaxPacket+1)) == nil {
		t.Fatal("oversize write")
	}
}
func TestLaneQueueAndFallback(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	l := NewLane(a, record(t, seed(1), sidA, "up"), record(t, seed(2), sidA, "down"))
	defer l.Close()
	for i := 0; i < 100; i++ {
		if !l.Offer(1, []byte{1}) {
			t.Fatal("congestion must consume not duplicate")
		}
	}
	if len(l.q) != PerFlowPackets {
		t.Fatal(len(l.q))
	}
	if !l.Offer(2, []byte{2}) || len(l.q) != PerFlowPackets+1 {
		t.Fatal("other flow starved")
	}
	for f := uint32(3); f < 100; f++ {
		_ = l.Offer(f, []byte{1})
	}
	if len(l.q) != QueuePackets {
		t.Fatal(len(l.q))
	}
	if l.Offer(1, make([]byte, MaxPlain+1)) {
		t.Fatal("large packet must use streams")
	}
	l.Close()
	if l.Offer(1, []byte{1}) {
		t.Fatal("closed lane must fall back")
	}
}
func TestLaneRoundTripCancel(t *testing.T) {
	a, b := net.Pipe()
	client := NewLane(a, record(t, seed(1), sidA, "up"), record(t, seed(2), sidA, "down"))
	server := NewLane(b, record(t, seed(2), sidA, "down"), record(t, seed(1), sidA, "up"))
	done := make(chan struct{})
	ch := make(chan []byte, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); client.Run(done, func(p []byte) { ch <- p }) }()
	go func() {
		defer wg.Done()
		server.Run(done, func(p []byte) {
			if !server.Offer(1, p) {
				t.Error("echo rejected")
			}
		})
	}()
	if !client.Offer(1, []byte("hello")) {
		t.Fatal("rejected")
	}
	select {
	case p := <-ch:
		if string(p) != "hello" {
			t.Fatal(string(p))
		}
	case <-time.After(time.Second):
		t.Fatal("no echo")
	}
	close(done)
	wait := make(chan struct{})
	go func() { wg.Wait(); close(wait) }()
	select {
	case <-wait:
	case <-time.After(time.Second):
		t.Fatal("lane leaked")
	}
	if client.Offer(1, []byte{1}) {
		t.Fatal("not closed")
	}
}
func TestPrivateSocket(t *testing.T) {
	dir, e := os.MkdirTemp("", "mbox-dgram-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "o.sock")
	ln, e := ListenPrivate(path)
	if e != nil {
		t.Fatal(e)
	}
	info, e := os.Stat(path)
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("insecure mode", e)
	}
	if _, e := ListenPrivate(path); e == nil {
		t.Fatal("replaced live socket")
	}
	ln.Close()
	if _, e = os.Lstat(path); !os.IsNotExist(e) {
		t.Fatal("socket leaked")
	}
	if e = os.Chmod(dir, 0777); e != nil {
		t.Fatal(e)
	}
	if _, e = ListenPrivate(path); e == nil {
		t.Fatal("insecure directory accepted")
	}
}
func FuzzRecordOpen(f *testing.F) {
	f.Add([]byte{0, 1})
	f.Add(make([]byte, MaxPacket))
	f.Fuzz(func(t *testing.T, p []byte) { rx := record(t, seed(1), sidA, "up"); _, _ = rx.Open(p) })
}

func TestEpochAttachAndIndependentTrafficKeys(t *testing.T) {
	s := seed(1)
	legacy := Attach(s, sidA)
	if !VerifyProof(s, sidA, "attach", legacy[32:]) {
		t.Fatal("legacy broken")
	}
	for _, epoch := range []uint32{1, 2, MaxEpoch} {
		a := AttachEpoch(s, sidA, epoch)
		if len(a) != AttachSize {
			t.Fatal("epoch attach length")
		}
		got, valid := ParseAttachEpoch(s, sidA, a[32:])
		if !valid || got != epoch {
			t.Fatal("bad epoch proof")
		}
		if VerifyProof(s, sidA, "attach", a[32:]) {
			t.Fatal("v2 accepted as legacy")
		}
		if _, ok := ParseAttachEpoch(seed(2), sidA, a[32:]); ok {
			t.Fatal("wrong key accepted")
		}
		if _, ok := ParseAttachEpoch(s, sidB, a[32:]); ok {
			t.Fatal("wrong session accepted")
		}
		b := append([]byte(nil), a...)
		b[36] ^= 1 // epoch tamper must invalidate HMAC
		if _, ok := ParseAttachEpoch(s, sidA, b[32:]); ok {
			t.Fatal("tampered epoch accepted")
		}
		proof := AcceptEpoch(s, sidA, epoch)
		if !VerifyAcceptEpoch(s, sidA, epoch, proof[:]) || VerifyAcceptEpoch(s, sidA, epoch-1, proof[:]) {
			t.Fatal("accept domain crossover")
		}
	}
	if AttachEpoch(s, sidA, 0) != nil || AttachEpoch(s, sidA, MaxEpoch+1) != nil {
		t.Fatal("unbounded epoch")
	}
	legacyTx := record(t, s, sidA, "up")
	fresh1, err := NewRecordEpoch(s, sidA, "up", 1)
	if err != nil {
		t.Fatal(err)
	}
	fresh1Rx, _ := NewRecordEpoch(s, sidA, "up", 1)
	fresh2, _ := NewRecordEpoch(s, sidA, "up", 2)
	p0 := seal(t, legacyTx, []byte("old"))
	p1 := seal(t, fresh1, []byte("new"))
	p2 := seal(t, fresh2, []byte("newer"))
	wantOpen(t, fresh1Rx, p1, []byte("new"))
	wantReject(t, fresh1Rx, p0)
	wantReject(t, fresh1Rx, p2)
	wantReject(t, record(t, s, sidA, "up"), p1)
	wantReject(t, record(t, s, sidA, "down"), p1)
	if bytes.Equal(p0[8:], p1[8:]) || bytes.Equal(p1[8:], p2[8:]) {
		t.Fatal("epochs reused key")
	}
}
