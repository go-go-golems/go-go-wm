package wmx11

import (
	"context"
	"fmt"
	"time"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/keybind"
	"github.com/jezek/xgbutil/mousebind"
	"github.com/jezek/xgbutil/xevent"

	"github.com/go-go-golems/go-go-wm/pkg/draw"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
)

// dragState tracks an in-progress pointer drag.
type dragState struct {
	kind    string        // "divider" | "grip" | "float"
	split   wmcore.NodeID // divider drags
	from    wmcore.NodeID // grip drags
	over    wmcore.NodeID
	zone    wmcore.Zone
	snapped bool

	fl         *frame // float drags: the frame being moved
	offX, offY int    // pointer offset inside the float frame

	// Divider drags repaint whole panes; X delivers motion far faster
	// than panes can paint, so motion is coalesced to ~60Hz and the
	// release applies the final pointer position (GGWM-005).
	lastPaint time.Time
}

func (w *WM) setupInput() {
	keybind.Initialize(w.X)
	mousebind.Initialize(w.X)

	root := w.X.RootWin()
	bind := func(combo string, fn func()) {
		err := keybind.KeyPressFun(func(_ *xgbutil.XUtil, _ xevent.KeyPressEvent) {
			fn()
		}).Connect(w.X, root, combo, true)
		if err != nil {
			log.Warn().Str("combo", combo).Err(err).Msg("keybinding failed")
		}
	}

	// An rc.js that owns the whole keyboard (an i3-style config) sets
	// NoDefaultBinds: a wm.bind on a combo the WM already grabbed would
	// fire both handlers. Escape stays — it is modal (accept/menu
	// cancellation), not a layout binding.
	if !w.cfg.NoDefaultBinds {
		bind("Mod4-Return", w.spawnTerminal)
		bind("Mod4-d", w.toggleLauncher)
		bind("Mod4-Shift-d", func() { w.splitFocused(wmcore.Row) })
		bind("Mod4-s", func() { w.splitFocused(wmcore.Col) })
		bind("Mod4-w", w.closeFocused)
		bind("Mod4-f", func() { _, _ = w.toggleFullscreen() })
		bind("Mod4-space", w.focusNext)
		bind("Mod4-n", func() { _, _ = w.Apply(wmcore.Op{Op: wmcore.OpAddWorkspace}) })
		for i := 1; i <= 9; i++ {
			idx := i - 1
			bind(fmt.Sprintf("Mod4-%d", i), func() { w.switchWorkspaceIndex(idx) })
		}
		bind("Mod4-Shift-q", func() { w.Shutdown() })
	}
	bind("Escape", w.cancelAccept) // grabbed only while accepting? kept global: harmless

	// X event wiring.
	xevent.MapRequestFun(func(_ *xgbutil.XUtil, ev xevent.MapRequestEvent) {
		w.handleMapRequest(ev)
	}).Connect(w.X, root)
	xevent.ConfigureRequestFun(func(_ *xgbutil.XUtil, ev xevent.ConfigureRequestEvent) {
		w.handleConfigureRequest(ev)
	}).Connect(w.X, root)
	xevent.DestroyNotifyFun(func(_ *xgbutil.XUtil, ev xevent.DestroyNotifyEvent) {
		w.handleDestroyNotify(ev)
	}).Connect(w.X, root)
	xevent.UnmapNotifyFun(func(_ *xgbutil.XUtil, ev xevent.UnmapNotifyEvent) {
		w.handleUnmapNotify(ev)
	}).Connect(w.X, root)
	xevent.ButtonPressFun(func(_ *xgbutil.XUtil, ev xevent.ButtonPressEvent) {
		w.handleRootPress(ev)
	}).Connect(w.X, root)
	xevent.MotionNotifyFun(func(_ *xgbutil.XUtil, ev xevent.MotionNotifyEvent) {
		w.handleMotion(int(ev.RootX), int(ev.RootY))
	}).Connect(w.X, root)
	xevent.ButtonReleaseFun(func(_ *xgbutil.XUtil, ev xevent.ButtonReleaseEvent) {
		w.handleRelease(int(ev.RootX), int(ev.RootY))
	}).Connect(w.X, root)
}

