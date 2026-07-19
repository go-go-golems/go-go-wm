package apps

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"github.com/go-go-golems/go-go-wm/pkg/draw"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
)

// World is the shared state behind the WM-embedded apps: the Go form of the
// prototype's World singleton (pbui-shell.jsx:95-123). It lives in the WM
// process; all builtin tiles are live views of it.
type World struct {
	Trace     []TraceEvent
	Listener  []Line
	Inspected Inspection
}

type TraceEvent struct {
	Seq    uint64
	Type   string
	Data   string
	Source string
}

// Line is one listener transcript line: a sequence of text and live
// presentation segments.
type Line struct {
	Segs []Seg
}

type Seg struct {
	Text   string
	Object *pbui.Object
}

type Inspection struct {
	Title string
	JSON  string
}

// NewWorld seeds the listener like the prototype does.
func NewWorld() *World {
	return &World{
		Listener: []Line{{Segs: []Seg{{Text: "PBUI listener. Commands here accept objects from ANY tile."}}}},
		Inspected: Inspection{
			Title: "about this shell",
			JSON:  "{\n  \"try\": \"click anything — or open about/help\"\n}",
		},
	}
}

// Print appends a transcript line.
func (w *World) Print(segs ...Seg) {
	w.Listener = append(w.Listener, Line{Segs: segs})
	if len(w.Listener) > 500 {
		w.Listener = w.Listener[len(w.Listener)-500:]
	}
}

// AddTrace appends an event (the bus feed).
func (w *World) AddTrace(ev TraceEvent) {
	w.Trace = append(w.Trace, ev)
	if len(w.Trace) > 1000 {
		w.Trace = w.Trace[len(w.Trace)-1000:]
	}
}

// Builtin app names (leaf.App = "builtin:<name>").
const (
	AppLauncher  = "launcher"
	AppAbout     = "about"
	AppTrace     = "trace"
	AppListener  = "listener"
	AppInspector = "inspector"
)

// BuiltinTitle / BuiltinColor give the title-strip face for a builtin.
func BuiltinTitle(name string) string {
	switch name {
	case AppLauncher:
		return "new tile"
	case AppAbout:
		return "about / help"
	case AppTrace:
		return "trace"
	case AppListener:
		return "listener"
	case AppInspector:
		return "inspector"
	}
	return name
}

func BuiltinColor(name string) color.RGBA {
	switch name {
	case AppLauncher:
		return draw.PaneAlt
	case AppAbout:
		return draw.Sel
	case AppTrace:
		return draw.Sage
	case AppListener:
		return draw.Mint
	case AppInspector:
		return draw.Lavender
	}
	return draw.Blue
}

// RenderBuiltin dispatches to the app renderers. accepting is the pending
// accept's ptype list (nil when idle) so presentations can highlight.
func RenderBuiltin(name string, w, h int, world *World, accepting []string) (*image.RGBA, []Region) {
	switch name {
	case AppAbout:
		return renderAbout(w, h)
	case AppTrace:
		return renderTrace(w, h, world, accepting)
	case AppListener:
		return renderListener(w, h, world, accepting)
	case AppInspector:
		return renderInspector(w, h, world)
	default:
		return renderLauncher(w, h)
	}
}

func renderLauncher(w, h int) (*image.RGBA, []Region) {
	img := NewSurface(w, h)
	var regions []Region
	y := Hint(img, 8, 18, "empty tile — pick a built-in app (X clients land in empty tiles too):")
	x := 8
	y += 6
	for _, name := range []string{AppAbout, AppTrace, AppListener, AppInspector} {
		r := Btn(img, x, y, BuiltinTitle(name), BuiltinColor(name))
		regions = append(regions, Region{
			Rect: r, Action: "launch:" + name,
			Doc: "open the " + BuiltinTitle(name) + " app in this tile",
		})
		x = r.Max.X + 8
		if x > w-120 {
			x = 8
			y += 30
		}
	}
	y += 40
	Hint(img, 8, y, "or Mod4-Return to spawn a terminal here")
	return img, regions
}

func renderAbout(w, h int) (*image.RGBA, []Region) {
	img := NewSurface(w, h)
	sec := func(y int, title string, lines ...string) int {
		tw := draw.TextWidth(title, true, 10.5) + 12
		draw.Fill(img, image.Rect(8, y-12, 8+tw, y+4), draw.Sel)
		draw.Border(img, image.Rect(8, y-12, 8+tw, y+4), 1, draw.Ink)
		draw.Text(img, 14, y, strings.ToUpper(title), true, 10.5, draw.Ink)
		y += 18
		for _, l := range lines {
			draw.Text(img, 10, y, l, false, 11, draw.Ink)
			y += 15
		}
		return y + 8
	}
	y := 22
	y = sec(y, "what this is",
		"A shell for presentation-based UIs (CLIM / Genera lineage).",
		"Every visible object is a typed presentation; commands read",
		"objects via the accept protocol, across ALL tiles.")
	y = sec(y, "things to try",
		"· listener: Sum… then click two <number> chips anywhere",
		"· right-click a color swatch → Mix with… → click another",
		"· drag a divider — it sticks at 1/4 1/3 1/2 2/3 3/4",
		"· drag ⠿ onto a tile: center swaps, edge split-docks",
		"· run: go-go-wm demo colors | numbers | notes",
		"· in kitty: git log | go-go-wm scrape")
	_ = sec(y, "writing an app",
		"Render objects as regions, speak accept via the broker,",
		"register verbs. See pkg/apps and go-go-wm demo sources.")
	return img, nil
}

