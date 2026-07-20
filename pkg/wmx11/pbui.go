package wmx11

import (
	"context"
	"strings"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil/xwindow"

	"github.com/go-go-golems/go-go-wm/pkg/draw"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/client"
	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
)

// connectBroker joins the PBUI protocol as the privileged "wm" client:
// it renders the ACCEPTING banner and object menus, feeds the mouse-doc
// line, and registers the tile/workspace verbs.
func (w *WM) connectBroker() {
	ctx, cancel := context.WithTimeout(w.ctx, brokerDialTimeout)
	defer cancel()
	cl, err := client.Connect(ctx, client.Options{
		Socket: w.cfg.BrokerSocket,
		Name:   "wm",
		Roles:  []string{"wm"},
	})
	if err != nil {
		log.Warn().Err(err).Msg("broker not reachable; running without presentations")
		return
	}
	w.broker = cl

	cl.OnAcceptMode(func(session string, ptypes []string, prompt string) {
		w.Post(func() {
			w.accepting = &acceptState{session: session, ptypes: ptypes, prompt: prompt}
			w.paintBars()
			w.repaintAllFrames()
		})
	})
	cl.OnAcceptClear(func(session, reason string) {
		w.Post(func() {
			if w.accepting != nil && w.accepting.session == session {
				w.accepting = nil
				w.paintBars()
				w.repaintAllFrames()
			}
		})
	})
	cl.OnMenuShow(func(obj pbui.Object, verbs []pbui.Verb, x, y int) {
		w.Post(func() { w.showMenu(obj, verbs, x, y) })
	})
	cl.OnDocHover(func(text, _ string) {
		w.Post(func() { w.setMouseDoc(text) })
	})
	cl.OnVerbRun(func(verbID string, obj *pbui.Object) {
		w.Post(func() { w.runVerb(verbID, obj) })
	})

	go func() {
		regCtx, regCancel := context.WithTimeout(w.ctx, brokerDialTimeout)
		defer regCancel()
		_ = cl.RegisterVerbs(regCtx, append(w.tileVerbs(), commandVerbs()...))
	}()
	w.watchEvents()
}

const brokerDialTimeout = 5e9 // 5s

// tileVerbs is the WM's contribution to the action table (ports the
// tile/workspace arms of actionsFor, pbui-shell.jsx:739-755).
func (w *WM) tileVerbs() []pbui.Verb {
	return []pbui.Verb{
		{ID: "any.inspect", Label: "Inspect", Ptypes: []string{"any"}},
		{ID: "tile.split-right", Label: "Split - new tile right", Ptypes: []string{"tile"}},
		{ID: "tile.split-down", Label: "Split - new tile below", Ptypes: []string{"tile"}},
		{ID: "tile.swap-with", Label: "Swap app with...  (accept a tile)", Ptypes: []string{"tile"}, Accepts: []string{"tile"}},
		{ID: "tile.close", Label: "Close tile", Ptypes: []string{"tile"}},
		{ID: "workspace.switch", Label: "Switch to", Ptypes: []string{"workspace"}},
		{ID: "workspace.duplicate", Label: "Duplicate", Ptypes: []string{"workspace"}},
		{ID: "workspace.delete", Label: "Delete", Ptypes: []string{"workspace"}},
	}
}

// runVerb executes a WM-owned verb (already on the WM loop).
func (w *WM) runVerb(verbID string, obj *pbui.Object) {
	if obj == nil {
		return
	}
	if strings.HasPrefix(verbID, "command.") {
		w.runCommandVerb(verbID, obj)
		return
	}
	leaf := wmcore.NodeID(obj.StringValue())
	switch verbID {
	case "any.inspect":
		w.inspect(*obj)
	case "tile.split-right":
		_, _ = w.Apply(wmcore.Op{Op: wmcore.OpSplitLeaf, Node: leaf, Dir: wmcore.Row, App: ""})
	case "tile.split-down":
		_, _ = w.Apply(wmcore.Op{Op: wmcore.OpSplitLeaf, Node: leaf, Dir: wmcore.Col, App: ""})
	case "tile.close":
		if f := w.frames[leaf]; f != nil {
			w.closeClient(f)
		} else {
			_, _ = w.Apply(wmcore.Op{Op: wmcore.OpCloseLeaf, Node: leaf})
		}
	case "tile.swap-with":
		// A verb that itself accepts: the composability point.
		b := w.broker
		if b == nil {
			return
		}
		go func() {
			other, err := b.Accept(context.Background(), []string{"tile"}, "SWAP — click another TILE's title (Esc cancels)")
			if err != nil || other == nil {
				return
			}
			w.Post(func() { w.swapFrames(leaf, wmcore.NodeID(other.StringValue())) })
		}()
	case "workspace.switch":
		_, _ = w.Apply(wmcore.Op{Op: wmcore.OpSwitchWorkspace, Workspace: obj.StringValue()})
	case "workspace.duplicate":
		_, _ = w.Apply(wmcore.Op{Op: wmcore.OpCloneWorkspace, Workspace: obj.StringValue()})
	case "workspace.delete":
		_, _ = w.Apply(wmcore.Op{Op: wmcore.OpRemoveWorkspace, Workspace: obj.StringValue()})
	}
}

