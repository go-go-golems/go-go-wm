package jsmod

import "testing"

func TestBoundedQueueOverflowCountsDrops(t *testing.T) {
	q := newBoundedQueue[int](3)
	for i := 0; i < 10; i++ {
		q.Push(i)
	}
	got := q.Drain()
	if len(got) != 3 || got[0] != 0 || got[2] != 2 {
		t.Fatalf("kept %v, want first 3 (contiguity over freshness)", got)
	}
	if d := q.TakeDropped(); d != 7 {
		t.Fatalf("dropped = %d, want 7", d)
	}
	if d := q.TakeDropped(); d != 0 {
		t.Fatalf("TakeDropped must reset, got %d", d)
	}
	// After a drain the queue accepts again.
	if !q.Push(42) {
		t.Fatal("push after drain should succeed")
	}
	if got := q.Drain(); len(got) != 1 || got[0] != 42 {
		t.Fatalf("drain after refill = %v", got)
	}
}

func TestBoundedQueueWakeSignal(t *testing.T) {
	q := newBoundedQueue[string](8)
	q.Push("a")
	q.Push("b")
	select {
	case <-q.Wake():
	default:
		t.Fatal("wake signal missing after push")
	}
	// Coalesced: many pushes, at most one buffered signal — but after a
	// drain the consumer must not deadlock waiting for pushes it already
	// consumed.
	if got := q.Drain(); len(got) != 2 {
		t.Fatalf("drain = %v", got)
	}
	select {
	case <-q.Wake():
		t.Fatal("no second wake should be buffered")
	default:
	}
}
