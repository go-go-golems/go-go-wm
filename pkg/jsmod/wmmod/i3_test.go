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

func TestApplyBatch(t *testing.T) {
	fake := newFake("")
	rt := newRuntime(t, fake)
	got := run(t, rt, `
var r = require("wm").apply([
  {op: "add-workspace"},
  {op: "add-workspace"},
]);
r.length + ":" + (r[0].new_workspace !== r[1].new_workspace)`)
	if got != "2:true" {
		t.Fatalf("batch result = %v", got)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.ops) != 2 || fake.d.Workspaces == nil || len(fake.d.Workspaces) != 3 {
		t.Fatalf("backend saw %d ops, %d workspaces", len(fake.ops), len(fake.d.Workspaces))
	}
}

func TestApplyBatchBadOpThrows(t *testing.T) {
	rt := newRuntime(t, newFake(""))
	err := runErr(t, rt, `require("wm").apply([{op: "add-workspace"}, {op: "no-such-op"}])`)
	if err == nil || !strings.Contains(err.Error(), "no-such-op") {
		t.Fatalf("expected bad-op error, got %v", err)
	}
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

func TestFloatRulesPushDown(t *testing.T) {
	fake := newFake("")
	rt := newRuntime(t, fake)
	// Float-only rules need no broker fan (no event watcher); they push
	// the compiled override list straight to the backend.
	run(t, rt, `
const wm = require("wm");
wm.rule({class: /Galculator/, float: true});
wm.rule({title: "File Transfer", float: true});
wm.rule({class: /mpv/, float: false});
wm.rules().length`)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.floatRules) != 3 {
		t.Fatalf("pushed %d float rules, want 3: %+v", len(fake.floatRules), fake.floatRules)
	}
	if fake.floatRules[0].Class != "Galculator" || !fake.floatRules[0].Float {
		t.Fatalf("rule 0 = %+v", fake.floatRules[0])
	}
	if fake.floatRules[2].Class != "mpv" || fake.floatRules[2].Float {
		t.Fatalf("rule 2 = %+v", fake.floatRules[2])
	}
}

func TestFloatRuleValidation(t *testing.T) {
	rt := newRuntime(t, newFake(""))
	err := runErr(t, rt, `require("wm").rule({class: /x/})`)
	if err == nil || !strings.Contains(err.Error(), "workspace and/or a float") {
		t.Fatalf("rule without workspace or float should be rejected, got %v", err)
	}
	err = runErr(t, rt, `require("wm").rule({class: /x/, float: "yes"})`)
	if err == nil || !strings.Contains(err.Error(), "float must be") {
		t.Fatalf("non-bool float should be rejected, got %v", err)
	}
}

func TestFloatToggleExport(t *testing.T) {
	fake := newFake("")
	rt := newRuntime(t, fake)
	got := run(t, rt, `
const wm = require("wm");
wm.float() + ":" + wm.float()`)
	if got != "true:false" {
		t.Fatalf("wm.float toggles = %v, want true:false", got)
	}
}

func TestLaunchAndLauncherExports(t *testing.T) {
	fake := newFake("")
	rt := newRuntime(t, fake)
	got := run(t, rt, `
const wm = require("wm");
const k1 = wm.launch("app:firefox");
const k2 = wm.launch("htop");
wm.launcher();
k1 + ":" + k2`)
	if got != "app:exec" {
		t.Fatalf("launch kinds = %v", got)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.launched) != 2 || fake.launched[0] != "app:firefox" || fake.launched[1] != "htop" {
		t.Fatalf("launched = %v", fake.launched)
	}
	if fake.launcherOpens != 1 {
		t.Fatalf("launcherOpens = %d", fake.launcherOpens)
	}
}

func TestCommandRegistersAndFires(t *testing.T) {
	fake := newFake("")
	rt := newRuntime(t, fake)
	run(t, rt, `
const wm = require("wm");
var fired = 0;
wm.command({ id: "proj", label: "project workspace",
             doc: "layout + editor", run() { fired++; } });`)
	fake.mu.Lock()
	fire := fake.commands["proj"]
	labels := append([]string(nil), fake.commandLabels...)
	fake.mu.Unlock()
	if fire == nil {
		t.Fatalf("command not registered: %v", labels)
	}
	if labels[0] != "proj|project workspace|layout + editor" {
		t.Fatalf("registration payload: %v", labels)
	}
	// Firing posts into the runtime; observe the side effect.
	fire()
	if got := run(t, rt, `fired`); got != int64(1) {
		t.Fatalf("run callback never fired: %v", got)
	}
}

func TestCommandValidation(t *testing.T) {
	rt := newRuntime(t, newFake(""))
	err := runErr(t, rt, `require("wm").command({label: "x", run() {}})`)
	if err == nil || !strings.Contains(err.Error(), "id and label") {
		t.Fatalf("missing id must be rejected, got %v", err)
	}
	err = runErr(t, rt, `require("wm").command({id: "x", label: "x"})`)
	if err == nil || !strings.Contains(err.Error(), "run must be a function") {
		t.Fatalf("missing run must be rejected, got %v", err)
	}
}

func TestFullscreenExport(t *testing.T) {
	rt := newRuntime(t, newFake(""))
	got := run(t, rt, `
const wm = require("wm");
wm.fullscreen() + ":" + wm.fullscreen()`)
	if got != "true:false" {
		t.Fatalf("wm.fullscreen toggles = %v", got)
	}
}

func TestCommandA2WithoutBrokerErrors(t *testing.T) {
	fake := newFake("")
	fake.a2 = true            // IPC-style backend: no in-process command hosting
	rt := newRuntime(t, fake) // fan is nil → the A2 path must error clearly
	err := runErr(t, rt, `require("wm").command({id: "x", label: "x", run() {}})`)
	if err == nil || !strings.Contains(err.Error(), "broker connection") {
		t.Fatalf("A2 without a broker must point at the broker, got %v", err)
	}
}
