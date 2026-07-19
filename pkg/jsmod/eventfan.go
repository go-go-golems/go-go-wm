package jsmod

import (
	"context"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/go-go-golems/go-go-goja/pkg/runtimebridge"

	"github.com/go-go-golems/go-go-wm/pkg/pbui"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/client"
)

// EventFan is the single event-bus subscription of a script process,
// fanned out to every JS handler registered through pbui.on or wm.on.
// Delivery chain: client read goroutine → bounded queue (drop+count on
// overflow) → one posted batch per wakeup on the owner loop. No hop ever
// blocks the previous one; overflow surfaces as one script.error event
// carrying the drop count.
type EventFan struct {
	cl *client.Client

	mu   sync.Mutex
	subs map[string][]namedHandler // event ("*" = all) → handlers

	pumpOnce  sync.Once
	queue     *boundedQueue[*pbui.Msg]
	queueSize int
}

type namedHandler struct {
	fn  goja.Callable
	tag string // for script.error attribution ("pbui.on", "wm.on")
}

// NewEventFan creates a fan over cl (nil is allowed; Subscribe then fails).
func NewEventFan(cl *client.Client, queueSize int) *EventFan {
	if queueSize <= 0 {
		queueSize = 256
	}
	return &EventFan{
		cl:        cl,
		subs:      map[string][]namedHandler{},
		queue:     newBoundedQueue[*pbui.Msg](queueSize),
		queueSize: queueSize,
	}
}

// Subscribe registers fn for event and lazily starts the pump. Must be
// called on the VM thread (it is: only loader-installed functions call it).
func (f *EventFan) Subscribe(vm *goja.Runtime, event, tag string, fn goja.Callable) error {
	services, ok := runtimebridge.Lookup(vm)
	if !ok {
		panic(vm.ToValue(tag + ": no runtime services"))
	}
	f.mu.Lock()
	f.subs[event] = append(f.subs[event], namedHandler{fn: fn, tag: tag})
	f.mu.Unlock()

	var startErr error
	f.pumpOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		ch, err := f.cl.Events(ctx)
		if err != nil {
			startErr = err
			return
		}
		lifetime := services.Lifetime()
		go func() { // pump: never blocks on the JS side
			for msg := range ch {
				f.queue.Push(msg)
			}
		}()
		go func() { // drainer: one Post delivers the whole batch
			for {
				select {
				case <-lifetime.Done():
					return
				case <-f.queue.Wake():
				}
				batch := f.queue.Drain()
				if dropped := f.queue.TakeDropped(); dropped > 0 {
					EmitScriptError(f.cl, "events", nil,
						map[string]interface{}{"dropped": dropped})
				}
				if len(batch) == 0 {
					continue
				}
				_ = services.PostWithLifetimeContext("events.batch",
					func(_ context.Context, vm *goja.Runtime) {
						for _, msg := range batch {
							f.dispatch(vm, msg)
						}
					})
			}
		}()
	})
	return startErr
}

// dispatch runs on the owner loop.
func (f *EventFan) dispatch(vm *goja.Runtime, msg *pbui.Msg) {
	f.mu.Lock()
	handlers := append([]namedHandler(nil), f.subs[msg.Event]...)
	handlers = append(handlers, f.subs["*"]...)
	f.mu.Unlock()
	if len(handlers) == 0 {
		return
	}
	arg := vm.ToValue(EventToJS(msg))
	for _, h := range handlers {
		if _, err := h.fn(goja.Undefined(), arg); err != nil {
			EmitScriptError(f.cl, h.tag+":"+msg.Event, err, nil)
		}
	}
}
