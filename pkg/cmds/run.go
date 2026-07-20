package cmds

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/dop251/goja"
	glazed_cmds "github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/go-go-goja/pkg/engine"

	"github.com/go-go-golems/go-go-wm/pkg/draw"
	"github.com/go-go-golems/go-go-wm/pkg/jsmod"
	"github.com/go-go-golems/go-go-wm/pkg/jsmod/pbuimod"
	"github.com/go-go-golems/go-go-wm/pkg/jsmod/uimod"
	"github.com/go-go-golems/go-go-wm/pkg/jsmod/wmmod"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/client"
)

// RunCommand executes a JavaScript file against the broker (and, from P2
// on, the WM control socket): the A2 "standalone script process"
// attachment point of the GGWM-002 design.
type RunCommand struct {
	*glazed_cmds.CommandDescription
}

type runSettings struct {
	Script    string `glazed:"script"`
	Socket    string `glazed:"socket"`
	WMSocket  string `glazed:"wm-socket"`
	Once      bool   `glazed:"once"`
	AllowExec bool   `glazed:"allow-exec"`
	NoBroker  bool   `glazed:"no-broker"`
}

func NewRunCommand() (*RunCommand, error) {
	return &RunCommand{glazed_cmds.NewCommandDescription("run",
		glazed_cmds.WithShort("Run a JavaScript automation script (pbui + wm modules)"),
		glazed_cmds.WithLong(`Runs script.js in a goja runtime with the pbui native module loaded
(require("pbui")). The script connects to the broker as a first-class
participant — its basename is the client name, which is also verb
ownership.

Without --once the process keeps running after the script's top-level
code finishes, serving verb handlers and event subscriptions until
interrupted (a script daemon). With --once the process exits as soon as
the script's completion value settles: if the script evaluates to a
Promise, run waits for it; rejection exits non-zero.

Capability flags follow the data-only vs host-access split: the runtime
always has the safe data modules; --allow-exec adds the exec module.

Examples:
  go-go-wm run --once examples/scripts/palette.js
  go-go-wm run examples/scripts/git-verbs.js          # daemon: serves verbs`),
		glazed_cmds.WithArguments(
			fields.New("script", fields.TypeString, fields.WithHelp("path to the JavaScript file"), fields.WithRequired(true)),
		),
		glazed_cmds.WithFlags(
			socketFlag(),
			fields.New("wm-socket", fields.TypeString, fields.WithDefault(""),
				fields.WithHelp("WM control socket for the wm module (default: $GO_GO_WM_SOCKET)")),
			fields.New("once", fields.TypeBool, fields.WithDefault(false),
				fields.WithHelp("exit when the script's result settles instead of serving forever")),
			fields.New("allow-exec", fields.TypeBool, fields.WithDefault(false),
				fields.WithHelp("expose the exec module (run subprocesses) to the script")),
			fields.New("no-broker", fields.TypeBool, fields.WithDefault(false),
				fields.WithHelp("do not connect to the broker (data-only pbui helpers still work)")),
		),
	)}, nil
}

func (c *RunCommand) Run(ctx context.Context, vals *values.Values) error {
	s := &runSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}
	src, err := os.ReadFile(s.Script)
	if err != nil {
		return err
	}
	name := filepath.Base(s.Script)

	// The broker connection (client name = script basename = verb owner).
	var cl *client.Client
	if !s.NoBroker {
		cl, err = client.Connect(ctx, client.Options{
			Socket: socketOrDefault(s.Socket), Name: name, Roles: []string{"script"},
		})
		if err != nil {
			return err
		}
		defer func() { _ = cl.Close() }()
	}

	rt, err := buildScriptRuntime(ctx, cl, socketOrDefault(s.Socket), s.WMSocket, s.AllowExec)
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = rt.Close(closeCtx)
	}()

	val, err := rt.Owner.Call(ctx, "run:"+name, func(_ context.Context, vm *goja.Runtime) (any, error) {
		v, err := vm.RunScript(name, string(src))
		if err != nil {
			return nil, err
		}
		return v.Export(), nil
	})
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	if s.Once {
		return waitForCompletion(ctx, rt, val)
	}

	// Daemon mode: serve verbs/subscriptions until signal or broker death.
	log.Info().Str("script", name).Msg("script loaded; serving (Ctrl-C to stop)")
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	if cl != nil {
		select {
		case <-ctx.Done():
		case <-sigCh:
		case <-cl.Done():
			return fmt.Errorf("broker connection closed")
		}
	} else {
		select {
		case <-ctx.Done():
		case <-sigCh:
		}
	}
	return nil
}

