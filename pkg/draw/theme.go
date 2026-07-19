// Package draw renders the paper-and-ink PBUI theme into plain image.RGBA:
// title strips, banners, menus, status lines, workspace chips. It knows
// nothing about X — xgbutil's xgraphics.Image embeds an image.RGBA, so the
// X shell uploads these directly, and tests diff them as golden PNGs.
//
// Hard rules (from the prototype's look, see the ticket screenshots):
// flat fills, 1-2px ink strokes, hard drop shadows, no gradients, no
// rounded corners, IBM Plex Mono everywhere.
package draw

import (
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Theme is one complete palette. Every renderer reads the package-level
// vars below at paint time, so swapping a theme in via SetTheme and
// repainting re-skins the whole surface (design GGWM-004 T-D1).
type Theme struct {
	Name string
	// Surfaces.
	Paper   color.RGBA // desktop / root background
	Pane    color.RGBA // tile background
	PaneAlt color.RGBA // alternate rows, chips at rest
	Field   color.RGBA // text-input background
	// Strokes and text.
	Ink   color.RGBA // borders, primary text
	Faint color.RGBA // secondary text, dotted leaders
	Red   color.RGBA // errors, accept outlines
	Sel   color.RGBA // accept/selection highlight
	// Accent tones (title strips, buttons, event chips).
	Sage, Blue, Rose, Mustard, Lavender, Mint color.RGBA
}

// Themes is the registry. "paper" ports C from the prototype
// (pbui-shell.jsx:30-36) and is the default; "light" is true white
// (#ffffff, deliberately not beige); "dark" anchors on #1f1f1f with
// accents dark enough that the light ink keeps contrast on top of them.
var Themes = map[string]Theme{
	"paper": {
		Name:  "paper",
		Paper: rgb(0xe9, 0xe2, 0xd0), Pane: rgb(0xf5, 0xf0, 0xe3),
		PaneAlt: rgb(0xef, 0xe9, 0xd9), Field: rgb(0xfb, 0xf8, 0xef),
		Ink: rgb(0x33, 0x30, 0x2a), Faint: rgb(0x7a, 0x73, 0x65),
		Red: rgb(0xb0, 0x56, 0x3f), Sel: rgb(0xf4, 0xe6, 0xb8),
		Sage: rgb(0xa9, 0xbd, 0xa2), Blue: rgb(0x9c, 0xb4, 0xc2),
		Rose: rgb(0xcf, 0xa0, 0x8f), Mustard: rgb(0xd3, 0xb5, 0x6a),
		Lavender: rgb(0xb3, 0xab, 0xc4), Mint: rgb(0xb7, 0xc9, 0xb3),
	},
	"light": {
		Name:  "light",
		Paper: rgb(0xff, 0xff, 0xff), Pane: rgb(0xff, 0xff, 0xff),
		PaneAlt: rgb(0xf2, 0xf2, 0xf2), Field: rgb(0xff, 0xff, 0xff),
		Ink: rgb(0x1a, 0x1a, 0x1a), Faint: rgb(0x6e, 0x6e, 0x6e),
		Red: rgb(0xb0, 0x56, 0x3f), Sel: rgb(0xf6, 0xe9, 0xa8),
		Sage: rgb(0xa9, 0xbd, 0xa2), Blue: rgb(0x9c, 0xb4, 0xc2),
		Rose: rgb(0xcf, 0xa0, 0x8f), Mustard: rgb(0xd3, 0xb5, 0x6a),
		Lavender: rgb(0xb3, 0xab, 0xc4), Mint: rgb(0xb7, 0xc9, 0xb3),
	},
	"dark": {
		Name:  "dark",
		Paper: rgb(0x1f, 0x1f, 0x1f), Pane: rgb(0x26, 0x26, 0x26),
		PaneAlt: rgb(0x2e, 0x2e, 0x2e), Field: rgb(0x19, 0x19, 0x19),
		Ink: rgb(0xe6, 0xe2, 0xda), Faint: rgb(0x8a, 0x86, 0x7e),
		Red: rgb(0xd0, 0x7a, 0x5e), Sel: rgb(0x55, 0x48, 0x2a),
		Sage: rgb(0x4c, 0x5c, 0x47), Blue: rgb(0x46, 0x58, 0x64),
		Rose: rgb(0x6e, 0x4a, 0x3e), Mustard: rgb(0x6d, 0x5c, 0x2f),
		Lavender: rgb(0x57, 0x4f, 0x68), Mint: rgb(0x4b, 0x5f, 0x4c),
	},
}

// ThemeNames returns the registry keys in stable order.
func ThemeNames() []string { return []string{"paper", "light", "dark"} }

// The live palette. Initialized to "paper"; reassigned by SetTheme.
// SetTheme must only be called from the goroutine that owns rendering
// (the WM loop, or a script runtime's JS loop before posting repaints) —
// paint paths read these vars without locks.
var (
	Paper    = Themes["paper"].Paper
	Pane     = Themes["paper"].Pane
	PaneAlt  = Themes["paper"].PaneAlt
	Ink      = Themes["paper"].Ink
	Faint    = Themes["paper"].Faint
	Sage     = Themes["paper"].Sage
	Blue     = Themes["paper"].Blue
	Rose     = Themes["paper"].Rose
	Mustard  = Themes["paper"].Mustard
	Lavender = Themes["paper"].Lavender
	Mint     = Themes["paper"].Mint
	Red      = Themes["paper"].Red
	Sel      = Themes["paper"].Sel
	Field    = Themes["paper"].Field
)

var currentTheme = "paper"

// CurrentTheme returns the name of the live palette.
func CurrentTheme() string { return currentTheme }

// SetTheme swaps the live palette. Callers repaint afterwards; nothing
// repaints on their behalf. See the goroutine rule on the var block.
func SetTheme(name string) error {
	t, ok := Themes[name]
	if !ok {
		return fmt.Errorf("draw: unknown theme %q (have %v)", name, ThemeNames())
	}
	Paper, Pane, PaneAlt, Field = t.Paper, t.Pane, t.PaneAlt, t.Field
	Ink, Faint, Red, Sel = t.Ink, t.Faint, t.Red, t.Sel
	Sage, Blue, Rose, Mustard, Lavender, Mint = t.Sage, t.Blue, t.Rose, t.Mustard, t.Lavender, t.Mint
	AppColors = []color.RGBA{Rose, Blue, Mint, Mustard, Lavender, Sage}
	currentTheme = name
	return nil
}

func rgb(r, g, b uint8) color.RGBA { return color.RGBA{R: r, G: g, B: b, A: 0xff} }

// AppColors assigns tile title-strip colors to app slots, cycling through
// the theme's accent palette. Rebuilt by SetTheme.
var AppColors = []color.RGBA{Rose, Blue, Mint, Mustard, Lavender, Sage}

func AppColor(i int) color.RGBA { return AppColors[((i%len(AppColors))+len(AppColors))%len(AppColors)] }

// IBM Plex Mono, vendored under the SIL Open Font License 1.1
// (https://github.com/IBM/plex).
//
//go:embed fonts/IBMPlexMono-Regular.ttf
var plexRegularTTF []byte

//go:embed fonts/IBMPlexMono-Bold.ttf
var plexBoldTTF []byte

type faceKey struct {
	bold bool
	size float64
}

var (
	fontOnce    sync.Once
	plexRegular *opentype.Font
	plexBold    *opentype.Font
	faceMu      sync.Mutex
	faces       = map[faceKey]font.Face{}
)

func loadFonts() {
	var err error
	plexRegular, err = opentype.Parse(plexRegularTTF)
	if err != nil {
		panic("draw: embedded IBMPlexMono-Regular.ttf: " + err.Error())
	}
	plexBold, err = opentype.Parse(plexBoldTTF)
	if err != nil {
		panic("draw: embedded IBMPlexMono-Bold.ttf: " + err.Error())
	}
}

// Face returns a cached Plex Mono face. Hinting is fixed so rendering is
// deterministic across platforms (golden tests depend on this).
func Face(bold bool, size float64) font.Face {
	fontOnce.Do(loadFonts)
	faceMu.Lock()
	defer faceMu.Unlock()
	k := faceKey{bold, size}
	if f, ok := faces[k]; ok {
		return f
	}
	src := plexRegular
	if bold {
		src = plexBold
	}
	f, err := opentype.NewFace(src, &opentype.FaceOptions{
		Size: size, DPI: 72, Hinting: font.HintingFull,
	})
	if err != nil {
		panic("draw: face: " + err.Error())
	}
	faces[k] = f
	return f
}

// --- primitives ------------------------------------------------------------

// Fill paints r with c.
func Fill(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}

// Border strokes an inset border of the given width inside r.
func Border(img *image.RGBA, r image.Rectangle, w int, c color.RGBA) {
	Fill(img, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+w), c)
	Fill(img, image.Rect(r.Min.X, r.Max.Y-w, r.Max.X, r.Max.Y), c)
	Fill(img, image.Rect(r.Min.X, r.Min.Y, r.Min.X+w, r.Max.Y), c)
	Fill(img, image.Rect(r.Max.X-w, r.Min.Y, r.Max.X, r.Max.Y), c)
}

