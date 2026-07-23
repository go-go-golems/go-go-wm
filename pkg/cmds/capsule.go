package cmds

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dop251/goja"
	"github.com/go-go-golems/go-go-goja/pkg/engine"

	"github.com/go-go-golems/go-go-wm/pkg/jsmod/semmod"
	"github.com/go-go-golems/go-go-wm/pkg/jsmod/uimod"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/client"
	"github.com/go-go-golems/go-go-wm/pkg/wmx11"
)

// Capsule runtime construction (GGWM-013 M5). This lives in the command
// layer because it needs goja and pkg/wmx11 stays goja-free (U-D3). The
// WM decides WHEN a capsule exists and owns its lease; this code only
// builds the runtime the spec describes.
//
// The endowment is the point: unlike rc.js — which receives wm, pbui, ui,
// and exec — a capsule runtime receives exactly two modules:
//
//	ui   the existing tile machinery (its surface)
//	sem  describe(), bound to the ONE capability the spec carries
//
// There is no wm module, no exec, no filesystem. Tier 1: this bounds the
// API surface and keeps VM crashes out of the WM loop; it is not a
// hostile-code security boundary (the sandbox tier slots in behind the
// same CapsuleSpec later).

// capsuleSpawner returns the wmx11.Config.SpawnCapsule implementation.
// getWM late-binds the WM pointer, which does not exist when Config is
// built; OnReady fills it before any verb can fire.
func capsuleSpawner(ctx context.Context, getWM func() *wmx11.WM, display, brokerSocket string, noBroker bool) func(wmx11.CapsuleSpec) (func(), error) {
	return func(spec wmx11.CapsuleSpec) (func(), error) {
		w := getWM()
		if w == nil {
			return nil, fmt.Errorf("capsule: wm not ready")
		}
		backend := &wmx11.ScriptBackend{WM: w}

		// The capsule's broker connection: gives it a principal of its own
		// and a registry entry, so `resource.list` shows the capsule and
		// its lease ends observably when the connection dies with the
		// runtime (M2). Degraded (nil) without a broker.
		var cl *client.Client
		if !noBroker {
			cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			var err error
			cl, err = client.Connect(cctx, client.Options{
				Socket: brokerSocket, Name: spec.ID, Roles: []string{"capsule"},
			})
			cancel()
			if err != nil {
				log.Warn().Err(err).Str("capsule", spec.ID).Msg("capsule: broker connect failed; running unregistered")
			} else {
				rctx, rcancel := context.WithTimeout(ctx, 2*time.Second)
				_ = cl.RegisterResource(rctx, pbui.Resource{
					ID: spec.ID, Kind: "wm.capsule", Label: "explain " + spec.Ref,
				})
				rcancel()
			}
		}

		uiMod := uimod.New(uimod.Options{
			Display:      display,
			BrokerSocket: brokerSocket,
			TileHost:     backend,
		})
		sem := &semmod.Module{
			Ref: spec.Ref,
			Describe: func() (interface{}, error) {
				desc, err := backend.DescribeWith(spec.CapID, spec.Ref)
				if err != nil {
					return nil, err
				}
				// Plain data for vm.ToValue: JSON round trip flattens the
				// embedded WindowInfo exactly like the IPC answer.
				raw, err := json.Marshal(desc)
				if err != nil {
					return nil, err
				}
				var m map[string]interface{}
				if err := json.Unmarshal(raw, &m); err != nil {
					return nil, err
				}
				return m, nil
			},
		}

		builder := engine.NewRuntimeFactoryBuilder()
		builder.WithModules(
			engine.NativeModuleRegistrar{
				ModuleID: "ui", ModuleName: uimod.ModuleName, Loader: uiMod.Loader(),
			},
			engine.NativeModuleRegistrar{
				ModuleID: "sem", ModuleName: semmod.ModuleName, Loader: sem.Loader(),
			},
		)
		factory, err := builder.Build()
		if err != nil {
			if cl != nil {
				_ = cl.Close()
			}
			return nil, fmt.Errorf("capsule factory: %w", err)
		}
		rt, err := factory.NewRuntime(engine.WithLifetimeContext(ctx))
		if err != nil {
			if cl != nil {
				_ = cl.Close()
			}
			return nil, fmt.Errorf("capsule runtime: %w", err)
		}
		cleanup := func() {
			closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = rt.Close(closeCtx)
			if cl != nil {
				_ = cl.Close() // ends the broker lease; resources revoke
			}
		}

		if _, err := rt.Owner.Call(ctx, "capsule:"+spec.ID, func(_ context.Context, vm *goja.Runtime) (any, error) {
			if err := vm.Set("CAPSULE_NAME", spec.Name); err != nil {
				return nil, err
			}
			return vm.RunScript(spec.Name+".js", explainCapsuleJS)
		}); err != nil {
			cleanup()
			return nil, fmt.Errorf("capsule script: %w", err)
		}
		return cleanup, nil
	}
}

// explainCapsuleJS is the Explain-window capsule. Everything it knows
// about the window comes through sem.describe(), i.e. through its one
// capability; the refresh button proves handlers and re-describe work.
const explainCapsuleJS = `
const sem = require("sem");
const ui = require("ui");

const app = ui.app({
  name: CAPSULE_NAME,
  title: "EXPLAIN",
  render() {
    const d = sem.describe();
    const alive = d.alive ? "alive" : ("DESTROYED " + (d.destroyed_at || ""));
    return [
      ui.row(ui.text("EXPLAIN WINDOW", { bold: true, size: 13 })),
      ui.row(ui.object("window", d.ref, { label: d.title, doc: "live window reference (GGWM-013 M3)" })),
      ui.row(ui.text(alive, { size: 11 })),
      ui.row(ui.hint("title      " + d.title)),
      ui.row(ui.hint("class      " + (d.class || "-") + " / " + (d.instance || "-"))),
      ui.row(ui.hint("leaf       " + (d.leaf || "-") + "    workspace " + (d.workspace || "-"))),
      ui.row(ui.hint("rect       " + d.rect)),
      ui.row(ui.hint("focused    " + !!d.focused + (d.floating ? "    floating" : ""))),
      ui.row(ui.button("refresh", "refresh", { color: "mint" })),
      ui.row(ui.hint("this capsule holds one grant: wm.window.read on " + sem.ref())),
    ];
  },
  actions: {
    // A handler run re-renders, and render() re-describes: the refresh
    // button is the whole action.
    refresh() {},
  },
});
app.tile();
`
