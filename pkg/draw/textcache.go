package draw

import (
	"image"
	"image/color"
	xdraw "image/draw"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// Text rasterization was the single most expensive operation in a frame
// repaint: benchmarking TitleStrip.Render at width 1272 showed 73.7us total,
// of which draw.Text alone was 52.8us — 72% (GGWM-012).
//
// A window's title does not change while its pane is being resized; only the
// pane's width does. So the glyph raster is reusable across every paint of a
// drag. This cache stores the rendered run as an alpha MASK rather than as
// coloured pixels, which means a theme swap or a focus change reuses the same
// entry and only recolours it — geometry work is not repeated for a colour
// change.
type textKey struct {
	s    string
	bold bool
	size float64
}

type textMask struct {
	mask    *image.Alpha // coverage, origin at the run's top-left
	originY int          // baseline offset within the mask
	advance int          // pen advance in pixels
}

var (
	textMu    sync.Mutex
	textCache = map[textKey]*textMask{}
)

// textCacheMax bounds the cache. Titles change as windows come and go, so
// this must not grow without limit; the working set is small (one entry per
// visible title plus button glyphs), and a wholesale clear is cheap and
// simpler than an LRU.
const textCacheMax = 512

func lookupTextMask(s string, bold bool, size float64) *textMask {
	k := textKey{s: s, bold: bold, size: size}
	textMu.Lock()
	if m, ok := textCache[k]; ok {
		textMu.Unlock()
		return m
	}
	textMu.Unlock()

	m := rasterizeText(s, bold, size)

	textMu.Lock()
	if len(textCache) >= textCacheMax {
		textCache = map[textKey]*textMask{}
	}
	textCache[k] = m
	textMu.Unlock()
	return m
}

// rasterizeText draws one run into a tight alpha mask.
func rasterizeText(s string, bold bool, size float64) *textMask {
	face := Face(bold, size)
	adv := font.MeasureString(face, s).Ceil()
	metrics := face.Metrics()
	ascent := metrics.Ascent.Ceil()
	descent := metrics.Descent.Ceil()
	h := ascent + descent
	if adv <= 0 || h <= 0 {
		return &textMask{mask: image.NewAlpha(image.Rect(0, 0, 0, 0)), advance: adv}
	}
	// Pad generously: some faces overhang the advance width.
	w := adv + int(size) + 2
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	d := font.Drawer{
		Dst:  mask,
		Src:  image.NewUniform(color.Alpha{A: 255}),
		Face: face,
		Dot:  fixed.P(0, ascent),
	}
	d.DrawString(s)
	return &textMask{mask: mask, originY: ascent, advance: adv}
}

// drawTextCached composites a cached run at the given baseline and colour.
func drawTextCached(img *image.RGBA, x, y int, s string, bold bool, size float64, c color.RGBA) int {
	m := lookupTextMask(s, bold, size)
	b := m.mask.Bounds()
	if b.Empty() {
		return m.advance
	}
	dst := image.Rect(x, y-m.originY, x+b.Dx(), y-m.originY+b.Dy())
	xdraw.DrawMask(img, dst, image.NewUniform(c), image.Point{},
		m.mask, b.Min, xdraw.Over)
	return m.advance
}

// ClearTextCache drops every cached run. Exported for tests and for callers
// that change font assets at runtime.
func ClearTextCache() {
	textMu.Lock()
	textCache = map[textKey]*textMask{}
	textMu.Unlock()
}
