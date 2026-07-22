package wmx11

import (
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/xevent"
	"github.com/jezek/xgbutil/xwindow"
)

// connectFrameEvents wires the per-window callbacks for a new frame.
func (w *WM) connectFrameEvents(fw *xwindow.Window) {
	id := fw.Id
	xevent.ButtonPressFun(func(_ *xgbutil.XUtil, ev xevent.ButtonPressEvent) {
		if f := w.byFrame[ev.Event]; f != nil {
			w.handleFramePress(f, int(ev.EventX), int(ev.EventY), byte(ev.Detail), int(ev.RootX), int(ev.RootY))
		}
	}).Connect(w.X, id)
	xevent.ExposeFun(func(_ *xgbutil.XUtil, ev xevent.ExposeEvent) {
		if f := w.byFrame[ev.Window]; f != nil && ev.Count == 0 {
			// The frame's content lives in its background pixmap: a
			// current shm surface needs nothing at all (the server
			// repainted the exposed region from it already), a current
			// ximg needs one re-blit. A full re-render on every Expose
			// was ~27% of the profile — each MoveResize during a drag
			// exposed a frame that had just been painted (GGWM-005).
			// Buffers are capacity-sized, not viewport-sized (GGWM-012
			// Phase 2), so currency is checked against the bucket. Testing
			// against f.rect made every buffer look stale the moment
			// capacity sizing landed, which turned each Expose back into a
			// full re-render — the exact cost this fast path exists to
			// avoid.
			capW, capH := w.bucketSizeFor(f.rect.W, f.rect.H)
			switch {
			case f.surf != nil && f.surf.W == capW && f.surf.H == capH:
				// server-side repair; no client work
			case f.ximg != nil && f.ximg.Bounds().Dx() == capW && f.ximg.Bounds().Dy() == capH:
				f.ximg.XPaint(f.win.Id)
			default:
				w.paintFrame(f)
			}
		}
	}).Connect(w.X, id)
	xevent.MotionNotifyFun(func(_ *xgbutil.XUtil, ev xevent.MotionNotifyEvent) {
		// Frames see motion during grip drags that started on them.
		w.handleMotion(int(ev.RootX), int(ev.RootY))
	}).Connect(w.X, id)
	xevent.KeyPressFun(func(_ *xgbutil.XUtil, ev xevent.KeyPressEvent) {
		if f := w.byFrame[ev.Event]; f != nil {
			w.handleFrameKey(f, ev.State, ev.Detail)
		}
	}).Connect(w.X, id)
	xevent.ButtonReleaseFun(func(_ *xgbutil.XUtil, ev xevent.ButtonReleaseEvent) {
		w.handleRelease(int(ev.RootX), int(ev.RootY))
	}).Connect(w.X, id)
	xevent.EnterNotifyFun(func(_ *xgbutil.XUtil, ev xevent.EnterNotifyEvent) {
		if f := w.byFrame[ev.Event]; f != nil {
			if f.floating {
				w.setMouseDoc("float [" + f.title + "] — strip: drag to move · ✕: close")
			} else {
				w.setMouseDoc("tile [" + f.title + "] — title: menu · ⠿: drag to move · buttons: split/close")
			}
		}
	}).Connect(w.X, id)
}

// connectMenuEvents wires the menu overlay window.
func (w *WM) connectMenuEvents(mw *xwindow.Window) {
	id := mw.Id
	xevent.ButtonPressFun(func(_ *xgbutil.XUtil, ev xevent.ButtonPressEvent) {
		w.menuPress(int(ev.EventX), int(ev.EventY))
	}).Connect(w.X, id)
	xevent.MotionNotifyFun(func(_ *xgbutil.XUtil, ev xevent.MotionNotifyEvent) {
		w.menuMotion(int(ev.EventX), int(ev.EventY))
	}).Connect(w.X, id)
	xevent.ExposeFun(func(_ *xgbutil.XUtil, ev xevent.ExposeEvent) {
		if w.menu != nil && ev.Count == 0 {
			w.blit(w.menu.win, w.menu.model.Render())
		}
	}).Connect(w.X, id)
}

// connectBarEvents wires the top bar (chip clicks) and bottom bar.
func (w *WM) connectBarEvents() {
	xevent.ButtonPressFun(func(_ *xgbutil.XUtil, ev xevent.ButtonPressEvent) {
		if w.launcher != nil {
			w.closeLauncher()
			return
		}
		if w.menu != nil {
			w.closeMenu()
			return
		}
		w.topBarClicked(int(ev.EventX), int(ev.EventY))
	}).Connect(w.X, w.topBar.Id)
	xevent.ExposeFun(func(_ *xgbutil.XUtil, ev xevent.ExposeEvent) {
		if ev.Count == 0 {
			w.paintBars()
		}
	}).Connect(w.X, w.topBar.Id)
	xevent.ExposeFun(func(_ *xgbutil.XUtil, ev xevent.ExposeEvent) {
		if ev.Count == 0 {
			w.paintBars()
		}
	}).Connect(w.X, w.bottomBar.Id)
}
