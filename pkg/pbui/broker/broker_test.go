package broker_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-go-golems/go-go-wm/pkg/pbui"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/broker"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/client"
)

// startBroker runs a broker on a tmpdir socket and returns the socket path.
func startBroker(t *testing.T) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "pbui.sock")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	b := broker.New()
	errCh := make(chan error, 1)
	go func() { errCh <- b.ListenAndServe(ctx, sock) }()
	// Wait for the socket to appear.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := connect(ctx, sock, "probe"); err == nil {
			_ = c.Close()
			return sock
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("broker did not come up on %s", sock)
	return ""
}

func connect(ctx context.Context, sock, name string, roles ...string) (*client.Client, error) {
	return client.Connect(ctx, client.Options{Socket: sock, Name: name, Roles: roles})
}

func mustConnect(t *testing.T, sock, name string, roles ...string) *client.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := connect(ctx, sock, name, roles...)
	if err != nil {
		t.Fatalf("connect %s: %v", name, err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestAcceptAnswerFlow(t *testing.T) {
	sock := startBroker(t)
	requester := mustConnect(t, sock, "req")
	answerer := mustConnect(t, sock, "ans")

	// The answerer learns of accept mode via its handler.
	gotMode := make(chan string, 1)
	answerer.OnAcceptMode(func(session string, ptypes []string, prompt string) {
		if len(ptypes) == 1 && ptypes[0] == "color" {
			gotMode <- session
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resultCh := make(chan *pbui.Object, 1)
	go func() {
		obj, err := requester.Accept(ctx, []string{"color"}, "MIX — click a COLOR")
		if err != nil {
			t.Errorf("accept: %v", err)
		}
		resultCh <- obj
	}()

	var session string
	select {
	case session = <-gotMode:
	case <-ctx.Done():
		t.Fatal("answerer never saw accept.mode")
	}

	obj, _ := pbui.NewObject("color", "#b0563f")
	if err := answerer.Answer(ctx, session, obj); err != nil {
		t.Fatalf("answer: %v", err)
	}

	select {
	case got := <-resultCh:
		if got == nil || got.Ptype != "color" || got.StringValue() != "#b0563f" {
			t.Fatalf("wrong result: %+v", got)
		}
	case <-ctx.Done():
		t.Fatal("requester never got accept.result")
	}
}

func TestAcceptCancel(t *testing.T) {
	sock := startBroker(t)
	requester := mustConnect(t, sock, "req")
	wm := mustConnect(t, sock, "wm", "wm")

	gotMode := make(chan string, 1)
	wm.OnAcceptMode(func(session string, _ []string, _ string) { gotMode <- session })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resultCh := make(chan *pbui.Object, 1)
	go func() {
		obj, err := requester.Accept(ctx, []string{"number"}, "SUM")
		if err != nil {
			t.Errorf("accept: %v", err)
		}
		resultCh <- obj
	}()

	session := <-gotMode
	if err := wm.Cancel(ctx, session); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	select {
	case got := <-resultCh:
		if got != nil {
			t.Fatalf("cancelled accept returned %+v, want nil", got)
		}
	case <-ctx.Done():
		t.Fatal("requester never resolved after cancel")
	}
}

func TestTypeMismatchRejected(t *testing.T) {
	sock := startBroker(t)
	requester := mustConnect(t, sock, "req")
	answerer := mustConnect(t, sock, "ans")

	gotMode := make(chan string, 1)
	answerer.OnAcceptMode(func(session string, _ []string, _ string) { gotMode <- session })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() { _, _ = requester.Accept(ctx, []string{"color"}, "PICK") }()
	session := <-gotMode

	obj, _ := pbui.NewObject("number", 42)
	if err := answerer.Answer(ctx, session, obj); err == nil {
		t.Fatal("type-mismatched answer should be rejected")
	}
}

func TestSecondAcceptSupersedesFirst(t *testing.T) {
	sock := startBroker(t)
	first := mustConnect(t, sock, "first")
	second := mustConnect(t, sock, "second")
	answerer := mustConnect(t, sock, "ans")

	modes := make(chan string, 4)
	answerer.OnAcceptMode(func(session string, _ []string, _ string) { modes <- session })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	firstResult := make(chan *pbui.Object, 1)
	go func() {
		obj, _ := first.Accept(ctx, []string{"color"}, "one")
		firstResult <- obj
	}()
	<-modes

	secondResult := make(chan *pbui.Object, 1)
	go func() {
		obj, _ := second.Accept(ctx, []string{"color"}, "two")
		secondResult <- obj
	}()
	s2 := <-modes

	// First accept must have resolved nil (superseded).
	select {
	case got := <-firstResult:
		if got != nil {
			t.Fatalf("superseded accept returned %+v, want nil", got)
		}
	case <-ctx.Done():
		t.Fatal("first accept never resolved")
	}

	obj, _ := pbui.NewObject("color", "#9cb4c2")
	if err := answerer.Answer(ctx, s2, obj); err != nil {
		t.Fatalf("answer second: %v", err)
	}
	select {
	case got := <-secondResult:
		if got == nil || got.StringValue() != "#9cb4c2" {
			t.Fatalf("second accept got %+v", got)
		}
	case <-ctx.Done():
		t.Fatal("second accept never resolved")
	}
}

func TestRequesterDisconnectClearsSession(t *testing.T) {
	sock := startBroker(t)
	requester := mustConnect(t, sock, "req")
	watcher := mustConnect(t, sock, "watch")

	mode := make(chan string, 1)
	cleared := make(chan string, 1)
	watcher.OnAcceptMode(func(session string, _ []string, _ string) { mode <- session })
	watcher.OnAcceptClear(func(session, reason string) { cleared <- reason })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() { _, _ = requester.Accept(ctx, []string{"any"}, "COLLECT") }()
	<-mode

	_ = requester.Close()

	select {
	case reason := <-cleared:
		if reason != "requester-disconnected" {
			t.Fatalf("clear reason = %q", reason)
		}
	case <-ctx.Done():
		t.Fatal("session not cleared on requester disconnect")
	}
}

func TestVerbRegistryAndInvoke(t *testing.T) {
	sock := startBroker(t)
	owner := mustConnect(t, sock, "colorlab")
	caller := mustConnect(t, sock, "caller")

	ran := make(chan string, 1)
	owner.OnVerbRun(func(verbID string, obj *pbui.Object) {
		ran <- verbID + ":" + obj.StringValue()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := owner.RegisterVerbs(ctx, []pbui.Verb{
		{ID: "color.mix", Label: "Mix with…", Ptypes: []string{"color"}, Accepts: []string{"color"}},
		{ID: "any.inspect", Label: "Inspect", Ptypes: []string{"any"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	verbs, err := caller.QueryVerbs(ctx, "color")
	if err != nil {
		t.Fatal(err)
	}
	if len(verbs) != 2 {
		t.Fatalf("verbs for color = %d, want 2 (mix + any.inspect): %+v", len(verbs), verbs)
	}

	obj, _ := pbui.NewObject("color", "#d3b56a")
	if err := caller.InvokeVerb(ctx, "color.mix", obj); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-ran:
		if got != "color.mix:#d3b56a" {
			t.Fatalf("verb.run got %q", got)
		}
	case <-ctx.Done():
		t.Fatal("owner never received verb.run")
	}
}

func TestMenuRequestFallsBackToVerbList(t *testing.T) {
	sock := startBroker(t)
	owner := mustConnect(t, sock, "colorlab")
	caller := mustConnect(t, sock, "caller")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = owner.RegisterVerbs(ctx, []pbui.Verb{
		{ID: "color.lighten", Label: "Lighten", Ptypes: []string{"color"}},
	})

	obj, _ := pbui.NewObject("color", "#b0563f")
	verbs, err := caller.RequestMenu(ctx, obj, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	// No WM connected → broker falls back to a verb list.
	if len(verbs) != 1 || verbs[0].ID != "color.lighten" {
		t.Fatalf("fallback verb list = %+v", verbs)
	}
}

func TestEventBus(t *testing.T) {
	sock := startBroker(t)
	emitter := mustConnect(t, sock, "emitter")
	listener := mustConnect(t, sock, "listener")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	events, err := listener.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := emitter.Emit(ctx, "color_added", map[string]string{"hex": "#b05f7c"}); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case ev := <-events:
			if ev == nil {
				t.Fatal("event channel closed")
			}
			if ev.Event == "color_added" {
				if ev.Source != "emitter" || ev.EventSeq == 0 {
					t.Fatalf("bad event: %+v", ev)
				}
				return
			}
			// Skip connection bookkeeping events.
		case <-ctx.Done():
			t.Fatal("event never delivered")
		}
	}
}
