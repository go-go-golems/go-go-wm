package wmmod

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
	backend   Backend
	fan       *jsmod.EventFan // nil → wm.on and rules unavailable
	ruleState *ruleState

	execAllowed bool
	execDisplay string
}

// Option configures the module.
type Option func(*Module)

// WithExec enables wm.exec(cmdline): `sh -c` in this process,
// fire-and-forget, with DISPLAY forced to display when non-empty. The
// rc.js runtime enables it unconditionally (an rc file is exactly as
// trusted as an i3 config); run/repl gate it behind --allow-exec.
func WithExec(display string) Option {
	return func(m *Module) { m.execAllowed = true; m.execDisplay = display }
}

// New creates the module. fan may be nil (no event subscriptions).
func New(backend Backend, fan *jsmod.EventFan, opts ...Option) *Module {
	m := &Module{backend: backend, fan: fan, ruleState: newRuleState()}
	for _, o := range opts {
		o(m)
	}
	return m
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
		// apply([op, ...]): a batch — one WM reconcile for the burst
		// (GGWM-006; workspace pre-creation went from 17 full repaints
		// to 2 this way).
		set("apply", func(call goja.FunctionCall) goja.Value {
			raw := call.Argument(0).Export()
			if arr, ok := raw.([]interface{}); ok {
				ops := make([]wmcore.Op, 0, len(arr))
				for i, e := range arr {
					op, err := jsmod.OpFromJS(e)
					if err != nil {
						panic(vm.ToValue(fmt.Sprintf("wm.apply[%d]: %v", i, err)))
					}
					ops = append(ops, op)
				}
				ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
				defer cancel()
				results, err := m.backend.ApplyBatch(ctx, ops)
				if err != nil {
					panic(vm.ToValue("wm.apply: " + err.Error()))
				}
				plain, perr := jsmod.ToPlain(results)
				if perr != nil {
					panic(vm.ToValue("wm.apply: " + perr.Error()))
				}
				return vm.ToValue(plain)
			}
			op, err := jsmod.OpFromJS(raw)
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

		// ---- themes, focus, exec (GGWM-004) --------------------------
		// theme() → current name; theme(name) → switch (repaints, emits
		// theme.changed).
		set("theme", func(call goja.FunctionCall) goja.Value {
			a := call.Argument(0)
			if goja.IsUndefined(a) || goja.IsNull(a) || a.String() == "" {
				return m.call(vm, "theme", func(ctx context.Context) (interface{}, error) {
					info, err := m.backend.Theme(ctx)
					if err != nil {
						return nil, err
					}
					return info.Theme, nil
				})
			}
			name := a.String()
			return m.call(vm, "theme", func(ctx context.Context) (interface{}, error) {
				return name, m.backend.SetTheme(ctx, name)
			})
		})
		set("themes", func(call goja.FunctionCall) goja.Value {
			return m.call(vm, "themes", func(ctx context.Context) (interface{}, error) {
				info, err := m.backend.Theme(ctx)
				if err != nil {
					return nil, err
				}
				return info.Available, nil
			})
		})
		// focus(target): leaf id | left|right|up|down|next|prev.
		set("focus", func(call goja.FunctionCall) goja.Value {
			target := call.Argument(0).String()
			if target == "" || goja.IsUndefined(call.Argument(0)) {
				panic(vm.ToValue("wm.focus: target must be a leaf id or left|right|up|down|next|prev"))
			}
			return m.call(vm, "focus", func(ctx context.Context) (interface{}, error) {
				return m.backend.Focus(ctx, target)
			})
		})
		// move(dir): swap the focused leaf with its geometric neighbor.
		set("move", func(call goja.FunctionCall) goja.Value {
			dir := call.Argument(0).String()
			return m.call(vm, "move", func(ctx context.Context) (interface{}, error) {
				return m.backend.Move(ctx, dir)
			})
		})
		// float(): toggle the focused window between the tiled and
		// floating worlds (GGWM-007); returns the new floating state.
		set("float", func(call goja.FunctionCall) goja.Value {
			return m.call(vm, "float", func(ctx context.Context) (interface{}, error) {
				return m.backend.Float(ctx)
			})
		})
		// exec(cmdline): spawn a process, i3-style. Fire-and-forget.
		set("exec", m.jsExec(vm))

		// ---- launcher (GGWM-008) -------------------------------------
		// launch(target): registry id ("app:firefox", "builtin:trace",
		// "script:x") or a raw command line; returns the routed kind.
		set("launch", func(call goja.FunctionCall) goja.Value {
			target := call.Argument(0).String()
			if target == "" || goja.IsUndefined(call.Argument(0)) {
				panic(vm.ToValue("wm.launch: target must be a command id or command line"))
			}
			return m.call(vm, "launch", func(ctx context.Context) (interface{}, error) {
				return m.backend.Launch(ctx, target)
			})
		})
		// launcher(): open the Mod4+d popup.
		set("launcher", func(call goja.FunctionCall) goja.Value {
			return m.call(vm, "launcher", func(ctx context.Context) (interface{}, error) {
				return nil, m.backend.OpenLauncher(ctx)
			})
		})
		// command({id, label, doc?, run}): register a launcher entry
		// whose run callback fires on this runtime (rc.js only).
		set("command", m.jsCommand(vm))

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

		// ---- rules and layouts (P4) ----------------------------------
		m.installRuleExports(vm, set)
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

// jsCommand: wm.command({id, label, doc?, run}) — a launcher registry
// entry served by this runtime. Mirrors jsBind: the fire callback is a
// single post back into the JS loop, never JS execution on the WM side.
func (m *Module) jsCommand(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		obj, ok := call.Argument(0).(*goja.Object)
		if !ok {
			panic(vm.ToValue("wm.command: argument must be {id, label, doc?, run}"))
		}
		for _, k := range obj.Keys() {
			switch k {
			case "id", "label", "doc", "run":
			default:
				panic(vm.ToValue("wm.command: unknown key " + k))
			}
		}
		get := func(k string) string {
			v := obj.Get(k)
			if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
				return ""
			}
			s, _ := v.Export().(string)
			return s
		}
		id, label, doc := get("id"), get("label"), get("doc")
		if id == "" || label == "" {
			panic(vm.ToValue("wm.command: id and label must be non-empty strings"))
		}
		fn, ok := goja.AssertFunction(obj.Get("run"))
		if !ok {
			panic(vm.ToValue("wm.command: run must be a function"))
		}
		services, sok := runtimebridgeLookup(vm)
		if !sok {
			panic(vm.ToValue("wm.command: no runtime services"))
		}
		err := m.backend.RegisterCommand(id, label, doc, func() {
			_ = services.PostWithLifetimeContext("wm.command:"+id,
				func(_ context.Context, vm *goja.Runtime) {
					if _, err := fn(goja.Undefined()); err != nil {
						jsmod.EmitScriptError(nil, "wm.command:"+id, err, nil)
					}
				})
		})
		if err != nil {
			panic(vm.ToValue("wm.command: " + err.Error()))
		}
		return goja.Undefined()
	}
}

// jsExec: wm.exec(cmdline) — the i3 `exec` gesture. The child runs in
// the script's own process tree (`sh -c`), detached from the JS loop;
// its exit is reaped and ignored, exactly like i3.
func (m *Module) jsExec(vm *goja.Runtime) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if !m.execAllowed {
			panic(vm.ToValue("wm.exec: subprocesses are disabled here (rc.js has them; `run`/`repl` need --allow-exec)"))
		}
		cmdline := call.Argument(0).String()
		if cmdline == "" || goja.IsUndefined(call.Argument(0)) {
			panic(vm.ToValue("wm.exec: command line must be a non-empty string"))
		}
		c := exec.Command("sh", "-c", cmdline)
		if m.execDisplay != "" {
			c.Env = append(os.Environ(), "DISPLAY="+m.execDisplay)
		}
		if err := c.Start(); err != nil {
			panic(vm.ToValue("wm.exec: " + err.Error()))
		}
		go func() { _ = c.Wait() }()
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
