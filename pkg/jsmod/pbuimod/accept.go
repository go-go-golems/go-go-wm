package pbuimod

import (
	"context"
	"fmt"

	"github.com/dop251/goja"

	"github.com/go-go-golems/go-go-goja/pkg/runtimebridge"
	"github.com/go-go-golems/go-go-wm/pkg/jsmod"
)

// jsAccept: pbui.accept(ptypes, prompt?) → Promise<object|null>.
//
// The canonical promise settlement pattern (fs_async.go shape): the
// promise is created on the VM thread, the blocking broker call runs in a
// worker, and resolve/reject fire only from closures posted back onto the
// owner loop. Cancellation resolves null — it is a normal outcome, not an
// error.
func (m *Module) jsAccept(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		cl := m.requireClient(vm)
		ptypes := ptypesArg(call.Argument(0))
		if len(ptypes) == 0 {
			panic(vm.ToValue("pbui.accept: ptypes must be a string or non-empty array"))
		}
		prompt := ""
		if a := call.Argument(1); !goja.IsUndefined(a) && !goja.IsNull(a) {
			prompt = a.String()
		}

		promise, resolve, reject := vm.NewPromise()
		services, ok := runtimebridge.Lookup(vm)
		if !ok {
			panic(vm.ToValue("pbui.accept: no runtime services (not running under the engine factory)"))
		}
		callCtx := services.Lifetime()
		go func() {
			obj, err := cl.Accept(callCtx, ptypes, prompt)
			if err != nil {
				_ = services.PostWithCustomContext(callCtx, "pbui.accept.reject",
					func(_ context.Context, vm *goja.Runtime) { _ = reject(vm.ToValue(err.Error())) })
				return
			}
			_ = services.PostWithCustomContext(callCtx, "pbui.accept.resolve",
				func(_ context.Context, vm *goja.Runtime) {
					if obj == nil {
						_ = resolve(goja.Null())
						return
					}
					_ = resolve(vm.ToValue(jsmod.ObjectToJS(obj)))
				})
		}()
		return vm.ToValue(promise)
	}
}

// jsAnswer: pbui.answer(session, obj) — resolve someone else's accept.
// Empty session answers whatever session is pending.
func (m *Module) jsAnswer(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		cl := m.requireClient(vm)
		session := call.Argument(0).String()
		obj, err := jsmod.JSToObject(exportArg(call.Argument(1)))
		if err != nil {
			panic(vm.ToValue(fmt.Sprintf("pbui.answer: %v", err)))
		}
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		if err := cl.Answer(ctx, session, obj); err != nil {
			panic(vm.ToValue(fmt.Sprintf("pbui.answer: %v", err)))
		}
		return goja.Undefined()
	}
}

// jsCancel: pbui.cancel(session?) — Escape.
func (m *Module) jsCancel(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		cl := m.requireClient(vm)
		session := ""
		if a := call.Argument(0); !goja.IsUndefined(a) && !goja.IsNull(a) {
			session = a.String()
		}
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		if err := cl.Cancel(ctx, session); err != nil {
			panic(vm.ToValue(fmt.Sprintf("pbui.cancel: %v", err)))
		}
		return goja.Undefined()
	}
}

// jsMenu: pbui.menu(obj, x?, y?) → Promise<verb[]> — ask the broker which
// verbs apply (and let the WM show its menu when one is running).
func (m *Module) jsMenu(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		cl := m.requireClient(vm)
		obj, err := jsmod.JSToObject(exportArg(call.Argument(0)))
		if err != nil {
			panic(vm.ToValue(fmt.Sprintf("pbui.menu: %v", err)))
		}
		x := int(call.Argument(1).ToInteger())
		y := int(call.Argument(2).ToInteger())

		promise, resolve, reject := vm.NewPromise()
		services, ok := runtimebridge.Lookup(vm)
		if !ok {
			panic(vm.ToValue("pbui.menu: no runtime services"))
		}
		callCtx := services.Lifetime()
		go func() {
			verbs, err := cl.RequestMenu(callCtx, obj, x, y)
			if err != nil {
				_ = services.PostWithCustomContext(callCtx, "pbui.menu.reject",
					func(_ context.Context, vm *goja.Runtime) { _ = reject(vm.ToValue(err.Error())) })
				return
			}
			_ = services.PostWithCustomContext(callCtx, "pbui.menu.resolve",
				func(_ context.Context, vm *goja.Runtime) {
					out := make([]map[string]interface{}, 0, len(verbs))
					for _, v := range verbs {
						out = append(out, map[string]interface{}{
							"id": v.ID, "label": v.Label, "ptypes": v.Ptypes,
							"accepts": v.Accepts, "owner": v.Owner,
						})
					}
					_ = resolve(vm.ToValue(out))
				})
		}()
		return vm.ToValue(promise)
	}
}
