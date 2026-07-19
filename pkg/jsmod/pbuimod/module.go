// Package pbuimod is the `pbui` native module: the broker participant API
// (accept, verbs, events, print) plus data-only presentation helpers,
// exposed to goja scripts.
//
// Concurrency contract (GGWM-002 design doc 02, Part VI):
//   - loader-installed functions always run on the VM owner loop
//   - blocking broker calls run in worker goroutines
//   - promises settle only via runtimebridge posts
//   - client callbacks (read goroutine) post to the loop, never call JS
package pbuimod

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/require"

	"github.com/go-go-golems/go-go-wm/pkg/jsmod"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/client"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/present"
)

// ModuleName is what scripts require().
const ModuleName = "pbui"

// requestTimeout bounds every single-round-trip broker call made from the
// JS loop (emit, register, answer, …). Accept is exempt: it is
// promise-based and may block for minutes.
const requestTimeout = 2 * time.Second

// Module binds one broker client connection to one goja runtime.
type Module struct {
	cl *client.Client

	mu        sync.Mutex
	verbs     map[string]pbui.Verb     // id → descriptor (registration set)
	handlers  map[string]goja.Callable // id → JS handler (VM-owned; call on loop only)
	subs      map[string][]goja.Callable
	acceptFns []goja.Callable // onAcceptMode handlers
	clearFns  []goja.Callable // onAcceptClear handlers

	pumpOnce     sync.Once // event pump started
	dispatchOnce sync.Once // OnVerbRun bridge installed
	acceptOnce   sync.Once // OnAcceptMode/Clear bridges installed
	queue        *boundedQueue[*pbui.Msg]
	queueSize    int
}

// Option configures a Module.
type Option func(*Module)

// WithQueueSize overrides the event queue bound (default 256).
func WithQueueSize(n int) Option { return func(m *Module) { m.queueSize = n } }

// New creates the module. cl may be nil, in which case only the data-only
// helpers work and everything else throws a clear error.
func New(cl *client.Client, opts ...Option) *Module {
	m := &Module{
		cl:        cl,
		verbs:     map[string]pbui.Verb{},
		handlers:  map[string]goja.Callable{},
		subs:      map[string][]goja.Callable{},
		queueSize: 256,
	}
	for _, o := range opts {
		o(m)
	}
	m.queue = newBoundedQueue[*pbui.Msg](m.queueSize)
	return m
}

// Loader returns the require.ModuleLoader that installs the exports.
func (m *Module) Loader() require.ModuleLoader {
	return func(vm *goja.Runtime, module *goja.Object) {
		exports := module.Get("exports").(*goja.Object)

		// Data-only helpers (no client, no I/O).
		mustSet(exports, "object", func(call goja.FunctionCall) goja.Value {
			ptype := call.Argument(0).String()
			obj, err := pbui.NewObject(ptype, exportArg(call.Argument(1)))
			if err != nil {
				panic(vm.ToValue(err.Error()))
			}
			return vm.ToValue(jsmod.ObjectToJS(&obj))
		})
		mustSet(exports, "uri", func(call goja.FunctionCall) goja.Value {
			obj, err := jsmod.JSToObject(exportArg(call.Argument(0)))
			if err != nil {
				panic(vm.ToValue(err.Error()))
			}
			return vm.ToValue(pbui.ObjectToURI(obj))
		})
		mustSet(exports, "parse", func(call goja.FunctionCall) goja.Value {
			obj, err := pbui.ObjectFromURI(call.Argument(0).String())
			if err != nil {
				panic(vm.ToValue(err.Error()))
			}
			return vm.ToValue(jsmod.ObjectToJS(&obj))
		})
		mustSet(exports, "link", func(call goja.FunctionCall) goja.Value {
			obj, err := jsmod.JSToObject(exportArg(call.Argument(0)))
			if err != nil {
				panic(vm.ToValue(err.Error()))
			}
			return vm.ToValue(present.Link(obj, call.Argument(1).String()))
		})

		// Everything below needs the broker.
		mustSet(exports, "accept", m.jsAccept(vm))
		mustSet(exports, "answer", m.jsAnswer(vm))
		mustSet(exports, "cancel", m.jsCancel(vm))
		mustSet(exports, "verb", m.jsVerb(vm))
		mustSet(exports, "print", m.jsPrint(vm))
		mustSet(exports, "emit", m.jsEmit(vm))
		mustSet(exports, "on", m.jsOn(vm))
		mustSet(exports, "menu", m.jsMenu(vm))
		mustSet(exports, "hover", m.jsHover(vm))
		mustSet(exports, "onAcceptMode", m.jsOnAcceptMode(vm))
		mustSet(exports, "onAcceptClear", m.jsOnAcceptClear(vm))
	}
}

// requireClient throws a JS exception when the module has no broker.
func (m *Module) requireClient(vm *goja.Runtime) *client.Client {
	if m.cl == nil {
		panic(vm.ToValue("pbui: not connected to a broker (data-only profile)"))
	}
	return m.cl
}

// jsEmit: pbui.emit(event, data) — single round trip, bounded deadline.
func (m *Module) jsEmit(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		cl := m.requireClient(vm)
		event := call.Argument(0).String()
		data := exportArg(call.Argument(1))
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		if err := cl.Emit(ctx, event, data); err != nil {
			panic(vm.ToValue(fmt.Sprintf("pbui.emit: %v", err)))
		}
		return goja.Undefined()
	}
}

// jsPrint: pbui.print("text", {ptype, value}, ...) — the listener.print
// convention (segments render as clickable presentations in the listener).
func (m *Module) jsPrint(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		cl := m.requireClient(vm)
		args := make([]interface{}, 0, len(call.Arguments))
		for _, a := range call.Arguments {
			args = append(args, exportArg(a))
		}
		segs, err := jsmod.SegsToWire(args)
		if err != nil {
			panic(vm.ToValue(err.Error()))
		}
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		if err := cl.Emit(ctx, "listener.print", map[string]interface{}{"segs": segs}); err != nil {
			panic(vm.ToValue(fmt.Sprintf("pbui.print: %v", err)))
		}
		return goja.Undefined()
	}
}

// jsHover: pbui.hover(text) — fire-and-forget mouse-doc line.
func (m *Module) jsHover(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		cl := m.requireClient(vm)
		if err := cl.Hover(call.Argument(0).String()); err != nil {
			panic(vm.ToValue(fmt.Sprintf("pbui.hover: %v", err)))
		}
		return goja.Undefined()
	}
}

// mustSet panics (programmer error) if an export cannot be installed.
func mustSet(exports *goja.Object, name string, v interface{}) {
	if err := exports.Set(name, v); err != nil {
		panic(fmt.Errorf("pbuimod: set %s: %w", name, err))
	}
}

// exportArg converts a goja value to plain Go data (maps/slices/scalars).
func exportArg(v goja.Value) interface{} {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil
	}
	return v.Export()
}

// ptypesArg accepts "color" or ["color","file"].
func ptypesArg(v goja.Value) []string {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil
	}
	switch t := v.Export().(type) {
	case string:
		return []string{t}
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
