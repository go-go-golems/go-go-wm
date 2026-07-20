package wmx11

import (
	"fmt"

	"github.com/jezek/xgb/xproto"

	"github.com/go-go-golems/go-go-wm/pkg/draw"
	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
)

// This file holds the pure, display-free decision logic for the focus and
// fullscreen state machines, plus the fullscreenState type that owns the
// fullscreen invariant. Extracting them makes the invariants behind the
// Codex review fixes (RC-5/6/7/12/13) unit-testable with a bare &WM{},
// matching the floatDecision pattern in float_test.go. Phases 1-2 of
// GGWM-011-FOCUS-FS: fullscreenState owns BOTH the reads and the writes of
// w.fullscreen, so call sites stop poking the field directly and can't
// re-derive the fullscreen-owns-geometry/focus invariant wrong.

// fullscreenState owns the "one window covers the screen" invariant. It
// is the single place that reads and writes w.fullscreen, so call sites
// stop poking the field directly and can't re-derive the
// fullscreen-owns-geometry/focus invariant wrong (the root cause of
// RC-5/6/7/12). The WM methods (fullscreen.go) are thin delegates.
type fullscreenState struct {
	wm *WM
}

// Active returns the fullscreen frame, or nil if none.
func (fs fullscreenState) Active() *frame { return fs.wm.fullscreen }

// Owns reports whether f is the fullscreen frame.
func (fs fullscreenState) Owns(f *frame) bool { return fs.wm.fullscreen == f }

// OwnsGeometry reports whether fullscreen currently owns geometry (i.e. a
// frame is fullscreen). relayout and handleConfigureRequest check this so
// they skip/honor the fullscreen frame.
func (fs fullscreenState) OwnsGeometry() bool { return fs.wm.fullscreen != nil }

// OwnsFocus reports whether fullscreen currently owns keyboard focus.
// focus() checks this instead of poking w.fullscreen directly.
func (fs fullscreenState) OwnsFocus() bool { return fs.wm.fullscreen != nil }

// FocusTarget returns the frame that should receive focus while fullscreen
// is active (handles the tiled-vs-floating distinction that RC-7/13 got
// wrong). Returns nil if not active.
func (fs fullscreenState) FocusTarget() *frame { return fs.wm.fullscreen }

// Toggle flips the focused window (tile or float) to/from fullscreen and
// returns the resulting state. Moved verbatim from WM.toggleFullscreen in
// Phase 2; the WM method is now a thin delegate.
func (fs *fullscreenState) Toggle() (bool, error) {
	w := fs.wm
	if fs.OwnsGeometry() {
		fs.Exit()
		return false, nil
	}
	f := w.frames[w.focused]
	if pf := w.floats[w.focusedFloat]; pf != nil {
		f = pf
	}
	if f == nil {
		return false, fmt.Errorf("nothing focused to fullscreen")
	}
	fs.Enter(f)
	return true, nil
}

// Enter makes f fullscreen. Moved verbatim from WM.enterFullscreen in
// Phase 2.
func (fs *fullscreenState) Enter(f *frame) {
	w := fs.wm
	w.fullscreen = f
	w.fsSavedRect = f.rect // floats restore from this; tiles from the tree
	full := wmcore.Rect{X: 0, Y: 0, W: w.screen.W, H: w.screen.H}
	f.rect = full
	f.win.MoveResize(full.X, full.Y, full.W, full.H)
	if f.client != 0 {
		// Full-bleed: the client covers the whole frame, hiding the strip.
		xproto.ConfigureWindow(w.X.Conn(), f.client,
			xproto.ConfigWindowX|xproto.ConfigWindowY|
				xproto.ConfigWindowWidth|xproto.ConfigWindowHeight,
			[]uint32{0, 0, draw.X32(full.W), draw.X32(full.H)})
	}
	f.win.Stack(xproto.StackModeAbove) // above bars and everything else
	if w.menu != nil && w.menu.win != nil {
		w.menu.win.Stack(xproto.StackModeAbove) // menus stay reachable
	}
	if f.client == 0 {
		w.paintFrame(f)
	}
	w.emitEvent("window.fullscreen", w.fullscreenEventData(f, true))
}