var traceTone = map[string]color.RGBA{
	"accept.started": draw.Mustard, "accept.answered": draw.Mustard, "accept.cleared": draw.Rose,
	"split-leaf": draw.Lavender, "close-leaf": draw.Lavender, "swap-leaves": draw.Lavender,
	"move-split": draw.Lavender, "set-ratio": draw.Lavender,
	"add-workspace": draw.Mint, "remove-workspace": draw.Rose, "clone-workspace": draw.Mint,
	"listener.print": draw.Blue, "verb.invoked": draw.Blue,
	"window.managed": draw.Sage, "close_tile": draw.Rose,
}

func renderTrace(w, h int, world *World, accepting []string) (*image.RGBA, []Region) {
	img := NewSurface(w, h)
	var regions []Region
	rowH := 18
	capRows := (h - 10) / rowH
	evs := world.Trace
	if len(evs) > capRows {
		evs = evs[len(evs)-capRows:]
	}
	y := 6
	for _, ev := range evs {
		seq := fmt.Sprintf("%3d", ev.Seq)
		draw.Text(img, 6, y+13, seq, false, 10, draw.Faint)
		obj, _ := pbui.NewObject("event", fmt.Sprintf("%d", ev.Seq))
		obj.Label = fmt.Sprintf("#%d %s", ev.Seq, ev.Type)
		tone, ok := traceTone[ev.Type]
		if !ok {
			tone = draw.PaneAlt
		}
		tw := draw.TextWidth(ev.Type, true, 9.5) + 10
		chipR := image.Rect(34, y+2, 34+tw, y+16)
		bg := tone
		if len(accepting) > 0 && pbui.TypeMatches(accepting, "event") {
			bg = draw.Sel
		}
		draw.Fill(img, chipR, bg)
		draw.Border(img, chipR, 1, draw.Ink)
		draw.Text(img, 39, y+13, ev.Type, true, 9.5, draw.Ink)
		regions = append(regions, Region{Rect: chipR, Object: &obj,
			Doc: "event #" + seq + " " + ev.Type + " — L/R: menu"})
		data := ev.Data
		if len(data) > 60 {
			data = data[:60] + "…"
		}
		draw.Text(img, 40+tw, y+13, data, false, 10, draw.Ink)
		y += rowH
	}
	if len(evs) == 0 {
		Hint(img, 8, 18, "nothing yet — do something")
	}
	return img, regions
}

func renderListener(w, h int, world *World, accepting []string) (*image.RGBA, []Region) {
	img := NewSurface(w, h)
	var regions []Region
	x := 8
	for _, b := range []struct {
		label, action, doc string
		tone               color.RGBA
	}{
		{"Describe…", "cmd:describe", "DESCRIBE — accepts ANY presentation, shows it in the inspector", draw.Blue},
		{"Sum…", "cmd:sum", "SUM — accepts two NUMBERs (from any tile) and prints the sum", draw.Sage},
		{"Pick color…", "cmd:pick-color", "PICK — accepts a COLOR and prints its luminance", draw.Rose},
	} {
		r := Btn(img, x, 8, b.label, b.tone)
		regions = append(regions, Region{Rect: r, Action: b.action, Doc: b.doc})
		x = r.Max.X + 8
	}
	y := 44
	rowH := 20
	maxRows := (h - y - 6) / rowH
	lines := world.Listener
	if len(lines) > maxRows {
		lines = lines[len(lines)-maxRows:]
	}
	for _, line := range lines {
		lx := 8
		for _, seg := range line.Segs {
			if seg.Object != nil {
				hl := len(accepting) > 0 && pbui.TypeMatches(accepting, seg.Object.Ptype)
				r := Chip(img, lx, y, *seg.Object, hl)
				doc := "<" + seg.Object.Ptype + "> " + seg.Object.StringValue()
				regions = append(regions, Region{Rect: r, Object: seg.Object, Doc: doc})
				lx = r.Max.X + 5
			} else {
				lx += draw.Text(img, lx, y+13, seg.Text, false, 11, draw.Ink)
			}
		}
		y += rowH
	}
	return img, regions
}

func renderInspector(w, h int, world *World) (*image.RGBA, []Region) {
	img := NewSurface(w, h)
	draw.Text(img, 8, 18, world.Inspected.Title, true, 11.5, draw.Ink)
	for x := 8; x < w-8; x += 4 {
		draw.Fill(img, image.Rect(x, 24, x+2, 25), draw.Faint)
	}
	y := 40
	for _, line := range strings.Split(world.Inspected.JSON, "\n") {
		if y > h-10 {
			break
		}
		draw.Text(img, 10, y, line, false, 10.5, draw.Ink)
		y += 14
	}
	return img, nil
}
