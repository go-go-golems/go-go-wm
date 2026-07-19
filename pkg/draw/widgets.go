package draw

import (
	"fmt"
	"image"
	"image/color"
)

// Widget sizes. Fixed pixel metrics keep the look crisp and the golden
// files stable.
const (
	TitleH   = 22 // tile title strip height
	BarH     = 24 // top/bottom bar height
	MenuRowH = 22
	BorderW  = 2
)

// TitleStrip is a tile's title bar: ⠿ grip, uppercase app title, and the
// ⬌ ⬍ ✕ buttons (ports TileView's header, pbui-shell.jsx:255-272).
type TitleStrip struct {
	Title   string
	Color   color.RGBA
	Focused bool
	Width   int
}

// Button hit zones, right-aligned: [✕][⬍][⬌] from the right edge.
const titleBtnW = 24

// TitleButtons returns the hit rects (in strip-local coordinates) for
// grip, split-right, split-down, close.
func TitleButtons(width int) (image.Rectangle, image.Rectangle, image.Rectangle, image.Rectangle) {
	grip := image.Rect(0, 0, 22, TitleH)
	closeBtn := image.Rect(width-titleBtnW, 0, width, TitleH)
	splitDown := image.Rect(width-2*titleBtnW, 0, width-titleBtnW, TitleH)
	splitRight := image.Rect(width-3*titleBtnW, 0, width-2*titleBtnW, TitleH)
	return grip, splitRight, splitDown, closeBtn
}

// Render draws the strip into a fresh image.
func (t TitleStrip) Render() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, t.Width, TitleH))
	bg := t.Color
	Fill(img, img.Bounds(), bg)
	// Bottom rule separating strip from client area.
	Fill(img, image.Rect(0, TitleH-BorderW, t.Width, TitleH), Ink)

	// ⠿ grip: a 3x2 dot matrix (glyph fallback-proof).
	for row := 0; row < 3; row++ {
		for col := 0; col < 2; col++ {
			x, y := 7+col*4, 5+row*4
			Fill(img, image.Rect(x, y, x+2, y+2), Ink)
		}
	}

	title := t.Title
	Text(img, 24, 15, upper(title), true, 11, Ink)
	if t.Focused {
		// Focused tiles get an underline accent under the title.
		w := TextWidth(upper(title), true, 11)
		Fill(img, image.Rect(24, 17, 24+w, 18), Ink)
	}

	// Buttons, right-aligned boxes with 1px borders.
	_, sr, sd, cl := TitleButtons(t.Width)
	for i, b := range []struct {
		r     image.Rectangle
		label string
	}{{sr, "|"}, {sd, "-"}, {cl, "x"}} {
		_ = i
		inner := image.Rect(b.r.Min.X+3, 3, b.r.Max.X-3, TitleH-5)
		Fill(img, inner, PaneAlt)
		Border(img, inner, 1, Ink)
		lw := TextWidth(b.label, true, 10)
		Text(img, inner.Min.X+(inner.Dx()-lw)/2, inner.Max.Y-4, b.label, true, 10, Ink)
	}
	return img
}

// Banner is the red ACCEPTING banner (pbui-shell.jsx:791-795).
type Banner struct {
	Ptypes []string
	Prompt string
	Width  int
}

func (b Banner) Render() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, b.Width, BarH))
	Fill(img, img.Bounds(), Red)
	label := "ACCEPTING <" + join(b.Ptypes, "|") + "> — " + b.Prompt + " — Esc cancels"
	Text(img, 10, 16, label, true, 11, Paper)
	return img
}

// StatusLine is the bottom bar: mode chip, mouse-doc text, tile/workspace
// counts (pbui-shell.jsx:829-835).
type StatusLine struct {
	Mode   string // READY | ACCEPT MODE | MOVING APP
	Doc    string
	Counts string // "6 tiles · 2 workspaces"
	Width  int
}

func (s StatusLine) Render() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, s.Width, BarH))
	Fill(img, img.Bounds(), Ink)
	x := 10
	x += Text(img, x, 16, s.Mode, true, 11, Mustard)
	x += 16
	Text(img, x, 16, s.Doc, false, 11, Paper)
	if s.Counts != "" {
		w := TextWidth(s.Counts, false, 11)
		Text(img, s.Width-w-10, 16, s.Counts, false, 11, Faint)
	}
	return img
}

// TopBar renders the workspace strip: chips for each workspace, the current
// one highlighted (pbui-shell.jsx:798-821).
type TopBar struct {
	Workspaces []string
	Current    int
	Width      int
}

