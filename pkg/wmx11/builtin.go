package wmx11

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"strings"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil/xevent"
	"github.com/jezek/xgbutil/xwindow"

	"github.com/go-go-golems/go-go-wm/pkg/apps"
	"github.com/go-go-golems/go-go-wm/pkg/draw"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
)

// Builtin tiles: leaves whose App is "" (launcher) or "builtin:<name>" are
// rendered by the WM itself — trace, listener, and inspector are embedded
// views of the WM process's World, exactly like the prototype's singleton
// apps.

const builtinPrefix = "builtin:"

func isBuiltinLeaf(app string) bool {
	return app == "" || app == apps.AppLauncher ||
		strings.HasPrefix(app, builtinPrefix) || strings.HasPrefix(app, scriptPrefix)
}

// builtinName maps a leaf app to its renderer name. Script tiles keep
// their "script:" prefix so the paint path can branch on it.
func builtinName(app string) string {
	if app == "" || app == apps.AppLauncher {
		return apps.AppLauncher
	}
	if strings.HasPrefix(app, scriptPrefix) {
		return app
	}
	return strings.TrimPrefix(app, builtinPrefix)
}

// syncBuiltins reconciles builtin frames with the desktop: every builtin
// leaf gets a frame window; frames whose leaves vanished are destroyed.
func (w *WM) syncBuiltins() {
	// Destroy frames for leaves that no longer exist anywhere.
	for leaf, f := range w.frames {
		if w.desktop.FindLeafWorkspace(leaf) == nil {
			if f.client == 0 {
				delete(w.frames, leaf)
				delete(w.byFrame, f.win.Id)
				xevent.Detach(w.X, f.win.Id)
				f.win.Destroy()
			}
			continue
		}
		// Leaf exists: if it now hosts a client but still has a builtin
		// frame, the client frame replaced it already (manage destroys it).
	}
	for i := range w.desktop.Workspaces {
		ws := &w.desktop.Workspaces[i]
		for _, l := range ws.Root.Leaves() {
			if !isBuiltinLeaf(l.App) {
				continue
			}
			if _, ok := w.frames[l.ID]; ok {
				continue
			}
			w.openBuiltin(l.ID, builtinName(l.App))
		}
	}
}

// openBuiltin creates the frame window for a builtin tile.
func (w *WM) openBuiltin(leafID wmcore.NodeID, name string) {
	fw, err := xwindow.Generate(w.X)
	if err != nil {
		return
	}
	err = fw.CreateChecked(w.X.RootWin(), 0, 0, 100, 100,
		xproto.CwBackPixel|xproto.CwEventMask,
		uint32(pixel(draw.Pane)),
		xproto.EventMaskButtonPress|
			xproto.EventMaskButtonRelease|
			xproto.EventMaskPointerMotion|
			xproto.EventMaskExposure|
			xproto.EventMaskEnterWindow)
	if err != nil {
		return
	}
	title := apps.BuiltinTitle(name)
	if strings.HasPrefix(name, scriptPrefix) {
		title = strings.TrimPrefix(name, scriptPrefix) + " (js)"
	}
	f := &frame{leaf: leafID, client: 0, win: fw, title: title}
	w.frames[leafID] = f
	w.byFrame[fw.Id] = f
	w.connectFrameEvents(fw)
	fw.Map()
}

// builtinAppOf returns the builtin name for a frame, or "".
func (w *WM) builtinAppOf(f *frame) string {
	if f.client != 0 {
		return ""
	}
	ws := w.desktop.FindLeafWorkspace(f.leaf)
	if ws == nil {
		return apps.AppLauncher
	}
	l := ws.Root.FindLeaf(f.leaf)
	if l == nil || !isBuiltinLeaf(l.App) {
		return apps.AppLauncher
	}
	return builtinName(l.App)
}

