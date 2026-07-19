package wmmod_test

// P2 test suite (design doc 02, Part VII.2): the wm module against a fake
// Backend that records every Op and applies it to a real wmcore.Desktop.
// Sugar is asserted as exact Op sequences; the recorded stream replayed
// onto a fresh desktop must reproduce the same tree (ops-as-data is the
// whole point).

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dop251/goja"
	"github.com/go-go-golems/go-go-goja/pkg/engine"

	"github.com/go-go-golems/go-go-wm/pkg/jsmod/wmmod"
	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
	"github.com/go-go-golems/go-go-wm/pkg/wmx11"
)

type fakeBackend struct {
	mu      sync.Mutex
	d       *wmcore.Desktop
	ops     []wmcore.Op
	wins    []wmx11.WindowInfo
	theme   string
	focused string
	moves   []string
}

func newFake(firstApp string) *fakeBackend {
	return &fakeBackend{d: wmcore.NewDesktop(firstApp)}
}

func (f *fakeBackend) Tree(context.Context) (*wmcore.Desktop, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, err := f.d.Serialize()
	if err != nil {
		return nil, err
	}
	return wmcore.DeserializeDesktop(raw)
}

func (f *fakeBackend) Windows(context.Context) ([]wmx11.WindowInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]wmx11.WindowInfo(nil), f.wins...), nil
}

func (f *fakeBackend) Apply(_ context.Context, op wmcore.Op) (wmcore.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	res, err := wmcore.Apply(f.d, op)
	if err == nil {
		f.ops = append(f.ops, op)
	}
	return res, err
}

func (f *fakeBackend) Bind(string, func()) error { return wmmod.ErrNoKeybindings }

func (f *fakeBackend) Theme(context.Context) (wmx11.ThemeInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	th := f.theme
	if th == "" {
		th = "paper"
	}
	return wmx11.ThemeInfo{Theme: th, Available: []string{"paper", "light", "dark"}}, nil
}

func (f *fakeBackend) SetTheme(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if name != "paper" && name != "light" && name != "dark" {
		return fmt.Errorf("unknown theme %q", name)
	}
	f.theme = name
	return nil
}

func (f *fakeBackend) Focus(_ context.Context, target string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.focused = target
	return target, nil
}

func (f *fakeBackend) Move(_ context.Context, dir string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.moves = append(f.moves, dir)
	return f.focused, nil
}

func newRuntime(t *testing.T, b wmmod.Backend) *engine.Runtime {
	t.Helper()
	builder := engine.NewRuntimeFactoryBuilder()
	builder.WithModules(engine.NativeModuleRegistrar{
		ModuleID: "wm", ModuleName: wmmod.ModuleName,
		Loader: wmmod.New(b, nil).Loader(),
	})
	factory, err := builder.Build()
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	rt, err := factory.NewRuntime()
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = rt.Close(ctx)
	})
	return rt
}

func run(t *testing.T, rt *engine.Runtime, src string) any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	v, err := rt.Owner.Call(ctx, "test", func(_ context.Context, vm *goja.Runtime) (any, error) {
		out, err := vm.RunScript("t.js", src)
		if err != nil {
			return nil, err
		}
		return out.Export(), nil
	})
	if err != nil {
		t.Fatalf("script: %v", err)
	}
	return v
}

func opNames(ops []wmcore.Op) []string {
	out := make([]string, len(ops))
	for i, o := range ops {
		out[i] = o.Op
	}
	return out
}

func TestSplitCompilesToOps(t *testing.T) {
	fake := newFake("editor")
	rootLeaf := string(fake.d.Workspaces[0].Root.ID)
	rt := newRuntime(t, fake)

	newLeaf := run(t, rt, fmt.Sprintf(`
		const wm = require("wm");
		wm.split(%q, "col", { ratio: 0.7, app: "terminal" });
	`, rootLeaf))

	got := opNames(fake.ops)
	if len(got) != 2 || got[0] != "split-leaf" || got[1] != "set-ratio" {
		t.Fatalf("ops = %v, want [split-leaf set-ratio]", got)
	}
	if fake.ops[0].Dir != wmcore.Col || fake.ops[0].App != "terminal" {
		t.Fatalf("split op operands: %+v", fake.ops[0])
	}
	if fake.ops[1].Ratio != 0.7 {
		t.Fatalf("ratio op: %+v", fake.ops[1])
	}
	// The set-ratio target must be the *parent split* of the new leaf.
	leafID := wmcore.NodeID(newLeaf.(string))
	parent := fake.d.Workspaces[0].Root
	if parent.ID != fake.ops[1].Node || parent.B.ID != leafID {
		t.Fatalf("set-ratio hit %q, want parent split %q of %q", fake.ops[1].Node, parent.ID, leafID)
	}
}