// TopBarChips returns the chip hit rects, aligned with Render.
func (t TopBar) TopBarChips() []image.Rectangle {
	var out []image.Rectangle
	x := 10 + TextWidth("WORKSPACES", true, 10) + 10
	for _, name := range t.Workspaces {
		w := TextWidth(name, true, 11) + 18
		out = append(out, image.Rect(x, 2, x+w, BarH-2))
		x += w + 6
	}
	return out
}

func (t TopBar) Render() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, t.Width, BarH))
	Fill(img, img.Bounds(), Paper)
	Fill(img, image.Rect(0, BarH-1, t.Width, BarH), Ink)
	Text(img, 10, 16, "WORKSPACES", true, 10, Ink)
	for i, r := range t.TopBarChips() {
		bg := PaneAlt
		bold := false
		if i == t.Current {
			bg, bold = Sel, true
		}
		Fill(img, r, bg)
		Border(img, r, 2, Ink)
		Text(img, r.Min.X+9, 17, t.Workspaces[i], bold, 11, Ink)
	}
	return img
}

// Menu is the object menu: inverse-video header naming the presentation,
// then one row per verb (pbui-shell.jsx:848-864).
type Menu struct {
	Header string // "<color> #d3b56a"
	Items  []string
	Hover  int // -1 for none
}

// MenuSize returns the pixel size the menu will render at.
func (m Menu) MenuSize() (int, int) {
	w := TextWidth(m.Header, true, 10) + 20
	for _, it := range m.Items {
		if tw := TextWidth(it, false, 11) + 24; tw > w {
			w = tw
		}
	}
	if w < 260 {
		w = 260
	}
	// header + items + border + hard shadow margin (4px)
	return w + 4, 18 + len(m.Items)*MenuRowH + 2*BorderW + 4
}

// MenuItemAt maps a menu-local point to an item index, or -1.
func (m Menu) MenuItemAt(x, y int) int {
	w, h := m.MenuSize()
	if x < 0 || x >= w-4 || y < 18+BorderW || y >= h-4 {
		return -1
	}
	i := (y - 18 - BorderW) / MenuRowH
	if i < 0 || i >= len(m.Items) {
		return -1
	}
	return i
}

func (m Menu) Render() *image.RGBA {
	w, h := m.MenuSize()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// Hard shadow: offset ink rect under the panel.
	Fill(img, image.Rect(4, 4, w, h), Ink)
	panel := image.Rect(0, 0, w-4, h-4)
	Fill(img, panel, Pane)
	Border(img, panel, BorderW, Ink)
	// Header band.
	hdr := image.Rect(BorderW, BorderW, panel.Max.X-BorderW, 18)
	Fill(img, hdr, Ink)
	Text(img, 8, 14, m.Header, true, 10, Paper)
	y := 18 + BorderW
	for i, it := range m.Items {
		row := image.Rect(BorderW, y, panel.Max.X-BorderW, y+MenuRowH)
		if i == m.Hover {
			Fill(img, row, Sel)
		}
		if i > 0 {
			// Dotted separator.
			for x := row.Min.X + 4; x < row.Max.X-4; x += 4 {
				Fill(img, image.Rect(x, y, x+2, y+1), Faint)
			}
		}
		Text(img, 12, y+15, it, false, 11, Ink)
		y += MenuRowH
	}
	return img
}

// DropPreview renders the dashed drop-zone overlay for a zone rect
// (pbui-shell.jsx:244-254), with a labeled chip in the middle.
type DropPreview struct {
	W, H  int
	Label string
}

func (d DropPreview) Render() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, d.W, d.H))
	Stipple(img, img.Bounds(), Red)
	DashedBorder(img, img.Bounds(), 3, 6, Red)
	lw := TextWidth(d.Label, true, 10) + 16
	chip := image.Rect((d.W-lw)/2, d.H/2-10, (d.W+lw)/2, d.H/2+10)
	Fill(img, image.Rect(chip.Min.X+2, chip.Min.Y+2, chip.Max.X+2, chip.Max.Y+2), Ink)
	Fill(img, chip, Pane)
	Border(img, chip, 2, Ink)
	Text(img, chip.Min.X+8, chip.Max.Y-6, d.Label, true, 10, Ink)
	return img
}

func upper(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			r -= 32
		}
		out = append(out, r)
	}
	return string(out)
}

func join(ss []string, sep string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}

// DividerColor maps a divider interaction mode to its fill (idle → paper,
// hot → paneAlt, dragging → sage, snapped → mustard; pbui-shell.jsx:192).
func DividerColor(mode int) color.RGBA {
	switch mode {
	case 1:
		return PaneAlt
	case 2:
		return Sage
	case 3:
		return Mustard
	default:
		return Paper
	}
}

var _ = fmt.Sprintf
