package wmx11

import (
	"fmt"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil/xwindow"

	"github.com/go-go-golems/go-go-wm/pkg/draw"
	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
)

// ThemeInfo answers {"q":"theme"} and wm.theme() queries.
type ThemeInfo struct {
	Theme     string   `json:"theme"`
	Available []string `json:"available"`
}

// setTheme swaps the live palette and repaints the whole world. WM loop
// only.
//
// The subtle part: frames and floats carry their content in a
// background pixmap (an MIT-SHM surface, GGWM-006) and the bars in an
// XSurfaceSet pixmap. Setting CwBackPixel on such a window *detaches*
// that pixmap (back_pixel and back_pixmap are mutually exclusive in
// X11), after which the cached repaint paths — which only ClearAll or
// XPaint — reveal the solid back pixel instead of the painted content.
// So the theme swap must fully rebuild those buffers, not poke their
// back pixel: dropBuffers frees the surface (and safely resets the
// pixel as a flash guard) so the next paintFrame recreates it and
// re-establishes it as the background; dropping the cached bar images
// makes blitCached re-run XSurfaceSet. Dividers keep the pixel poke —
// their blit path re-runs XSurfaceSet on every paint anyway.
func (w *WM) setTheme(name string) error {
	if err := draw.SetTheme(name); err != nil {
		return err
	}
	root := xwindow.New(w.X, w.X.RootWin())
	root.Change(xproto.CwBackPixel, uint32(pixel(draw.Current().Paper)))
	root.ClearAll()
	for _, f := range w.frames {
		f.dropBuffers()
	}
	for _, f := range w.floats {
		f.dropBuffers()
	}
	for _, d := range w.dividers {
		d.win.Change(xproto.CwBackPixel, uint32(pixel(draw.Current().Paper)))
	}
	// Drop the cached bar surfaces so blitCached rebuilds them via
	// XSurfaceSet (re-establishing the background pixmap after the
	// flash-guard pixel change below).
	if w.topBarImg != nil {
		w.topBarImg.Destroy()
		w.topBarImg = nil
	}
	if w.bottomBarImg != nil {
		w.bottomBarImg.Destroy()
		w.bottomBarImg = nil
	}
	if w.topBar != nil {
		w.topBar.Change(xproto.CwBackPixel, uint32(pixel(draw.Current().Paper)))
	}
	if w.bottomBar != nil {
		w.bottomBar.Change(xproto.CwBackPixel, uint32(pixel(draw.Current().Paper)))
	}
	if w.launcher != nil {
		w.paintLauncher()
	}
	// relayout repaints every visible frame (rebuilding its surface);
	// the fullscreen frame is skipped by relayout (it owns its geometry)
	// so it repaints explicitly; paintBars redraws both bars.
	w.relayout()
	if w.fs.OwnsGeometry() {
		w.paintFrame(w.fs.Active())
	}
	w.paintBars()
	w.emitEvent("theme.changed", map[string]interface{}{"theme": name})
	return nil
}

// focusTarget resolves wm.focus() targets on the WM loop: a leaf id, or
// left|right|up|down (geometric, workspace-local), or next|prev (layout
// order, wrapping).
func (w *WM) focusTarget(target string) error {
	ws := w.desktop.CurrentWorkspace()
	if ws == nil {
		return fmt.Errorf("no current workspace")
	}
	switch target {
	case "left", "right", "up", "down":
		if w.focused == "" {
			return nil
		}
		if n := wmcore.NeighborLeaf(ws.Root, w.area, Gap, w.fstate.FocusedLeaf(), target); n != "" {
			w.focus(n)
		}
		return nil
	case "next":
		w.focusNext()
		return nil
	case "prev":
		leaves := ws.Root.Leaves()
		if len(leaves) == 0 {
			return nil
		}
		prev := leaves[len(leaves)-1].ID
		for i, l := range leaves {
			if l.ID == w.fstate.FocusedLeaf() && i > 0 {
				prev = leaves[i-1].ID
				break
			}
		}
		w.focus(prev)
		return nil
	default:
		if ws.Root.FindLeaf(wmcore.NodeID(target)) == nil {
			return fmt.Errorf("focus: no leaf %q in the current workspace", target)
		}
		w.focus(wmcore.NodeID(target))
		return nil
	}
}

// moveDir swaps the focused leaf with its geometric neighbor — the
// i3 `move left` gesture expressed through the existing swap-leaves op,
// so it lands in the trace and the op stream like every other mutation.
func (w *WM) moveDir(dir string) error {
	ws := w.desktop.CurrentWorkspace()
	if ws == nil {
		return fmt.Errorf("no current workspace")
	}
	if w.focused == "" {
		return nil
	}
	switch dir {
	case "left", "right", "up", "down":
	default:
		return fmt.Errorf("move: dir must be left|right|up|down, got %q", dir)
	}
	n := wmcore.NeighborLeaf(ws.Root, w.area, Gap, w.fstate.FocusedLeaf(), dir)
	if n == "" {
		return nil // at the workspace edge: i3 would cross outputs; we stop
	}
	w.swapFrames(w.fstate.FocusedLeaf(), n)
	return nil
}
