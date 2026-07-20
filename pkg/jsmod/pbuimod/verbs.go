package pbuimod

import (
	"context"
	"fmt"

	"github.com/dop251/goja"
	"github.com/go-go-golems/go-go-goja/pkg/runtimebridge"

	"github.com/go-go-golems/go-go-wm/pkg/jsmod"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/client"
)

// jsVerb: pbui.verb({id, label?, ptypes, accepts?}, handler).
//
// The descriptor is validated at definition time (normalize-then-fail-
// early); the broker holds one verb set per client, so each registration
// re-sends the module's accumulated set. Re-registering an id replaces
// both descriptor and handler.
func (m *Module) jsVerb(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		cl := m.requireClient(vm)
		desc, ok := exportArg(call.Argument(0)).(map[string]interface{})
		if !ok {
			panic(vm.ToValue("pbui.verb: descriptor must be {id, label, ptypes, accepts?}"))
		}
		handler, ok := goja.AssertFunction(call.Argument(1))
		if !ok {
			panic(vm.ToValue("pbui.verb: second argument must be a function"))
		}

		id, _ := desc["id"].(string)
		if id == "" {
			panic(vm.ToValue("pbui.verb: id must be a non-empty string"))
		}
		v := pbui.Verb{ID: id, Ptypes: toStrings(desc["ptypes"]), Accepts: toStrings(desc["accepts"])}
		if len(v.Ptypes) == 0 {
			panic(vm.ToValue("pbui.verb: ptypes must be a string or non-empty array (use \"any\" for all)"))
		}
		if label, _ := desc["label"].(string); label != "" {
			v.Label = label
		} else {
			v.Label = id
		}

		m.mu.Lock()
		m.verbs[id] = v
		m.handlers[id] = handler
		verbs := make([]pbui.Verb, 0, len(m.verbs))
		for _, vv := range m.verbs {
			verbs = append(verbs, vv)
		}
		m.mu.Unlock()

		m.ensureVerbDispatch(vm)

		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		if err := cl.RegisterVerbs(ctx, verbs); err != nil {
			panic(vm.ToValue(fmt.Sprintf("pbui.verb: register: %v", err)))
		}
		return goja.Undefined()
	}
}

// ensureVerbDispatch installs the OnVerbRun → owner-loop bridge once.
// The callback fires on the client's read goroutine; its body is a single
// post (concurrency rule 6).
func (m *Module) ensureVerbDispatch(vm *goja.Runtime) {
	services, ok := runtimebridge.Lookup(vm)
	if !ok {
		panic(vm.ToValue("pbui.verb: no runtime services"))
	}
	m.dispatchOnce.Do(func() {
		cl := m.cl
		m.cl.OnVerbRun(func(verbID string, obj *pbui.Object) {
			_ = services.PostWithLifetimeContext("pbui.verb."+verbID,
				func(_ context.Context, vm *goja.Runtime) {
					m.mu.Lock()
					fn := m.handlers[verbID]
					m.mu.Unlock()
					if fn == nil {
						return
					}
					if _, err := fn(goja.Undefined(), vm.ToValue(jsmod.ObjectToJS(obj))); err != nil {
						jsmod.EmitScriptError(cl, "verb:"+verbID, err, nil)
					}
				})
		})
	})
}

// jsOnAcceptMode / jsOnAcceptClear: observe the desktop-wide accept
// session (this is how a script answers someone else's accept — it learns
// the session id here and calls pbui.answer).
func (m *Module) jsOnAcceptMode(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		cl := m.requireClient(vm)
		fn, ok := goja.AssertFunction(call.Argument(0))
		if !ok {
			panic(vm.ToValue("pbui.onAcceptMode: argument must be a function"))
		}
		services, sok := runtimebridge.Lookup(vm)
		if !sok {
			panic(vm.ToValue("pbui.onAcceptMode: no runtime services"))
		}
		m.mu.Lock()
		m.acceptFns = append(m.acceptFns, fn)
		m.mu.Unlock()
		m.ensureAcceptDispatch(cl, services)
		return goja.Undefined()
	}
}

func (m *Module) jsOnAcceptClear(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		cl := m.requireClient(vm)
		fn, ok := goja.AssertFunction(call.Argument(0))
		if !ok {
			panic(vm.ToValue("pbui.onAcceptClear: argument must be a function"))
		}
		services, sok := runtimebridge.Lookup(vm)
		if !sok {
			panic(vm.ToValue("pbui.onAcceptClear: no runtime services"))
		}
		m.mu.Lock()
		m.clearFns = append(m.clearFns, fn)
		m.mu.Unlock()
		m.ensureAcceptDispatch(cl, services)
		return goja.Undefined()
	}
}

// ensureAcceptDispatch installs the accept.mode/clear read-goroutine →
// owner-loop bridges once, whichever subscription arrives first.
func (m *Module) ensureAcceptDispatch(cl *client.Client, services runtimebridge.RuntimeServices) {
	m.acceptOnce.Do(func() {
		cl.OnAcceptMode(func(session string, ptypes []string, prompt string) {
			_ = services.PostWithLifetimeContext("pbui.acceptMode",
				func(_ context.Context, vm *goja.Runtime) {
					arg := vm.ToValue(map[string]interface{}{
						"session": session, "ptypes": ptypes, "prompt": prompt,
					})
					m.mu.Lock()
					fns := append([]goja.Callable(nil), m.acceptFns...)
					m.mu.Unlock()
					for _, f := range fns {
						if _, err := f(goja.Undefined(), arg); err != nil {
							jsmod.EmitScriptError(cl, "onAcceptMode", err, nil)
						}
					}
				})
		})
		cl.OnAcceptClear(func(session, reason string) {
			_ = services.PostWithLifetimeContext("pbui.acceptClear",
				func(_ context.Context, vm *goja.Runtime) {
					arg := vm.ToValue(map[string]interface{}{"session": session, "reason": reason})
					m.mu.Lock()
					fns := append([]goja.Callable(nil), m.clearFns...)
					m.mu.Unlock()
					for _, f := range fns {
						if _, err := f(goja.Undefined(), arg); err != nil {
							jsmod.EmitScriptError(cl, "onAcceptClear", err, nil)
						}
					}
				})
		})
	})
}

func toStrings(v interface{}) []string {
	switch t := v.(type) {
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
