package wmx11

import (
	"fmt"
	"image"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil/xgraphics"
	"github.com/jezek/xgbutil/xwindow"

	"github.com/go-go-golems/go-go-wm/pkg/draw"
	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
)

func image_Pt(x, y int) image.Point { return image.Point{X: x, Y: y} }

func (w *WM) setupBars() error {
	mk := func(x, y, width, height int) (*xwindow.Window, error) {
		win, err := xwindow.Generate(w.X)
		if err != nil {
			return nil, err
		}
		err = win.CreateChecked(w.X.RootWin(), x, y, width, height,
			xproto.CwBackPixel|xproto.CwOverrideRedirect|xproto.CwEventMask,
			uint32(pixel(draw.Current().Paper)), 1,
			xproto.EventMaskExposure|xproto.EventMaskButtonPress)
		if err != nil {
			return nil, err
		}
		win.Map()
		return win, nil
	}
	var err error
	if w.topBar, err = mk(0, 0, w.screen.W, draw.BarH); err != nil {
		return err
	}
	if w.bottomBar, err = mk(0, w.screen.H-draw.BarH, w.screen.W, draw.BarH); err != nil {
		return err
	}
	// Overlay for drop previews: created on demand.
	w.connectBarEvents()
	w.paintBars()
	return nil
}

func (w *WM) topBarModel() draw.TopBar {
	names := make([]string, 0, len(w.desktop.Workspaces))
	cur := 0
	for i, ws := range w.desktop.Workspaces {
		names = append(names, ws.Name)
		if ws.ID == w.desktop.Current {
			cur = i
		}
	}
	return draw.TopBar{Workspaces: names, Current: cur, Width: w.screen.W}
}

func (w *WM) paintBars() {
	if w.topBar == nil || w.bottomBar == nil {
		return
	}
	// Top: banner while accepting, workspace strip otherwise.
	var top *image.RGBA
	if w.accepting != nil {
		top = draw.Banner{Ptypes: w.accepting.ptypes, Prompt: w.accepting.prompt, Width: w.screen.W}.Render()
	} else {
		top = w.topBarModel().Render()
	}
	w.blit(w.topBar, top)

	mode := "READY"
	switch {
	case w.accepting != nil:
		mode = "ACCEPT MODE"
	case w.drag != nil && w.drag.kind == "grip":
		mode = "MOVING APP"
	case w.drag != nil:
		mode = "RESIZING"
	}
	doc := w.mouseDoc
	if doc == "" {
		if w.accepting != nil {
			doc = w.accepting.prompt + "   (Esc: abort)"
		} else {
			doc = "Mod4-Return terminal · Mod4-d/s split · Mod4-w close · drag borders (sticky ¼ ⅓ ½ ⅔ ¾) · drag ⠿: center = swap, edge = split-dock"
		}
	}
	ws := w.desktop.CurrentWorkspace()
	counts := ""
	if ws != nil {
		counts = fmt.Sprintf("%d tiles · %d workspaces", ws.Root.CountLeaves(), len(w.desktop.Workspaces))
	}
	bottom := draw.StatusLine{Mode: mode, Doc: doc, Counts: counts, Width: w.screen.W}.Render()
	w.blit(w.bottomBar, bottom)
}

// blit paints an RGBA image onto a window. The two bar windows are
// painted on every op, so their X images are cached (the same buffer
// discipline as frames, GGWM-005/006); transient windows (menus,
// dividers, overlay) keep the allocate-and-destroy path.
func (w *WM) blit(win *xwindow.Window, img *image.RGBA) {
	if w.topBar != nil && win.Id == w.topBar.Id {
		w.blitCached(&w.topBarImg, win, img)
		return
	}
	if w.bottomBar != nil && win.Id == w.bottomBar.Id {
		w.blitCached(&w.bottomBarImg, win, img)
		return
	}
	ximg := draw.ToXImage(w.X, img)
	if err := ximg.XSurfaceSet(win.Id); err == nil {
		ximg.XDraw()
		ximg.XPaint(win.Id)
	}
	ximg.Destroy()
}

func (w *WM) blitCached(slot **xgraphics.Image, win *xwindow.Window, img *image.RGBA) {
	if *slot == nil || (*slot).Bounds() != img.Bounds() {
		if *slot != nil {
			(*slot).Destroy()
		}
		*slot = xgraphics.New(w.X, img.Bounds())
		if err := (*slot).XSurfaceSet(win.Id); err != nil {
			(*slot).Destroy()
			*slot = nil
			return
		}
	}
	draw.CopyToXImage(*slot, img)
	(*slot).XDraw()
	(*slot).XPaint(win.Id)
}

// --- drop preview ----------------------------------------------------------

func (w *WM) showDropPreview(tile wmcore.Rect, zone wmcore.Zone) {
	r := zoneRect(tile, zone)
	label := "= swap apps"
	if zone != wmcore.ZoneCenter {
		label = "split-dock here - old tile closes"
	}
	if w.overlay == nil {
		win, err := xwindow.Generate(w.X)
		if err != nil {
			return
		}
		err = win.CreateChecked(w.X.RootWin(), r.X, r.Y, r.W, r.H,
			xproto.CwBackPixel|xproto.CwOverrideRedirect,
			uint32(pixel(draw.Current().Paper)), 1)
		if err != nil {
			return
		}
		w.overlay = win
	}
	w.overlay.MoveResize(r.X, r.Y, r.W, r.H)
	w.overlay.Map()
	w.overlay.Stack(xproto.StackModeAbove)
	w.blit(w.overlay, draw.DropPreview{W: r.W, H: r.H, Label: label}.Render())
}

func (w *WM) hideDropPreview() {
	if w.overlay != nil {
		w.overlay.Unmap()
	}
}

func zoneRect(t wmcore.Rect, zone wmcore.Zone) wmcore.Rect {
	switch zone {
	case wmcore.ZoneLeft:
		return wmcore.Rect{X: t.X, Y: t.Y, W: t.W / 2, H: t.H}
	case wmcore.ZoneRight:
		return wmcore.Rect{X: t.X + t.W/2, Y: t.Y, W: t.W / 2, H: t.H}
	case wmcore.ZoneTop:
		return wmcore.Rect{X: t.X, Y: t.Y, W: t.W, H: t.H / 2}
	case wmcore.ZoneBottom:
		return wmcore.Rect{X: t.X, Y: t.Y + t.H/2, W: t.W, H: t.H / 2}
	case wmcore.ZoneCenter:
		return t
	default:
		return t
	}
}
