package uimod

import (
	"context"
	"fmt"
	"image"
	"strings"
	"sync"

	"github.com/dop251/goja"
	"github.com/go-go-golems/go-go-goja/pkg/runtimebridge"

	"github.com/go-go-golems/go-go-wm/pkg/apps"
	"github.com/go-go-golems/go-go-wm/pkg/apps/uispec"
	"github.com/go-go-golems/go-go-wm/pkg/apps/xapp"
	"github.com/go-go-golems/go-go-wm/pkg/jsmod"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
)

// jsAppState is one ui.app definition plus its live snapshot. The
// snapshot side (Render, tile render closures) is pure Go; the JS side
// (handlers, render) runs only via posted closures.
type jsAppState struct {
	name  string
	title string
	verbs []pbui.Verb

	// VM-owned — touch only on the JS loop.
	renderFn goja.Callable
	actions  map[string]goja.Callable
	verbFns  map[string]goja.Callable
	onKey    goja.Callable

	services runtimebridge.RuntimeServices

	mu     sync.Mutex
	rows   uispec.Spec
	redraw func() // posts a repaint to the render side; may be nil early

	shown bool
	tiled bool
}

// jsApp: ui.app({name, title, render, actions?, verbs?, onKey?}).
func (m *Module) jsApp(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		def, ok := call.Argument(0).(*goja.Object)
		if !ok {
			panic(vm.ToValue("ui.app: definition must be an object"))
		}
		services, sok := runtimebridge.Lookup(vm)
		if !sok {
			panic(vm.ToValue("ui.app: no runtime services"))
		}

		a := &jsAppState{
			name:     stringProp(def, "name"),
			title:    stringProp(def, "title"),
			actions:  map[string]goja.Callable{},
			verbFns:  map[string]goja.Callable{},
			services: services,
		}
		if a.name == "" {
			panic(vm.ToValue("ui.app: name is required (it is the broker client name)"))
		}
		if a.title == "" {
			a.title = strings.ToUpper(a.name)
		}
		if fn, ok := goja.AssertFunction(def.Get("render")); ok {
			a.renderFn = fn
		} else {
			panic(vm.ToValue("ui.app: render must be a function returning rows"))
		}
		if acts, ok := def.Get("actions").(*goja.Object); ok {
			for _, k := range acts.Keys() {
				if fn, ok := goja.AssertFunction(acts.Get(k)); ok {
					a.actions[k] = fn
				} else {
					panic(vm.ToValue("ui.app: actions." + k + " must be a function"))
				}
			}
		}
		if verbsVal := def.Get("verbs"); verbsVal != nil && !goja.IsUndefined(verbsVal) && !goja.IsNull(verbsVal) {
			verbsObj, ok := verbsVal.(*goja.Object)
			if !ok {
				panic(vm.ToValue("ui.app: verbs must be an array"))
			}
			n := int(verbsObj.Get("length").ToInteger())
			for i := 0; i < n; i++ {
				vd, ok := verbsObj.Get(fmt.Sprint(i)).(*goja.Object)
				if !ok {
					panic(vm.ToValue("ui.app: verbs entries must be objects"))
				}
				verb := pbui.Verb{
					ID:      stringProp(vd, "id"),
					Label:   stringProp(vd, "label"),
					Ptypes:  stringsProp(vd, "ptypes"),
					Accepts: stringsProp(vd, "accepts"),
				}
				if verb.ID == "" || len(verb.Ptypes) == 0 {
					panic(vm.ToValue("ui.app: verbs need id and ptypes"))
				}
				if verb.Label == "" {
					verb.Label = verb.ID
				}
				fn, ok := goja.AssertFunction(vd.Get("run"))
				if !ok {
					panic(vm.ToValue("ui.app: verb " + verb.ID + " needs a run function"))
				}
				a.verbs = append(a.verbs, verb)
				a.verbFns[verb.ID] = fn
			}
		}
		if fn, ok := goja.AssertFunction(def.Get("onKey")); ok {
			a.onKey = fn
		}

		// First snapshot, so show()/tile() paint content immediately.
		// We are on the JS loop here, so calling render is legal.
		if err := a.rerenderOnLoop(vm); err != nil {
			panic(vm.ToValue("ui.app: initial render: " + err.Error()))
		}

		handle := vm.NewObject()
		mustSet(handle, "name", a.name)
		mustSet(handle, "show", func(goja.FunctionCall) goja.Value {
			if a.shown {
				return goja.Undefined()
			}
			a.shown = true
			go func() {
				err := xapp.Run(services.Lifetime(), m.opts.Display, m.opts.BrokerSocket, &jsXApp{a: a})
				if err != nil {
					jsmod.EmitScriptError(nil, "ui.show:"+a.name, err, nil)
				}
			}()
			return goja.Undefined()
		})
		mustSet(handle, "tile", func(goja.FunctionCall) goja.Value {
			if m.opts.TileHost == nil {
				panic(vm.ToValue("ui.tile: script tiles require the in-process runtime; put this in rc.js (go-go-wm wm --rc)"))
			}
			tileApp := "script:" + a.name
			if a.tiled {
				return vm.ToValue(tileApp)
			}
			err := m.opts.TileHost.RegisterTile(a.name,
				func(w, h int, accepting []string) (*image.RGBA, []apps.Region) {
					a.mu.Lock()
					rows := a.rows
					a.mu.Unlock()
					img, regions := uispec.Render(w, h, rows, accepting)
					return img, regions
				},
				func(action string) { a.dispatchAction(action, nil) },
				func(key string) { a.dispatchKey(key) },
			)
			if err != nil {
				panic(vm.ToValue("ui.tile: " + err.Error()))
			}
			a.tiled = true
			host := m.opts.TileHost
			name := a.name
			a.setRedraw(func() { host.RepaintTile(name) })
			return vm.ToValue(tileApp)
		})
		mustSet(handle, "refresh", func(goja.FunctionCall) goja.Value {
			// On the JS loop already; re-render and repaint.
			if err := a.rerenderOnLoop(vm); err != nil {
				panic(vm.ToValue("ui.refresh: " + err.Error()))
			}
			a.postRedraw()
			return goja.Undefined()
		})
		return handle
	}
}

