package wmx11

import (
	"fmt"
	"regexp"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil/ewmh"
	"github.com/jezek/xgbutil/icccm"
	"github.com/jezek/xgbutil/xevent"
	"github.com/jezek/xgbutil/xwindow"

	"github.com/go-go-golems/go-go-wm/pkg/draw"
	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
)

// The floating overlay layer (GGWM-007). Floats are frames that never
// enter the wmcore tree: no leaf, no ops, no replay footprint. They live
// in WM.floats (client → frame with floating=true), belong to the
// workspace that was current when they mapped, and stack above the tiled
// world but below the WM's own chrome (bars, menus).

// FloatRule is one float override pushed down from the scripting layer
// (wm.rule({class:…, float:true})). Title/Class are case-insensitive Go
// regexp sources; a present pattern must match. Float=false forces a
// window that detection would float back into the tiling world.
type FloatRule struct {
	Title string `json:"title,omitempty"`
	Class string `json:"class,omitempty"`
	Float bool   `json:"float"`
}

type compiledFloatRule struct {
	FloatRule
	titleRe *regexp.Regexp
	classRe *regexp.Regexp
}

// SetFloatRules replaces the WM-side float override list. WM loop only
// (callers post); invalid patterns reject the whole set.
func (w *WM) SetFloatRules(rules []FloatRule) error {
	compiled := make([]compiledFloatRule, 0, len(rules))
	for _, r := range rules {
		c := compiledFloatRule{FloatRule: r}
		var err error
		if r.Title != "" {
			if c.titleRe, err = regexp.Compile("(?i)" + r.Title); err != nil {
				return fmt.Errorf("float rule title %q: %w", r.Title, err)
			}
		}
		if r.Class != "" {
			if c.classRe, err = regexp.Compile("(?i)" + r.Class); err != nil {
				return fmt.Errorf("float rule class %q: %w", r.Class, err)
			}
		}
		if c.titleRe == nil && c.classRe == nil {
			return fmt.Errorf("float rule needs a title and/or class pattern")
		}
		compiled = append(compiled, c)
	}
	w.floatRules = compiled
	return nil
}

// floatRuleVerdict returns the first matching rule's float value, or nil
// when no rule matches (detection decides).
func (w *WM) floatRuleVerdict(title, class, instance string) *bool {
	for i := range w.floatRules {
		r := &w.floatRules[i]
		if r.titleRe != nil && !r.titleRe.MatchString(title) {
			continue
		}
		if r.classRe != nil && !r.classRe.MatchString(class) && !r.classRe.MatchString(instance) {
			continue
		}
		v := r.Float
		return &v
	}
	return nil
}

// floatProps are the X properties the float decision reads.
type floatProps struct {
	leader                 xproto.Window // WM_TRANSIENT_FOR, 0 if unset
	types                  []string      // _NET_WM_WINDOW_TYPE atom names
	fixedSize              bool          // WM_NORMAL_HINTS min == max (both set)
	minW, minH, maxW, maxH int           // 0 = unbounded
	reqW, reqH             int           // client-requested geometry
}

func fetchFloatProps(w *WM, win xproto.Window) floatProps {
	var p floatProps
	if leader, err := icccm.WmTransientForGet(w.X, win); err == nil {
		p.leader = leader
	}
	if types, err := ewmh.WmWindowTypeGet(w.X, win); err == nil {
		p.types = types
	}
	if h, err := icccm.WmNormalHintsGet(w.X, win); err == nil && h != nil {
		if h.Flags&icccm.SizeHintPMinSize > 0 {
			p.minW, p.minH = int(h.MinWidth), int(h.MinHeight)
		}
		if h.Flags&icccm.SizeHintPMaxSize > 0 {
			p.maxW, p.maxH = int(h.MaxWidth), int(h.MaxHeight)
		}
		p.fixedSize = p.minW > 0 && p.minH > 0 &&
			p.minW == p.maxW && p.minH == p.maxH
	}
	if g, err := xproto.GetGeometry(w.X.Conn(), xproto.Drawable(win)).Reply(); err == nil {
		p.reqW, p.reqH = int(g.Width), int(g.Height)
	}
	return p
}

