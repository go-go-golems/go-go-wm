package cmds

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/dop251/goja"
	"github.com/go-go-golems/go-go-goja/pkg/engine"

	"github.com/go-go-golems/go-go-wm/pkg/jsmod"
	"github.com/go-go-golems/go-go-wm/pkg/jsmod/pbuimod"
	"github.com/go-go-golems/go-go-wm/pkg/jsmod/uimod"
	"github.com/go-go-golems/go-go-wm/pkg/jsmod/wmmod"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/client"
	"github.com/go-go-golems/go-go-wm/pkg/wmx11"
)

// startRC boots the in-process rc.js runtime (attachment point A1): its
// wm module posts straight into the WM loop (ScriptBackend), while its
// pbui module speaks to the broker over a second socket connection — the
// WM's own client stays the WM's; scripts go through the front door like
// everyone else (GGWM-001 decision D2).
//
// Called from Config.OnReady, so it must not block: evaluation runs on a
// goroutine, and posted ops queue until the WM loop starts.
func startRC(ctx context.Context, w *wmx11.WM, rcPath, brokerSocket string, noBroker bool) {
	src, err := os.ReadFile(rcPath)
	if err != nil {
		log.Error().Err(err).Str("rc", rcPath).Msg("rc: cannot read")
		return
	}
	name := filepath.Base(rcPath)

	go func() {
		var cl *client.Client
		if !noBroker {
			cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			cl, err = client.Connect(cctx, client.Options{
				Socket: brokerSocket, Name: name, Roles: []string{"script"},
			})
			cancel()
			if err != nil {
				log.Warn().Err(err).Msg("rc: broker connect failed; pbui module degraded")
			}
		}

		backend := &wmx11.ScriptBackend{WM: w}
		fan := jsmod.NewEventFan(cl, 256)
		builder := engine.NewRuntimeFactoryBuilder()
		builder.WithModules(
			engine.NativeModuleRegistrar{
				ModuleID: "pbui", ModuleName: pbuimod.ModuleName,
				Loader: pbuimod.New(cl, pbuimod.WithEventFan(fan)).Loader(),
			},
			engine.NativeModuleRegistrar{
				ModuleID: "wm", ModuleName: wmmod.ModuleName,
				Loader: wmmod.New(backend, fan).Loader(),
			},
			engine.NativeModuleRegistrar{
				ModuleID: "ui", ModuleName: uimod.ModuleName,
				Loader: uimod.New(uimod.Options{
					BrokerSocket: brokerSocket,
					TileHost:     backend,
				}).Loader(),
			},
		)
		factory, err := builder.Build()
		if err != nil {
			log.Error().Err(err).Msg("rc: factory")
			return
		}
		rt, err := factory.NewRuntime(engine.WithLifetimeContext(ctx))
		if err != nil {
			log.Error().Err(err).Msg("rc: runtime")
			return
		}
		// The runtime lives for the whole WM session (keybinding and verb
		// callbacks land on its loop); tear down with the WM context.
		go func() {
			<-ctx.Done()
			closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = rt.Close(closeCtx)
			if cl != nil {
				_ = cl.Close()
			}
		}()

		if _, err := rt.Owner.Call(ctx, "rc:"+name, func(_ context.Context, vm *goja.Runtime) (any, error) {
			return vm.RunScript(name, string(src))
		}); err != nil {
			log.Error().Err(err).Str("rc", rcPath).Msg("rc: script failed")
			jsmod.EmitScriptError(cl, "rc:"+name, err, nil)
			return
		}
		log.Info().Str("rc", rcPath).Msg("rc: loaded")
	}()
}
