package wmx11

import (
	"fmt"

	"github.com/jezek/xgb/xproto"

	"github.com/go-go-golems/go-go-wm/pkg/draw"
	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
)

// Fullscreen (GGWM-007 follow-up): shell state like floats — never a
// tree property. One window at a time covers the whole screen (bars
// included, i3 semantics). Client windows go full-bleed (the client
// covers the frame, chrome and all); builtin tiles keep their chrome at
// screen size. The tree still owns the tiled geometry: exiting is just
// a relayout.

// toggleFullscreen flips the focused window (tile or float); returns
// the resulting state.
func (w *WM) toggleFullscreen() (bool, error) {
	if w.fs.OwnsGeometry() {
		w.exitFullscreen()
		return false, nil
	}
	f := w.frames[w.focused]
	if pf := w.floats[w.focusedFloat]; pf != nil {
		f = pf
	}
	if f == nil {
		return false, fmt.Errorf("nothing focused to fullscreen")
	}
	w.enterFullscreen(f)
	return true, nil
}

func (w *WM) enterFullscreen(f *frame) {
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

func (w *WM) exitFullscreen() {
	f := w.fs.Active()
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

// clearFullscreenFor drops fullscreen state when its window goes away
// (unmanage paths) — state only, the window is being destroyed.
func (w *WM) clearFullscreenFor(f *frame) {
	if w.fs.Owns(f) {
		w.fullscreen = nil
	}
}

func (w *WM) fullscreenEventData(f *frame, on bool) map[string]interface{} {
	data := map[string]interface{}{"on": on, "title": f.title}
	if f.floating {
		data["client"] = uint32(f.client)
	} else {
		data["leaf"] = string(f.leaf)
	}
	return data
}