func (w *WM) spawnTerminal() {
	cmd := w.cfg.Spawn
	if cmd == "" {
		cmd = "xterm"
	}
	w.execCommand(cmd)
}

func (w *WM) splitFocused(dir wmcore.Dir) {
	if w.fstate.FocusedLeaf() == "" {
		return
	}
	_, _ = w.Apply(wmcore.Op{Op: wmcore.OpSplitLeaf, Node: w.fstate.FocusedLeaf(), Dir: dir, App: ""})
}

func (w *WM) closeFocused() {
	f := w.frames[w.fstate.FocusedLeaf()]
	if pf := w.floats[w.fstate.FocusedFloat()]; pf != nil {
		f = pf // the float band holds focus; Mod4-w closes the float
	}
	if f == nil {
		return
	}
	w.closeClient(f)
}

func (w *WM) focusNext() {
	ws := w.desktop.CurrentWorkspace()
	leaves := ws.Root.Leaves()
	if len(leaves) == 0 {
		return
	}
	next := leaves[0].ID
	for i, l := range leaves {
		if l.ID == w.fstate.FocusedLeaf() && i+1 < len(leaves) {
			next = leaves[i+1].ID
			break
		}
	}
	w.focus(next)
}

func (w *WM) switchWorkspaceIndex(i int) {
	if i < 0 || i >= len(w.desktop.Workspaces) {
		return
	}
	_, _ = w.Apply(wmcore.Op{Op: wmcore.OpSwitchWorkspace, Workspace: w.desktop.Workspaces[i].ID})
}

// --- clicks ---------------------------------------------------------------

// FramePress handles a ButtonPress inside a frame window (title strip area).
func (w *WM) handleFramePress(f *frame, x, y int, button byte, rootX, rootY int) {
	if f.floating {
		// Float strips have close only (splitting a dialog is
		// meaningless); everywhere else on the strip drags the float.
		if y < draw.TitleH {
			_, _, _, cl := draw.TitleButtons(f.rect.W)
			if image_Pt(x, y).In(cl) {
				w.closeClient(f)
				return
			}
			w.focusFloat(f)
			w.beginFloatDrag(f, rootX, rootY)
			return
		}
		w.focusFloat(f)
		return
	}
	if y >= draw.TitleH {
		w.focus(f.leaf)
		if f.client == 0 {
			// Builtin tile content: presentation click contract.
			w.builtinClick(f, x, y, rootX, rootY, int(button))
		}
		return
	}
	grip, sr, sd, cl := draw.TitleButtons(f.rect.W)
	pt := image_Pt(x, y)
	switch {
	case pt.In(grip):
		w.focus(f.leaf)
		w.beginGripDrag(f.leaf)
	case pt.In(sr):
		_, _ = w.Apply(wmcore.Op{Op: wmcore.OpSplitLeaf, Node: f.leaf, Dir: wmcore.Row, App: ""})
	case pt.In(sd):
		_, _ = w.Apply(wmcore.Op{Op: wmcore.OpSplitLeaf, Node: f.leaf, Dir: wmcore.Col, App: ""})
	case pt.In(cl):
		w.closeClient(f)
	default:
		// The title itself: the tile is a presentation.
		w.focus(f.leaf)
		w.tileClicked(f.leaf, button, rootX, rootY)
	}
}