// cancelAccept is the Escape handler. Escape is a global grab, so an
// open launcher popup never sees the KeyPress itself — close it here.
func (w *WM) cancelAccept() {
	if w.launcher != nil {
		w.closeLauncher()
		return
	}
	if w.menu != nil {
		w.closeMenu()
		return
	}
	if w.accepting == nil || w.broker == nil {
		// No modal state: Escape clears the focused launcher tile's query.
		if st := w.launcherTiles[w.focused]; st != nil && st.query != "" {
			st.query, st.sel = "", 0
			if f := w.frames[w.focused]; f != nil {
				w.paintFrame(f)
			}
		}
		return
	}
	session := w.accepting.session
	b := w.broker
	go func() { _ = b.Cancel(context.Background(), session) }()
}

func (w *WM) repaintAllFrames() {
	for _, f := range w.frames {
		if f.rect.W > 0 {
			w.paintFrame(f)
		}
	}
	for _, f := range w.floats {
		if f.ws == w.desktop.Current {
			w.paintFrame(f)
		}
	}
}

// --- menu rendering ---------------------------------------------------------

func (w *WM) showMenu(obj pbui.Object, verbs []pbui.Verb, x, y int) {
	w.closeMenu()
	items := make([]string, len(verbs))
	for i, v := range verbs {
		items[i] = v.Label
	}
	if len(items) == 0 {
		items = []string{"(no verbs registered)"}
	}
	label := obj.Label
	if label == "" {
		label = obj.StringValue()
	}
	model := draw.Menu{Header: "<" + obj.Ptype + "> " + label, Items: items, Hover: -1}
	mw, mh := model.MenuSize()
	if x+mw > w.screen.W {
		x = w.screen.W - mw
	}
	if y+mh > w.screen.H {
		y = w.screen.H - mh
	}

	win, err := xwindow.Generate(w.X)
	if err != nil {
		return
	}
	err = win.CreateChecked(w.X.RootWin(), x, y, mw, mh,
		xproto.CwBackPixel|xproto.CwOverrideRedirect|xproto.CwEventMask,
		uint32(pixel(draw.Current().Pane)), 1,
		xproto.EventMaskButtonPress|xproto.EventMaskPointerMotion|
			xproto.EventMaskLeaveWindow|xproto.EventMaskExposure)
	if err != nil {
		return
	}
	win.Map()
	win.Stack(xproto.StackModeAbove)
	w.menu = &menuState{obj: obj, verbs: verbs, win: win, model: model, hover: -1, x: x, y: y}
	w.connectMenuEvents(win)
	w.blit(win, model.Render())
}

func (w *WM) closeMenu() {
	if w.menu == nil {
		return
	}
	w.menu.win.Destroy()
	w.menu = nil
}

// menuMotion / menuPress are called from the frame-level event router when
// the event window is the menu window.
func (w *WM) menuMotion(x, y int) {
	m := w.menu
	if m == nil {
		return
	}
	hover := m.model.MenuItemAt(x, y)
	if hover != m.hover {
		m.hover = hover
		m.model.Hover = hover
		w.blit(m.win, m.model.Render())
	}
}

func (w *WM) menuPress(x, y int) {
	m := w.menu
	if m == nil {
		return
	}
	i := m.model.MenuItemAt(x, y)
	w.closeMenu()
	if i < 0 || i >= len(m.verbs) {
		return
	}
	v := m.verbs[i]
	if v.Owner == "wm" {
		w.runVerb(v.ID, &m.obj)
		return
	}
	if w.broker != nil {
		b := w.broker
		obj := m.obj
		go func() { _ = b.InvokeVerb(context.Background(), v.ID, obj) }()
	}
}
