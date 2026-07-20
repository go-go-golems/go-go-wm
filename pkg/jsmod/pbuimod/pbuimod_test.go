package pbuimod_test

// P1 test suite (design doc 02, Part VII.1): the pbui module against a
// bare broker — no X anywhere. Scripts run in a real factory-built
// runtime; assertions come from a second Go client or from script globals
// read back through the owner.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dop251/goja"
	"github.com/go-go-golems/go-go-goja/pkg/engine"

	"github.com/go-go-golems/go-go-wm/pkg/jsmod/pbuimod"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/broker"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/client"
)

func startBroker(t *testing.T) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "b.sock")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = broker.New().ListenAndServe(ctx, sock) }()
	for i := 0; i < 200; i++ {
		if _, err := os.Stat(sock); err == nil {
			return sock
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("broker socket %s never appeared", sock)
	return ""
}

func connect(t *testing.T, sock, name string) *client.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cl, err := client.Connect(ctx, client.Options{Socket: sock, Name: name})
	if err != nil {
		t.Fatalf("connect %s: %v", name, err)
	}
	t.Cleanup(func() { _ = cl.Close() })
	return cl
}

func newRuntime(t *testing.T, cl *client.Client, opts ...pbuimod.Option) *engine.Runtime {
	t.Helper()
	builder := engine.NewRuntimeFactoryBuilder()
	builder.WithModules(engine.NativeModuleRegistrar{
		ModuleID: "pbui", ModuleName: pbuimod.ModuleName,
		Loader: pbuimod.New(cl, opts...).Loader(),
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

func runScript(t *testing.T, rt *engine.Runtime, src string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := rt.Owner.Call(ctx, "test.run", func(_ context.Context, vm *goja.Runtime) (any, error) {
		return vm.RunScript("test.js", src)
	}); err != nil {
		t.Fatalf("script: %v", err)
	}
}

// global reads a script global as exported Go data.
func global(t *testing.T, rt *engine.Runtime, name string) any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	v, err := rt.Owner.Call(ctx, "test.global", func(_ context.Context, vm *goja.Runtime) (any, error) {
		g := vm.GlobalObject().Get(name)
		if g == nil {
			return nil, nil
		}
		return g.Export(), nil
	})
	if err != nil {
		t.Fatalf("global %s: %v", name, err)
	}
	return v
}

// waitGlobal polls a script global until pred is happy.
func waitGlobal(t *testing.T, rt *engine.Runtime, name string, pred func(any) bool) any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		v := global(t, rt, name)
		if pred(v) {
			return v
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("global %s never satisfied predicate (last: %v)", name, global(t, rt, name))
	return nil
}

func TestAcceptResolvesWithAnsweredObject(t *testing.T) {
	sock := startBroker(t)
	scriptCl := connect(t, sock, "script")
	answerer := connect(t, sock, "answerer")

	answerer.OnAcceptMode(func(session string, ptypes []string, prompt string) {
		obj, _ := pbui.NewObject("color", "#b0563f")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = answerer.Answer(ctx, session, obj)
	})

	rt := newRuntime(t, scriptCl)
	runScript(t, rt, `
		var done = false, got = null;
		require("pbui").accept("color", "pick a color").then(o => { got = o; done = true; });
	`)
	waitGlobal(t, rt, "done", func(v any) bool { b, _ := v.(bool); return b })
	got, _ := global(t, rt, "got").(map[string]interface{})
	if got == nil || got["ptype"] != "color" || got["value"] != "#b0563f" {
		t.Fatalf("accept resolved with %v", got)
	}
}

func TestAcceptCancelledResolvesNull(t *testing.T) {
	sock := startBroker(t)
	scriptCl := connect(t, sock, "script")
	canceller := connect(t, sock, "canceller")

	canceller.OnAcceptMode(func(session string, ptypes []string, prompt string) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = canceller.Cancel(ctx, session)
	})

	rt := newRuntime(t, scriptCl)
	runScript(t, rt, `
		var done = false, got = "unset";
		require("pbui").accept(["color"], "doomed").then(o => { got = o; done = true; });
	`)
	waitGlobal(t, rt, "done", func(v any) bool { b, _ := v.(bool); return b })
	if got := global(t, rt, "got"); got != nil {
		t.Fatalf("cancelled accept resolved with %v, want null", got)
	}
}

func TestVerbRoundTrip(t *testing.T) {
	sock := startBroker(t)
	scriptCl := connect(t, sock, "script")
	invoker := connect(t, sock, "invoker")

	rt := newRuntime(t, scriptCl)
	runScript(t, rt, `
		var hits = [];
		require("pbui").verb(
			{ id: "test.shout", label: "Shout", ptypes: ["word"] },
			o => { hits.push(o.value.toUpperCase()); });
	`)

	obj, _ := pbui.NewObject("word", "quiet")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := invoker.InvokeVerb(ctx, "test.shout", obj); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	waitGlobal(t, rt, "hits", func(v any) bool { s, _ := v.([]interface{}); return len(s) == 1 })
	hits, _ := global(t, rt, "hits").([]interface{})
	if hits[0] != "QUIET" {
		t.Fatalf("handler saw %v", hits)
	}
}

func TestVerbReregistrationReplacesHandler(t *testing.T) {
	sock := startBroker(t)
	scriptCl := connect(t, sock, "script")
	invoker := connect(t, sock, "invoker")

	rt := newRuntime(t, scriptCl)
	runScript(t, rt, `
		var calls = [];
		const pbui = require("pbui");
		pbui.verb({ id: "test.v", ptypes: ["any"] }, () => calls.push("old"));
		pbui.verb({ id: "test.v", ptypes: ["any"] }, () => calls.push("new"));
	`)
	obj, _ := pbui.NewObject("word", "x")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := invoker.InvokeVerb(ctx, "test.v", obj); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	got := waitGlobal(t, rt, "calls", func(v any) bool { s, _ := v.([]interface{}); return len(s) == 1 })
	if got.([]interface{})[0] != "new" {
		t.Fatalf("stale handler ran: %v", got)
	}
}

func TestPrintEmitsListenerPrint(t *testing.T) {
	sock := startBroker(t)
	scriptCl := connect(t, sock, "script")
	watcher := connect(t, sock, "watcher")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events, err := watcher.Events(ctx)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	rt := newRuntime(t, scriptCl)
	runScript(t, rt, `
		const pbui = require("pbui");
		pbui.print("the color ", pbui.object("color", "#5a7a58"), " looks nice");
	`)

	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("event stream closed")
			}
			if ev.Event != "listener.print" {
				continue
			}
			var data struct {
				Segs []map[string]interface{} `json:"segs"`
			}
			if err := json.Unmarshal(ev.Data, &data); err != nil {
				t.Fatalf("bad segs payload: %v", err)
			}
			if len(data.Segs) != 3 || data.Segs[0]["text"] != "the color " ||
				data.Segs[1]["ptype"] != "color" || data.Segs[1]["value"] != "#5a7a58" {
				t.Fatalf("segs = %v", data.Segs)
			}
			return
		case <-ctx.Done():
			t.Fatal("listener.print never arrived")
		}
	}
}

