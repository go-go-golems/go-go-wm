package cmds

// The rich REPL surface (GGWM-009 R2): `repl --ui` hosts the cell model
// from pkg/repl in a standalone xapp window. The kernel is replapi,
// unmodified (R-D3); raw-value capture uses a session prelude plus an
// expression wrap with statement fallback (see the ticket diary, Step 1).

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"strconv"
	"strings"
	"sync"

	"github.com/dop251/goja"
	"github.com/go-go-golems/go-go-goja/pkg/engine"
	"github.com/go-go-golems/go-go-goja/pkg/replapi"
	"github.com/rs/zerolog"

	"github.com/go-go-golems/go-go-wm/pkg/apps"
	"github.com/go-go-golems/go-go-wm/pkg/apps/uispec"
	"github.com/go-go-golems/go-go-wm/pkg/apps/xapp"
	"github.com/go-go-golems/go-go-wm/pkg/jsmod"
	"github.com/go-go-golems/go-go-wm/pkg/jsmod/pbuimod"
	"github.com/go-go-golems/go-go-wm/pkg/jsmod/uimod"
	"github.com/go-go-golems/go-go-wm/pkg/jsmod/wmmod"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/client"
	"github.com/go-go-golems/go-go-wm/pkg/repl"
)

// replPrelude installs Out(n), $_ (plain bindings returning raw JS
// values — history is for computing) and a console shim: the Raw
// kernel profile does not capture console, so the prelude collects
// lines into __pbui_console for the per-cell drain. Original console
// methods still run (lines also reach the process log).
const replPrelude = `globalThis.__pbui_hist = {};
globalThis.__pbui_console = [];
// The notebook pre-binds the modules — typing wm.tree() must just work
// (the terminal REPL keeps explicit require, matching pasted scripts).
try { globalThis.wm = require("wm"); } catch (e) {}
try { globalThis.pbui = require("pbui"); } catch (e) {}
try { globalThis.ui = require("ui"); } catch (e) {}
globalThis.Out = function (n) { return globalThis.__pbui_hist[n]; };
Object.defineProperty(globalThis, "$_", {
  get: function () { return globalThis.__pbui_hist.last; },
  configurable: true,
});
(function () {
  var orig = globalThis.console || {};
  function wrap(kind) {
    return function () {
      var parts = [];
      for (var i = 0; i < arguments.length; i++) {
        var a = arguments[i];
        try { parts.push(typeof a === "string" ? a : JSON.stringify(a)); }
        catch (e) { parts.push(String(a)); }
      }
      globalThis.__pbui_console.push(kind + ": " + parts.join(" "));
      if (orig[kind]) { try { orig[kind].apply(orig, arguments); } catch (e) {} }
    };
  }
  globalThis.console = {
    log: wrap("log"), info: wrap("info"), warn: wrap("warn"),
    error: wrap("error"), debug: wrap("debug"),
  };
})();
undefined;`

// replKernel wraps a replapi session with the capture strategy.
type replKernel struct {
	app *replapi.App
	sid string
}

func (k *replKernel) prelude(ctx context.Context) error {
	_, err := k.app.Evaluate(ctx, k.sid, replPrelude)
	return err
}