// tileClicked implements the presentation click contract for <tile>:
// pending matching accept → answer; else → object menu (L=R here since
// tiles have no primary action beyond focus).
func (w *WM) tileClicked(leaf wmcore.NodeID, _ byte, rootX, rootY int) {
	obj, _ := pbui.NewObject("tile", string(leaf))
	obj.Label = "tile " + string(leaf)
	if w.accepting != nil && pbui.TypeMatches(w.accepting.ptypes, "tile") {
		if w.broker != nil {
			session := w.accepting.session
			b := w.broker
			go func() { _ = b.Answer(context.Background(), session, obj) }()
		}
		return
	}
	if w.broker != nil {
		b := w.broker
		go func() { _, _ = b.RequestMenu(context.Background(), obj, rootX, rootY) }()
	}
}

// handleRootPress: clicks on bare root — dividers, bar chips.
func (w *WM) handleRootPress(ev xevent.ButtonPressEvent) {
	x, y := int(ev.RootX), int(ev.RootY)

	// Popup open? Any root press outside it closes it.
	if w.launcher != nil {
		w.closeLauncher()
		return
	}
	if w.menu != nil {
		w.closeMenu()
		return
	}
	// Top bar chips.
	if y < draw.BarH {
		w.topBarClicked(x, y)
		return
	}
	// Divider hit?
	ws := w.desktop.CurrentWorkspace()
	items := wmcore.Layout(ws.Root, w.area, Gap)
	for id, item := range items {
		n := ws.Root.Find(id)
		if n == nil || n.Kind != wmcore.Split {
			continue
		}
		if item.DividerRect.W > 0 && item.DividerRect.Contains(x, y) {
			w.beginDividerDrag(id)
			return
		}
	}
}

func (w *WM) topBarClicked(x, y int) {
	tb := w.topBarModel()
	for i, r := range tb.TopBarChips() {
		if image_Pt(x, y).In(r) {
			ws := w.desktop.Workspaces[i]
			if w.accepting != nil && pbui.TypeMatches(w.accepting.ptypes, "workspace") && w.broker != nil {
				obj, _ := pbui.NewObject("workspace", ws.ID)
				obj.Label = ws.Name
				session := w.accepting.session
				b := w.broker
				go func() { _ = b.Answer(context.Background(), session, obj) }()
				return
			}
			// L on a chip switches (the chip's primary action).
			_, _ = w.Apply(wmcore.Op{Op: wmcore.OpSwitchWorkspace, Workspace: ws.ID})
			return
		}
	}
}

// --- drags ----------------------------------------------------------------

func (w *WM) beginDividerDrag(split wmcore.NodeID) {
	if !w.grabPointer() {
		return
	}
	w.drag = &dragState{kind: "divider", split: split}
	w.setMouseDoc("drag divider — sticky at ¼ ⅓ ½ ⅔ ¾")
}

func (w *WM) beginFloatDrag(f *frame, rootX, rootY int) {
	if !w.grabPointer() {
		return
	}
	w.drag = &dragState{kind: "float", fl: f,
		offX: rootX - f.rect.X, offY: rootY - f.rect.Y}
	w.setMouseDoc("drag float — release to place")
}

func (w *WM) beginGripDrag(from wmcore.NodeID) {
	if !w.grabPointer() {
		return
	}
	w.drag = &dragState{kind: "grip", from: from}
	w.setMouseDoc("drop on a tile: center = swap apps · edge = split-dock (source tile closes)")
}

func (w *WM) grabPointer() bool {
	ok, err := mousebind.GrabPointer(w.X, w.X.RootWin(), xproto.WindowNone, xproto.CursorNone)
	if err != nil || !ok {
		return false
	}
	return true
}

func (w *WM) handleMotion(x, y int) {
	d := w.drag
	if d == nil {
		return
	}
	switch d.kind {
	case "divider":
		w.dividerMotion(d, x, y)
	case "grip":
		w.gripMotion(d, x, y)
	case "float":
		w.floatMotion(d, x, y)
	}
}

// floatMotion moves the dragged float with the pointer. No repaint is
// needed: the frame's content rides along in its background pixmap.
func (w *WM) floatMotion(d *dragState, x, y int) {
	f := d.fl
	r := f.rect
	r.X = x - d.offX
	r.Y = y - d.offY
	r = w.clampFloatRect(r)
	if r == f.rect {
		return
	}
	f.rect = r
	f.win.Move(r.X, r.Y)
}