// floatDecision is the pure precedence function from the design doc:
// rule override → WM_TRANSIENT_FOR → window type → fixed size.
func floatDecision(rule *bool, leader xproto.Window, types []string, fixedSize bool) bool {
	if rule != nil {
		return *rule
	}
	if leader != 0 {
		return true
	}
	for _, t := range types {
		switch t {
		case "_NET_WM_WINDOW_TYPE_DIALOG", "_NET_WM_WINDOW_TYPE_UTILITY",
			"_NET_WM_WINDOW_TYPE_SPLASH", "_NET_WM_WINDOW_TYPE_TOOLBAR",
			// Usually override-redirect and never seen here; float them
			// if a client maps one anyway.
			"_NET_WM_WINDOW_TYPE_MENU", "_NET_WM_WINDOW_TYPE_DROPDOWN_MENU",
			"_NET_WM_WINDOW_TYPE_POPUP_MENU", "_NET_WM_WINDOW_TYPE_TOOLTIP",
			"_NET_WM_WINDOW_TYPE_NOTIFICATION":
			return true
		}
	}
	return fixedSize
}

// floatPlacement sizes and positions a new float: requested size clamped
// to hints and the work area, centered on the leader's frame when the
// leader is a window we manage, else centered on the work area.
func (w *WM) floatPlacement(p floatProps) wmcore.Rect {
	cw, ch := p.reqW, p.reqH
	if cw < 40 {
		cw = 400
	}
	if ch < 30 {
		ch = 300
	}
	cw = clampInt(cw, p.minW, p.maxW)
	ch = clampInt(ch, p.minH, p.maxH)
	maxW := w.area.W - 2*draw.BorderW
	maxH := w.area.H - draw.TitleH - draw.BorderW
	if cw > maxW {
		cw = maxW
	}
	if ch > maxH {
		ch = maxH
	}
	r := wmcore.Rect{W: cw + 2*draw.BorderW, H: ch + draw.TitleH + draw.BorderW}

	center := wmcore.Rect{X: w.area.X, Y: w.area.Y, W: w.area.W, H: w.area.H}
	if p.leader != 0 {
		if lf := w.byClient[p.leader]; lf != nil && lf.rect.W > 0 {
			center = lf.rect
		}
	}
	r.X = center.X + (center.W-r.W)/2
	r.Y = center.Y + (center.H-r.H)/2
	return w.clampFloatRect(r)
}

// clampFloatRect keeps a float's title strip reachable inside the work
// area (a dialog dragged or placed off-screen is unrecoverable by mouse).
func (w *WM) clampFloatRect(r wmcore.Rect) wmcore.Rect {
	const margin = 40 // px of strip that must stay on screen
	if r.X > w.area.X+w.area.W-margin {
		r.X = w.area.X + w.area.W - margin
	}
	if r.X+r.W < w.area.X+margin {
		r.X = w.area.X + margin - r.W
	}
	if r.Y < w.area.Y {
		r.Y = w.area.Y
	}
	if r.Y > w.area.Y+w.area.H-draw.TitleH {
		r.Y = w.area.Y + w.area.H - draw.TitleH
	}
	return r
}

func clampInt(v, lo, hi int) int {
	if lo > 0 && v < lo {
		v = lo
	}
	if hi > 0 && v > hi {
		v = hi
	}
	return v
}

// manageFloat is the third exit from handleMapRequest's front door: wrap
// the client in a strip-and-border frame, but never touch the tree.
func (w *WM) manageFloat(clientWin xproto.Window, title, class, instance string, p floatProps) {
	f := &frame{
		client: clientWin, title: title, class: class, instance: instance,
		floating: true, leader: p.leader, ws: w.desktop.Current,
		minW: p.minW, minH: p.minH, maxW: p.maxW, maxH: p.maxH,
	}
	f.rect = w.floatPlacement(p)

	fw, err := xwindow.Generate(w.X)
	if err != nil {
		return
	}
	err = fw.CreateChecked(w.X.RootWin(), f.rect.X, f.rect.Y, f.rect.W, f.rect.H,
		xproto.CwBackPixel|xproto.CwEventMask,
		uint32(pixel(draw.Current().Pane)),
		xproto.EventMaskSubstructureRedirect|
			xproto.EventMaskButtonPress|
			xproto.EventMaskButtonRelease|
			xproto.EventMaskPointerMotion|
			xproto.EventMaskExposure|
			xproto.EventMaskEnterWindow)
	if err != nil {
		return
	}
	f.win = fw

	xproto.ChangeSaveSet(w.X.Conn(), xproto.SetModeInsert, clientWin)
	xproto.ConfigureWindow(w.X.Conn(), clientWin,
		xproto.ConfigWindowBorderWidth, []uint32{0})
	xproto.ReparentWindow(w.X.Conn(), clientWin, fw.Id, draw.BorderW, draw.TitleH)
	w.configureFloatClient(f)
	w.wireClient(f)

	w.floats[clientWin] = f
	w.byClient[clientWin] = f
	w.byFrame[fw.Id] = f
	w.connectFrameEvents(fw)

	fw.Map()
	xproto.MapWindow(w.X.Conn(), clientWin)
	w.focusFloat(f)
	w.paintFrame(f)
	w.emitEvent("window.managed", map[string]interface{}{
		"title": title, "class": class, "instance": instance,
		"workspace": f.ws, "floating": true,
		"client": uint32(clientWin), "leader": uint32(p.leader),
	})
	w.updateEWMH()
	w.paintBars()
}

