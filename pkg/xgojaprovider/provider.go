// Package xgojaprovider packages go-go-wm's scripting modules (wm, pbui,
// ui) as an xgoja provider, so any xgoja-generated binary can compile
// them in and require() them (GGWM-003 U4; compile-time composition per
// the xgoja playbook).
//
// Provider modules connect lazily and degrade (design decision U-D4):
// module setup never dials anything, so a generated binary builds its
// runtime fine with no broker running; the first broker-touching call
// connects (sockets from module config JSON, falling back to
// $PBUI_SOCKET / $GO_GO_WM_SOCKET), and failures surface as the standard
// "not connected" errors.
package xgojaprovider

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/require"
	"github.com/go-go-golems/go-go-goja/pkg/xgoja/providerapi"

	"github.com/go-go-golems/go-go-wm/pkg/jsmod"
	"github.com/go-go-golems/go-go-wm/pkg/jsmod/pbuimod"
	"github.com/go-go-golems/go-go-wm/pkg/jsmod/uimod"
	"github.com/go-go-golems/go-go-wm/pkg/jsmod/wmmod"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/client"
)

// PackageID is the provider package id used in xgoja.yaml.
const PackageID = "go-go-wm"

// moduleConfig is the per-module config JSON accepted in runtime profiles.
type moduleConfig struct {
	Socket   string `json:"socket,omitempty"`    // broker socket
	WMSocket string `json:"wm_socket,omitempty"` // WM control socket
	Display  string `json:"display,omitempty"`   // X display for ui.show
	Name     string `json:"name,omitempty"`      // broker client name
}

func parseConfig(raw json.RawMessage) moduleConfig {
	var cfg moduleConfig
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &cfg)
	}
	if cfg.Name == "" {
		cfg.Name = "xgoja-script"
	}
	return cfg
}

// runtimeState is the lazily-connected shared state of one generated
// runtime: one broker client + one event fan, shared by all three
// modules (mirrors what run.go wires by hand).
type runtimeState struct {
	cfg    moduleConfig
	cfgSet bool // guards cfg so a late factory parse doesn't clobber it

	mu  sync.Mutex
	cl  *client.Client
	fan *jsmod.EventFan
	err error
	up  bool
}

// init captures this runtime's module config the first time any module
// factory runs. It is called from both the pbui and wm factories; the
// config is identical per runtime profile, so the second call is a no-op
// guarded by cfgSet (keeps a late ui module's separate parse from
// clobbering an already-connected state).
func (s *runtimeState) init(mctx providerapi.ModuleSetupContext) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.cfgSet {
		s.cfg = parseConfig(mctx.Config)
		s.cfgSet = true
	}
}

// connect dials the broker once; later calls return the cached outcome.
func (s *runtimeState) connect() (*client.Client, *jsmod.EventFan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.up {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		s.cl, s.err = client.Connect(ctx, client.Options{
			Socket: s.cfg.Socket, Name: s.cfg.Name, Roles: []string{"script"},
		})
		cancel()
		if s.err == nil {
			s.fan = jsmod.NewEventFan(s.cl, 256)
		}
		s.up = true
	}
	return s.cl, s.fan, s.err
}

// Register exposes the go-go-wm modules to an xgoja provider registry.
func Register(registry *providerapi.ProviderRegistry) error {
	// One runtimeState is shared by the pbui and wm modules so a runtime
	// that requires both opens exactly ONE broker connection + event fan
	// under one client name. Previously each NewModuleFactory created its
	// own state, so requiring both opened two connections with the same
	// default name — the broker routed verb.run to the first matching
	// connection (nondeterministically the wm one, which has no verb
	// handler) and the verb was silently lost (Codex review RC-11).
	state := &runtimeState{}
	return registry.Package(PackageID,
		providerapi.Module{
			Name:        "pbui",
			DefaultAs:   "pbui",
			Description: "PBUI participation: typed objects, promise-based accept, verbs, listener print, event bus",
			NewModuleFactory: func(mctx providerapi.ModuleSetupContext) (require.ModuleLoader, error) {
				state.init(mctx)
				return lazyLoader(func() (require.ModuleLoader, error) {
					cl, fan, err := state.connect()
					if err != nil {
						// Data-only pbui helpers still work with a nil client.
						return pbuimod.New(nil).Loader(), nil
					}
					return pbuimod.New(cl, pbuimod.WithEventFan(fan)).Loader(), nil
				}), nil
			},
		},
		providerapi.Module{
			Name:        "wm",
			DefaultAs:   "wm",
			Description: "go-go-wm layout control: tree queries, op-compiling mutations, workspaces, rules, layouts",
			NewModuleFactory: func(mctx providerapi.ModuleSetupContext) (require.ModuleLoader, error) {
				state.init(mctx)
				return lazyLoader(func() (require.ModuleLoader, error) {
					_, fan, _ := state.connect() // fan may be nil: wm.on degrades
					backend := &wmmod.IPCBackend{Socket: state.cfg.WMSocket}
					return wmmod.New(backend, fan).Loader(), nil
				}), nil
			},
		},
		providerapi.Module{
			Name:        "ui",
			DefaultAs:   "ui",
			Description: "declarative PBUI surfaces: ui.app render specs shown as X windows via the xapp shell",
			NewModuleFactory: func(mctx providerapi.ModuleSetupContext) (require.ModuleLoader, error) {
				cfg := parseConfig(mctx.Config)
				return uimod.New(uimod.Options{
					Display:      cfg.Display,
					BrokerSocket: cfg.Socket,
				}).Loader(), nil
			},
		},
	)
}

// init captures this runtime's module config the first time any module
// factory runs. It is called from both the pbui and wm factories; the
// config is identical per runtime profile, so the second call is a no-op
// guarded by cfgSet (keeps a late ui module's separate parse from
// clobbering an already-connected state).

// lazyLoader defers loader construction to first require(), so module
// setup never performs I/O.
func lazyLoader(build func() (require.ModuleLoader, error)) require.ModuleLoader {
	var once sync.Once
	var loader require.ModuleLoader
	return func(vm *goja.Runtime, module *goja.Object) {
		once.Do(func() {
			l, err := build()
			if err != nil || l == nil {
				l = func(vm *goja.Runtime, module *goja.Object) {
					panic(vm.ToValue("go-go-wm provider: module unavailable: " + errString(err)))
				}
			}
			loader = l
		})
		loader(vm, module)
	}
}

func errString(err error) string {
	if err == nil {
		return "unknown error"
	}
	return err.Error()
}
