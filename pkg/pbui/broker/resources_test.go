package broker_test

import (
	"context"
	"testing"
	"time"

	"github.com/go-go-golems/go-go-wm/pkg/pbui"
)

// waitFor polls until cond returns true or the deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestWelcomeAssignsPrincipal(t *testing.T) {
	sock := startBroker(t)
	a := mustConnect(t, sock, "alpha")
	b := mustConnect(t, sock, "alpha") // same NAME on purpose
	defer func() { _ = a.Close(); _ = b.Close() }()

	if a.Principal() == "" || b.Principal() == "" {
		t.Fatalf("principals not assigned: %q / %q", a.Principal(), b.Principal())
	}
	if a.Principal() == b.Principal() {
		t.Fatalf("two connections share principal %q", a.Principal())
	}
}

// TestSameNameVerbCleanup pins the M1 fix: two clients sharing a name, one
// disconnects, the survivor's verbs must remain routable. Under the old
// name-keyed sweep the survivor's verbs were dropped with the dead client's.
func TestSameNameVerbCleanup(t *testing.T) {
	sock := startBroker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	a := mustConnect(t, sock, "twin")
	b := mustConnect(t, sock, "twin")
	caller := mustConnect(t, sock, "caller")
	defer func() { _ = b.Close(); _ = caller.Close() }()

	if err := a.RegisterVerbs(ctx, []pbui.Verb{{ID: "a.verb", Label: "A", Ptypes: []string{"thing"}}}); err != nil {
		t.Fatal(err)
	}
	if err := b.RegisterVerbs(ctx, []pbui.Verb{{ID: "b.verb", Label: "B", Ptypes: []string{"thing"}}}); err != nil {
		t.Fatal(err)
	}

	ran := make(chan string, 1)
	b.OnVerbRun(func(verbID string, _ *pbui.Object) { ran <- verbID })

	_ = a.Close() // twin #1 dies; twin #2's verb must survive

	waitFor(t, "a.verb to be revoked", func() bool {
		verbs, err := caller.QueryVerbs(ctx, "thing")
		if err != nil {
			return false
		}
		hasA, hasB := false, false
		for _, v := range verbs {
			if v.ID == "a.verb" {
				hasA = true
			}
			if v.ID == "b.verb" {
				hasB = true
			}
		}
		return !hasA && hasB
	})

	if err := caller.InvokeVerb(ctx, "b.verb", pbui.Object{Ptype: "thing", Value: []byte(`"x"`)}); err != nil {
		t.Fatalf("invoking survivor's verb: %v", err)
	}
	select {
	case id := <-ran:
		if id != "b.verb" {
			t.Fatalf("wrong verb ran: %s", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("survivor never received verb.run")
	}
}

func TestResourceLifecycle(t *testing.T) {
	sock := startBroker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	owner := mustConnect(t, sock, "owner")
	other := mustConnect(t, sock, "other")
	defer func() { _ = other.Close() }()

	if err := owner.RegisterResource(ctx, pbui.Resource{
		ID: "capsule/test-1", Kind: "wm.capsule", Label: "Test capsule",
	}); err != nil {
		t.Fatal(err)
	}

	find := func(id string) *pbui.Resource {
		rs, err := other.ListResources(ctx)
		if err != nil {
			return nil
		}
		for i := range rs {
			if rs[i].ID == id {
				return &rs[i]
			}
		}
		return nil
	}
	r := find("capsule/test-1")
	if r == nil {
		t.Fatal("registered resource not listed")
	}
	if r.Owner != owner.Principal() || r.OwnerLabel != "owner" {
		t.Fatalf("resource owner = %q/%q, want %q/owner", r.Owner, r.OwnerLabel, owner.Principal())
	}

	// A different principal cannot claim or close it.
	if err := other.RegisterResource(ctx, pbui.Resource{ID: "capsule/test-1", Kind: "wm.capsule"}); err == nil {
		t.Fatal("foreign re-register should conflict")
	}
	if err := other.CloseLease(ctx, "capsule/test-1"); err == nil {
		t.Fatal("foreign lease.close should be rejected")
	}

	// Owner close revokes; a second close is idempotent.
	if err := owner.CloseLease(ctx, "capsule/test-1"); err != nil {
		t.Fatal(err)
	}
	if err := owner.CloseLease(ctx, "capsule/test-1"); err != nil {
		t.Fatalf("lease.close is not idempotent: %v", err)
	}
	if find("capsule/test-1") != nil {
		t.Fatal("resource survived lease.close")
	}
	_ = owner.Close()
}

func TestDisconnectRevokesAllResources(t *testing.T) {
	sock := startBroker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	watcher := mustConnect(t, sock, "watcher")
	defer func() { _ = watcher.Close() }()
	events, err := watcher.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}

	doomed := mustConnect(t, sock, "doomed")
	if err := doomed.RegisterVerbs(ctx, []pbui.Verb{{ID: "d.verb", Label: "D", Ptypes: []string{"x"}}}); err != nil {
		t.Fatal(err)
	}
	if err := doomed.RegisterResource(ctx, pbui.Resource{ID: "capsule/doomed", Kind: "wm.capsule"}); err != nil {
		t.Fatal(err)
	}
	if _, err := doomed.Events(ctx); err != nil { // creates a subscription resource
		t.Fatal(err)
	}
	doomedPrincipal := doomed.Principal()
	_ = doomed.Close()

	// Everything doomed owned must vanish, with a lease.ended fact each.
	waitFor(t, "doomed's resources to be revoked", func() bool {
		rs, err := watcher.ListResources(ctx)
		if err != nil {
			return false
		}
		for _, r := range rs {
			if r.Owner == doomedPrincipal {
				return false
			}
		}
		return true
	})

	ended := map[string]bool{}
	deadline := time.After(2 * time.Second)
	for len(ended) < 3 {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("event stream closed early")
			}
			if ev.Event == "lease.ended" {
				ended[string(ev.Data)] = true
			}
		case <-deadline:
			t.Fatalf("saw %d lease.ended facts, want 3 (verb, capsule, subscription)", len(ended))
		}
	}
}
