package wmmod

import (
	"context"
	"fmt"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/require"

	"github.com/go-go-golems/go-go-wm/pkg/jsmod"
	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
)

// ModuleName is what scripts require().
const ModuleName = "wm"

// queryTimeout bounds every backend call made from the JS loop. Queries
// and mutations are synchronous from the script's point of view (a local
// socket round trip; promises here would make every rc.js line an await
// for no benefit) but never unbounded — a stuck WM throws, not hangs.
const queryTimeout = 2 * time.Second

// Module binds a Backend (and the shared event fan) to one goja runtime.
type Module struct {
	backend Backend
	fan     *jsmod.EventFan // nil → wm.on unavailable
}

// New creates the module. fan may be nil (no event subscriptions).
func New(backend Backend, fan *jsmod.EventFan) *Module {
	return &Module{backend: backend, fan: fan}
}

func (m *Module) call(vm *goja.Runtime, what string, fn func(ctx context.Context) (interface{}, error)) goja.Value {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()
	out, err := fn(ctx)
	if err != nil {
		panic(vm.ToValue(fmt.Sprintf("wm.%s: %v", what, err)))
	}
	if out == nil {
		return goja.Undefined()
	}
	return vm.ToValue(out)
}

// apply is the shared mutation path: everything goes through one Op.
func (m *Module) apply(vm *goja.Runtime, what string, op wmcore.Op) wmcore.Result {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()
	res, err := m.backend.Apply(ctx, op)
	if err != nil {
		panic(vm.ToValue(fmt.Sprintf("wm.%s: %v", what, err)))
	}
	return res
}

func (m *Module) tree(vm *goja.Runtime) *wmcore.Desktop {
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()
	d, err := m.backend.Tree(ctx)
	if err != nil {
		panic(vm.ToValue(fmt.Sprintf("wm.tree: %v", err)))
	}
	return d
}

