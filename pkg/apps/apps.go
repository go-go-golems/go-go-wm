// Package apps is the presentation-surface framework shared by the WM's
// embedded apps (launcher, about, trace, listener, inspector) and the
// standalone demo clients (color lab, number field, notes).
//
// An app render produces an image plus a list of Regions — rectangles that
// carry either a presentation Object (clickable per the PBUI contract:
// pending matching accept → answer, else primary action, else object menu)
// or an Action id (a plain button). Renderers are pure functions of app
// state, so they golden-test with no X and no broker.
package apps

import (
	"image"
	"image/color"

	"github.com/go-go-golems/go-go-wm/pkg/draw"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
)

// Region is a clickable rectangle in a rendered surface.
type Region struct {
	Rect   image.Rectangle
	Object *pbui.Object // presentation region (accept/menu contract)
	Action string       // plain button action id (e.g. "cmd:sum", "launch:trace")
	Doc    string       // mouse-doc line text on hover
}

// RegionAt returns the topmost region containing (x, y), or nil.
func RegionAt(regions []Region, x, y int) *Region {
	for i := len(regions) - 1; i >= 0; i-- {
		if image.Pt(x, y).In(regions[i].Rect) {
			return &regions[i]
		}
	}
	return nil
}

// Click is the outcome of applying the PBUI click contract to a region.
type Click struct {
	Answer *pbui.Object // answer the pending accept with this object
	Action string       // run this app action
	Menu   *pbui.Object // pop the object menu for this object
}

// Resolve applies the prototype's click contract (pbui-shell.jsx:63-68).
// Left click: pending matching accept wins, then the region's primary
// action, then the object menu. Right click (button 3): always the object
// menu. A region may carry both an Object and an Action — e.g. a directory
// row navigates on left click but still has a menu on right click.
func Resolve(accepting []string, r *Region, button int) Click {
	if r == nil {
		return Click{}
	}
	if button == 3 {
		if r.Object != nil {
			return Click{Menu: r.Object}
		}
		return Click{}
	}
	if r.Object != nil && len(accepting) > 0 && pbui.TypeMatches(accepting, r.Object.Ptype) {
		return Click{Answer: r.Object}
	}
	if r.Action != "" {
		return Click{Action: r.Action}
	}
	if r.Object != nil {
		return Click{Menu: r.Object}
	}
	return Click{}
}

// --- shared widget vocabulary ----------------------------------------------

// Btn draws the prototype's hard-shadow button (pbui-shell.jsx:287-295) and
// returns its rect.
func Btn(img *image.RGBA, x, y int, label string, tone color.RGBA) image.Rectangle {
	w := draw.TextWidth(label, true, 11) + 20
	h := 22
	r := image.Rect(x, y, x+w, y+h)
	draw.Fill(img, image.Rect(x+2, y+2, x+w+2, y+h+2), draw.Current().Ink)
	draw.Fill(img, r, tone)
	draw.Border(img, r, 2, draw.Current().Ink)
	draw.Text(img, x+10, y+15, label, true, 11, draw.Current().Ink)
	return r
}

// Hint draws faint helper text (the prototype's Hint component).
func Hint(img *image.RGBA, x, y int, text string) int {
	draw.Text(img, x, y, text, false, 10.5, draw.Current().Faint)
	return y + 14
}

// Chip draws an inline presentation chip (the Pres default face,
// pbui-shell.jsx:77-89): a bordered box, with a swatch square for colors.
// Returns the rect.
func Chip(img *image.RGBA, x, y int, o pbui.Object, highlight bool) image.Rectangle {
	label := o.Label
	if label == "" {
		label = o.StringValue()
	}
	pad := 5
	sw := 0
	if o.Ptype == "color" {
		sw = 14
	}
	w := draw.TextWidth(label, false, 11) + 2*pad + sw
	h := 18
	r := image.Rect(x, y, x+w, y+h)
	bg := draw.Current().PaneAlt
	if highlight {
		bg = draw.Current().Sel
	}
	draw.Fill(img, r, bg)
	draw.Border(img, r, 1, draw.Current().Ink)
	if highlight {
		draw.Border(img, r.Inset(-1), 1, draw.Current().Red)
	}
	tx := x + pad
	if sw > 0 {
		c := parseHex(o.StringValue())
		draw.Fill(img, image.Rect(x+3, y+4, x+3+11, y+4+11), c)
		draw.Border(img, image.Rect(x+3, y+4, x+3+11, y+4+11), 1, draw.Current().Ink)
		tx = x + 3 + 11 + 3
	}
	draw.Text(img, tx, y+13, label, false, 11, draw.Current().Ink)
	return r
}

func parseHex(s string) color.RGBA {
	if len(s) != 7 || s[0] != '#' {
		return draw.Current().Faint
	}
	hex := func(c byte) uint8 {
		switch {
		case c >= '0' && c <= '9':
			return c - '0'
		case c >= 'a' && c <= 'f':
			return c - 'a' + 10
		case c >= 'A' && c <= 'F':
			return c - 'A' + 10
		}
		return 0
	}
	return color.RGBA{
		R: hex(s[1])<<4 | hex(s[2]),
		G: hex(s[3])<<4 | hex(s[4]),
		B: hex(s[5])<<4 | hex(s[6]),
		A: 0xff,
	}
}

// NewSurface allocates a pane-colored content image.
func NewSurface(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Fill(img, img.Bounds(), draw.Current().Pane)
	return img
}