// eval runs one cell. The input is first submitted expression-wrapped
// (capturing the raw value into __pbui_hist); a parse failure falls
// back to evaluating the source verbatim — safe, because a parse error
// executes nothing, so the fallback is the first execution.
func (k *replKernel) eval(ctx context.Context, n int, input string) ([]string, string, string, *repl.Value) {
	trimmed := strings.TrimRight(strings.TrimSpace(input), "; \t\n")
	wrapped := fmt.Sprintf("globalThis.__pbui_hist[%d] = globalThis.__pbui_hist.last = (\n%s\n);", n, trimmed)

	resp, err := k.app.Evaluate(ctx, k.sid, wrapped)
	captured := true
	// Fall back to evaluating the raw input ONLY when the wrapper itself
	// failed to parse (Status == "parse-error"). A runtime SyntaxError
	// thrown by the wrapped body (e.g. JSON.parse("{")) has Status
	// "runtime-error" and must NOT trigger a retry, because the body
	// already executed — re-evaluating the raw input would run side
	// effects a second time (Codex review RC-1).
	if err == nil && resp.Cell != nil && resp.Cell.Execution.Status == "parse-error" {
		captured = false
		resp, err = k.app.Evaluate(ctx, k.sid, input)
	}
	if err != nil {
		return nil, err.Error(), "", nil
	}
	if resp.Cell == nil {
		return nil, "kernel returned no cell", "", nil
	}
	exec := resp.Cell.Execution
	var console []string
	for _, ev := range exec.Console {
		console = append(console, "["+ev.Kind+"] "+ev.Message)
	}

	// Capture pass: drain the prelude's console shim (both paths) and,
	// when the wrap ran, export the raw value and call __pbui__ — on
	// the JS loop via the owner, never during render.
	var exported, richRaw interface{}
	var richErr error
	werr := k.app.WithRuntime(ctx, k.sid, func(_ context.Context, rt *engine.Runtime) error {
		_, cerr := rt.Owner.Call(ctx, "richrepl.capture", func(_ context.Context, vm *goja.Runtime) (any, error) {
			if cv := vm.Get("__pbui_console"); cv != nil && !goja.IsUndefined(cv) {
				if lines, ok := cv.Export().([]interface{}); ok {
					for _, l := range lines {
						if s, ok := l.(string); ok {
							console = append(console, s)
						}
					}
				}
				_ = vm.Set("__pbui_console", vm.NewArray())
			}
			if !captured {
				return nil, nil
			}
			histV := vm.Get("__pbui_hist")
			if histV == nil || goja.IsUndefined(histV) {
				return nil, nil
			}
			v := histV.ToObject(vm).Get(strconv.Itoa(n))
			if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
				return nil, nil
			}
			if obj, isObj := v.(*goja.Object); isObj {
				if fn, has := goja.AssertFunction(obj.Get("__pbui__")); has {
					if res, ferr := fn(obj); ferr == nil {
						richRaw = res.Export()
					} else {
						richErr = ferr
					}
				}
			}
			exported = v.Export()
			return nil, nil
		})
		return cerr
	})
	if exec.Error != "" {
		return console, exec.Error, "", nil
	}
	if !captured || werr != nil {
		return console, "", exec.Result, nil
	}
	if richErr != nil {
		console = append(console, "__pbui__ failed: "+richErr.Error()+" (derived view shown)")
	}
	if richRaw != nil {
		desc, ok := richRaw.(map[string]interface{})
		if !ok {
			console = append(console, "__pbui__ must return an object (derived view shown)")
		} else if v, nerr := repl.NormalizeRich(exported, desc); nerr == nil {
			return console, "", exec.Result, &v
		} else {
			console = append(console, "__pbui__ invalid: "+nerr.Error()+" (derived view shown)")
		}
	}
	v := repl.Derive(exported)
	return console, "", exec.Result, &v
}

// richReplApp is the xapp.App around the session model.
type richReplApp struct {
	mu     sync.Mutex
	sess   repl.Session
	kernel *replKernel
	root   context.Context
	budget int // last render's row budget (for scroll clamping)

	uiCtx     xapp.Ctx // captured in Started (for out-of-band redraws)
	uiReady   bool
	evalQueue chan evalJob // serializes cell evaluate-and-capture (RC-2)
}

// evalJob is one queued cell evaluation. Cells are processed in submission
// order by a single worker so the global __pbui_console buffer is always
// drained by the cell that filled it — without this, two concurrent eval
// calls interleave their Evaluate + WithRuntime capture and a later cell
// can observe an earlier cell's history or steal its console output
// (Codex review RC-2).
type evalJob struct {
	n   int
	src string
	ctx xapp.Ctx
}

func (a *richReplApp) Name() string  { return "repl" }
func (a *richReplApp) Title() string { return "repl" }

// replProducedPtypes is what derivation can emit — the verbs below
// attach to these (the broker assembles type-specific verbs from other
// daemons on the same menu; nothing REPL-specific there).
var replProducedPtypes = []string{"color", "number", "string", "boolean", "series", "dataset", "palette", "json"}

func (a *richReplApp) Verbs() []pbui.Verb {
	return []pbui.Verb{
		{ID: "repl.use", Label: "Insert Out[n] into input", Ptypes: replProducedPtypes},
		{ID: "repl.copy-input", Label: "Copy as input", Ptypes: replProducedPtypes},
	}
}

func (a *richReplApp) Render(w, h int, accepting []string) (*image.RGBA, []apps.Region) {
	a.mu.Lock()
	a.budget = (h - 60) / 22
	spec := a.sess.Spec(a.budget)
	a.mu.Unlock()
	return uispec.Render(w, h, spec, accepting)
}

