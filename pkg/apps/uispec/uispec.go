// Package uispec is the declarative surface IR of the ui scripting
// module (GGWM-003): scripts describe rows of segments, Normalize
// validates them at definition time, and Render turns a normalized spec
// into pixels plus the apps.Region list that gives script apps the same
// click contract as every built-in app.
//
// This package is pure — no goja, no X, no I/O — which is what makes it
// the testable core of the ui module.
package uispec

import (
	"fmt"
	"image"
	"image/color"
	stddraw "image/draw"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-go-golems/go-go-wm/pkg/apps"
	"github.com/go-go-golems/go-go-wm/pkg/draw"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
)

// Seg kinds.
const (
	KindText   = "text"
	KindObject = "object"
	KindButton = "button"
	KindHint   = "hint"

	// GGWM-009 extensions. table and field may come from JS (__pbui__
	// views); image is Go-side only (R-D2: JS supplies data, Go renders
	// pixels) and Normalize rejects it.
	KindTable = "table"
	KindImage = "image"
	KindField = "field"
)

// Seg is one inline element of a row.
type Seg struct {
	Kind string `json:"kind"`

	// text / hint / field (field: Text is the editable content)
	Text string  `json:"text,omitempty"`
	Bold bool    `json:"bold,omitempty"`
	Size float64 `json:"size,omitempty"` // text only; default 11

	// object → Region.Object (the click contract does the rest)
	Ptype string      `json:"ptype,omitempty"`
	Value interface{} `json:"value,omitempty"`
	Label string      `json:"label,omitempty"`
	Doc   string      `json:"doc,omitempty"`

	// button → Region.Action; field → the submit action name
	Action string `json:"action,omitempty"`
	Color  string `json:"color,omitempty"` // named tone: rose|blue|mint|mustard|lavender|sage

	// table (a block segment: rendered full-width on its own line).
	// Cells whose text looks like #rrggbb render as live color chips.
	Columns []string   `json:"columns,omitempty"`
	Cells   [][]string `json:"cells,omitempty"`
	More    int        `json:"more,omitempty"` // "… N more rows"

	// image (block): a pre-rendered strip (sparklines, bar plots) that
	// travels through the normal snapshot path. Never from JS.
	Img *image.RGBA `json:"-"`

	// field: render the block caret (the host tracks focus).
	Focus bool `json:"focus,omitempty"`
}

// Row is one horizontal band of segments (wrapped if too wide).
type Row []Seg

// Spec is a whole surface.
type Spec []Row

// tone resolves a named accent at paint time so theme swaps
// (draw.SetTheme) reach already-normalized specs.
func tone(name string) (color.RGBA, bool) {
	switch name {
	case "rose":
		return draw.Rose, true
	case "blue":
		return draw.Blue, true
	case "mint":
		return draw.Mint, true
	case "mustard":
		return draw.Mustard, true
	case "lavender":
		return draw.Lavender, true
	case "sage":
		return draw.Sage, true
	}
	return color.RGBA{}, false
}

// Normalize validates raw JS-shaped rows ([]interface{} of []interface{}
// of map[string]interface{}) into a Spec. All errors are definition-time
// errors: they carry enough context to fix the render() that produced
// them, and nothing is drawn from an invalid spec.
func Normalize(raw interface{}) (Spec, error) {
	rows, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("render() must return an array of rows, got %T", raw)
	}
	spec := make(Spec, 0, len(rows))
	for i, r := range rows {
		segs, ok := r.([]interface{})
		if !ok {
			return nil, fmt.Errorf("row %d: must be ui.row(...), got %T", i, r)
		}
		row := make(Row, 0, len(segs))
		for j, s := range segs {
			seg, err := normalizeSeg(s)
			if err != nil {
				return nil, fmt.Errorf("row %d seg %d: %w", i, j, err)
			}
			row = append(row, seg)
		}
		spec = append(spec, row)
	}
	return spec, nil
}