func TestWorkspaceFluentFindOrCreate(t *testing.T) {
	fake := newFake("a")
	rt := newRuntime(t, fake)

	run(t, rt, `
		const wm = require("wm");
		const ws = wm.workspace("mail");     // absent → add + rename
		ws.switch();
		const again = wm.workspace("mail");  // present → no new ops
		if (again.created) throw new Error("second lookup re-created");
		if (again.id !== ws.id) throw new Error("ids differ");
	`)
	got := opNames(fake.ops)
	want := []string{"add-workspace", "rename-workspace", "switch-workspace"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("ops = %v, want %v", got, want)
	}
}

func TestAdoptMovesLeafAcrossWorkspaces(t *testing.T) {
	fake := newFake("editor")
	rootLeaf := string(fake.d.Workspaces[0].Root.ID)
	rt := newRuntime(t, fake)

	run(t, rt, fmt.Sprintf(`
		const wm = require("wm");
		const extra = wm.split(%q, "row", { app: "terminal" });
		wm.workspace("scratch").adopt(extra, { dir: "col" });
	`, rootLeaf))

	// The moved leaf lives in "scratch" with its app intact.
	var scratch *wmcore.Workspace
	for i := range fake.d.Workspaces {
		if fake.d.Workspaces[i].Name == "scratch" {
			scratch = &fake.d.Workspaces[i]
		}
	}
	if scratch == nil {
		t.Fatal("scratch workspace missing")
	}
	found := false
	scratch.Root.Walk(func(n *wmcore.Node) {
		if n.Kind == wmcore.Leaf && n.App == "terminal" {
			found = true
		}
	})
	if !found {
		t.Fatalf("terminal leaf not adopted into scratch: %+v", scratch.Root)
	}
}

