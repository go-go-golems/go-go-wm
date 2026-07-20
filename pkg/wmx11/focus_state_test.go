package wmx11

import (
	"testing"

	"github.com/jezek/xgb/xproto"

	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
)

// These tests lock in the CURRENT (post-fix) focus/fullscreen behavior so
// the GGWM-011-FOCUS-FS refactor (Phases 1-5) catches any regression. They
// exercise the pure, display-free decision logic that the production paths
// consult (computeFocusDecision, shouldHonorFloatConfigure,
// shouldExitFullscreenOnSwitch), matching the floatDecision test pattern.

// tileFrame and floatFrame build minimal frames for the decision tests.
func tileFrame(leaf wmcore.NodeID, client xproto.Window) *frame {
	return &frame{leaf: leaf, client: client}
}
func floatFrame(client xproto.Window) *frame {
	return &frame{floating: true, client: client}
}

// newTestWM builds a bare WM with the fullscreenState back-reference wired
// (the production constructor does this; tests construct &WM{} directly so
// they must set it for the fs helpers to dereference).
func newTestWM() *WM {
	w := &WM{}
	w.fs.wm = w
	return w
}

// RC-5: while a tiled frame is fullscreen, navigation (Mod4-space) must
// pin focus to the fullscreen frame's leaf, NOT the hidden tiled client
// underneath. The user keeps seeing the fullscreen window; keystrokes
// must not leak to the client beneath it.
func TestRC5_FocusPinsToFullscreenTile(t *testing.T) {
	w := newTestWM()
	w.frames = map[wmcore.NodeID]*frame{"l1": tileFrame("l1", 100)}
	w.fullscreen = tileFrame("l1", 100) // a tiled frame is fullscreen
	// Navigation asks to focus l2 (a hidden tile under the fullscreen).
	dec := w.computeFocusDecision("l2")
	if dec.kind != focusFullscreenTile {
		t.Fatalf("RC-5: want focusFullscreenTile, got %v", dec.kind)
	}
	if dec.leaf != "l1" {
		t.Fatalf("RC-5: focus must pin to fullscreen leaf l1, got %q", dec.leaf)
	}
}

// RC-7: a floating fullscreen frame has an empty leaf, so focus must pin
// to the float's client (tracked via focusedFloat), not be lost by setting
// leaf to "". The float must remain targetable by close/float/fullscreen.
func TestRC7_FocusPinsToFullscreenFloat(t *testing.T) {
	w := newTestWM()
	w.fullscreen = floatFrame(200)
	dec := w.computeFocusDecision("l1")
	if dec.kind != focusFullscreenFloat {
		t.Fatalf("RC-7: want focusFullscreenFloat, got %v", dec.kind)
	}
	if dec.client != 200 {
		t.Fatalf("RC-7: focus must pin to float client 200, got %d", dec.client)
	}
}

// RC-13: when focus pins to a fullscreen float, the tiled leaf beneath
// must be PRESERVED (not cleared) so unmanageFloat can restore it after
// the fullscreen dialog closes. computeFocusDecision returns the float
// target; the caller (focus) keeps w.focused intact. This test asserts
// the decision does NOT carry a "clear the tile" instruction — the
// preserved tile is whatever w.focused already is.
func TestRC13_TiledLeafPreservedUnderFullscreenFloat(t *testing.T) {
	w := newTestWM()
	w.focused = "l3" // the tile the user was on
	w.fullscreen = floatFrame(200)
	dec := w.computeFocusDecision("l9")
	if dec.kind != focusFullscreenFloat {
		t.Fatalf("RC-13: want focusFullscreenFloat, got %v", dec.kind)
	}
	// The decision must not change the tile register: w.focused stays "l3".
	// (focus() implements this by not assigning to w.focused in this branch.)
	// We assert the contract: the decision targets the float, and the
	// caller is responsible for leaving w.focused alone. Verify the
	// decision carries no tile-clearing leaf.
	if dec.leaf != "" {
		t.Fatalf("RC-13: fullscreen-float decision must not set a leaf (would clobber the preserved tile), got %q", dec.leaf)
	}
	// And the preserved tile is still readable on the WM.
	if w.focused != "l3" {
		t.Fatalf("RC-13: preserved tile must stay l3, got %q", w.focused)
	}
}

// RC-12: a floating client's ConfigureRequest must be ignored while that
// float is the fullscreen frame — fullscreen owns geometry, and honoring
// the request would shrink the frame below the screen while w.fullscreen
// stays set (fullscreen-locked but visibly not fullscreen).
func TestRC12_FloatConfigureIgnoredWhileFullscreen(t *testing.T) {
	fs := floatFrame(300)
	w := newTestWM()
	w.fullscreen = fs
	if w.shouldHonorFloatConfigure(fs) {
		t.Fatal("RC-12: configure must be ignored for the fullscreen float")
	}
	// A different (non-fullscreen) float's configure is still honored.
	other := floatFrame(301)
	if !w.shouldHonorFloatConfigure(other) {
		t.Fatal("RC-12: configure must be honored for a non-fullscreen float")
	}
	// No fullscreen at all: honored.
	w.fullscreen = nil
	if !w.shouldHonorFloatConfigure(other) {
		t.Fatal("RC-12: configure must be honored when nothing is fullscreen")
	}
	// A nil or non-float frame is never honored.
	if w.shouldHonorFloatConfigure(nil) {
		t.Fatal("RC-12: nil frame must not be honored")
	}
	if w.shouldHonorFloatConfigure(tileFrame("l1", 400)) {
		t.Fatal("RC-12: tiled frame must not be honored by the float path")
	}
}

// RC-6: a workspace switch (or add) must exit fullscreen before
// refocusing, because the old fullscreen frame is unmapped by relayout
// and leaving w.fullscreen set strands focus on it. Fullscreen is
// workspace-local.
func TestRC6_SwitchExitsFullscreen(t *testing.T) {
	w := newTestWM()
	w.fullscreen = tileFrame("l1", 100)
	if !w.shouldExitFullscreenOnSwitch(true) {
		t.Fatal("RC-6: a switch while fullscreen must exit fullscreen")
	}
	// No switch: never exit (avoids spurious exits on other batches).
	if w.shouldExitFullscreenOnSwitch(false) {
		t.Fatal("RC-6: a non-switch batch must not exit fullscreen")
	}
	// Switch but nothing fullscreen: no exit needed (and the call is a
	// no-op, but the decision must report false so callers don't rely on
	// exitFullscreen's nil-guard alone).
	w.fullscreen = nil
	if w.shouldExitFullscreenOnSwitch(true) {
		t.Fatal("RC-6: a switch with nothing fullscreen must not report an exit")
	}
}

// Cross-check: with no fullscreen at all, focus goes to the requested
// tiled leaf (the common path). This guards against the refactor
// accidentally always pinning.
func TestFocusDecisionNoFullscreen(t *testing.T) {
	w := newTestWM()
	dec := w.computeFocusDecision("l7")
	if dec.kind != focusTile || dec.leaf != "l7" {
		t.Fatalf("no-fullscreen: want focusTile/l7, got %v/%q", dec.kind, dec.leaf)
	}
}