// rerenderOnLoop runs render() and swaps the snapshot. JS loop only.
func (a *jsAppState) rerenderOnLoop(vm *goja.Runtime) error {
	out, err := a.renderFn(goja.Undefined())
	if err != nil {
		return err
	}
	rows, err := uispec.Normalize(out.Export())
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.rows = rows
	a.mu.Unlock()
	return nil
}

func (a *jsAppState) setRedraw(fn func()) {
	a.mu.Lock()
	a.redraw = fn
	a.mu.Unlock()
}

func (a *jsAppState) postRedraw() {
	a.mu.Lock()
	fn := a.redraw
	a.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// dispatchAction posts a handler run + re-render to the JS loop. Safe
// from any goroutine (xapp loop, WM loop).
func (a *jsAppState) dispatchAction(action string, obj *pbui.Object) {
	_ = a.services.PostWithLifetimeContext("ui.action:"+action,
		func(_ context.Context, vm *goja.Runtime) {
			var fn goja.Callable
			arg := goja.Undefined()
			if obj != nil {
				fn = a.verbFns[action]
				arg = vm.ToValue(jsmod.ObjectToJS(obj))
			} else {
				fn = a.actions[action]
			}
			if fn == nil {
				return
			}
			if _, err := fn(goja.Undefined(), arg); err != nil {
				jsmod.EmitScriptError(nil, "ui:"+a.name+":"+action, err, nil)
				return
			}
			if err := a.rerenderOnLoop(vm); err != nil {
				jsmod.EmitScriptError(nil, "ui:"+a.name+":render", err, nil)
				return // keep the previous snapshot on screen
			}
			a.postRedraw()
		})
}

func (a *jsAppState) dispatchKey(key string) {
	_ = a.services.PostWithLifetimeContext("ui.key:"+a.name,
		func(_ context.Context, vm *goja.Runtime) {
			if a.onKey == nil {
				return
			}
			if _, err := a.onKey(goja.Undefined(), vm.ToValue(key)); err != nil {
				jsmod.EmitScriptError(nil, "ui:"+a.name+":onKey", err, nil)
				return
			}
			if err := a.rerenderOnLoop(vm); err != nil {
				jsmod.EmitScriptError(nil, "ui:"+a.name+":render", err, nil)
				return
			}
			a.postRedraw()
		})
}

// jsXApp adapts a jsAppState to the xapp shell. Everything here runs on
// the xapp loop and is VM-free.
type jsXApp struct {
	a *jsAppState
}

func (x *jsXApp) Name() string       { return x.a.name }
func (x *jsXApp) Title() string      { return x.a.title }
func (x *jsXApp) Verbs() []pbui.Verb { return x.a.verbs }

func (x *jsXApp) Render(w, h int, accepting []string) (*image.RGBA, []apps.Region) {
	x.a.mu.Lock()
	rows := x.a.rows
	x.a.mu.Unlock()
	return uispec.Render(w, h, rows, accepting)
}

// Started hands the app its redraw hook (design: Starter extension).
func (x *jsXApp) Started(ctx xapp.Ctx) {
	x.a.setRedraw(func() { ctx.Post(ctx.Redraw) })
}

func (x *jsXApp) HandleAction(_ xapp.Ctx, action string) {
	x.a.dispatchAction(strings.TrimPrefix(action, "cmd:"), nil)
}

func (x *jsXApp) HandleVerb(_ xapp.Ctx, verbID string, obj *pbui.Object) {
	x.a.dispatchAction(verbID, obj)
}

func (x *jsXApp) HandleKey(_ xapp.Ctx, key string) {
	x.a.dispatchKey(key)
}

func mustSet(o *goja.Object, name string, v interface{}) {
	if err := o.Set(name, v); err != nil {
		panic(fmt.Errorf("uimod: set %s: %w", name, err))
	}
}

func stringProp(o *goja.Object, name string) string {
	v := o.Get(name)
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return ""
	}
	s, _ := v.Export().(string)
	return s
}

func stringsProp(o *goja.Object, name string) []string {
	v := o.Get(name)
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
