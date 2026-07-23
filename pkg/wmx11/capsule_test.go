package wmx11

import (
	"testing"

	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
)

// plantCapsule installs a placed capsule with a grant and a tile renderer,
// as explainWindow + a successful spawn would leave it.
func plantCapsule(w *WM, name, ref string) *capsuleState {
	spec := CapsuleSpec{ID: "capsule/" + name, Name: name, Ref: ref}
	spec.CapID = w.mintCapability(spec.ID, ActionWindowRead, ref).ID
	cs := &capsuleState{spec: spec, placed: true, close: func() {}}
	if w.capsules == nil {
		w.capsules = map[string]*capsuleState{}
	}
	w.capsules[spec.ID] = cs
	if w.scriptTiles == nil {
		w.scriptTiles = map[string]*scriptTile{}
	}
	w.scriptTiles[name] = &scriptTile{}
	return cs
}

func TestReapCapsulesEndsLeaseWhenTileGone(t *testing.T) {
	w := newRefTestWM()
	cs := plantCapsule(w, "explain-1", "wm.window/0x100")

	// Tile on a leaf → capsule survives reap.
	first := w.desktop.CurrentWorkspace().Root.Leaves()[0].ID
	res, err := wmcore.Apply(w.desktop, wmcore.Op{
		Op: wmcore.OpSplitLeaf, Node: first, Dir: wmcore.Row, App: scriptPrefix + "explain-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	w.reapCapsules()
	if w.capsules[cs.spec.ID] == nil {
		t.Fatal("capsule reaped while its tile is on screen")
	}

	// Close the tile's leaf → the next reap ends the lease: capsule gone,
	// capability revoked, renderer dropped.
	if _, err := wmcore.Apply(w.desktop, wmcore.Op{Op: wmcore.OpCloseLeaf, Node: res.NewLeaf}); err != nil {
		t.Fatal(err)
	}
	w.reapCapsules()
	if w.capsules[cs.spec.ID] != nil {
		t.Fatal("capsule not reaped after its tile closed")
	}
	if err := w.checkCapability(cs.spec.CapID, ActionWindowRead, cs.spec.Ref); err == nil {
		t.Fatal("capability survived the capsule's lease")
	}
	if _, ok := w.scriptTiles["explain-1"]; ok {
		t.Fatal("script tile renderer survived the capsule's lease")
	}
}

func TestTeardownCapsuleIdempotent(t *testing.T) {
	w := newRefTestWM()
	cs := plantCapsule(w, "explain-2", "wm.window/0x200")
	w.teardownCapsule(cs.spec.ID, "test")
	w.teardownCapsule(cs.spec.ID, "test") // second teardown is a no-op
	if len(w.capsules) != 0 || len(w.caps) != 0 {
		t.Fatalf("teardown left state: %d capsules, %d caps", len(w.capsules), len(w.caps))
	}
}

func TestUnplacedCapsuleNotReaped(t *testing.T) {
	w := newRefTestWM()
	cs := plantCapsule(w, "explain-3", "wm.window/0x300")
	cs.placed = false // spawn still in flight; its tile is not in the tree yet
	w.reapCapsules()
	if w.capsules[cs.spec.ID] == nil {
		t.Fatal("in-flight capsule reaped before placement")
	}
}
