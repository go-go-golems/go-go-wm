package pbuimod

import (
	"context"

	"github.com/dop251/goja"
	"github.com/go-go-golems/go-go-goja/pkg/runtimebridge"

	"github.com/go-go-golems/go-go-wm/pkg/jsmod"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
)

// jsOn: pbui.on(event, fn) — subscribe to the broker event bus. "*"
// matches every event. Handlers run on the owner loop; delivery goes
// read-goroutine → bounded queue → one posted drain per wakeup, so a slow
// script batches instead of flooding the loop, and overflow surfaces as a
// script.error event carrying the drop count.
func (m *Module) jsOn(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		cl := m.requireClient(vm)
		event := call.Argument(0).String()
		fn, ok := goja.AssertFunction(call.Argument(1))
		if !ok {
			panic(vm.ToValue("pbui.on: second argument must be a function"))
		}
		services, sok := runtimebridge.Lookup(vm)
		if !sok {
			panic(vm.ToValue("pbui.on: no runtime services"))
		}

		m.mu.Lock()
		m.subs[event] = append(m.subs[event], fn)
		m.mu.Unlock()

		var startErr error
		m.pumpOnce.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			defer cancel()
			ch, err := cl.Events(ctx)
			if err != nil {
				startErr = err
				return
			}
			lifetime := services.Lifetime()
			// Pump: read goroutine side. Push never blocks.
			go func() {
				for msg := range ch {
					m.queue.Push(msg)
				}
			}()
			// Drainer: one Post per wakeup delivers the whole batch.
			go func() {
				for {
					select {
					case <-lifetime.Done():
						return
					case <-m.queue.Wake():
					}
					batch := m.queue.Drain()
					if dropped := m.queue.TakeDropped(); dropped > 0 {
						jsmod.EmitScriptError(cl, "pbui.on", nil,
							map[string]interface{}{"dropped": dropped})
					}
					if len(batch) == 0 {
						continue
					}
					_ = services.PostWithLifetimeContext("pbui.on.batch",
						func(_ context.Context, vm *goja.Runtime) {
							for _, msg := range batch {
								m.dispatchEvent(vm, msg)
							}
						})
				}
			}()
		})
		if startErr != nil {
			panic(vm.ToValue("pbui.on: subscribe: " + startErr.Error()))
		}
		return goja.Undefined()
	}
}

// dispatchEvent runs on the owner loop.
func (m *Module) dispatchEvent(vm *goja.Runtime, msg *pbui.Msg) {
	m.mu.Lock()
	fns := append([]goja.Callable(nil), m.subs[msg.Event]...)
	fns = append(fns, m.subs["*"]...)
	m.mu.Unlock()
	if len(fns) == 0 {
		return
	}
	arg := vm.ToValue(jsmod.EventToJS(msg))
	for _, f := range fns {
		if _, err := f(goja.Undefined(), arg); err != nil {
			jsmod.EmitScriptError(m.cl, "event:"+msg.Event, err, nil)
		}
	}
}