func normalizeSeg(v interface{}) (Seg, error) {
	m, ok := v.(map[string]interface{})
	if !ok {
		return Seg{}, fmt.Errorf("segment must be a ui.text/object/button/hint value, got %T", v)
	}
	kind, _ := m["kind"].(string)
	seg := Seg{Kind: kind}
	switch kind {
	case KindText, KindHint:
		seg.Text, _ = m["text"].(string)
		seg.Bold, _ = m["bold"].(bool)
		if f, ok := toFloat(m["size"]); ok {
			if f < 6 || f > 48 {
				return Seg{}, fmt.Errorf("text size %v out of range [6,48]", f)
			}
			seg.Size = f
		}
	case KindObject:
		seg.Ptype, _ = m["ptype"].(string)
		if !pbui.ValidPtype(seg.Ptype) {
			return Seg{}, fmt.Errorf("object ptype %q is not a valid slug", seg.Ptype)
		}
		seg.Value = m["value"]
		seg.Label, _ = m["label"].(string)
		seg.Doc, _ = m["doc"].(string)
	case KindButton:
		seg.Text, _ = m["text"].(string)
		seg.Action, _ = m["action"].(string)
		if seg.Text == "" || seg.Action == "" {
			return Seg{}, fmt.Errorf("button needs a label and an action name")
		}
		seg.Doc, _ = m["doc"].(string)
		if c, ok := m["color"].(string); ok && c != "" {
			if _, known := tone(c); !known {
				return Seg{}, fmt.Errorf("button color %q unknown (rose|blue|mint|mustard|lavender|sage)", c)
			}
			seg.Color = c
		}
	case KindTable:
		cols, ok := toStrings(m["columns"])
		if !ok || len(cols) == 0 {
			return Seg{}, fmt.Errorf("table needs a columns array of strings")
		}
		seg.Columns = cols
		rows, ok := m["cells"].([]interface{})
		if !ok {
			return Seg{}, fmt.Errorf("table needs a cells array of rows")
		}
		for i, r := range rows {
			cells, ok := toStrings(r)
			if !ok {
				return Seg{}, fmt.Errorf("table row %d: must be an array of strings", i)
			}
			if len(cells) != len(cols) {
				return Seg{}, fmt.Errorf("table row %d: %d cells for %d columns", i, len(cells), len(cols))
			}
			seg.Cells = append(seg.Cells, cells)
		}
		if f, ok := toFloat(m["more"]); ok {
			seg.More = int(f)
		}
	case KindField:
		seg.Text, _ = m["text"].(string)
		seg.Action, _ = m["action"].(string)
		seg.Focus, _ = m["focus"].(bool)
		seg.Doc, _ = m["doc"].(string)
	case KindImage:
		return Seg{}, fmt.Errorf("image segments are Go-side only (render data, not pixels, from JS)")
	default:
		return Seg{}, fmt.Errorf("unknown segment kind %q", kind)
	}
	for k := range m {
		switch k {
		case "kind", "text", "bold", "size", "ptype", "value", "label", "doc", "action", "color",
			"columns", "cells", "more", "focus":
		default:
			return Seg{}, fmt.Errorf("unknown segment key %q", k)
		}
	}
	return seg, nil
}

// toStrings converts a JS-shaped array into strings, stringifying
// numbers and booleans (table cells commonly hold numerics).
func toStrings(v interface{}) ([]string, bool) {
	arr, ok := v.([]interface{})
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		switch t := e.(type) {
		case string:
			out = append(out, t)
		case float64:
			out = append(out, trimNum(t))
		case int64:
			out = append(out, fmt.Sprintf("%d", t))
		case bool:
			out = append(out, fmt.Sprintf("%v", t))
		case nil:
			out = append(out, "")
		default:
			return nil, false
		}
	}
	return out, true
}

func trimNum(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%g", f)
}

func toFloat(v interface{}) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int64:
		return float64(t), true
	}
	return 0, false
}