// Text draws s with its baseline at (x, y) and returns the advance width in
// pixels.
func Text(img *image.RGBA, x, y int, s string, bold bool, size float64, c color.RGBA) int {
	face := Face(bold, size)
	d := font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(c),
		Face: face,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(s)
	return (d.Dot.X - fixed.I(x)).Ceil()
}

// TextWidth measures s without drawing.
func TextWidth(s string, bold bool, size float64) int {
	return font.MeasureString(Face(bold, size), s).Ceil()
}

// Stipple fills r with a checkerboard of c on transparent-ish base — the
// old-school drop-preview texture that needs no compositor.
func Stipple(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if (x+y)%2 == 0 {
				img.SetRGBA(x, y, c)
			}
		}
	}
}

// DashedBorder strokes a dashed border (the drop-zone preview look,
// pbui-shell.jsx:247: "3px dashed red").
func DashedBorder(img *image.RGBA, r image.Rectangle, w, dash int, c color.RGBA) {
	on := func(i int) bool { return (i/dash)%2 == 0 }
	for x := r.Min.X; x < r.Max.X; x++ {
		if on(x - r.Min.X) {
			Fill(img, image.Rect(x, r.Min.Y, x+1, r.Min.Y+w), c)
			Fill(img, image.Rect(x, r.Max.Y-w, x+1, r.Max.Y), c)
		}
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		if on(y - r.Min.Y) {
			Fill(img, image.Rect(r.Min.X, y, r.Min.X+w, y+1), c)
			Fill(img, image.Rect(r.Max.X-w, y, r.Max.X, y+1), c)
		}
	}
}