func TestBindThrowsHelpfulErrorOverIPC(t *testing.T) {
	rt := newRuntime(t, newFake("a"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := rt.Owner.Call(ctx, "test", func(_ context.Context, vm *goja.Runtime) (any, error) {
		return vm.RunScript("t.js", `require("wm").bind("Mod4-e", () => {})`)
	})
	if err == nil {
		t.Fatal("bind over IPC should throw")
	}
	if want := "rc.js"; !containsStr(err.Error(), want) {
		t.Fatalf("error should point at %s: %v", want, err)
	}
}

func TestFailedOpThrowsAndMutatesNothing(t *testing.T) {
	fake := newFake("a")
	rt := newRuntime(t, fake)
	before, _ := fake.d.Serialize()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := rt.Owner.Call(ctx, "test", func(_ context.Context, vm *goja.Runtime) (any, error) {
		return vm.RunScript("t.js", `require("wm").close("no-such-leaf")`)
	})
	if err == nil {
		t.Fatal("close of unknown leaf should throw")
	}
	after, _ := fake.d.Serialize()
	if string(before) != string(after) {
		t.Fatal("failed op mutated the desktop")
	}
	if len(fake.ops) != 0 {
		t.Fatalf("failed op recorded: %v", fake.ops)
	}
}

// TestReplayReproducesTree is the ops-as-data property: the op stream a
// script session produced, replayed onto a fresh desktop, must yield the
// identical serialized tree.
func TestReplayReproducesTree(t *testing.T) {
	fake := newFake("editor")
	rootLeaf := string(fake.d.Workspaces[0].Root.ID)
	rt := newRuntime(t, fake)
	run(t, rt, fmt.Sprintf(`
		const wm = require("wm");
		const a = wm.split(%q, "row", { ratio: 0.62, app: "terminal" });
		const b = wm.split(a, "col", { app: "notes" });
		wm.swap(a, b);
		const ws = wm.workspace("side");
		ws.adopt(b);
		ws.switch();
	`, rootLeaf))

	mirror := wmcore.NewDesktop("editor")
	for i, op := range fake.ops {
		if _, err := wmcore.Apply(mirror, op); err != nil {
			t.Fatalf("replay op %d (%s): %v", i, op.Op, err)
		}
	}
	want, _ := fake.d.Serialize()
	got, _ := mirror.Serialize()
	if string(want) != string(got) {
		t.Fatalf("replay diverged:\nlive:   %s\nreplay: %s", want, got)
	}
}

func containsStr(s, sub string) bool { return strings.Contains(s, sub) }

func TestLayoutNormalizeAndInspect(t *testing.T) {
	fake := newFake("a")
	rt := newRuntime(t, fake)
	run(t, rt, `
		const wm = require("wm");
		wm.layout("dev", {
			split: "row", ratio: 0.62,
			a: { app: "editor" },
			b: { split: "col", a: { app: "terminal" }, b: { app: "notes" } },
		});
		var plans = wm.layouts();
	`)
	plans, _ := run(t, rt, `wm.layouts()`).(map[string]interface{})
	dev, _ := plans["dev"].(map[string]interface{})
	if dev == nil || dev["kind"] != "split" || dev["ratio"] != 0.62 {
		t.Fatalf("normalized plan wrong: %v", dev)
	}
	b, _ := dev["b"].(map[string]interface{})
	if b == nil || b["kind"] != "split" || b["ratio"] != 0.5 {
		t.Fatalf("nested split not normalized with default ratio: %v", b)
	}

	// Definition-time failure: unknown keys and bad ratios throw.
	for _, bad := range []string{
		`wm.layout("x", { split: "diagonal", a: {app:"a"}, b: {app:"b"} })`,
		`wm.layout("x", { split: "row", frobnicate: 1, a: {app:"a"}, b: {app:"b"} })`,
		`wm.layout("x", { split: "row", ratio: 0.05, a: {app:"a"}, b: {app:"b"} })`,
		`wm.layout("x", { app: "a", extra: true })`,
		`wm.layout("x", {})`,
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err := rt.Owner.Call(ctx, "test", func(_ context.Context, vm *goja.Runtime) (any, error) {
			return vm.RunScript("t.js", bad)
		})
		cancel()
		if err == nil {
			t.Fatalf("bad layout accepted: %s", bad)
		}
	}
	if len(fake.ops) != 0 {
		t.Fatalf("defining layouts must not mutate: %v", fake.ops)
	}
}

func TestLayoutApplyBuildsOnceAndIsIdempotent(t *testing.T) {
	fake := newFake("") // fresh desktop, single empty leaf
	rt := newRuntime(t, fake)
	built := run(t, rt, `
		const wm = require("wm");
		wm.layout("dev", {
			split: "row", ratio: 0.62,
			a: { app: "editor" },
			b: { split: "col", a: { app: "terminal" }, b: { app: "notes" } },
		});
		wm.workspace("ws-1").apply("dev");
	`)
	if built != true {
		t.Fatalf("first apply should build, got %v", built)
	}
	root := fake.d.Workspaces[0].Root
	if root.Kind != wmcore.Split || root.Ratio != 0.62 || root.Dir != wmcore.Row {
		t.Fatalf("root split wrong: %+v", root)
	}
	apps := map[string]bool{}
	root.Walk(func(n *wmcore.Node) {
		if n.Kind == wmcore.Leaf {
			apps[n.App] = true
		}
	})
	if !apps["editor"] || !apps["terminal"] || !apps["notes"] {
		t.Fatalf("apps not placed: %v", apps)
	}

	before := len(fake.ops)
	again := run(t, rt, `wm.workspace("ws-1").apply("dev")`)
	if again != false {
		t.Fatalf("second apply should no-op, got %v", again)
	}
	if len(fake.ops) != before {
		t.Fatalf("idempotent apply minted ops: %v", fake.ops[before:])
	}
}

func TestRuleNormalization(t *testing.T) {
	fake := newFake("a")
	rt := newRuntime(t, fake)
	// Rules need a fan (broker); without one wm.rule must throw clearly.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := rt.Owner.Call(ctx, "test", func(_ context.Context, vm *goja.Runtime) (any, error) {
		return vm.RunScript("t.js", `require("wm").rule({ title: "zoom", workspace: "calls" })`)
	})
	if err == nil || !strings.Contains(err.Error(), "broker") {
		t.Fatalf("rule without broker should throw about the broker, got %v", err)
	}
	// normalizeRule runs before the broker check, so shape errors throw
	// regardless of the fan.
	for _, bad := range []string{
		`require("wm").rule({ title: "x", workspace: "w", frobnicate: 1 })`,
		`require("wm").rule({ workspace: "w" })`,
		`require("wm").rule({ title: "x" })`,
		`require("wm").rule({ title: "[", workspace: "w" })`,
	} {
		c2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
		_, err := rt.Owner.Call(c2, "test", func(_ context.Context, vm *goja.Runtime) (any, error) {
			return vm.RunScript("t.js", bad)
		})
		cancel2()
		if err == nil {
			t.Fatalf("bad rule accepted: %s", bad)
		}
	}
}