// unmanageFloat tears a float down: the same complete-teardown list as
// tiles (detach, drop buffers, destroy), plus focus handoff back to the
// tiled world.
func (w *WM) unmanageFloat(f *frame) {
	// Capture whether this float holds focus BEFORE teardown: clearFullscreenFor
	// and the float-map deletes below would make FocusedFloat() return 0 for a
	// fullscreen float (its map entry is gone), skipping Restore and leaving
	// focus pointing at the removed client (Codex review #18).
	heldFocus := w.fstate.FocusedFloat() == f.client ||
		(w.fs.Owns(f) && f.floating)
	w.clearFullscreenFor(f)
	delete(w.floats, f.client)
	delete(w.byClient, f.client)
	delete(w.byFrame, f.win.Id)
	xevent.Detach(w.X, f.client)
	xevent.Detach(w.X, f.win.Id)
	f.dropBuffers()
	f.win.Destroy()

	if heldFocus {
		// Route through focusState (Option B B6): Restore returns focus to
		// the preserved tile, replacing the implicit w.focused convention.
		w.fstate.Restore()
		if w.frames[w.fstate.FocusedLeaf()] != nil {
			w.focus(w.fstate.FocusedLeaf())
		}
	}
	w.emitEvent("window.float-closed", map[string]interface{}{
		"client": uint32(f.client), "title": f.title, "class": f.class,
	})
	w.updateEWMH()
	w.paintBars()
}

// focusFloat gives a float the keyboard and the top of the float band.
// w.fstate.FocusedLeaf() (the tile register) is preserved — dismissing the float
// returns focus to it.
func (w *WM) focusFloat(f *frame) {
	prevFloat := w.fstate.FocusedFloat()
	// Route through focusState (Option B B5): FocusFloat preserves the
	// current tile in preservedTile for restoration (RC-13's contract, now
	// explicit) and keeps the shadow fields in sync.
	w.fstate.FocusFloat(f)
	xwindow.New(w.X, f.client).Focus()
	_ = ewmh.ActiveWindowSet(w.X, f.client)
	f.win.Stack(xproto.StackModeAbove)
	w.raiseChrome()
	if pf := w.floats[prevFloat]; pf != nil && prevFloat != f.client {
		w.paintFrame(pf)
	}
	// The focused tile keeps w.fstate.FocusedLeaf() but loses its highlight.
	if tf := w.frames[w.fstate.FocusedLeaf()]; tf != nil {
		w.paintFrame(tf)
	}
	w.paintFrame(f)
}

// frameFocused is replaced by focusState.Focused (Option B B7). The
// predicate now lives in focus_state.go so the exactly-one invariant
// has a single owner.
// raiseChrome restacks the WM's own windows above the float band: bars
// always, the menu and drop overlay when present. Menus are transient to
// the whole desktop and must never hide under a dialog.
func (w *WM) raiseChrome() {
	if w.topBar != nil {
		w.topBar.Stack(xproto.StackModeAbove)
	}
	if w.bottomBar != nil {
		w.bottomBar.Stack(xproto.StackModeAbove)
	}
	if w.overlay != nil {
		w.overlay.Stack(xproto.StackModeAbove)
	}
	if w.menu != nil && w.menu.win != nil {
		w.menu.win.Stack(xproto.StackModeAbove)
	}
}

// syncFloats maps floats of the current workspace and hides the rest —
// the float half of relayout's "make the screen match the model".
func (w *WM) syncFloats(paintAll bool) {
	for _, f := range w.floats {
		if f.ws == w.desktop.Current {
			f.win.Map()
			if paintAll {
				w.paintFrame(f)
			}
		} else {
			f.win.Unmap()
			f.dropBuffers()
		}
	}
}

