package flowctl

import (
	"bytes"
	"testing"
)

func TestByteQueueBoundsOwnershipAndDrain(t *testing.T) {
	q := NewByteQueue(3)
	src := []byte{1, 2}
	if !q.TryPush(src) {
		t.Fatal("initial enqueue rejected")
	}
	src[0] = 9
	if q.TryPush([]byte{3, 4}) {
		t.Fatal("queue overflow accepted")
	}
	got, ok := q.Pop(make(chan struct{}))
	if !ok || !bytes.Equal(got, []byte{1, 2}) {
		t.Fatalf("queue did not own bytes: %v, %v", got, ok)
	}
	if !q.TryPush([]byte{5, 6, 7}) {
		t.Fatal("queue did not return capacity")
	}
	if n := q.CloseAndDrain(); n != 3 {
		t.Fatalf("drained %d bytes, want 3", n)
	}
	if n := q.CloseAndDrain(); n != 0 {
		t.Fatalf("double close drained %d bytes", n)
	}
	if q.TryPush([]byte{8}) {
		t.Fatal("closed queue accepted bytes")
	}
	if p, ok := q.Pop(make(chan struct{})); ok || p != nil {
		t.Fatalf("closed queue returned %v, %v", p, ok)
	}
}

func TestQueueCancellationAndWake(t *testing.T) {
	q := NewByteQueue(5)
	done := make(chan struct{})
	close(done)
	if p, ok := q.Pop(done); ok || p != nil {
		t.Fatalf("cancellation returned %v, %v", p, ok)
	}
	result := make(chan []byte, 1)
	go func() {
		p, _ := q.Pop(make(chan struct{}))
		result <- p
	}()
	if !q.TryPush([]byte("hi")) {
		t.Fatal("enqueue failed")
	}
	if p := <-result; !bytes.Equal(p, []byte("hi")) {
		t.Fatalf("unexpected bytes: %q", p)
	}
}

func TestWindowCreditEncodingAndBounds(t *testing.T) {
	for _, n := range []int{1, 100, MaxCredit} {
		p := EncodeWindowCredit(n)
		got, err := DecodeWindowCredit(p)
		if err != nil || got != n {
			t.Fatalf("credit %d: got=%d err=%v", n, got, err)
		}
	}
	for _, p := range [][]byte{nil, {0}, {0, 0, 0, 0}, {0, 4, 0, 1}, {1, 2, 3, 4, 5}} {
		if _, err := DecodeWindowCredit(p); err == nil {
			t.Fatalf("accepted invalid WINDOW %x", p)
		}
	}
}

func FuzzWindowCreditDecoder(f *testing.F) {
	f.Add([]byte{0, 0, 0, 1})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = DecodeWindowCredit(data) })
}
