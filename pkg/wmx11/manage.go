package wmx11

import (
	"image"
	"strings"
	"time"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/ewmh"
	"github.com/jezek/xgbutil/icccm"
	"github.com/jezek/xgbutil/xevent"
	"github.com/jezek/xgbutil/xgraphics"
	"github.com/jezek/xgbutil/xwindow"

	"github.com/go-go-golems/go-go-wm/pkg/apps"
	"github.com/go-go-golems/go-go-wm/pkg/draw"
	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
	"github.com/go-go-golems/go-go-wm/pkg/xshm"
)

// manageExisting adopts windows that were mapped before we started
// (Xephyr restarts, --replace-style flows).
func (w *WM) manageExisting() {
	tree, err := xproto.QueryTree(w.X.Conn(), w.X.RootWin()).Reply()
	if err != nil {
		return
	}
	for _, child := range tree.Children {
		attrs, err := xproto.GetWindowAttributes(w.X.Conn(), child).Reply()
		if err != nil || attrs.OverrideRedirect || attrs.MapState != xproto.MapStateViewable {
			continue
		}
		w.manage(child)
	}
}

// handleMapRequest is the front door: a client asked to be mapped.
func (w *WM) handleMapRequest(ev xevent.MapRequestEvent) {
	if f := w.byClient[ev.Window]; f != nil {
		f.win.Map()
		f.mapped = mapMapped
		return
	}
	attrs, err := xproto.GetWindowAttributes(w.X.Conn(), ev.Window).Reply()
	if err == nil && attrs.OverrideRedirect {
		// Menus/tooltips manage themselves.
		xproto.MapWindow(w.X.Conn(), ev.Window)
		return
	}
	w.manage(ev.Window)
}

