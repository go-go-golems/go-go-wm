// Package uimod is the `ui` native module (GGWM-003): scripts define
// full PBUI application surfaces as declarative specs; Go normalizes,
// renders (pkg/apps/uispec), and wires them into the standard Region
// click contract — as standalone X windows (app.show, the xapp shell)
// or WM-embedded script tiles (app.tile, rc.js only).
//
// Concurrency: render paths never call JS. Handlers run on the JS loop;
// after each handler the module re-runs render(), normalizes the result,
// and posts the snapshot to the render side (design decision U-D2).
package uimod

import (
	"fmt"
	"image"
	"sync"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/require"

	"github.com/go-go-golems/go-go-wm/pkg/apps"
	"github.com/go-go-golems/go-go-wm/pkg/apps/uispec"
)

// ModuleName is what scripts require().
const ModuleName = "ui"

// TileHost is what the in-process runtime provides for app.tile():
// registering a renderer under "script:<name>" and repainting it.
// Render/action/key callbacks must be VM-free (they close over
// snapshots); the host calls them from the WM loop.
type TileHost interface {
	RegisterTile(name string,
		render func(w, h int, accepting []string) (*image.RGBA, []apps.Region),
		action func(action string),
		key func(key string)) error
	RepaintTile(name string)
}

// Options configures the module for its attachment point.
type Options struct {
	Display      string // X display for app.show ("" → $DISPLAY)
	BrokerSocket string // broker socket for app.show shells
	TileHost     TileHost
}

// Module binds one runtime's ui exports.
type Module struct {
	opts Options

	mu   sync.Mutex
	apps []*jsAppState // live apps, for cross-cutting repaints (themes)
}

// New creates the module.
func New(opts Options) *Module { return &Module{opts: opts} }

// Retheme repaints every live app surface. Callers swap the palette
// first (draw.SetTheme); render paths pick the new colors up on the next
// paint, so a posted redraw per app is the whole re-theme.
func (m *Module) Retheme() {
	m.mu.Lock()
	apps := append([]*jsAppState(nil), m.apps...)
	m.mu.Unlock()
	for _, a := range apps {
		a.postRedraw()
	}
}

func (m *Module) trackApp(a *jsAppState) {
	m.mu.Lock()
	m.apps = append(m.apps, a)
	m.mu.Unlock()
}

// Loader installs the exports.
func (m *Module) Loader() require.ModuleLoader {
	return func(vm *goja.Runtime, module *goja.Object) {
		exports := module.Get("exports").(*goja.Object)
		set := func(name string, v interface{}) {
			if err := exports.Set(name, v); err != nil {
				panic(fmt.Errorf("uimod: set %s: %w", name, err))
			}
		}

		// ---- data-only spec builders ---------------------------------
		// These return plain objects; Normalize validates them later, so
		// builders stay permissive and cheap.
		set("row", func(call goja.FunctionCall) goja.Value {
			segs := make([]interface{}, 0, len(call.Arguments))
			for _, a := range call.Arguments {
				segs = append(segs, a.Export())
			}
			return vm.ToValue(segs)
		})
		set("text", func(call goja.FunctionCall) goja.Value {
			seg := map[string]interface{}{"kind": uispec.KindText, "text": call.Argument(0).String()}
			mergeOpts(seg, call.Argument(1))
			return vm.ToValue(seg)
		})
		set("hint", func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(map[string]interface{}{"kind": uispec.KindHint, "text": call.Argument(0).String()})
		})
		set("object", func(call goja.FunctionCall) goja.Value {
			seg := map[string]interface{}{
				"kind":  uispec.KindObject,
				"ptype": call.Argument(0).String(),
				"value": call.Argument(1).Export(),
			}
			mergeOpts(seg, call.Argument(2))
			return vm.ToValue(seg)
		})
		set("button", func(call goja.FunctionCall) goja.Value {
			seg := map[string]interface{}{
				"kind":   uispec.KindButton,
				"text":   call.Argument(0).String(),
				"action": call.Argument(1).String(),
			}
			mergeOpts(seg, call.Argument(2))
			return vm.ToValue(seg)
		})

		// ---- ui.app(def) ---------------------------------------------
		set("app", m.jsApp(vm))
	}
}

// mergeOpts copies an options object ({bold, size, label, doc, color})
// into seg. Unknown keys survive to Normalize, which rejects them with a
// good error.
func mergeOpts(seg map[string]interface{}, v goja.Value) {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return
	}
	if m, ok := v.Export().(map[string]interface{}); ok {
		for k, val := range m {
			seg[k] = val
		}
	}
}