// Render draws a normalized spec: rows top-to-bottom, segments packed
// left-to-right with wrapping, objects and buttons emitting Regions.
// Objects matching an active accept get the standard highlight.
func Render(w, h int, spec Spec, accepting []string) (*image.RGBA, []apps.Region) {
	img := apps.NewSurface(w, h)
	var regions []apps.Region
	const margin = 8
	y := margin
	for _, row := range spec {
		x := margin
		rowH := 0
		place := func(need int) {
			if x > margin && x+need > w-margin {
				x = margin
				y += rowH + 6
				rowH = 0
			}
		}
		for _, seg := range row {
			switch seg.Kind {
			case KindText:
				size := seg.Size
				if size == 0 {
					size = 11
				}
				tw := draw.TextWidth(seg.Text, seg.Bold, size)
				place(tw)
				draw.Text(img, x, y+int(size)+1, seg.Text, seg.Bold, size, draw.Ink)
				x += tw + 8
				rowH = maxInt(rowH, int(size)+6)
			case KindHint:
				tw := draw.TextWidth(seg.Text, false, 10.5)
				place(tw)
				draw.Text(img, x, y+12, seg.Text, false, 10.5, draw.Faint)
				x += tw + 8
				rowH = maxInt(rowH, 14)
			case KindObject:
				obj, err := pbui.NewObject(seg.Ptype, seg.Value)
				if err != nil {
					continue // Normalize validated the ptype; value marshal cannot fail for exported JS data
				}
				obj.Label, obj.Doc = seg.Label, seg.Doc
				hl := len(accepting) > 0 && pbui.TypeMatches(accepting, obj.Ptype)
				// Rough pre-measure (Chip computes its own width).
				label := seg.Label
				if label == "" {
					label = obj.StringValue()
				}
				estimate := draw.TextWidth(label, false, 11) + 30
				place(estimate)
				r := apps.Chip(img, x, y, obj, hl)
				doc := seg.Doc
				if doc == "" {
					doc = obj.Ptype + " " + obj.StringValue()
				}
				regions = append(regions, apps.Region{Rect: r, Object: &obj, Doc: doc})
				x = r.Max.X + 8
				rowH = maxInt(rowH, r.Dy()+4)
			case KindButton:
				btnTone := draw.Mustard
				if seg.Color != "" {
					if t, ok := tone(seg.Color); ok {
						btnTone = t
					}
				}
				estimate := draw.TextWidth(seg.Text, true, 11) + 26
				place(estimate)
				r := apps.Btn(img, x, y, seg.Text, btnTone)
				doc := seg.Doc
				if doc == "" {
					doc = seg.Text
				}
				regions = append(regions, apps.Region{Rect: r, Action: "cmd:" + seg.Action, Doc: doc})
				x = r.Max.X + 8
				rowH = maxInt(rowH, r.Dy()+6)
			case KindTable:
				if x > margin {
					x = margin
					y += rowH + 6
					rowH = 0
				}
				th, regs := renderTable(img, seg, margin, y, w-2*margin, accepting)
				regions = append(regions, regs...)
				rowH = maxInt(rowH, th)
				x = w // block: nothing else on this line
			case KindImage:
				if seg.Img == nil {
					continue
				}
				b := seg.Img.Bounds()
				if x > margin && x+b.Dx() > w-margin {
					x = margin
					y += rowH + 6
					rowH = 0
				}
				stddraw.Draw(img, image.Rect(x, y, x+b.Dx(), y+b.Dy()), seg.Img, b.Min, stddraw.Src)
				x += b.Dx() + 8
				rowH = maxInt(rowH, b.Dy())
			case KindField:
				if x > margin {
					x = margin
					y += rowH + 6
					rowH = 0
				}
				fw := w - x - margin
				fr := image.Rect(x, y, x+fw, y+26)
				draw.Fill(img, fr, draw.Field)
				draw.Border(img, fr, 1, draw.Ink)
				tx := fr.Min.X + 8
				tx += draw.Text(img, tx, fr.Min.Y+18, seg.Text, true, 12, draw.Ink)
				if seg.Focus {
					draw.Fill(img, image.Rect(tx+2, fr.Min.Y+6, tx+9, fr.Max.Y-6), draw.Sel)
				}
				if seg.Action != "" {
					doc := seg.Doc
					if doc == "" {
						doc = "input"
					}
					regions = append(regions, apps.Region{Rect: fr, Action: "field:" + seg.Action, Doc: doc})
				}
				x = w
				rowH = maxInt(rowH, 26)
			}
		}
		if rowH == 0 {
			rowH = 8 // empty row = spacer
		}
		y += rowH + 6
		if y > h {
			break
		}
	}
	return img, regions
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var colorCellRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// renderTable draws a table block segment: bold headers over a rule,
// right-aligned numeric columns, color-hex cells as live chips (the
// click contract reaches inside tables), and a trailing "… N more"
// hint when the source was capped.
func renderTable(img *image.RGBA, seg Seg, x0, y0, maxW int, accepting []string) (int, []apps.Region) {
	const (
		size   = 10.5
		rowH   = 18
		colPad = 14
	)
	nCols := len(seg.Columns)
	widths := make([]int, nCols)
	numeric := make([]bool, nCols)
	for c := 0; c < nCols; c++ {
		widths[c] = draw.TextWidth(seg.Columns[c], true, size)
		numeric[c] = len(seg.Cells) > 0
	}
	for _, row := range seg.Cells {
		for c, cell := range row {
			tw := draw.TextWidth(cell, false, size)
			if colorCellRe.MatchString(cell) {
				tw += 16
			}
			if tw > widths[c] {
				widths[c] = tw
			}
			if _, err := strconv.ParseFloat(strings.TrimSpace(cell), 64); err != nil && cell != "" {
				numeric[c] = false
			}
		}
	}
	// Clamp: drop trailing columns that do not fit.
	shown := nCols
	total := 0
	for c := 0; c < nCols; c++ {
		total += widths[c] + colPad
		if total > maxW && c > 0 {
			shown = c
			break
		}
	}

	var regions []apps.Region
	y := y0
	x := x0
	for c := 0; c < shown; c++ {
		draw.Text(img, x, y+13, seg.Columns[c], true, size, draw.Ink)
		x += widths[c] + colPad
	}
	draw.Fill(img, image.Rect(x0, y+16, x0+minInt(total, maxW), y+17), draw.Faint)
	y += rowH + 2
	for _, row := range seg.Cells {
		x = x0
		for c := 0; c < shown; c++ {
			cell := row[c]
			cx := x
			if numeric[c] {
				cx = x + widths[c] - draw.TextWidth(cell, false, size)
			}
			if colorCellRe.MatchString(cell) {
				obj, err := pbui.NewObject("color", cell)
				if err == nil {
					sw := image.Rect(x, y+3, x+12, y+15)
					draw.Fill(img, sw, hexTone(cell))
					draw.Border(img, sw, 1, draw.Ink)
					if len(accepting) > 0 && pbui.TypeMatches(accepting, "color") {
						draw.Border(img, sw.Inset(-2), 2, draw.Sel)
					}
					regions = append(regions, apps.Region{
						Rect: sw, Object: &obj, Doc: "color " + cell,
					})
				}
				draw.Text(img, x+16, y+13, cell, false, size, draw.Ink)
			} else {
				draw.Text(img, cx, y+13, cell, false, size, draw.Ink)
			}
			x += widths[c] + colPad
		}
		y += rowH
	}
	if shown < nCols {
		draw.Text(img, x0, y+13, fmt.Sprintf("… %d more columns", nCols-shown), false, 10, draw.Faint)
		y += rowH
	}
	if seg.More > 0 {
		draw.Text(img, x0, y+13, fmt.Sprintf("… %d more rows", seg.More), false, 10, draw.Faint)
		y += rowH
	}
	return y - y0, regions
}

func hexTone(s string) color.RGBA {
	var r, g, b int
	_, _ = fmt.Sscanf(s[1:], "%02x%02x%02x", &r, &g, &b)
	return color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
