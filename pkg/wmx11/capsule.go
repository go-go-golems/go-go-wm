package wmx11

import (
	"fmt"

	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
)

// Transient app capsules (GGWM-013 M5).
//
// A capsule is a bounded computation with one surface and an explicit
// grant list. v0 has one capsule kind — "Explain this window" — spawned by
// a verb on tile presentations. The WM owns the whole lifecycle:
//
//	verb invoked → mint capability → spawn runtime (injected, so this
//	package stays goja-free per design decision U-D3) → place the tile →
//	tile closed → revoke capability, dispose runtime, drop the tile.
//
// The runtime itself is Tier 1: a separate Goja VM with only the ui and
// sem modules — an API and concurrency boundary, deliberately NOT claimed
// as a security boundary. The out-of-process Tier 2 spawner slots in
// behind the same CapsuleSpec later.

// CapsuleSpec is everything the injected spawner needs to build a capsule
// runtime: its identity (the capability holder), its tile name, and the
// one grant it holds.
type CapsuleSpec struct {
	ID    string // capability holder, e.g. "capsule/explain-3"
	Name  string // script tile name; the tile app is "script:"+Name
	Ref   string // the window ref the grant covers
	CapID string // the minted wm.window.read capability
}

// capsuleState tracks one live capsule on the WM loop.
type capsuleState struct {
	spec   CapsuleSpec
	close  func() // disposes the runtime + its broker client; run OFF the WM loop
	placed bool   // the tile has been applied to a leaf; reap may consider it
}

// explainWindow spawns the Explain capsule for a tiled client window.
// WM loop only (verb dispatch).
func (w *WM) explainWindow(leaf wmcore.NodeID) {
	f := w.frames[leaf]
	if f == nil || f.client == 0 {
		w.setMouseDoc("explain: this tile has no client window")
		return
	}
	if w.cfg.SpawnCapsule == nil {
		w.setMouseDoc("explain: capsule runtime unavailable (no --rc host)")
		return
	}
	w.nextCapsule++
	name := fmt.Sprintf("explain-%d", w.nextCapsule)
	spec := CapsuleSpec{
		ID:   "capsule/" + name,
		Name: name,
		Ref:  windowRef(f.client),
	}
	spec.CapID = w.mintCapability(spec.ID, ActionWindowRead, spec.Ref).ID
	if w.capsules == nil {
		w.capsules = map[string]*capsuleState{}
	}
	cs := &capsuleState{spec: spec}
	w.capsules[spec.ID] = cs

	// Runtime construction dials the broker and evaluates JS — never on
	// the WM loop.
	go func() {
		closeFn, err := w.cfg.SpawnCapsule(spec)
		w.Post(func() {
			if err != nil {
				log.Warn().Err(err).Str("capsule", spec.ID).Msg("capsule spawn failed")
				w.teardownCapsule(spec.ID, "spawn-failed")
				return
			}
			cs.close = closeFn
			// Place the tile next to the window it explains. The capsule
			// itself has no wm module, so placement is the WM's act.
			if _, aerr := w.Apply(wmcore.Op{
				Op: wmcore.OpSplitLeaf, Node: leaf, Dir: wmcore.Row,
				App: scriptPrefix + spec.Name,
			}); aerr != nil {
				log.Warn().Err(aerr).Str("capsule", spec.ID).Msg("capsule placement failed")
				w.teardownCapsule(spec.ID, "placement-failed")
				return
			}
			cs.placed = true
			w.emitEvent("capsule.started", map[string]interface{}{
				"capsule": spec.ID, "ref": spec.Ref,
			})
		})
	}()
}

// reapCapsules tears down every placed capsule whose tile no longer exists
// on any leaf — the user closed it, or its workspace died. Called from
// syncBuiltins, so it runs after every desktop mutation. WM loop only.
func (w *WM) reapCapsules() {
	for id, cs := range w.capsules {
		if !cs.placed {
			continue
		}
		if !w.tileAppInUse(scriptPrefix + cs.spec.Name) {
			w.teardownCapsule(id, "tile-closed")
		}
	}
}

// tileAppInUse reports whether any leaf in any workspace shows the app.
func (w *WM) tileAppInUse(app string) bool {
	for i := range w.desktop.Workspaces {
		for _, l := range w.desktop.Workspaces[i].Root.Leaves() {
			if l.App == app {
				return true
			}
		}
	}
	return false
}

// teardownCapsule ends a capsule's lease: capability revoked, tile
// renderer dropped, runtime disposed (off-loop), lifecycle fact emitted.
// Idempotent. WM loop only.
func (w *WM) teardownCapsule(id, reason string) {
	cs := w.capsules[id]
	if cs == nil {
		return
	}
	delete(w.capsules, id)
	w.revokeCapabilitiesFor(id)
	delete(w.scriptTiles, cs.spec.Name)
	if cs.close != nil {
		go cs.close() // runtime + broker teardown blocks; never on the WM loop
	}
	w.emitEvent("capsule.ended", map[string]interface{}{
		"capsule": id, "ref": cs.spec.Ref, "reason": reason,
	})
}

// semSnapshot is the {"q":"sem"} answer: the semantic kernel's WM-side
// state, for tests and debugging. Broker-side resources are listed by the
// broker itself (resource.list); this dump is deliberately WM-only so it
// never blocks the loop on a broker round trip.
type semSnapshot struct {
	Capsules     []semCapsule    `json:"capsules"`
	Capabilities []semCapability `json:"capabilities"`
	Tombstones   int             `json:"tombstones"`
}

type semCapsule struct {
	ID     string `json:"id"`
	Ref    string `json:"ref"`
	Placed bool   `json:"placed"`
}

// semCapability reports a grant WITHOUT its ID: the ID is the bearer
// token, and the debug surface should prove existence, not mint access.
type semCapability struct {
	Holder string `json:"holder"`
	Action string `json:"action"`
	Ref    string `json:"ref"`
}

// semState builds the dump. WM loop only.
func (w *WM) semState() semSnapshot {
	s := semSnapshot{
		Capsules:     []semCapsule{},
		Capabilities: []semCapability{},
		Tombstones:   len(w.tombstones),
	}
	for _, cs := range w.capsules {
		s.Capsules = append(s.Capsules, semCapsule{ID: cs.spec.ID, Ref: cs.spec.Ref, Placed: cs.placed})
	}
	for _, c := range w.caps {
		s.Capabilities = append(s.Capabilities, semCapability{Holder: c.Holder, Action: c.Action, Ref: c.Ref})
	}
	return s
}