// Exit leaves fullscreen and restores geometry. Moved verbatim from
// WM.exitFullscreen in Phase 2.
func (fs *fullscreenState) Exit() {
	w := fs.wm
	f := fs.Active()
	if f == nil {
		return
	}
	w.fullscreen = nil
	if f.floating {
		f.rect = w.clampFloatRect(w.fsSavedRect)
		f.win.MoveResize(f.rect.X, f.rect.Y, f.rect.W, f.rect.H)
		w.configureFloatClient(f)
		f.win.Stack(xproto.StackModeAbove)
		w.raiseChrome()
		w.paintFrame(f)
	} else {
		// The tree owns tiled geometry; relayout restores everything
		// (frame rect, client offsets, repaint).
		f.rect = wmcore.Rect{}
		w.relayout()
	}
	w.emitEvent("window.fullscreen", w.fullscreenEventData(f, false))
}

// Clear drops fullscreen state when its window goes away (unmanage
// paths) — state only, the window is being destroyed. Moved verbatim
// from WM.clearFullscreenFor in Phase 2.
func (fs *fullscreenState) Clear(f *frame) {
	w := fs.wm
	if fs.Owns(f) {
		w.fullscreen = nil
	}
}

// focusDecision is the outcome of deciding where keyboard focus should go
// given the current fullscreen/focus state. It is computed without
// touching X, so it tests with no display.
type focusDecision struct {
	// kind classifies the focus target.
	kind focusTargetKind
	// leaf is the tiled leaf to focus (set when kind == focusTile, or the
	// tile preserved under a fullscreen float).
	leaf wmcore.NodeID
	// client is the X window to SetInputFocus on (set for float and
	// fullscreen-float targets; 0 for plain tiles, which focus their frame).
	client xproto.Window
}

type focusTargetKind uint8

const (
	focusNone focusTargetKind = iota
	focusTile
	focusFloat
	focusFullscreenTile
	focusFullscreenFloat
)

// computeFocusDecision answers "when focus(leaf) is called, where should
// focus actually go?" It encodes the RC-5/7/13 invariants:
//
//   - RC-5: while a frame is fullscreen, navigation pins to it (never to a
//     hidden tiled client underneath).
//   - RC-7: a floating fullscreen frame has an empty leaf, so it must be
//     tracked through its client, not leaf — and the focused tile beneath
//     is preserved rather than clobbered.
//   - RC-13: the tiled leaf is preserved so unmanageFloat can restore it.
//
// The returned decision tells the caller which X window to focus and
// whether the tiled register (w.focused) should change.
func (w *WM) computeFocusDecision(leaf wmcore.NodeID) focusDecision {
	fs := w.fs.FocusTarget()
	if fs == nil {
		return focusDecision{kind: focusTile, leaf: leaf}
	}
	if fs.floating {
		// Floating fullscreen: pin to the float's client, keep the tile
		// register intact (do NOT clear w.focused).
		return focusDecision{kind: focusFullscreenFloat, client: fs.client}
	}
	// Tiled fullscreen: pin to the fullscreen frame's leaf.
	return focusDecision{kind: focusFullscreenTile, leaf: fs.leaf}
}

// shouldHonorFloatConfigure answers whether a floating client's
// ConfigureRequest should be applied. Fullscreen owns the geometry of the
// fullscreen frame, so a resize while fullscreen would shrink the frame
// below the screen and leave the WM fullscreen-locked but visibly not
// fullscreen (Codex review RC-12).
func (w *WM) shouldHonorFloatConfigure(f *frame) bool {
	if f == nil || !f.floating {
		return false
	}
	// Fullscreen owns the geometry of the fullscreen frame.
	return !w.fs.Owns(f)
}

// shouldExitFullscreenOnSwitch answers whether a workspace switch (or add)
// must exit fullscreen before refocusing. Fullscreen is workspace-local:
// switching away exits it, because the old fullscreen frame is unmapped by
// relayout and leaving w.fullscreen set strands focus on it (Codex review
// RC-6). anySwitch is true when the batch contained an OpSwitchWorkspace
// or OpAddWorkspace.
func (w *WM) shouldExitFullscreenOnSwitch(anySwitch bool) bool {
	return anySwitch && w.fs.OwnsGeometry()
}