// Loader returns the require.ModuleLoader installing the exports.
func (m *Module) Loader() require.ModuleLoader {
	return func(vm *goja.Runtime, module *goja.Object) {
		exports := module.Get("exports").(*goja.Object)
		set := func(name string, v interface{}) {
			if err := exports.Set(name, v); err != nil {
				panic(fmt.Errorf("wmmod: set %s: %w", name, err))
			}
		}

		// ---- queries -------------------------------------------------
		set("tree", func(call goja.FunctionCall) goja.Value {
			return m.call(vm, "tree", func(ctx context.Context) (interface{}, error) {
				d, err := m.backend.Tree(ctx)
				if err != nil {
					return nil, err
				}
				return jsmod.ToPlain(d) // wire field names, not Go names
			})
		})
		set("windows", func(call goja.FunctionCall) goja.Value {
			return m.call(vm, "windows", func(ctx context.Context) (interface{}, error) {
				wins, err := m.backend.Windows(ctx)
				if err != nil {
					return nil, err
				}
				return jsmod.ToPlain(wins)
			})
		})
		set("focused", func(call goja.FunctionCall) goja.Value {
			return m.call(vm, "focused", func(ctx context.Context) (interface{}, error) {
				wins, err := m.backend.Windows(ctx)
				if err != nil {
					return nil, err
				}
				for _, w := range wins {
					if w.Focused {
						return w.Leaf, nil
					}
				}
				return nil, nil
			})
		})
		// leaves(workspace?) → [{id, app}] in layout order.
		set("leaves", func(call goja.FunctionCall) goja.Value {
			d := m.tree(vm)
			wsID := d.Current
			if a := call.Argument(0); !goja.IsUndefined(a) && !goja.IsNull(a) && a.String() != "" {
				wsID = a.String()
			}
			ws := d.WorkspaceByID(wsID)
			if ws == nil {
				panic(vm.ToValue("wm.leaves: no workspace " + wsID))
			}
			var out []map[string]interface{}
			var walk func(n *wmcore.Node)
			walk = func(n *wmcore.Node) {
				if n == nil {
					return
				}
				if n.Kind == wmcore.Leaf {
					out = append(out, map[string]interface{}{"id": string(n.ID), "app": n.App})
					return
				}
				walk(n.A)
				walk(n.B)
			}
			walk(ws.Root)
			return vm.ToValue(out)
		})

		// ---- mutations (all compile to Ops) --------------------------
		// apply(op): the generic escape hatch — any raw op object.
		set("apply", func(call goja.FunctionCall) goja.Value {
			op, err := jsmod.OpFromJS(call.Argument(0).Export())
			if err != nil {
				panic(vm.ToValue("wm.apply: " + err.Error()))
			}
			res := m.apply(vm, "apply", op)
			plain, err := jsmod.ToPlain(res)
			if err != nil {
				panic(vm.ToValue("wm.apply: " + err.Error()))
			}
			return vm.ToValue(plain)
		})
		// split(leaf, dir?, {ratio?, app?}) → new leaf id.
		set("split", func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(m.split(vm, call))
		})
		set("close", func(call goja.FunctionCall) goja.Value {
			m.apply(vm, "close", wmcore.Op{Op: wmcore.OpCloseLeaf, Node: nodeArg(vm, call, 0, "wm.close: leaf id")})
			return goja.Undefined()
		})
		set("setApp", func(call goja.FunctionCall) goja.Value {
			m.apply(vm, "setApp", wmcore.Op{
				Op: wmcore.OpSetLeafApp, Node: nodeArg(vm, call, 0, "wm.setApp: leaf id"),
				App: call.Argument(1).String(),
			})
			return goja.Undefined()
		})
		set("setRatio", func(call goja.FunctionCall) goja.Value {
			m.apply(vm, "setRatio", wmcore.Op{
				Op: wmcore.OpSetRatio, Node: nodeArg(vm, call, 0, "wm.setRatio: split id"),
				Ratio: call.Argument(1).ToFloat(),
			})
			return goja.Undefined()
		})
		set("swap", func(call goja.FunctionCall) goja.Value {
			m.apply(vm, "swap", wmcore.Op{
				Op:   wmcore.OpSwapLeaves,
				Node: nodeArg(vm, call, 0, "wm.swap: leaf a"), Target: nodeArg(vm, call, 1, "wm.swap: leaf b"),
			})
			return goja.Undefined()
		})
		set("moveSplit", func(call goja.FunctionCall) goja.Value {
			m.apply(vm, "moveSplit", wmcore.Op{
				Op:   wmcore.OpMoveSplit,
				Node: nodeArg(vm, call, 0, "wm.moveSplit: from"), Target: nodeArg(vm, call, 1, "wm.moveSplit: target"),
				Zone: wmcore.Zone(call.Argument(2).String()),
			})
			return goja.Undefined()
		})
		// moveLeaf(leaf, workspaceId, {target?, dir?})
		set("moveLeaf", func(call goja.FunctionCall) goja.Value {
			op := wmcore.Op{
				Op: wmcore.OpMoveLeaf, Node: nodeArg(vm, call, 0, "wm.moveLeaf: leaf"),
				Workspace: call.Argument(1).String(),
			}
			if opts, ok := call.Argument(2).Export().(map[string]interface{}); ok {
				if t, _ := opts["target"].(string); t != "" {
					op.Target = wmcore.NodeID(t)
				}
				if dir, _ := opts["dir"].(string); dir != "" {
					op.Dir = wmcore.Dir(dir)
				}
			}
			m.apply(vm, "moveLeaf", op)
			return goja.Undefined()
		})

		// workspace(name) → fluent handle (find by name/id, create if absent).
		set("workspace", func(call goja.FunctionCall) goja.Value {
			return m.workspaceObj(vm, call.Argument(0).String())
		})

		// ---- events and keys -----------------------------------------
		set("on", func(call goja.FunctionCall) goja.Value {
			if m.fan == nil {
				panic(vm.ToValue("wm.on: no broker connection (run without --no-broker)"))
			}
			event := call.Argument(0).String()
			fn, ok := goja.AssertFunction(call.Argument(1))
			if !ok {
				panic(vm.ToValue("wm.on: second argument must be a function"))
			}
			if err := m.fan.Subscribe(vm, event, "wm.on", fn); err != nil {
				panic(vm.ToValue("wm.on: subscribe: " + err.Error()))
			}
			return goja.Undefined()
		})
		set("bind", m.jsBind(vm))
	}
}

// jsBind: wm.bind(combo, fn) — in-process runtimes only; the IPC backend
// throws ErrNoKeybindings with the rc.js pointer.
func (m *Module) jsBind(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		combo := call.Argument(0).String()
		fn, ok := goja.AssertFunction(call.Argument(1))
		if !ok {
			panic(vm.ToValue("wm.bind: second argument must be a function"))
		}
		services, sok := runtimebridgeLookup(vm)
		if !sok {
			panic(vm.ToValue("wm.bind: no runtime services"))
		}
		err := m.backend.Bind(combo, func() {
			// Fired on the WM loop (or X callback): a single post, never
			// JS execution here (concurrency rule 2).
			_ = services.PostWithLifetimeContext("wm.bind:"+combo,
				func(_ context.Context, vm *goja.Runtime) {
					if _, err := fn(goja.Undefined()); err != nil {
						jsmod.EmitScriptError(nil, "wm.bind:"+combo, err, nil)
					}
				})
		})
		if err != nil {
			panic(vm.ToValue("wm.bind: " + err.Error()))
		}
		return goja.Undefined()
	}
}

func nodeArg(vm *goja.Runtime, call goja.FunctionCall, i int, what string) wmcore.NodeID {
	a := call.Argument(i)
	if goja.IsUndefined(a) || goja.IsNull(a) || a.String() == "" {
		panic(vm.ToValue(what + " must be a node id"))
	}
	return wmcore.NodeID(a.String())
}
