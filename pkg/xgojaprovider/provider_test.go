package xgojaprovider_test

// U4 test (design doc, Testing): register the provider into a real
// registry, build runtimes with each module, and exercise the lazy /
// degraded paths — data-only pbui works with no broker anywhere; a live
// broker upgrades the same module to full participation. No X involved.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dop251/goja"
	"github.com/go-go-golems/go-go-goja/pkg/engine"
	"github.com/go-go-golems/go-go-goja/pkg/xgoja/providerapi"

	"github.com/go-go-golems/go-go-wm/pkg/pbui/broker"
	"github.com/go-go-golems/go-go-wm/pkg/xgojaprovider"
)

func registryModules(t *testing.T) map[string]providerapi.Module {
	t.Helper()
	reg := providerapi.NewProviderRegistry()
	if err := xgojaprovider.Register(reg); err != nil {
		t.Fatalf("register: %v", err)
	}
	pkgs := reg.Packages()
	if len(pkgs) != 1 || pkgs[0].ID != xgojaprovider.PackageID {
		t.Fatalf("packages = %+v", pkgs)
	}
	return pkgs[0].Modules
}

func runtimeWith(t *testing.T, name string, mod providerapi.Module, config string) *engine.Runtime {
	t.Helper()
	loader, err := mod.NewModuleFactory(providerapi.ModuleSetupContext{
		Context: context.Background(),
		Name:    name, As: name,
		Config: json.RawMessage(config),
	})
	if err != nil {
		t.Fatalf("factory %s: %v", name, err)
	}
	builder := engine.NewRuntimeFactoryBuilder()
	builder.WithModules(engine.NativeModuleRegistrar{ModuleID: name, ModuleName: name, Loader: loader})
	factory, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	rt, err := factory.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = rt.Close(ctx)
	})
	return rt
}

func eval(t *testing.T, rt *engine.Runtime, src string) (any, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return rt.Owner.Call(ctx, "test", func(_ context.Context, vm *goja.Runtime) (any, error) {
		v, err := vm.RunScript("t.js", src)
		if err != nil {
			return nil, err
		}
		return v.Export(), nil
	})
}

func TestProviderExposesAllThreeModules(t *testing.T) {
	mods := registryModules(t)
	for _, want := range []string{"wm", "pbui", "ui"} {
		if _, ok := mods[want]; !ok {
			t.Errorf("module %q missing (have %v)", want, keys(mods))
		}
	}
}

func TestPbuiDataOnlyWorksWithoutAnyBroker(t *testing.T) {
	mods := registryModules(t)
	// Point at a socket that does not exist: connect fails, module
	// degrades to data-only.
	rt := runtimeWith(t, "pbui", mods["pbui"], `{"socket":"/tmp/definitely-not-a-broker.sock"}`)
	out, err := eval(t, rt, `
		const pbui = require("pbui");
		pbui.parse(pbui.uri(pbui.object("color", "#33302a"))).value;
	`)
	if err != nil {
		t.Fatalf("data-only path: %v", err)
	}
	if out != "#33302a" {
		t.Fatalf("uri round trip via provider = %v", out)
	}
	// Broker-touching calls must throw the standard error, not crash.
	if _, err := eval(t, rt, `require("pbui").emit("x", {})`); err == nil {
		t.Fatal("emit without a broker should throw")
	}
}

func TestPbuiConnectsLazilyToARealBroker(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "b.sock")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = broker.New().ListenAndServe(ctx, sock) }()
	for i := 0; i < 200; i++ {
		if _, err := os.Stat(sock); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mods := registryModules(t)
	rt := runtimeWith(t, "pbui", mods["pbui"], `{"socket":"`+sock+`","name":"provider-test"}`)
	if _, err := eval(t, rt, `require("pbui").print("hello from a generated binary")`); err != nil {
		t.Fatalf("print through provider: %v", err)
	}
}

func TestWmModuleThrowsCleanlyWithoutAWM(t *testing.T) {
	mods := registryModules(t)
	rt := runtimeWith(t, "wm", mods["wm"], `{"wm_socket":"/tmp/definitely-no-wm.sock"}`)
	if _, err := eval(t, rt, `require("wm").tree()`); err == nil {
		t.Fatal("wm.tree without a WM should throw")
	}
}

func TestUiBuildersWorkStandalone(t *testing.T) {
	mods := registryModules(t)
	rt := runtimeWith(t, "ui", mods["ui"], `{}`)
	out, err := eval(t, rt, `
		const ui = require("ui");
		ui.row(ui.text("hi", { bold: true }), ui.button("go", "act")).length;
	`)
	if err != nil {
		t.Fatalf("ui builders: %v", err)
	}
	if out != int64(2) {
		t.Fatalf("row length = %v", out)
	}
}

func keys(m map[string]providerapi.Module) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
