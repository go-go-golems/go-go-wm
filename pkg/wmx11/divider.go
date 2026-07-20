package wmx11

import (
	"image"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/xcursor"
	"github.com/jezek/xgbutil/xevent"
	"github.com/jezek/xgbutil/xwindow"

	"github.com/go-go-golems/go-go-wm/pkg/draw"
	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
)

// Dividers are real windows living in the gap between split children, with
// the prototype's four interaction states (pbui-shell.jsx:171-196):
//
//	0 idle      paper, dotted grip mark
//	1 hot       paneAlt (pointer hovering)
//	2 dragging  sage
//	3 snapped   mustard — the flash that says "you are at ¼ ⅓ ½ ⅔ ¾"
//
// plus a col-resize / row-resize cursor.
type dividerWin struct {
	split wmcore.NodeID
	dir   wmcore.Dir
	win   *xwindow.Window
	rect  wmcore.Rect
	mode  int
}

// syncDividers reconciles divider windows with the current workspace's
// layout (called from relayout).
func (w *WM) syncDividers(items map[wmcore.NodeID]wmcore.LayoutItem, ws *wmcore.Workspace) {
	if w.dividers == nil {
		w.dividers = map[wmcore.NodeID]*dividerWin{}
	}
	seen := map[wmcore.NodeID]bool{}
	for id, item := range items {
		n := ws.Root.Find(id)
		if n == nil || n.Kind != wmcore.Split || item.DividerRect.W <= 0 || item.DividerRect.H <= 0 {
			continue
		}
		seen[id] = true
		d := w.dividers[id]
		if d == nil {
			d = w.createDivider(id, n.Dir)
			if d == nil {
				continue
			}
			w.dividers[id] = d
		}
		d.dir = n.Dir
		if d.rect != item.DividerRect {
			d.rect = item.DividerRect
			d.win.MoveResize(d.rect.X, d.rect.Y, d.rect.W, d.rect.H)
		}
		d.win.Map()
		w.paintDivider(d)
	}
	for id, d := range w.dividers {
		if !seen[id] {
			d.win.Destroy()
			delete(w.dividers, id)
		}
	}
}

func (w *WM) createDivider(split wmcore.NodeID, dir wmcore.Dir) *dividerWin {
	win, err := xwindow.Generate(w.X)
	if err != nil {
		return nil
	}
	cursorShape := uint16(xcursor.SBHDoubleArrow) // row split: horizontal resize
	if dir == wmcore.Col {
		cursorShape = xcursor.SBVDoubleArrow
	}
	cursor, _ := xcursor.CreateCursor(w.X, cursorShape)
	err = win.CreateChecked(w.X.RootWin(), 0, 0, 1, 1,
		xproto.CwBackPixel|xproto.CwEventMask|xproto.CwCursor,
		uint32(pixel(draw.Current().Paper)),
		uint32(xproto.EventMaskButtonPress|xproto.EventMaskButtonRelease|
			xproto.EventMaskPointerMotion|xproto.EventMaskExposure|
			xproto.EventMaskEnterWindow|xproto.EventMaskLeaveWindow),
		uint32(cursor))
	if err != nil {
		return nil
	}
	d := &dividerWin{split: split, dir: dir, win: win}
	id := win.Id

	xevent.EnterNotifyFun(func(_ *xgbutil.XUtil, _ xevent.EnterNotifyEvent) {
		if d.mode == 0 {
			d.mode = 1
			w.paintDivider(d)
			w.setMouseDoc("drag divider — sticky at ¼ ⅓ ½ ⅔ ¾")
		}
	}).Connect(w.X, id)
	xevent.LeaveNotifyFun(func(_ *xgbutil.XUtil, _ xevent.LeaveNotifyEvent) {
		if d.mode == 1 {
			d.mode = 0
			w.paintDivider(d)
			w.setMouseDoc("")
		}
	}).Connect(w.X, id)
	xevent.ButtonPressFun(func(_ *xgbutil.XUtil, _ xevent.ButtonPressEvent) {
		d.mode = 2
		w.paintDivider(d)
		w.beginDividerDrag(d.split)
	}).Connect(w.X, id)
	xevent.MotionNotifyFun(func(_ *xgbutil.XUtil, ev xevent.MotionNotifyEvent) {
		w.handleMotion(int(ev.RootX), int(ev.RootY))
	}).Connect(w.X, id)
	xevent.ButtonReleaseFun(func(_ *xgbutil.XUtil, ev xevent.ButtonReleaseEvent) {
		w.handleRelease(int(ev.RootX), int(ev.RootY))
	}).Connect(w.X, id)
	xevent.ExposeFun(func(_ *xgbutil.XUtil, ev xevent.ExposeEvent) {
		if ev.Count == 0 {
			w.paintDivider(d)
		}
	}).Connect(w.X, id)
	return d
}

// paintDivider renders a divider in its current mode: state fill plus the
// dotted 26px grip mark across the middle.
func (w *WM) paintDivider(d *dividerWin) {
	if d.rect.W < 1 || d.rect.H < 1 {
		return
	}
	img := image.NewRGBA(image.Rect(0, 0, d.rect.W, d.rect.H))
	draw.Fill(img, img.Bounds(), draw.DividerColor(d.mode))
	const mark = 26
	if d.dir == wmcore.Row { // vertical bar → vertical dotted line
		x := d.rect.W/2 - 1
		y0 := d.rect.H/2 - mark/2
		for y := y0; y < y0+mark; y += 4 {
			draw.Fill(img, image.Rect(x, y, x+2, y+2), draw.Current().Faint)
		}
	} else {
		y := d.rect.H/2 - 1
		x0 := d.rect.W/2 - mark/2
		for x := x0; x < x0+mark; x += 4 {
			draw.Fill(img, image.Rect(x, y, x+2, y+2), draw.Current().Faint)
		}
	}
	w.blit(d.win, img)
}

// dividerDragFeedback updates the dragged divider's mode from the snap
// state (called from dividerMotion).
func (w *WM) dividerDragFeedback(split wmcore.NodeID, snapped bool) {
	d := w.dividers[split]
	if d == nil {
		return
	}
	mode := 2
	if snapped {
		mode = 3
	}
	if d.mode != mode {
		d.mode = mode
		w.paintDivider(d)
	}
}

// dividerDragEnd resets the divider to idle (called from handleRelease).
func (w *WM) dividerDragEnd(split wmcore.NodeID) {
	if d := w.dividers[split]; d != nil {
		d.mode = 0
		w.paintDivider(d)
	}
}