// manage wraps a client in a frame — a tiled one (a leaf in the tree) or,
// when the float decision says so, a floating one (shell state only).
func (w *WM) manage(clientWin xproto.Window) {
	title, _ := ewmh.WmNameGet(w.X, clientWin)
	if title == "" {
		title, _ = icccm.WmNameGet(w.X, clientWin)
	}
	if title == "" {
		title = "window"
	}
	var class, instance string
	if wmClass, err := icccm.WmClassGet(w.X, clientWin); err == nil && wmClass != nil {
		class = wmClass.Class
		instance = wmClass.Instance
	}

	// The third exit from the front door (GGWM-007): dialogs, utility
	// palettes, and rule-forced windows float above the tree.
	props := fetchFloatProps(w, clientWin)
	verdict := w.floatRuleVerdict(title, class, instance)
	if floatDecision(verdict, props.leader, props.types, props.fixedSize) {
		w.manageFloat(clientWin, title, class, instance, props)
		return
	}

	// Pick a leaf: reuse the current workspace's empty leaf if there is
	// exactly one unoccupied slot, else split the focused (or last) leaf.
	leafID := w.placementLeaf()
	if leafID == "" {
		return
	}

	f := &frame{leaf: leafID, client: clientWin, title: title, class: class, instance: instance}

	fw, err := xwindow.Generate(w.X)
	if err != nil {
		return
	}
	err = fw.CreateChecked(w.X.RootWin(), 0, 0, 100, 100,
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

	// Survive WM death: put the client in the save-set so the server
	// reparents it back to root if we crash.
	xproto.ChangeSaveSet(w.X.Conn(), xproto.SetModeInsert, clientWin)
	xproto.ConfigureWindow(w.X.Conn(), clientWin,
		xproto.ConfigWindowBorderWidth, []uint32{0})
	xproto.ReparentWindow(w.X.Conn(), clientWin, fw.Id, 0, draw.TitleH)
	w.wireClient(f)

	_, _ = wmcore.Apply(w.desktop, wmcore.Op{Op: wmcore.OpSetLeafApp, Node: leafID, App: "win"})
	w.frames[leafID] = f
	w.byClient[clientWin] = f
	w.byFrame[fw.Id] = f
	w.connectFrameEvents(fw)

	fw.Map()
	f.mapped = mapMapped
	xproto.MapWindow(w.X.Conn(), clientWin)
	w.focus(leafID)
	managedData := map[string]interface{}{
		"leaf": string(leafID), "title": title,
		"class": f.class, "instance": f.instance,
	}
	if ws := w.desktop.FindLeafWorkspace(leafID); ws != nil {
		managedData["workspace"] = ws.ID
	}
	w.emitEvent("window.managed", managedData)
	w.relayout()
	w.updateEWMH()
	w.paintBars()
}

// wireClient connects the per-client lifecycle handlers and the
// click-to-focus grab — shared by the tile and float paths.
//
// Track client lifetime and title changes. The callbacks must be
// connected to the CLIENT window: after reparenting, the client's
// StructureNotify events (DestroyNotify, UnmapNotify) carry the
// client as their event window, and xgbutil dispatches callbacks by
// that window. The root-connected handlers in setupInput never see
// them — which left zombie frames whenever a client exited on its
// own (Ctrl-D in an xterm), with BadWindow spam from focusing and
// configuring the dead client id.
func (w *WM) wireClient(f *frame) {
	clientWin := f.client
	cw := xwindow.New(w.X, clientWin)
	_ = cw.Listen(xproto.EventMaskStructureNotify, xproto.EventMaskPropertyChange)
	xevent.DestroyNotifyFun(func(_ *xgbutil.XUtil, ev xevent.DestroyNotifyEvent) {
		w.handleDestroyNotify(ev)
	}).Connect(w.X, clientWin)
	xevent.UnmapNotifyFun(func(_ *xgbutil.XUtil, ev xevent.UnmapNotifyEvent) {
		w.handleUnmapNotify(ev)
	}).Connect(w.X, clientWin)

	// Click-to-focus: clients consume their own clicks, so without this
	// only the title strip could focus a tile. A synchronous passive
	// grab lets the WM see the press first; ReplayPointer then hands
	// the click to the app untouched.
	xproto.GrabButton(w.X.Conn(), true, clientWin,
		uint16(xproto.EventMaskButtonPress), xproto.GrabModeSync, xproto.GrabModeAsync,
		xproto.WindowNone, xproto.CursorNone, xproto.ButtonIndexAny, xproto.ModMaskAny)
	xevent.ButtonPressFun(func(_ *xgbutil.XUtil, ev xevent.ButtonPressEvent) {
		if f.floating {
			if w.fstate.FocusedFloat() != f.client {
				w.focusFloat(f)
			}
		} else if w.fstate.FocusedLeaf() != f.leaf || w.fstate.FocusedFloat() != 0 {
			w.focus(f.leaf)
		}
		xproto.AllowEvents(w.X.Conn(), xproto.AllowReplayPointer, ev.Time)
	}).Connect(w.X, clientWin)
}

// placementLeaf decides which leaf a new client lands in.
func (w *WM) placementLeaf() wmcore.NodeID {
	ws := w.desktop.CurrentWorkspace()
	// First choice: a launcher leaf (App == "") in the current workspace.
	// Builtin app tiles (trace, listener, …) are occupied and not stolen.
	for _, l := range ws.Root.Leaves() {
		if l.App == "" {
			// Evict the launcher frame; the client frame replaces it.
			if f := w.frames[l.ID]; f != nil && f.client == 0 {
				delete(w.frames, l.ID)
				delete(w.byFrame, f.win.Id)
				xevent.Detach(w.X, f.win.Id)
				f.dropBuffers()
				f.win.Destroy()
			}
			return l.ID
		}
	}
	// Otherwise split the focused leaf (or the first).
	at := w.fstate.FocusedLeaf()
	if at == "" || ws.Root.FindLeaf(at) == nil {
		at = ws.Root.Leaves()[0].ID
	}
	res, err := wmcore.Apply(w.desktop, wmcore.Op{Op: wmcore.OpSplitLeaf, Node: at, Dir: wmcore.Row, App: ""})
	if err != nil {
		return ""
	}
	w.emitEvent("split_tile", map[string]interface{}{"node": string(at), "auto": true})
	return res.NewLeaf
}

// unmanage removes a client (destroyed or withdrawn).
func (w *WM) unmanage(clientWin xproto.Window) {
	f := w.byClient[clientWin]
	if f == nil {
		return
	}
	if f.floating {
		w.unmanageFloat(f)
		return
	}
	w.clearFullscreenFor(f)
	delete(w.byClient, clientWin)
	delete(w.byFrame, f.win.Id)
	delete(w.frames, f.leaf)
	// Drop the per-window callbacks registered in manage/connectFrameEvents;
	// X recycles window ids, and stale callbacks would fire for strangers.
	xevent.Detach(w.X, clientWin)
	xevent.Detach(w.X, f.win.Id)
	f.dropBuffers()
	f.win.Destroy()

	// Close the leaf if its workspace still has siblings; a lone leaf just
	// becomes empty (and syncBuiltins gives it a launcher frame again).
	if ws := w.desktop.FindLeafWorkspace(f.leaf); ws != nil {
		if ws.Root.Kind == wmcore.Leaf {
			_, _ = wmcore.Apply(w.desktop, wmcore.Op{Op: wmcore.OpSetLeafApp, Node: f.leaf, App: ""})
		} else {
			_, _ = wmcore.Apply(w.desktop, wmcore.Op{Op: wmcore.OpCloseLeaf, Node: f.leaf})
		}
	}
	w.syncBuiltins()
	if w.fstate.FocusedLeaf() == f.leaf {
		w.fstate.ClearTile()
		if ws := w.desktop.CurrentWorkspace(); ws != nil {
			for _, l := range ws.Root.Leaves() {
				if _, ok := w.frames[l.ID]; ok {
					w.focus(l.ID)
					break
				}
			}
		}
	}
	w.emitEvent("close_tile", map[string]interface{}{"leaf": string(f.leaf)})
	w.relayout()
	w.updateEWMH()
	w.paintBars()
}

// reapOrphanFrames destroys frames whose leaves vanished from the desktop
// (e.g. close-leaf via IPC killed the leaf first).
func (w *WM) reapOrphanFrames() {
	for leaf, f := range w.frames {
		if w.desktop.FindLeafWorkspace(leaf) == nil {
			// Ask the client to close, ICCCM-politely.
			w.closeClient(f)
		}
	}
}

// closeClient sends WM_DELETE_WINDOW if supported, else kills. Builtin
// tiles have no client to ask: their ✕ closes the leaf itself (a lone
// leaf becomes an empty launcher, mirroring unmanage) — the resulting
// afterOp/syncBuiltins pass destroys the frame.
func (w *WM) closeClient(f *frame) {
	if f.client == 0 {
		ws := w.desktop.FindLeafWorkspace(f.leaf)
		if ws == nil {
			return // orphan frame; syncBuiltins reaps it
		}
		if ws.Root.Kind == wmcore.Leaf {
			_, _ = w.Apply(wmcore.Op{Op: wmcore.OpSetLeafApp, Node: f.leaf, App: ""})
		} else {
			_, _ = w.Apply(wmcore.Op{Op: wmcore.OpCloseLeaf, Node: f.leaf})
		}
		return
	}
	protos, _ := icccm.WmProtocolsGet(w.X, f.client)
	for _, p := range protos {
		if p == "WM_DELETE_WINDOW" {
			wmProtocols, err := xprop_Atm(w.X, "WM_PROTOCOLS")
			if err != nil {
				break
			}
			deleteAtom, err := xprop_Atm(w.X, "WM_DELETE_WINDOW")
			if err != nil {
				break
			}
			cm, err := clientMessage(f.client, wmProtocols, uint32(deleteAtom))
			if err == nil {
				xproto.SendEvent(w.X.Conn(), false, f.client,
					xproto.EventMaskNoEvent, string(cm.Bytes()))
				return
			}
		}
	}
	xwindow.New(w.X, f.client).Kill()
}

// relayout applies wmcore geometry to every frame in the current workspace
// and hides frames on other workspaces. It repaints every visible frame —
// the "make the screen match the model" primitive (theme swaps and
// workspace switches depend on that).
func (w *WM) relayout() { w.relayoutPaint(true) }

// relayoutResized repaints only frames whose rect changed — the divider
// drag path, where repainting untouched panes at motion rate dominated
// the CPU profile (GGWM-005). Exposure repaints cover everything else.
func (w *WM) relayoutResized() { w.relayoutPaint(false) }

func (w *WM) relayoutPaint(paintAll bool) {
	ws := w.desktop.CurrentWorkspace()
	if ws == nil {
		return
	}
	t0 := time.Now()
	w.perf.relayouts++
	w.perf.layoutCalls++
	items := wmcore.Layout(ws.Root, w.area, Gap)
	// One index per pass replaces a Root.Find (O(n) DFS) per layout item,
	// which made this loop O(n^2) in tree size (GGWM-012).
	if w.idxScratch == nil {
		w.idxScratch = wmcore.Index{}
	}
	idx := wmcore.BuildIndexInto(ws.Root, w.idxScratch)
	w.syncDividers(items, idx)
	defer func() { w.perf.relayoutNanos += uint64(time.Since(t0).Nanoseconds()) }()
	visible := map[wmcore.NodeID]bool{}
	for id, item := range items {
		if n := idx[id]; n == nil || n.Kind != wmcore.Leaf {
			continue
		}
		visible[id] = true
		f := w.frames[id]
		if f == nil || w.fs.Owns(f) {
			continue // fullscreen owns its geometry until it exits
		}
		r := item.Rect
		resized := f.rect != r
		// A pure move keeps the frame's pixels valid; only a size change
		// invalidates the paint buffers. Tracking them separately lets the
		// resize path skip paint for moves (GGWM-012).
		sizeChanged := f.rect.W != r.W || f.rect.H != r.H
		if resized {
			w.perf.framesMoved++
			if sizeChanged {
				w.perf.framesResized++
			}
			f.rect = r
			f.win.MoveResize(r.X, r.Y, r.W, r.H)
			// Inner client area: inside the 2px border, below the strip.
			cw := r.W - 2*draw.BorderW
			ch := r.H - draw.TitleH - draw.BorderW
			if cw < 1 {
				cw = 1
			}
			if ch < 1 {
				ch = 1
			}
			if f.client != 0 {
				xproto.ConfigureWindow(w.X.Conn(), f.client,
					xproto.ConfigWindowX|xproto.ConfigWindowY|
						xproto.ConfigWindowWidth|xproto.ConfigWindowHeight,
					[]uint32{uint32(draw.BorderW), uint32(draw.TitleH), uint32(cw), uint32(ch)})
			}
		}
		if paintAll || resized {
			// GO_GO_WM_NO_RESIZE_PAINT measures the upper bound of removing
			// decoration paint from the drag path: geometry still commits,
			// pixels keep their old contents until release. It answers "how
			// much of the drag cost is paint?" without restructuring the
			// frame into chrome + content first (GGWM-012 Phase 2).
			if suppressResizePaint && w.drag != nil && w.drag.kind == "divider" && !paintAll {
				w.perf.resizePaintSuppressed++
			} else {
				w.paintFrame(f)
			}
		}
	}
	// Hide everything not on this workspace. Issue the request only on a
	// visibility transition: this loop walks every frame in the process,
	// across all workspaces, so unconditional Map/Unmap meant dozens of
	// redundant requests per relayout (GGWM-012).
	for leaf, f := range w.frames {
		if !visible[leaf] {
			if f.mapped != mapUnmapped {
				f.win.Unmap()
				f.mapped = mapUnmapped
			} else {
				w.perf.mapReqSkipped++
			}
			f.rect = wmcore.Rect{}
			f.dropBuffers() // off-screen frames don't hold megabytes
		} else if f.mapped != mapMapped {
			f.win.Map()
			f.mapped = mapMapped
		} else {
			w.perf.mapReqSkipped++
		}
	}
	w.syncFloats(paintAll)
}

// paintFrame draws the title strip + border for a frame (and, for builtin
// tiles, the WM-rendered app content).
func (w *WM) paintFrame(f *frame) {
	if f.rect.W < 4 || f.rect.H < 4 {
		return
	}
	w.perf.framesPainted++
	defer func(t0 time.Time) {
		d := time.Since(t0)
		w.perf.addPaint(d, f.rect.W*f.rect.H)
		log.Debug().Dur("ms", d).Str("leaf", string(f.leaf)).
			Int("w", f.rect.W).Int("h", f.rect.H).Msg("paintFrame")
	}(time.Now())
	composeStart := time.Now()
	if f.img == nil || f.img.Bounds().Dx() != f.rect.W || f.img.Bounds().Dy() != f.rect.H {
		f.img = image.NewRGBA(image.Rect(0, 0, f.rect.W, f.rect.H))
	}
	img := f.img
	draw.Fill(img, img.Bounds(), draw.Current().Pane)
	stripColor := draw.AppColor(leafColor(f.leaf))
	if name := w.builtinAppOf(f); f.client == 0 && name != "" {
		if strings.HasPrefix(name, scriptPrefix) {
			stripColor = draw.Current().Lavender
			f.title = strings.TrimPrefix(name, scriptPrefix) + " (js)"
		} else {
			stripColor = apps.BuiltinColor(name)
			f.title = apps.BuiltinTitle(name)
		}
	}
	strip := draw.TitleStrip{
		Title:   f.title,
		Color:   stripColor,
		Focused: w.fstate.Focused(f),
		Width:   f.rect.W,
		Float:   f.floating,
	}
	if !f.floating && w.accepting != nil && matchesTile(w.accepting.ptypes) {
		// Tiles are acceptable: highlight the strip (the pulsing red
		// outline of the prototype, statically).
		strip.Color = draw.Current().Sel
	}
	stripImg := strip.Render()
	copyImage(img, stripImg, 0, 0)
	if f.client == 0 {
		f.regions = w.paintBuiltin(f, img)
	}
	draw.Border(img, img.Bounds(), draw.BorderW, draw.Current().Ink)

	w.perf.composeNanos += uint64(time.Since(composeStart).Nanoseconds())
	uploadStart := time.Now()
	defer func() { w.perf.uploadNanos += uint64(time.Since(uploadStart).Nanoseconds()) }()
	// Upload. Preferred path (GGWM-006): a MIT-SHM shared pixmap set as
	// the window background — WriteRGBA is the only pixel pass, and
	// ClearAll makes the server blit it. Fallback: the cached
	// xgraphics image (PutImage chunks over the socket).
	if xshm.Available(w.X) {
		if f.surf != nil && (f.surf.W != f.rect.W || f.surf.H != f.rect.H) {
			// Every dimension change tears the shared pixmap down and
			// builds a new one. xshm.New issues two CHECKED requests, i.e.
			// two synchronous X round trips, plus a shmget/shmat/IPC_RMID
			// triple — and a divider drag changes dimensions on every
			// tick. These counters exist to size that (GGWM-012).
			w.perf.shmDestroys++
			f.surf.Destroy()
			f.surf = nil
		}
		if f.surf == nil {
			w.perf.shmCreates++
			if surf, err := xshm.New(w.X, xproto.Drawable(f.win.Id), f.rect.W, f.rect.H); err == nil {
				f.surf = surf
				xproto.ChangeWindowAttributes(w.X.Conn(), f.win.Id,
					xproto.CwBackPixmap, []uint32{uint32(surf.Pixmap)})
			} else {
				log.Warn().Err(err).Msg("xshm surface failed; falling back to PutImage")
			}
		}
		if f.surf != nil {
			f.surf.WriteRGBA(img)
			f.win.ClearAll()
			return
		}
	}
	// XSurfaceSet only when the pixmap is (re)created, XDraw+XPaint
	// every time. Keeping the ximg alive also makes Expose a single
	// XPaint (see connectFrameEvents).
	if f.ximg == nil || f.ximg.Bounds() != img.Bounds() {
		// The PutImage fallback has the same shape of churn as the shm
		// path: a client image plus a server pixmap recreated on every
		// size change. On hosts where the server reports no shared-pixmap
		// support this is the ONLY path, so it is the one that matters
		// there (GGWM-012).
		if f.ximg != nil {
			w.perf.ximgDestroys++
			f.ximg.Destroy()
		}
		w.perf.ximgCreates++
		f.ximg = xgraphics.New(w.X, img.Bounds())
		if err := f.ximg.XSurfaceSet(f.win.Id); err != nil {
			f.dropBuffers()
			return
		}
	}
	draw.CopyToXImage(f.ximg, img)
	f.ximg.XDraw()
	f.ximg.XPaint(f.win.Id)
}

func matchesTile(ptypes []string) bool {
	for _, p := range ptypes {
		if p == "tile" || p == "any" {
			return true
		}
	}
	return false
}

// copyImage blits src into dst at (x, y) by row copies. The obvious
// per-pixel dst.Set(src.At(...)) version allocates a color interface
// per pixel and was 73% of the workspace-creation CPU (GGWM-005
// follow-up profile) — blits are memmove jobs.
func copyImage(dst *image.RGBA, src *image.RGBA, x, y int) {
	b := src.Bounds()
	w := b.Dx()
	if w <= 0 {
		return
	}
	db := dst.Bounds()
	for yy := b.Min.Y; yy < b.Max.Y; yy++ {
		dy := y + yy - b.Min.Y
		if dy < db.Min.Y || dy >= db.Max.Y {
			continue
		}
		// Clip the row horizontally against dst.
		sx, cw := b.Min.X, w
		dx := x
		if dx < db.Min.X {
			sx += db.Min.X - dx
			cw -= db.Min.X - dx
			dx = db.Min.X
		}
		if dx+cw > db.Max.X {
			cw = db.Max.X - dx
		}
		if cw <= 0 {
			continue
		}
		so := src.PixOffset(sx, yy)
		do := dst.PixOffset(dx, dy)
		copy(dst.Pix[do:do+cw*4], src.Pix[so:so+cw*4])
	}
}

// focus gives input focus to a leaf's client. Builtin tiles have no
// client (0); focusing window 0 would SetInputFocus(None), after which
// the server discards keyboard processing and even root-grabbed
// keybindings die — focus the frame window instead.
func (w *WM) focus(leaf wmcore.NodeID) {
	// Decide where focus actually goes given the fullscreen state (RC-5/7/13).
	// The decision is the single source of truth for the tiled-vs-floating
	// distinction; focus() routes the state change through focusState
	// (Option B B4) so the exactly-one invariant is structural.
	dec := w.computeFocusDecision(leaf)
	if dec.kind == focusFullscreenFloat {
		// A floating fullscreen frame has an empty leaf (floats are not
		// tree leaves). Pin to the float's client and PRESERVE the tiled
		// leaf beneath (RC-7/13) via focusState.FocusFullscreen.
		w.fstate.FocusFullscreen(w.fs.FocusTarget())
		if dec.client != 0 {
			xwindow.New(w.X, dec.client).Focus()
			_ = ewmh.ActiveWindowSet(w.X, dec.client)
		}
		if pf := w.frames[w.fstate.FocusedLeaf()]; pf != nil && pf.leaf != "" {
			w.paintFrame(pf)
		}
		return
	}
	if dec.kind == focusFullscreenTile {
		// Tiled fullscreen: pin to the fullscreen frame's leaf (RC-5).
		leaf = dec.leaf
	}
	prev := w.fstate.FocusedLeaf()
	// A navigation or tile click means "back to the tiled world": the
	// float band keeps its windows but loses the keyboard.
	w.fstate.FocusTile(leaf)
	if pf := w.floats[w.fstate.FocusedFloat()]; pf != nil {
		w.paintFrame(pf)
	}
	if f := w.frames[leaf]; f != nil {
		if f.client != 0 {
			xwindow.New(w.X, f.client).Focus()
			_ = ewmh.ActiveWindowSet(w.X, f.client)
		} else {
			f.win.Focus()
		}
	}
	if pf := w.frames[prev]; pf != nil && prev != leaf {
		w.paintFrame(pf)
	}
	if f := w.frames[leaf]; f != nil {
		w.paintFrame(f)
	}
}

// handleDestroyNotify / handleUnmapNotify keep the tree honest.
func (w *WM) handleDestroyNotify(ev xevent.DestroyNotifyEvent) { w.unmanage(ev.Window) }

func (w *WM) handleUnmapNotify(ev xevent.UnmapNotifyEvent) {
	// Ignore unmaps we caused (workspace switches); a client-initiated
	// withdraw has the event window == client and the client still exists
	// in our map but is on the *current* workspace.
	f := w.byClient[ev.Window]
	if f == nil {
		return
	}
	if f.floating {
		// Same distinction for floats: workspace switches unmap only the
		// frame (the client stays mapped inside it), so a client-window
		// UnmapNotify here is a genuine withdraw.
		if f.ws == w.desktop.Current {
			w.unmanage(ev.Window)
		}
		return
	}
	if ws := w.desktop.CurrentWorkspace(); ws != nil && ws.Root.FindLeaf(f.leaf) != nil {
		w.unmanage(ev.Window)
	}
}

// handleConfigureRequest honors client resize wishes only for unmanaged
// windows; managed geometry belongs to the tree.
func (w *WM) handleConfigureRequest(ev xevent.ConfigureRequestEvent) {
	if f := w.byClient[ev.Window]; f != nil {
		if f.floating {
			// Fullscreen owns the geometry of the fullscreen frame, so a
			// ConfigureRequest while fullscreen is ignored (RC-12). The
			// decision is display-free and unit-tested.
			if !w.shouldHonorFloatConfigure(f) {
				return
			}
			// Floats own their geometry: honor the request (clamped).
			w.configureFloat(f, ev)
			return
		}
		// Re-assert our geometry. ICCCM 4.1.5 requires a synthetic
		// ConfigureNotify carrying root-relative values when a request is
		// denied; it does NOT require recomputing the workspace. Calling
		// relayout() here let a client that spams ConfigureRequest drive
		// full relayouts and repaints at its own rate (GGWM-012).
		w.sendSyntheticConfigureNotify(f)
		return
	}
	xwindow.New(w.X, ev.Window).Configure(int(ev.ValueMask),
		int(ev.X), int(ev.Y), int(ev.Width), int(ev.Height),
		ev.Sibling, ev.StackMode)
}

var _ = xgbutil.XUtil{}

// sendSyntheticConfigureNotify tells a tiled client the geometry it actually
// has, in root coordinates, after we declined the geometry it asked for.
//
// This is the whole correct response to a denied ConfigureRequest: no layout
// traversal, no paint, no buffer allocation.
func (w *WM) sendSyntheticConfigureNotify(f *frame) {
	if f == nil || f.client == 0 {
		return
	}
	cw := f.rect.W - 2*draw.BorderW
	ch := f.rect.H - draw.TitleH - draw.BorderW
	if cw < 1 {
		cw = 1
	}
	if ch < 1 {
		ch = 1
	}
	ev := xproto.ConfigureNotifyEvent{
		Event:            f.client,
		Window:           f.client,
		AboveSibling:     0,
		X:                int16(f.rect.X + draw.BorderW),
		Y:                int16(f.rect.Y + draw.TitleH),
		Width:            uint16(cw),
		Height:           uint16(ch),
		BorderWidth:      0,
		OverrideRedirect: false,
	}
	xproto.SendEvent(w.X.Conn(), false, f.client,
		xproto.EventMaskStructureNotify, string(ev.Bytes()))
}
