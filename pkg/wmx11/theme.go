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

// setTheme swaps the live palette and repaints the world. WM loop only.
// Back pixels are set at window-create time, so they are re-set here —
// otherwise expose gaps (resizes, workspace switches) flash the old
// theme between paints.
func (w *WM) setTheme(name string) error {
	if err := draw.SetTheme(name); err != nil {
		return err
	}
	root := xwindow.New(w.X, w.X.RootWin())
	root.Change(xproto.CwBackPixel, uint32(pixel(draw.Paper)))
	root.ClearAll()
	for _, f := range w.frames {
		f.win.Change(xproto.CwBackPixel, uint32(pixel(draw.Pane)))
	}
	for _, d := range w.dividers {
		d.win.Change(xproto.CwBackPixel, uint32(pixel(draw.Paper)))
	}
	if w.topBar != nil {
		w.topBar.Change(xproto.CwBackPixel, uint32(pixel(draw.Paper)))
	}
	if w.bottomBar != nil {
		w.bottomBar.Change(xproto.CwBackPixel, uint32(pixel(draw.Paper)))
	}
	// relayout repaints every visible frame; paintBars redraws both bars.
	w.relayout()
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
		if n := wmcore.NeighborLeaf(ws.Root, w.area, Gap, w.focused, target); n != "" {
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
			if l.ID == w.focused && i > 0 {
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
	n := wmcore.NeighborLeaf(ws.Root, w.area, Gap, w.focused, dir)
	if n == "" {
		return nil // at the workspace edge: i3 would cross outputs; we stop
	}
	w.swapFrames(w.focused, n)
	return nil
}