func (a *richReplApp) Started(ctx xapp.Ctx) {
	a.mu.Lock()
	a.uiCtx, a.uiReady = ctx, true
	a.mu.Unlock()
}

// themeRedraw repaints after a palette swap (posted to the xapp loop;
// called from the event-fan drainer, which swapped the palette first —
// same goroutine, so the ordering is deterministic).
func (a *richReplApp) themeRedraw() {
	a.mu.Lock()
	ctx, ready := a.uiCtx, a.uiReady
	a.mu.Unlock()
	if ready {
		ctx.Post(ctx.Redraw)
	}
}

func (a *richReplApp) HandleAction(ctx xapp.Ctx, action string) {
	action = strings.TrimPrefix(action, "cmd:")
	a.mu.Lock()
	switch {
	case strings.HasPrefix(action, "view:"):
		var n, view int
		if _, err := fmt.Sscanf(action, "view:%d:%d", &n, &view); err == nil {
			a.sess.SetView(n, view)
		}
	case strings.HasPrefix(action, "fold:"):
		if n, err := strconv.Atoi(strings.TrimPrefix(action, "fold:")); err == nil {
			a.sess.ToggleFold(n)
		}
	}
	a.mu.Unlock()
	ctx.Redraw()
}

func (a *richReplApp) HandleVerb(ctx xapp.Ctx, verbID string, obj *pbui.Object) {
	if obj == nil {
		return
	}
	a.mu.Lock()
	n := a.findCellByValue(obj)
	switch verbID {
	case "repl.use":
		if n > 0 {
			a.sess.Input += fmt.Sprintf("Out(%d)", n)
		}
	case "repl.copy-input":
		if n > 0 {
			c := a.sess.Cells[n-1]
			if c.Value != nil && c.Value.Input != "" {
				a.sess.Input = c.Value.Input
			} else {
				a.sess.Input = c.Input
			}
		}
	}
	a.mu.Unlock()
	ctx.Redraw()
}

// findCellByValue maps a verb target back to the newest cell holding
// that value (objects carry values, not cell numbers).
func (a *richReplApp) findCellByValue(obj *pbui.Object) int {
	want := obj.StringValue()
	for i := len(a.sess.Cells) - 1; i >= 0; i-- {
		c := a.sess.Cells[i]
		if c.Value == nil || c.Value.Ptype != obj.Ptype {
			continue
		}
		var v interface{}
		_ = jsonUnmarshalRaw(c.Value.Raw, &v)
		probe, _ := pbui.NewObject(c.Value.Ptype, v)
		if probe.StringValue() == want {
			return c.N
		}
	}
	return 0
}

func (a *richReplApp) HandleKey(ctx xapp.Ctx, key string) {
	a.mu.Lock()
	redraw := true
	var submitN int
	switch key {
	case "Return", "KP_Enter":
		if strings.TrimSpace(a.sess.Input) != "" {
			submitN = a.sess.Submit()
		}
	case "BackSpace":
		if a.sess.Input != "" {
			a.sess.Input = a.sess.Input[:len(a.sess.Input)-1]
		}
	case "Up":
		a.sess.HistoryPrev()
	case "Down":
		a.sess.HistoryNext()
	case "Prior", "Page_Up":
		a.sess.ScrollBy(a.budget/2, a.budget)
	case "Next", "Page_Down":
		a.sess.ScrollBy(-a.budget/2, a.budget)
	case "Escape":
		a.sess.Input = ""
	case "space":
		a.sess.Input += " "
	default:
		if len(key) == 1 && key[0] >= 0x20 && key[0] < 0x7f {
			a.sess.Input += key
		} else {
			redraw = false
		}
	}
	input := ""
	if submitN > 0 {
		input = a.sess.Cells[submitN-1].Input
	}
	a.mu.Unlock()
	if redraw {
		ctx.Redraw()
	}
	if submitN == 0 {
		return
	}
	// Queue, don't spawn: a single worker drains cells in submission
	// order so evaluate-and-capture never interleaves across cells (RC-2).
	a.evalQueue <- evalJob{n: submitN, src: input, ctx: ctx}
}

