package wmx11

// Fullscreen (GGWM-007 follow-up): shell state like floats — never a
// tree property. One window at a time covers the whole screen (bars
// included, i3 semantics). Client windows go full-bleed (the client
// covers the frame, chrome and all); builtin tiles keep their chrome at
// screen size. The tree still owns the tiled geometry: exiting is just
// a relayout.
//
// Phase 2 of GGWM-011-FOCUS-FS: the state machine lives on
// fullscreenState (focus_state.go); these WM methods are thin delegates
// so callers keep the idiomatic w.toggleFullscreen() spelling while the
// invariant is owned in one place.

// toggleFullscreen flips the focused window (tile or float); returns the
// resulting state.
func (w *WM) toggleFullscreen() (bool, error) { return w.fs.Toggle() }

// exitFullscreen leaves fullscreen and restores geometry.
func (w *WM) exitFullscreen() { w.fs.Exit() }

// clearFullscreenFor drops fullscreen state when its window goes away
// (unmanage paths) — state only, the window is being destroyed.
func (w *WM) clearFullscreenFor(f *frame) { w.fs.Clear(f) }

func (w *WM) fullscreenEventData(f *frame, on bool) map[string]interface{} {
	data := map[string]interface{}{"on": on, "title": f.title}
	if f.floating {
		data["client"] = uint32(f.client)
	} else {
		data["leaf"] = string(f.leaf)
	}
	return data
}
