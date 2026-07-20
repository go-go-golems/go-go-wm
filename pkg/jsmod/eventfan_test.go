package jsmod

import (
	"sync"
	"testing"

	"github.com/go-go-golems/go-go-wm/pkg/pbui"
)

// TestEventFanSnapshotConcurrentSubscribe is the regression test for Codex
// review comment RC-3: copying only the map header (goSubs := f.goSubs) and
// iterating after unlock races a concurrent SubscribeGo append, producing a
// fatal concurrent map read/write. The fix deep-copies the map under the
// lock in snapshot(). Run with -race to catch the data race.
func TestEventFanSnapshotConcurrentSubscribe(t *testing.T) {
	f := NewEventFan(nil, 16)

	var wg sync.WaitGroup
	wg.Add(2)

	// Writer: subscribe to many events concurrently with the drainer.
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			ev := "event." + string(rune('a'+i%26))
			f.SubscribeGo(ev, func(*pbui.Msg) {})
		}
	}()

	// Reader: snapshot (the drainer's view) concurrently.
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_, _, goSubs := f.snapshot()
			// Iterate the snapshot — must be a private copy, not the
			// shared map, or this races the writer above.
			for _, fns := range goSubs {
				for _, fn := range fns {
					fn(nil)
				}
			}
		}
	}()

	wg.Wait()

	// Final sanity: the snapshot must reflect a consistent state.
	_, _, goSubs := f.snapshot()
	total := 0
	for _, fns := range goSubs {
		total += len(fns)
	}
	if total != 200 {
		t.Fatalf("expected 200 total subscriptions, got %d", total)
	}
}