// evalWorker is the single goroutine that runs queued cell evaluations in
// submission order. Because eval performs an Evaluate round-trip followed
// by a separate WithRuntime capture pass, two concurrent evals would
// interleave and the global __pbui_console buffer could be drained by the
// wrong cell (Codex review RC-2). Serializing through this worker keeps
// output attribution correct. The complete result is posted back to the
// xapp loop (never touches session state off-loop).
func (a *richReplApp) evalWorker() {
	for job := range a.evalQueue {
		console, errText, result, val := a.kernel.eval(a.root, job.n, job.src)
		n, src, ctx := job.n, job.src, job.ctx
		ctx.Post(func() {
			a.mu.Lock()
			a.sess.Complete(n, console, errText, result, val)
			a.mu.Unlock()
			data := map[string]interface{}{"n": n, "input": src, "error": errText, "console": len(console)}
			if val != nil {
				data["ptype"] = val.Ptype
				data["summary"] = val.Summary
			}
			ctx.Emit("repl.cell-done", data)
			ctx.Redraw()
		})
	}
}

func jsonUnmarshalRaw(raw []byte, out interface{}) error {
	if len(raw) == 0 {
		return fmt.Errorf("empty")
	}
	return json.Unmarshal(raw, out)
}

// runReplUI wires the kernel (same module set as the terminal REPL)
// into the xapp-hosted surface and blocks until the window closes.
func (c *ReplCommand) runReplUI(ctx context.Context, s *replSettings) error {
	var cl *client.Client
	if !s.NoBroker {
		var err error
		cl, err = client.Connect(ctx, client.Options{
			Socket: socketOrDefault(s.Socket), Name: "repl", Roles: []string{"script"},
		})
		if err != nil {
			return err
		}
		defer func() { _ = cl.Close() }()
	}
	applyInitialTheme(ctx, s.WMSocket)
	fan := jsmod.NewEventFan(cl, 256)
	var wmOpts []wmmod.Option
	if s.AllowExec {
		wmOpts = append(wmOpts, wmmod.WithExec(s.Display))
	}
	uiMod := uimod.New(uimod.Options{BrokerSocket: socketOrDefault(s.Socket)})
	builder := engine.NewRuntimeFactoryBuilder()
	builder.WithModules(
		engine.NativeModuleRegistrar{
			ModuleID: "pbui", ModuleName: pbuimod.ModuleName,
			Loader: pbuimod.New(cl, pbuimod.WithEventFan(fan)).Loader(),
		},
		engine.NativeModuleRegistrar{
			ModuleID: "wm", ModuleName: wmmod.ModuleName,
			Loader: wmmod.New(&wmmod.IPCBackend{Socket: s.WMSocket}, fan, wmOpts...).Loader(),
		},
		engine.NativeModuleRegistrar{
			ModuleID: "ui", ModuleName: uimod.ModuleName,
			Loader: uiMod.Loader(),
		},
	)
	if s.AllowExec {
		builder.UseModuleMiddleware(engine.MiddlewareOnly("exec"))
	}
	followThemeChanges(ctx, fan, cl, uiMod, true)
	factory, err := builder.Build()
	if err != nil {
		return err
	}
	// RawConfig, like the terminal REPL: identical cell semantics, and
	// the Interactive profile's binding-capture rewriter rejects the
	// multi-line capture wrap ("Unexpected token ;"). Console capture
	// comes from the prelude shim instead.
	app, err := replapi.NewWithConfig(ctx, factory, zerolog.Nop(), replapi.RawConfig())
	if err != nil {
		return err
	}
	defer func() { _ = app.Close(context.Background()) }()
	sess, err := app.CreateSession(ctx)
	if err != nil {
		return err
	}
	kernel := &replKernel{app: app, sid: sess.ID}
	if err := kernel.prelude(ctx); err != nil {
		return fmt.Errorf("repl prelude: %w", err)
	}

	surface := &richReplApp{
		kernel:    kernel,
		root:      ctx,
		evalQueue: make(chan evalJob, 64),
	}
	// Single eval worker: processes cells strictly in submission order so
	// the evaluate + WithRuntime capture pair never interleaves across
	// cells (RC-2). The queue is buffered so submit never blocks the UI;
	// a full queue (64 pending cells) drops on the floor, which is far
	// beyond what a human types.
	go surface.evalWorker()
	// Registered AFTER followThemeChanges: same fan, same event — the
	// drainer runs handlers in registration order, so the palette swap
	// happens before this redraw is posted.
	fan.SubscribeGo("theme.changed", func(*pbui.Msg) { surface.themeRedraw() })
	return xapp.Run(ctx, s.Display, socketOrDefault(s.Socket), surface)
}
