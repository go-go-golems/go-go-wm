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
			w.paintFrame(f)
		}
	}).Connect(w.X, id)
	xevent.MotionNotifyFun(func(_ *xgbutil.XUtil, ev xevent.MotionNotifyEvent) {
		// Frames see motion during grip drags that started on them.
		w.handleMotion(int(ev.RootX), int(ev.RootY))
	}).Connect(w.X, id)
	xevent.ButtonReleaseFun(func(_ *xgbutil.XUtil, ev xevent.ButtonReleaseEvent) {
		w.handleRelease(int(ev.RootX), int(ev.RootY))
	}).Connect(w.X, id)
	xevent.EnterNotifyFun(func(_ *xgbutil.XUtil, ev xevent.EnterNotifyEvent) {
		if f := w.byFrame[ev.Event]; f != nil {
			w.setMouseDoc("tile [" + f.title + "] — title: menu · ⠿: drag to move · buttons: split/close")
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
