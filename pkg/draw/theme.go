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
	"sync/atomic"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

// Theme is one complete palette. A live snapshot (Palette) is exposed
// atomically via Current(): renderers call Current() at the start of a
// paint pass and read colors off the returned struct, so a theme swap
// can never produce a frame assembled from mixed old/new colors. This
// replaces the older package-level mutable vars, which were read
// lock-free by 18 files and raced SetTheme when a process had a render
// loop separate from the theme-swap goroutine (Codex review RC-14).
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

// Palette is a live, immutable snapshot of the active theme's colors
// plus its derived AppColors cycle. Current() returns one; readers treat
// it as read-only. A Palette is small (~60 bytes) so capturing it per
// paint pass is cheaper than a lock.
type Palette struct {
	Theme
	AppColors []color.RGBA
}

// AppColor returns the title-strip color for app slot i, cycling through
// the palette's accent colors. Reads the snapshot's AppColors.
func (p Palette) AppColor(i int) color.RGBA {
	ac := p.AppColors
	return ac[((i%len(ac))+len(ac))%len(ac)]
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

// The live palette. Stored as an atomic pointer to an immutable Palette
// snapshot so renderers can read it lock-free: Current() returns the
// pointer, SetTheme swaps it. A theme change publishes a whole Palette at
// once, so a paint pass that captured Current() at its start never sees
// half-old/half-new colors (Codex review RC-14).
var livePalette atomic.Pointer[Palette]

func init() {
	livePalette.Store(initialPalette("paper"))
}

// initialPalette builds the immutable Palette snapshot for a theme name.
func initialPalette(name string) *Palette {
	t := Themes[name]
	return &Palette{
		Theme:     t,
		AppColors: []color.RGBA{t.Rose, t.Blue, t.Mint, t.Mustard, t.Lavender, t.Sage},
	}
}

var currentTheme = "paper"

// Current returns the live palette snapshot. Renderers should capture it
// once at the start of a paint pass and read colors off the result; the
// returned Palette is immutable for its lifetime.
func Current() Palette { return *livePalette.Load() }

// CurrentTheme returns the name of the live palette.
func CurrentTheme() string { return currentTheme }

// SetTheme swaps the live palette atomically. Safe to call from any
// goroutine: it publishes a whole Palette at once, so renderers reading
// via Current() never see a torn palette. Callers repaint afterwards;
// nothing repaints on their behalf.
func SetTheme(name string) error {
	if _, ok := Themes[name]; !ok {
		return fmt.Errorf("draw: unknown theme %q (have %v)", name, ThemeNames())
	}
	livePalette.Store(initialPalette(name))
	currentTheme = name
	return nil
}

func rgb(r, g, b uint8) color.RGBA { return color.RGBA{R: r, G: g, B: b, A: 0xff} }

// AppColor returns the title-strip color for app slot i, cycling through
// the live palette's accent colors. Convenience for callers that don't
// already hold a Palette snapshot.
func AppColor(i int) color.RGBA { return Current().AppColor(i) }

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

// Fill paints r with c. Writes the first row's byte pattern once and
// row-copies it — per-pixel SetRGBA was a quarter of the WM's CPU at
// full-frame sizes (GGWM-005 profile).
func Fill(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	r = r.Intersect(img.Bounds())
	if r.Empty() {
		return
	}
	w := r.Dx()
	o0 := img.PixOffset(r.Min.X, r.Min.Y)
	first := img.Pix[o0 : o0+w*4 : o0+w*4]
	for i := 0; i < w*4; i += 4 {
		first[i+0] = c.R
		first[i+1] = c.G
		first[i+2] = c.B
		first[i+3] = c.A
	}
	for y := r.Min.Y + 1; y < r.Max.Y; y++ {
		o := img.PixOffset(r.Min.X, y)
		copy(img.Pix[o:o+w*4], first)
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
	// Rasterizing glyphs was 72% of a title-strip render and repeated on
	// every repaint even though the string never changed; the run is cached
	// as an alpha mask and recoloured here (GGWM-012, ~7x faster).
	return drawTextCached(img, x, y, s, bold, size, c)
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
