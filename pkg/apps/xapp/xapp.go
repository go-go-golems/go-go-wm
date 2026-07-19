// Package xapp is the client-side shell for standalone PBUI demo apps: it
// owns an X window (a plain client the WM manages like any other), renders
// an apps.Region surface into it, applies the PBUI click contract, and
// bridges the broker (accept highlighting, verb dispatch, hover docs).
package xapp

import (
	"context"
	"image"
	"time"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/ewmh"
	"github.com/jezek/xgbutil/keybind"
	"github.com/jezek/xgbutil/xevent"
	"github.com/jezek/xgbutil/xgraphics"
	"github.com/jezek/xgbutil/xwindow"

	"github.com/go-go-golems/go-go-wm/pkg/apps"
	"github.com/go-go-golems/go-go-wm/pkg/draw"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/client"
)

// App is what a demo app implements. Render must be pure; the other hooks
// run on the xapp loop.
type App interface {
	Name() string  // broker client name, verb owner ("demo-colors")
	Title() string // WM_NAME shown in the title strip
	Verbs() []pbui.Verb
	Render(w, h int, accepting []string) (*image.RGBA, []apps.Region)
	// HandleAction runs a plain-button action ("cmd:add-random").
	HandleAction(ctx Ctx, action string)
	// HandleVerb runs one of the app's registered verbs on an object.
	HandleVerb(ctx Ctx, verbID string, obj *pbui.Object)
}

// Keyer is an optional App extension: apps that implement it receive
// keyboard input (the looked-up string for each KeyPress — single
// characters, or names like "Return", "BackSpace", "Escape").
type Keyer interface {
	HandleKey(ctx Ctx, key string)
}

// Ctx is what handlers get: broker access plus a repaint trigger.
type Ctx struct {
	Broker *client.Client
	Post   func(func()) // run on the xapp loop
	Redraw func()       // repaint from current state (call after state changes)
	Emit   func(event string, data interface{})
	Print  func(segs ...apps.Seg) // print a line to the WM listener (via event bus)
}

// Run opens the window and blocks until the window is closed or ctx ends.
func Run(ctx context.Context, display, brokerSocket string, app App) error {
	X, err := connect(display)
	if err != nil {
		return err
	}
	defer X.Conn().Close()

	win, err := xwindow.Generate(X)
	if err != nil {
		return err
	}
	if err := win.CreateChecked(X.RootWin(), 0, 0, 640, 420,
		xproto.CwBackPixel|xproto.CwEventMask,
		0xf5f0e3,
		xproto.EventMaskExposure|xproto.EventMaskButtonPress|
			xproto.EventMaskStructureNotify|xproto.EventMaskPointerMotion|
			xproto.EventMaskKeyPress); err != nil {
		return err
	}
	_ = ewmh.WmNameSet(X, win.Id, app.Title())
	win.Map()

	a := &shell{X: X, win: win, app: app, ops: make(chan func(), 64)}

	// Broker (best effort — the app still renders without it).
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	cl, err := client.Connect(cctx, client.Options{Socket: brokerSocket, Name: app.Name()})
	cancel()
	if err == nil {
		a.broker = cl
		defer func() { _ = cl.Close() }()
		cl.OnAcceptMode(func(session string, ptypes []string, _ string) {
			a.post(func() { a.session, a.accepting = session, ptypes; a.redraw() })
		})
		cl.OnAcceptClear(func(session, _ string) {
			a.post(func() {
				if a.session == session {
					a.session, a.accepting = "", nil
					a.redraw()
				}
			})
		})
		cl.OnVerbRun(func(verbID string, obj *pbui.Object) {
			a.post(func() { app.HandleVerb(a.appCtx(), verbID, obj) })
		})
		rctx, rcancel := context.WithTimeout(ctx, 5*time.Second)
		_ = cl.RegisterVerbs(rctx, app.Verbs())
		rcancel()
	}

	// X events.
	xevent.ExposeFun(func(_ *xgbutil.XUtil, ev xevent.ExposeEvent) {
		if ev.Count == 0 {
			a.redraw()
		}
	}).Connect(X, win.Id)
	xevent.ConfigureNotifyFun(func(_ *xgbutil.XUtil, ev xevent.ConfigureNotifyEvent) {
		if int(ev.Width) != a.w || int(ev.Height) != a.h {
			a.w, a.h = int(ev.Width), int(ev.Height)
			a.redraw()
		}
	}).Connect(X, win.Id)
	xevent.ButtonPressFun(func(_ *xgbutil.XUtil, ev xevent.ButtonPressEvent) {
		a.click(int(ev.EventX), int(ev.EventY), int(ev.RootX), int(ev.RootY), int(ev.Detail))
	}).Connect(X, win.Id)
	if keyer, ok := app.(Keyer); ok {
		keybind.Initialize(X)
		xevent.KeyPressFun(func(_ *xgbutil.XUtil, ev xevent.KeyPressEvent) {
			s := keybind.LookupString(X, ev.State, ev.Detail)
			keyer.HandleKey(a.appCtx(), s)
		}).Connect(X, win.Id)
	}
	xevent.MotionNotifyFun(func(_ *xgbutil.XUtil, ev xevent.MotionNotifyEvent) {
		a.hover(int(ev.EventX), int(ev.EventY))
	}).Connect(X, win.Id)
	xevent.DestroyNotifyFun(func(_ *xgbutil.XUtil, _ xevent.DestroyNotifyEvent) {
		xevent.Quit(X)
	}).Connect(X, win.Id)

	a.w, a.h = 640, 420
	a.redraw()

	pingBefore, pingAfter, pingQuit := xevent.MainPing(X)
	for {
		select {
		case <-pingBefore:
			<-pingAfter
		case fn := <-a.ops:
			fn()
		case <-ctx.Done():
			xevent.Quit(X)
			return nil
		case <-pingQuit:
			return nil
		}
	}
}