// configureFloatClient re-fits the client window inside the float frame.
func (w *WM) configureFloatClient(f *frame) {
	cw := f.rect.W - 2*draw.BorderW
	ch := f.rect.H - draw.TitleH - draw.BorderW
	if cw < 1 {
		cw = 1
	}
	if ch < 1 {
		ch = 1
	}
	xproto.ConfigureWindow(w.X.Conn(), f.client,
		xproto.ConfigWindowX|xproto.ConfigWindowY|
			xproto.ConfigWindowWidth|xproto.ConfigWindowHeight,
		[]uint32{uint32(draw.BorderW), uint32(draw.TitleH), uint32(cw), uint32(ch)})
}

// configureFloat honors a float client's ConfigureRequest — the exact
// opposite of tiles, where the tree owns geometry. Requested client
// coordinates are translated to frame coordinates, clamped, applied.
func (w *WM) configureFloat(f *frame, ev xevent.ConfigureRequestEvent) {
	r := f.rect
	if ev.ValueMask&xproto.ConfigWindowX > 0 {
		r.X = int(ev.X) - draw.BorderW
	}
	if ev.ValueMask&xproto.ConfigWindowY > 0 {
		r.Y = int(ev.Y) - draw.TitleH
	}
	if ev.ValueMask&xproto.ConfigWindowWidth > 0 {
		r.W = clampInt(int(ev.Width), f.minW, f.maxW) + 2*draw.BorderW
	}
	if ev.ValueMask&xproto.ConfigWindowHeight > 0 {
		r.H = clampInt(int(ev.Height), f.minH, f.maxH) + draw.TitleH + draw.BorderW
	}
	r = w.clampFloatRect(r)
	resized := r.W != f.rect.W || r.H != f.rect.H
	f.rect = r
	f.win.MoveResize(r.X, r.Y, r.W, r.H)
	w.configureFloatClient(f)
	if resized {
		w.paintFrame(f)
	}
}

// toggleFloat flips the focused window between the tiled and floating
// worlds (T4). Returns the resulting floating state.
func (w *WM) toggleFloat() (bool, error) {
	if pf := w.floats[w.fstate.FocusedFloat()]; pf != nil {
		return false, w.sinkFloat(pf)
	}
	f := w.frames[w.fstate.FocusedLeaf()]
	if f == nil || f.client == 0 {
		return false, fmt.Errorf("no floatable window focused (builtin tiles cannot float)")
	}
	w.liftTile(f)
	return true, nil
}

// liftTile converts a tile to a float: the frame and client are
// untouched; only the maps and the tree change. Mirrors unmanage's tree
// handling — a lone leaf empties instead of closing.
func (w *WM) liftTile(f *frame) {
	leaf := f.leaf
	delete(w.frames, leaf)
	f.floating = true
	f.leaf = ""
	f.ws = w.desktop.Current
	f.rect = w.clampFloatRect(f.rect)
	w.floats[f.client] = f
	if w.fstate.FocusedLeaf() == leaf {
		w.fstate.ClearTile()
	}

	if ws := w.desktop.FindLeafWorkspace(leaf); ws != nil {
		if ws.Root.Kind == wmcore.Leaf {
			_, _ = w.Apply(wmcore.Op{Op: wmcore.OpSetLeafApp, Node: leaf, App: ""})
		} else {
			_, _ = w.Apply(wmcore.Op{Op: wmcore.OpCloseLeaf, Node: leaf})
		}
	}
	f.win.MoveResize(f.rect.X, f.rect.Y, f.rect.W, f.rect.H)
	w.configureFloatClient(f)
	w.focusFloat(f)
	w.emitEvent("window.float-toggled", map[string]interface{}{
		"client": uint32(f.client), "floating": true,
	})
}

// sinkFloat converts a float back to a tile via the normal placement
// path (reuse an empty leaf or split the focused one).
func (w *WM) sinkFloat(f *frame) error {
	leafID := w.placementLeaf()
	if leafID == "" {
		return fmt.Errorf("no leaf available for the window")
	}
	delete(w.floats, f.client)
	w.fstate.ClearFloat()
	f.floating = false
	f.leaf = leafID
	f.ws = ""
	f.leader = 0
	w.frames[leafID] = f
	_, _ = w.Apply(wmcore.Op{Op: wmcore.OpSetLeafApp, Node: leafID, App: "win"})
	w.focus(leafID)
	w.emitEvent("window.float-toggled", map[string]interface{}{
		"client": uint32(f.client), "floating": false, "leaf": string(leafID),
	})
	return nil
}
