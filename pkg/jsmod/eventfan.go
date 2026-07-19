package jsmod

import (
	"context"
	"fmt"
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

	mu     sync.Mutex
	subs   map[string][]namedHandler // event ("*" = all) → JS handlers
	goSubs map[string][]func(*pbui.Msg)

	services    runtimebridge.RuntimeServices // captured on first JS Subscribe
	hasServices bool

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
		goSubs:    map[string][]func(*pbui.Msg){},
		queue:     newBoundedQueue[*pbui.Msg](queueSize),
		queueSize: queueSize,
	}
}

// SubscribeGo registers a Go-side handler (no VM involved — e.g. the
// wmmod rule engine). Handlers run on the drainer goroutine and must not
// touch goja state. The pump must already be running or be started later
// by a JS Subscribe; call EnsurePump to start it explicitly.
func (f *EventFan) SubscribeGo(event string, fn func(*pbui.Msg)) {
	f.mu.Lock()
	f.goSubs[event] = append(f.goSubs[event], fn)
	f.mu.Unlock()
}

// Subscribe registers a JS handler for event and lazily starts the pump.
// Must be called on the VM thread (it is: only loader-installed functions
// call it).
func (f *EventFan) Subscribe(vm *goja.Runtime, event, tag string, fn goja.Callable) error {
	services, ok := runtimebridge.Lookup(vm)
	if !ok {
		panic(vm.ToValue(tag + ": no runtime services"))
	}
	f.mu.Lock()
	f.subs[event] = append(f.subs[event], namedHandler{fn: fn, tag: tag})
	if !f.hasServices {
		f.services, f.hasServices = services, true
	}
	f.mu.Unlock()
	return f.EnsurePump(services.Lifetime())
}

// EnsurePump starts the read-pump and drainer once. ctx bounds the
// drainer's life. Safe to call multiple times.
func (f *EventFan) EnsurePump(ctx context.Context) error {
	if f.cl == nil {
		return fmt.Errorf("no broker connection")
	}
	var startErr error
	f.pumpOnce.Do(func() {
		subCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		ch, err := f.cl.Events(subCtx)
		if err != nil {
			startErr = err
			return
		}
		go func() { // pump: never blocks on the consumer side
			for msg := range ch {
				f.queue.Push(msg)
			}
		}()
		go func() { // drainer: Go handlers inline, one Post per JS batch
			for {
				select {
				case <-ctx.Done():
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
				f.mu.Lock()
				services, hasJS := f.services, f.hasServices && f.jsHandlerCount() > 0
				goSubs := f.goSubs
				f.mu.Unlock()
				for _, msg := range batch {
					for _, fn := range goSubs[msg.Event] {
						fn(msg)
					}
					for _, fn := range goSubs["*"] {
						fn(msg)
					}
				}
				if hasJS {
					_ = services.PostWithLifetimeContext("events.batch",
						func(_ context.Context, vm *goja.Runtime) {
							for _, msg := range batch {
								f.dispatch(vm, msg)
							}
						})
				}
			}
		}()
	})
	return startErr
}

// jsHandlerCount must be called with f.mu held.
func (f *EventFan) jsHandlerCount() int {
	n := 0
	for _, hs := range f.subs {
		n += len(hs)
	}
	return n
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

// Client exposes the underlying broker client (nil in broker-less
// runs) — for modules that need identity or registration beyond event
// delivery (the A2 wm.command path).
func (f *EventFan) Client() *client.Client { return f.cl }