func connect(display string) (*xgbutil.XUtil, error) {
	if display != "" {
		return xgbutil.NewConnDisplay(display)
	}
	return xgbutil.NewConn()
}

type shell struct {
	X         *xgbutil.XUtil
	win       *xwindow.Window
	app       App
	broker    *client.Client
	ops       chan func()
	w, h      int
	regions   []apps.Region
	accepting []string
	session   string
	lastDoc   string
}

func (a *shell) post(fn func()) {
	select {
	case a.ops <- fn:
	default:
		go func() { a.ops <- fn }()
	}
}

func (a *shell) appCtx() Ctx {
	return Ctx{
		Broker: a.broker,
		Post:   a.post,
		Redraw: a.redraw,
		Emit: func(event string, data interface{}) {
			if a.broker == nil {
				return
			}
			b := a.broker
			go func() { _ = b.Emit(context.Background(), event, data) }()
		},
		Print: func(segs ...apps.Seg) {
			if a.broker == nil {
				return
			}
			b := a.broker
			payload := map[string]interface{}{"segs": segsToWire(segs)}
			go func() { _ = b.Emit(context.Background(), "listener.print", payload) }()
		},
	}
}

func segsToWire(segs []apps.Seg) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(segs))
	for _, s := range segs {
		m := map[string]interface{}{}
		if s.Object != nil {
			m["ptype"] = s.Object.Ptype
			m["value"] = s.Object.StringValue()
		} else {
			m["text"] = s.Text
		}
		out = append(out, m)
	}
	return out
}

func (a *shell) redraw() {
	if a.w < 4 || a.h < 4 {
		return
	}
	img, regions := a.app.Render(a.w, a.h, a.accepting)
	a.regions = regions
	ximg := xgraphics.NewConvert(a.X, img)
	if err := ximg.XSurfaceSet(a.win.Id); err == nil {
		ximg.XDraw()
		ximg.XPaint(a.win.Id)
	}
	ximg.Destroy()
}

func (a *shell) click(x, y, rootX, rootY, button int) {
	r := apps.RegionAt(a.regions, x, y)
	c := apps.Resolve(a.accepting, r, button)
	switch {
	case c.Answer != nil && a.broker != nil:
		b, session, obj := a.broker, a.session, *c.Answer
		go func() { _ = b.Answer(context.Background(), session, obj) }()
	case c.Action != "":
		a.app.HandleAction(a.appCtx(), c.Action)
	case c.Menu != nil && a.broker != nil:
		b, obj := a.broker, *c.Menu
		go func() { _, _ = b.RequestMenu(context.Background(), obj, rootX, rootY) }()
	}
}

func (a *shell) hover(x, y int) {
	r := apps.RegionAt(a.regions, x, y)
	doc := ""
	if r != nil {
		doc = r.Doc
	}
	if doc != a.lastDoc && a.broker != nil {
		a.lastDoc = doc
		_ = a.broker.Hover(doc)
	}
	_ = draw.Ink // keep draw import for future cursor affordances
}
