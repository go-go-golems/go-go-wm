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
)

// Seg is one inline element of a row.
type Seg struct {
	Kind string `json:"kind"`

	// text / hint
	Text string  `json:"text,omitempty"`
	Bold bool    `json:"bold,omitempty"`
	Size float64 `json:"size,omitempty"` // text only; default 11

	// object → Region.Object (the click contract does the rest)
	Ptype string      `json:"ptype,omitempty"`
	Value interface{} `json:"value,omitempty"`
	Label string      `json:"label,omitempty"`
	Doc   string      `json:"doc,omitempty"`

	// button → Region.Action
	Action string `json:"action,omitempty"`
	Color  string `json:"color,omitempty"` // named tone: rose|blue|mint|mustard|lavender|sage
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
	default:
		return Seg{}, fmt.Errorf("unknown segment kind %q", kind)
	}
	for k := range m {
		switch k {
		case "kind", "text", "bold", "size", "ptype", "value", "label", "doc", "action", "color":
		default:
			return Seg{}, fmt.Errorf("unknown segment key %q", k)
		}
	}
	return seg, nil
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