// paintBuiltin renders a builtin tile's content (called from paintFrame).
func (w *WM) paintBuiltin(f *frame, img *image.RGBA) []apps.Region {
	name := w.builtinAppOf(f)
	cw := f.rect.W - 2*draw.BorderW
	ch := f.rect.H - draw.TitleH - draw.BorderW
	if cw < 4 || ch < 4 {
		return nil
	}
	var accepting []string
	if w.accepting != nil {
		accepting = w.accepting.ptypes
	}
	var content *image.RGBA
	var regions []apps.Region
	if strings.HasPrefix(name, scriptPrefix) {
		content, regions = w.renderScriptTile(name, cw, ch, accepting)
	} else {
		content, regions = apps.RenderBuiltin(name, cw, ch, w.world, accepting)
	}
	copyImage(img, content, draw.BorderW, draw.TitleH)
	// Shift regions into frame coordinates.
	for i := range regions {
		regions[i].Rect = regions[i].Rect.Add(image.Pt(draw.BorderW, draw.TitleH))
	}
	return regions
}

// builtinClick applies the click contract inside a builtin tile.
func (w *WM) builtinClick(f *frame, x, y, rootX, rootY, button int) {
	r := apps.RegionAt(f.regions, x, y)
	var accepting []string
	session := ""
	if w.accepting != nil {
		accepting = w.accepting.ptypes
		session = w.accepting.session
	}
	c := apps.Resolve(accepting, r, button)
	switch {
	case c.Answer != nil && w.broker != nil:
		b, obj := w.broker, *c.Answer
		go func() { _ = b.Answer(context.Background(), session, obj) }()
	case c.Action != "":
		w.builtinAction(f, c.Action)
	case c.Menu != nil && w.broker != nil:
		b, obj := w.broker, *c.Menu
		go func() { _, _ = b.RequestMenu(context.Background(), obj, rootX, rootY) }()
	}
}

// builtinAction runs launcher buttons and listener commands.
func (w *WM) builtinAction(f *frame, action string) {
	// Script tiles own their whole action namespace.
	if name := w.builtinAppOf(f); strings.HasPrefix(name, scriptPrefix) {
		w.scriptTileAction(name, action)
		return
	}
	switch {
	case strings.HasPrefix(action, "launch:"):
		name := strings.TrimPrefix(action, "launch:")
		_, _ = w.Apply(wmcore.Op{Op: wmcore.OpSetLeafApp, Node: f.leaf, App: builtinPrefix + name})
	case action == "cmd:describe":
		w.listenerCommand([]string{"any"}, "DESCRIBE — click any presentation anywhere (Esc cancels)",
			func(obj *pbui.Object) {
				w.inspect(*obj)
				w.world.Print(apps.Seg{Text: "described "}, apps.Seg{Object: obj}, apps.Seg{Text: " → see inspector"})
			})
	case action == "cmd:sum":
		w.sumCommand()
	case action == "cmd:pick-color":
		w.listenerCommand([]string{"color"}, "PICK — click a COLOR swatch anywhere (Esc cancels)",
			func(obj *pbui.Object) {
				w.world.Print(apps.Seg{Text: "picked "}, apps.Seg{Object: obj})
			})
	}
}

// listenerCommand runs a single-accept listener command off-loop.
func (w *WM) listenerCommand(ptypes []string, prompt string, done func(*pbui.Object)) {
	if w.broker == nil {
		return
	}
	b := w.broker
	go func() {
		obj, err := b.Accept(context.Background(), ptypes, prompt)
		if err != nil || obj == nil {
			return
		}
		w.Post(func() {
			done(obj)
			w.repaintBuiltins()
		})
	}()
}

// sumCommand ports the listener's Sum… (pbui-shell.jsx:389-397): two
// sequential accepts, result printed as a live <number>.
func (w *WM) sumCommand() {
	if w.broker == nil {
		return
	}
	b := w.broker
	go func() {
		a, err := b.Accept(context.Background(), []string{"number"}, "SUM — click a NUMBER (1 of 2)")
		if err != nil || a == nil {
			return
		}
		bb, err := b.Accept(context.Background(), []string{"number"}, "SUM — click a NUMBER (2 of 2)")
		if err != nil || bb == nil {
			return
		}
		var x, y float64
		_ = json.Unmarshal(a.Value, &x)
		_ = json.Unmarshal(bb.Value, &y)
		sum := x + y
		res, _ := pbui.NewObject("number", sum)
		w.Post(func() {
			w.world.Print(
				apps.Seg{Text: fmt.Sprintf("%v + %v = ", trimFloat(x), trimFloat(y))},
				apps.Seg{Object: &res})
			w.repaintBuiltins()
		})
	}()
}