func (w *WM) dividerMotion(d *dragState, x, y int) {
	// Coalesce: skip repaints closer than a frame apart; handleRelease
	// runs a final dividerMotion with the release coordinates.
	if time.Since(d.lastPaint) < 16*time.Millisecond {
		return
	}
	d.lastPaint = time.Now()
	ws := w.desktop.CurrentWorkspace()
	n := ws.Root.Find(d.split)
	if n == nil {
		return
	}
	items := wmcore.Layout(ws.Root, w.area, Gap)
	item, ok := items[d.split]
	if !ok {
		return
	}
	f := wmcore.RatioForPointer(item.Rect, n.Dir, x, y)
	f, snapped := wmcore.Snap(f)
	d.snapped = snapped
	_, _ = wmcore.Apply(w.desktop, wmcore.Op{Op: wmcore.OpSetRatio, Node: d.split, Ratio: f})
	w.relayoutResized()
	w.dividerDragFeedback(d.split, snapped)
}

func (w *WM) gripMotion(d *dragState, x, y int) {
	ws := w.desktop.CurrentWorkspace()
	items := wmcore.Layout(ws.Root, w.area, Gap)
	d.over, d.zone = "", ""
	for id, item := range items {
		n := ws.Root.Find(id)
		if n == nil || n.Kind != wmcore.Leaf || id == d.from {
			continue
		}
		if item.Rect.Contains(x, y) {
			d.over = id
			d.zone = wmcore.ZoneAt(item.Rect, x, y)
			w.showDropPreview(item.Rect, d.zone)
			return
		}
	}
	w.hideDropPreview()
}

func (w *WM) handleRelease(x, y int) {
	d := w.drag
	if d == nil {
		return
	}
	w.drag = nil
	mousebind.UngrabPointer(w.X)
	w.hideDropPreview()
	w.setMouseDoc("")

	if d.kind == "grip" && d.over != "" && d.over != d.from {
		if d.zone == wmcore.ZoneCenter {
			w.swapFrames(d.from, d.over)
		} else {
			w.moveSplitFrames(d.from, d.over, d.zone)
		}
	}
	if d.kind == "divider" {
		// Final position: the throttle above may have dropped the last
		// few motion events.
		d.lastPaint = time.Time{}
		w.dividerMotion(d, x, y)
		w.dividerDragEnd(d.split)
		w.emitEvent("move_split_ratio", map[string]interface{}{"split": string(d.split), "snapped": d.snapped})
	}
}

// swapFrames swaps the frames (and apps) of two leaves.
func (w *WM) swapFrames(a, b wmcore.NodeID) {
	fa, fb := w.frames[a], w.frames[b]
	if _, err := w.Apply(wmcore.Op{Op: wmcore.OpSwapLeaves, Node: a, Target: b}); err != nil {
		return
	}
	// The frames swap leaves with the apps.
	delete(w.frames, a)
	delete(w.frames, b)
	if fa != nil {
		fa.leaf = b
		w.frames[b] = fa
	}
	if fb != nil {
		fb.leaf = a
		w.frames[a] = fb
	}
	switch w.fstate.FocusedLeaf() {
	case a:
		w.fstate.SetTile(b)
	case b:
		w.fstate.SetTile(a)
	}
	w.relayout()
	w.paintBars()
}

// moveSplitFrames performs the ⠿ edge-drop: the frame keeps its leaf id
// (wmcore moves the leaf node itself).
func (w *WM) moveSplitFrames(from, target wmcore.NodeID, zone wmcore.Zone) {
	_, _ = w.Apply(wmcore.Op{Op: wmcore.OpMoveSplit, Node: from, Target: target, Zone: zone})
}

func (w *WM) setMouseDoc(s string) {
	w.mouseDoc = s
	w.paintBars()
}
