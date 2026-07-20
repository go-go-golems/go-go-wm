package jsmod

import "sync"

// boundedQueue is the backpressure buffer between the broker client's read
// goroutine and the JS loop. Push never blocks: when the queue is full the
// oldest unprocessed item is NOT overwritten — the new item is dropped and
// counted, because event consumers care about contiguity of what they did
// see more than about the newest item.
//
// The drop counter is drained by the consumer (TakeDropped) so overflow
// can be reported as a single script.error with a count instead of one
// error per lost event.
type boundedQueue[T any] struct {
	mu      sync.Mutex
	items   []T
	max     int
	dropped int
	wake    chan struct{} // 1-buffered signal
}

func newBoundedQueue[T any](limit int) *boundedQueue[T] {
	if limit <= 0 {
		limit = 256
	}
	return &boundedQueue[T]{max: limit, wake: make(chan struct{}, 1)}
}

// Push enqueues item or drops it when full. Returns false on drop.
func (q *boundedQueue[T]) Push(item T) bool {
	q.mu.Lock()
	ok := len(q.items) < q.max
	if ok {
		q.items = append(q.items, item)
	} else {
		q.dropped++
	}
	q.mu.Unlock()
	if ok {
		select {
		case q.wake <- struct{}{}:
		default:
		}
	}
	return ok
}

// Drain removes and returns all queued items.
func (q *boundedQueue[T]) Drain() []T {
	q.mu.Lock()
	items := q.items
	q.items = nil
	q.mu.Unlock()
	return items
}

// TakeDropped returns the drop count since the last call and resets it.
func (q *boundedQueue[T]) TakeDropped() int {
	q.mu.Lock()
	n := q.dropped
	q.dropped = 0
	q.mu.Unlock()
	return n
}

// Wake returns the signal channel; it receives after successful pushes.
func (q *boundedQueue[T]) Wake() <-chan struct{} { return q.wake }