func trimFloat(f float64) interface{} {
	if f == float64(int64(f)) {
		return int64(f)
	}
	return f
}

// inspect sets the inspector state (the WM's "Inspect" verb, plus the
// listener's Describe…).
func (w *WM) inspect(obj pbui.Object) {
	desc := describeObject(obj)
	raw, _ := json.MarshalIndent(desc, "", "  ")
	w.world.Inspected = apps.Inspection{
		Title: "<" + obj.Ptype + "> " + obj.StringValue(),
		JSON:  string(raw),
	}
	w.repaintBuiltins()
}

// describeObject ports describe() (pbui-shell.jsx:698-709).
func describeObject(obj pbui.Object) map[string]interface{} {
	out := map[string]interface{}{"presentationType": obj.Ptype}
	s := obj.StringValue()
	switch obj.Ptype {
	case "color":
		out["hex"] = s
		if len(s) == 7 && s[0] == '#' {
			var r, g, b int
			_, _ = fmt.Sscanf(s[1:], "%02x%02x%02x", &r, &g, &b)
			out["rgb"] = []int{r, g, b}
			out["luminance"] = float64(int(float64(r+g+b)/7.65)) / 100
		}
	case "number":
		var n float64
		_ = json.Unmarshal(obj.Value, &n)
		out["value"] = n
		if n == float64(int(n)) && n >= 2 {
			out["prime"] = isPrimeInt(int(n))
			out["factors"] = factorize(int(n))
		}
	default:
		out["value"] = s
	}
	return out
}

func isPrimeInt(n int) bool {
	if n < 2 {
		return false
	}
	for i := 2; i*i <= n; i++ {
		if n%i == 0 {
			return false
		}
	}
	return true
}

func factorize(n int) []int {
	var f []int
	m := n
	for d := 2; d*d <= m; d++ {
		for m%d == 0 {
			f = append(f, d)
			m /= d
		}
	}
	if m > 1 {
		f = append(f, m)
	}
	return f
}

// repaintBuiltins repaints all visible builtin tiles (world changed).
func (w *WM) repaintBuiltins() {
	for _, f := range w.frames {
		if f.client == 0 && f.rect.W > 0 {
			w.paintFrame(f)
		}
	}
}

// watchEvents subscribes to the broker bus and feeds the trace + listener.
func (w *WM) watchEvents() {
	if w.broker == nil {
		return
	}
	b := w.broker
	go func() {
		events, err := b.Events(context.Background())
		if err != nil {
			return
		}
		for ev := range events {
			seq, typ, src := ev.EventSeq, ev.Event, ev.Source
			data := ev.Data
			w.Post(func() {
				w.world.AddTrace(apps.TraceEvent{
					Seq: seq, Type: typ, Source: src, Data: compactData(data),
				})
				if typ == "listener.print" {
					w.world.Print(parsePrintSegs(data)...)
				}
				w.repaintBuiltins()
			})
		}
	}()
}

func compactData(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return string(raw)
	}
	parts := make([]string, 0, len(m))
	for k, v := range m {
		s := fmt.Sprintf("%v", v)
		if s == "" || s == "map[]" {
			continue
		}
		if len(s) > 24 {
			s = s[:24] + "…"
		}
		parts = append(parts, k+"="+s)
	}
	return strings.Join(parts, " ")
}

// parsePrintSegs decodes a listener.print event payload into segments.
func parsePrintSegs(raw []byte) []apps.Seg {
	var payload struct {
		Segs []struct {
			Text  string      `json:"text"`
			Ptype string      `json:"ptype"`
			Value interface{} `json:"value"`
		} `json:"segs"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return []apps.Seg{{Text: string(raw)}}
	}
	var out []apps.Seg
	for _, s := range payload.Segs {
		if s.Ptype != "" {
			obj, err := pbui.NewObject(s.Ptype, s.Value)
			if err == nil {
				out = append(out, apps.Seg{Object: &obj})
				continue
			}
		}
		out = append(out, apps.Seg{Text: s.Text})
	}
	return out
}
