package draw

import (
	"image"
	"image/color"
)

// LauncherPanel is the launcher's face (GGWM-008): a query field on
// top, result rows below. Pure rendering + hit tests; the model
// (registry, selection state) lives with the caller. Shared by the
// Mod4+d popup and the launcher tile.
type LauncherPanel struct {
	Query    string
	Prompt   string // field placeholder / mode label ("RUN", "PICK COMMAND")
	Rows     []LauncherRow
	Selected int
	Width    int
	Height   int
	Compact  bool // tile mode: no outer border, tighter header
}

// LauncherRow is one result line.
type LauncherRow struct {
	Label string
	Doc   string     // dimmed description
	Tag   string     // kind tag, right-aligned ("app", "builtin", "script")
	Tone  color.RGBA // accent chip
}

// Launcher layout metrics (fixed pixels, golden-stable).
const (
	launcherPad    = 8
	launcherFieldH = 30
	LauncherRowH   = 26
)

// fieldRect returns the query field rectangle.
func (p LauncherPanel) fieldRect() image.Rectangle {
	b := 0
	if !p.Compact {
		b = BorderW
	}
	return image.Rect(b+launcherPad, b+launcherPad,
		p.Width-b-launcherPad, b+launcherPad+launcherFieldH)
}

// MaxRows returns how many result rows fit.
func (p LauncherPanel) MaxRows() int {
	top := p.fieldRect().Max.Y + 6
	n := (p.Height - top - launcherPad) / LauncherRowH
	if n < 0 {
		n = 0
	}
	return n
}

// RowRects returns the hit rectangles for the visible rows, in panel
// coordinates, parallel to Rows (capped at MaxRows).
func (p LauncherPanel) RowRects() []image.Rectangle {
	b := 0
	if !p.Compact {
		b = BorderW
	}
	top := p.fieldRect().Max.Y + 6
	n := len(p.Rows)
	if m := p.MaxRows(); n > m {
		n = m
	}
	out := make([]image.Rectangle, n)
	for i := 0; i < n; i++ {
		out[i] = image.Rect(b+4, top+i*LauncherRowH,
			p.Width-b-4, top+(i+1)*LauncherRowH)
	}
	return out
}

// RowAt returns the row index at (x, y), or -1.
func (p LauncherPanel) RowAt(x, y int) int {
	for i, r := range p.RowRects() {
		if image.Pt(x, y).In(r) {
			return i
		}
	}
	return -1
}

// Render draws the panel.
func (p LauncherPanel) Render() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, p.Width, p.Height))
	Fill(img, img.Bounds(), Pane)

	// Query field: Field surface, ink rule, caret after the query.
	fr := p.fieldRect()
	Fill(img, fr, Field)
	Border(img, fr, 1, Ink)
	tx := fr.Min.X + 8
	ty := fr.Min.Y + 20
	if p.Query == "" && p.Prompt != "" {
		Text(img, tx, ty, p.Prompt, false, 11.5, Faint)
	} else {
		tx += Text(img, tx, ty, p.Query, true, 12, Ink)
	}
	// Block caret.
	Fill(img, image.Rect(tx+2, fr.Min.Y+7, tx+9, fr.Max.Y-7), Sel)

	rects := p.RowRects()
	for i, r := range rects {
		row := p.Rows[i]
		if i == p.Selected {
			Fill(img, r, PaneAlt)
			Border(img, r, 1, Ink)
		}
		// Accent chip.
		chip := image.Rect(r.Min.X+6, r.Min.Y+8, r.Min.X+16, r.Min.Y+18)
		Fill(img, chip, row.Tone)
		Border(img, chip, 1, Ink)
		x := r.Min.X + 24
		x += Text(img, x, r.Min.Y+17, row.Label, true, 11.5, Ink)
		if row.Doc != "" {
			Text(img, x+10, r.Min.Y+17, row.Doc, false, 10.5, Faint)
		}
		if row.Tag != "" {
			tw := TextWidth(row.Tag, false, 9.5)
			Text(img, r.Max.X-tw-8, r.Min.Y+17, row.Tag, false, 9.5, Faint)
		}
	}
	if len(p.Rows) == 0 {
		Text(img, fr.Min.X+2, fr.Max.Y+24, "no matches", false, 11, Faint)
	}
	if !p.Compact {
		Border(img, img.Bounds(), BorderW, Ink)
	}
	return img
}