// buildScriptRuntime assembles the goja runtime for script processes: the
// pbui and wm modules plus the data-only default modules, and exec only
// when the capability flag grants it. Both modules share one event-bus
// subscription (the EventFan).
func buildScriptRuntime(ctx context.Context, cl *client.Client, brokerSocket, wmSocket string, allowExec bool) (*engine.Runtime, error) {
	applyInitialTheme(ctx, wmSocket)
	fan := jsmod.NewEventFan(cl, 256)
	var wmFan *jsmod.EventFan
	if cl != nil {
		wmFan = fan
	}
	var wmOpts []wmmod.Option
	if allowExec {
		wmOpts = append(wmOpts, wmmod.WithExec(""))
	}
	uiMod := uimod.New(uimod.Options{BrokerSocket: brokerSocket})
	builder := engine.NewRuntimeFactoryBuilder()
	builder.WithModules(
		engine.NativeModuleRegistrar{
			ModuleID:   "pbui",
			ModuleName: pbuimod.ModuleName,
			Loader:     pbuimod.New(cl, pbuimod.WithEventFan(fan)).Loader(),
		},
		engine.NativeModuleRegistrar{
			ModuleID:   "wm",
			ModuleName: wmmod.ModuleName,
			Loader:     wmmod.New(&wmmod.IPCBackend{Socket: wmSocket}, wmFan, wmOpts...).Loader(),
		},
		engine.NativeModuleRegistrar{
			ModuleID:   "ui",
			ModuleName: uimod.ModuleName,
			Loader:     uiMod.Loader(),
		},
	)
	if allowExec {
		builder.UseModuleMiddleware(engine.MiddlewareOnly("exec"))
	}
	followThemeChanges(ctx, fan, cl, uiMod, true)
	factory, err := builder.Build()
	if err != nil {
		return nil, err
	}
	return factory.NewRuntime(engine.WithLifetimeContext(ctx))
}

// applyInitialTheme aligns this process's palette with the WM before
// anything renders: ask the control socket, fall back to $GO_GO_WM_THEME,
// silently keep the default when neither answers.
func applyInitialTheme(ctx context.Context, wmSocket string) {
	qctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	b := &wmmod.IPCBackend{Socket: wmSocket}
	if info, err := b.Theme(qctx); err == nil && info.Theme != "" {
		_ = draw.SetTheme(info.Theme)
		return
	}
	if env := os.Getenv("GO_GO_WM_THEME"); env != "" {
		_ = draw.SetTheme(env)
	}
}

// followThemeChanges keeps this process's palette (and its live ui.app
// surfaces) in sync with WM theme switches via the broker event stream.
//
// swapPalette must be false in the rc.js runtime: there the WM shares
// this process's draw package and already swapped the palette on its
// own loop — a second SetTheme from the fan drainer would race it and
// tear the palette (mixed old/new slots, found the hard way in the
// GGWM-004 live tests). Out-of-process runtimes (run/repl) pass true.
func followThemeChanges(ctx context.Context, fan *jsmod.EventFan, cl *client.Client, uiMod *uimod.Module, swapPalette bool) {
	if cl == nil {
		return
	}
	fan.SubscribeGo("theme.changed", func(msg *pbui.Msg) {
		var d struct {
			Theme string `json:"theme"`
		}
		if err := json.Unmarshal(msg.Data, &d); err != nil || d.Theme == "" {
			return
		}
		if swapPalette {
			if err := draw.SetTheme(d.Theme); err != nil {
				return
			}
		}
		uiMod.Retheme()
	})
	_ = fan.EnsurePump(ctx)
}

// waitForCompletion implements --once: when the script's completion value
// is a Promise, poll its state on the owner loop until it settles.
// Rejection is a script failure (non-zero exit).
func waitForCompletion(ctx context.Context, rt *engine.Runtime, val any) error {
	p, ok := val.(*goja.Promise)
	if !ok {
		return nil // synchronous script: done
	}
	for {
		state, err := rt.Owner.Call(ctx, "run.poll", func(_ context.Context, vm *goja.Runtime) (any, error) {
			st := p.State()
			if st == goja.PromiseStateRejected {
				return st, fmt.Errorf("script rejected: %v", p.Result())
			}
			return st, nil
		})
		if err != nil {
			return err
		}
		if state.(goja.PromiseState) == goja.PromiseStateFulfilled {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}
