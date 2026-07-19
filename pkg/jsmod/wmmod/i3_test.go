package wmmod_test

// GGWM-004 H3 tests: the i3-parity surface (theme, focus/move, exec
// gating, class rules) against the fake backend.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dop251/goja"
	"github.com/go-go-golems/go-go-goja/pkg/engine"

	"github.com/go-go-golems/go-go-wm/pkg/jsmod/wmmod"
)

// runErr evaluates src and returns the script error (nil on success).
func runErr(t *testing.T, rt *engine.Runtime, src string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := rt.Owner.Call(ctx, "test", func(_ context.Context, vm *goja.Runtime) (any, error) {
		_, err := vm.RunScript("t.js", src)
		return nil, err
	})
	return err
}

func TestThemeQueryAndSwitch(t *testing.T) {
	fake := newFake("")
	rt := newRuntime(t, fake)
	if got := run(t, rt, `const wm = require("wm"); wm.theme()`); got != "paper" {
		t.Fatalf("initial theme = %v", got)
	}
	if got := run(t, rt, `require("wm").theme("dark")`); got != "dark" {
		t.Fatalf("theme(dark) returned %v", got)
	}
	if got := run(t, rt, `require("wm").theme()`); got != "dark" {
		t.Fatalf("after switch theme() = %v", got)
	}
	got := run(t, rt, `require("wm").themes().join(",")`)
	if got != "paper,light,dark" {
		t.Fatalf("themes() = %v", got)
	}
}

func TestThemeUnknownThrows(t *testing.T) {
	rt := newRuntime(t, newFake(""))
	err := runErr(t, rt, `require("wm").theme("solarized")`)
	if err == nil || !strings.Contains(err.Error(), "unknown theme") {
		t.Fatalf("expected unknown-theme error, got %v", err)
	}
}

func TestFocusAndMoveReachBackend(t *testing.T) {
	fake := newFake("")
	rt := newRuntime(t, fake)
	if got := run(t, rt, `require("wm").focus("left")`); got != "left" {
		t.Fatalf("focus returned %v", got)
	}
	run(t, rt, `require("wm").move("right")`)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.focused != "left" || len(fake.moves) != 1 || fake.moves[0] != "right" {
		t.Fatalf("backend saw focused=%q moves=%v", fake.focused, fake.moves)
	}
}

func TestExecDisabledByDefault(t *testing.T) {
	rt := newRuntime(t, newFake(""))
	err := runErr(t, rt, `require("wm").exec("true")`)
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected exec-disabled error, got %v", err)
	}
}

func TestExecSpawnsWhenEnabled(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "touched")
	fake := newFake("")
	builder := engine.NewRuntimeFactoryBuilder()
	builder.WithModules(engine.NativeModuleRegistrar{
		ModuleID: "wm", ModuleName: wmmod.ModuleName,
		Loader: wmmod.New(fake, nil, wmmod.WithExec("")).Loader(),
	})
	factory, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	rt, err := factory.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rt.Close(t.Context()) }()
	run(t, rt, `require("wm").exec("touch `+marker+`")`)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("wm.exec child never created %s", marker)
}

func TestRuleClassNormalization(t *testing.T) {
	fake := newFake("")
	rt := newRuntime(t, fake)
	// Class-only rules normalize without a title (fan is nil, so arming
	// fails — expected here; normalization runs first and its error wins
	// only when the shape is bad).
	err := runErr(t, rt, `require("wm").rule({class: /Slack/, workspace: "8"})`)
	if err == nil || !strings.Contains(err.Error(), "broker connection") {
		t.Fatalf("class rule should normalize then fail on missing fan, got %v", err)
	}
	err = runErr(t, rt, `require("wm").rule({workspace: "8"})`)
	if err == nil || !strings.Contains(err.Error(), "title and/or class") {
		t.Fatalf("pattern-less rule should be rejected, got %v", err)
	}
	err = runErr(t, rt, `require("wm").rule({class: "([", workspace: "8"})`)
	if err == nil || !strings.Contains(err.Error(), "rule.class") {
		t.Fatalf("bad class regexp should be rejected, got %v", err)
	}
}