func TestEventSubscriptionAndWildcard(t *testing.T) {
	sock := startBroker(t)
	scriptCl := connect(t, sock, "script")
	emitter := connect(t, sock, "emitter")

	rt := newRuntime(t, scriptCl)
	runScript(t, rt, `
		var pings = [], all = [];
		const pbui = require("pbui");
		pbui.on("test.ping", ev => pings.push(ev.data.n));
		pbui.on("*", ev => all.push(ev.event));
	`)

	for i := 1; i <= 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := emitter.Emit(ctx, "test.ping", map[string]interface{}{"n": i})
		cancel()
		if err != nil {
			t.Fatalf("emit %d: %v", i, err)
		}
	}
	got := waitGlobal(t, rt, "pings", func(v any) bool { s, _ := v.([]interface{}); return len(s) == 3 })
	want := fmt.Sprint([]interface{}{int64(1), int64(2), int64(3)})
	if fmt.Sprint(got) != want {
		t.Fatalf("pings = %v (ordering must be preserved)", got)
	}
	all, _ := global(t, rt, "all").([]interface{})
	if len(all) < 3 {
		t.Fatalf("wildcard missed events: %v", all)
	}
}

func TestDataHelpersRoundTrip(t *testing.T) {
	// Data-only: no broker at all.
	rt := newRuntime(t, nil)
	runScript(t, rt, `
		const pbui = require("pbui");
		const obj = pbui.object("git-commit", "deadbeef");
		const uri = pbui.uri(obj);
		const back = pbui.parse(uri);
		var result = { uri: uri, ptype: back.ptype, value: back.value };
		var linked = pbui.link(obj, "deadbeef");
	`)
	res, _ := global(t, rt, "result").(map[string]interface{})
	if res == nil || res["ptype"] != "git-commit" || res["value"] != "deadbeef" {
		t.Fatalf("uri round trip: %v", res)
	}
	if linked, _ := global(t, rt, "linked").(string); linked == "" {
		t.Fatal("link produced empty string")
	}
}

func TestClientlessBrokerCallsThrow(t *testing.T) {
	rt := newRuntime(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := rt.Owner.Call(ctx, "test.run", func(_ context.Context, vm *goja.Runtime) (any, error) {
		return vm.RunScript("t.js", `require("pbui").emit("x", {})`)
	})
	if err == nil {
		t.Fatal("emit without a broker should throw")
	}
}
