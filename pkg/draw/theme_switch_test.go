package draw

import (
	"image/color"
	"testing"
)

// lum is the quick perceptual luminance in [0,255].
func lum(c color.RGBA) float64 {
	return 0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)
}

func TestSetThemeSwapsEverySlotAndAppColors(t *testing.T) {
	t.Cleanup(func() { _ = SetTheme("paper") })
	if err := SetTheme("dark"); err != nil {
		t.Fatal(err)
	}
	d := Themes["dark"]
	got := map[string][2]color.RGBA{
		"Paper": {Paper, d.Paper}, "Pane": {Pane, d.Pane}, "PaneAlt": {PaneAlt, d.PaneAlt},
		"Field": {Field, d.Field}, "Ink": {Ink, d.Ink}, "Faint": {Faint, d.Faint},
		"Red": {Red, d.Red}, "Sel": {Sel, d.Sel}, "Sage": {Sage, d.Sage},
		"Blue": {Blue, d.Blue}, "Rose": {Rose, d.Rose}, "Mustard": {Mustard, d.Mustard},
		"Lavender": {Lavender, d.Lavender}, "Mint": {Mint, d.Mint},
	}
	for slot, pair := range got {
		if pair[0] != pair[1] {
			t.Errorf("%s: live %v != theme %v", slot, pair[0], pair[1])
		}
	}
	if AppColors[0] != d.Rose {
		t.Errorf("AppColors not rebuilt: %v", AppColors[0])
	}
	if CurrentTheme() != "dark" {
		t.Errorf("CurrentTheme = %q", CurrentTheme())
	}
	if err := SetTheme("nope"); err == nil {
		t.Error("unknown theme accepted")
	}
}

func TestDarkDiffersFromPaperEverywhere(t *testing.T) {
	p, d := Themes["paper"], Themes["dark"]
	pairs := [][2]color.RGBA{
		{p.Paper, d.Paper}, {p.Pane, d.Pane}, {p.PaneAlt, d.PaneAlt}, {p.Field, d.Field},
		{p.Ink, d.Ink}, {p.Faint, d.Faint}, {p.Red, d.Red}, {p.Sel, d.Sel},
		{p.Sage, d.Sage}, {p.Blue, d.Blue}, {p.Rose, d.Rose},
		{p.Mustard, d.Mustard}, {p.Lavender, d.Lavender}, {p.Mint, d.Mint},
	}
	for i, pr := range pairs {
		if pr[0] == pr[1] {
			t.Errorf("slot %d: dark == paper (%v)", i, pr[0])
		}
	}
}

func TestLightIsTrueWhite(t *testing.T) {
	l := Themes["light"]
	white := color.RGBA{0xff, 0xff, 0xff, 0xff}
	if l.Paper != white || l.Pane != white || l.Field != white {
		t.Errorf("light surfaces must be #ffffff: paper=%v pane=%v field=%v", l.Paper, l.Pane, l.Field)
	}
}

// Ink is drawn on top of every accent tone (chips, buttons, title strips)
// and on Pane/PaneAlt/Sel. Require a luminance gap in every theme so text
// stays readable — asserted numerically, not by eyeball.
func TestInkContrastOnTonesPerTheme(t *testing.T) {
	const minGap = 60.0
	for name, th := range Themes {
		bgs := map[string]color.RGBA{
			"Pane": th.Pane, "PaneAlt": th.PaneAlt, "Sel": th.Sel, "Field": th.Field,
			"Sage": th.Sage, "Blue": th.Blue, "Rose": th.Rose,
			"Mustard": th.Mustard, "Lavender": th.Lavender, "Mint": th.Mint,
		}
		for slot, bg := range bgs {
			gap := lum(th.Ink) - lum(bg)
			if gap < 0 {
				gap = -gap
			}
			if gap < minGap {
				t.Errorf("%s: ink on %s gap %.1f < %.1f", name, slot, gap, minGap)
			}
		}
	}
}
