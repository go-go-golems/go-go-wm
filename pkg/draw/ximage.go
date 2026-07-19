package draw

import (
	"image"
	"runtime"
	"sync"

	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/xgraphics"
)

// ToXImage converts a rendered image.RGBA into an xgraphics.Image
// (BGRA, ready for XSurfaceSet/XDraw/XPaint).
//
// xgraphics.NewConvert exists for exactly this, but its conversion loop
// iterates column-major — every inner step jumps a full row stride, so
// a 1272×744 frame walks ~950k cache-missing pixels and shows up as a
// third of the WM's CPU (GGWM-005 profile). This version walks both Pix
// slices row-major and swaps R/B in place: same result, ~10× cheaper.
func ToXImage(X *xgbutil.XUtil, img *image.RGBA) *xgraphics.Image {
	ximg := xgraphics.New(X, img.Bounds())
	CopyToXImage(ximg, img)
	return ximg
}

// CopyToXImage converts img into an existing same-sized xgraphics.Image
// — the reuse path for paint loops that keep buffers between frames
// (allocating two multi-megabyte images per paint made the GC ~30% of
// the WM's profile).
func CopyToXImage(ximg *xgraphics.Image, img *image.RGBA) {
	r := img.Bounds()
	ConvertRows(r, func(y int, w int) ([]byte, []byte) {
		so := img.PixOffset(r.Min.X, y)
		do := ximg.PixOffset(r.Min.X, y)
		return img.Pix[so : so+w*4 : so+w*4], ximg.Pix[do : do+w*4 : do+w*4]
	})
}

// ConvertRows runs the RGBA→BGRA swap over the rows of r, splitting
// large images across a few goroutines — the conversion is the last
// remaining pixel pass (GGWM-006) and is embarrassingly parallel.
// rowSlices returns the (src, dst) byte windows for one row.
func ConvertRows(r image.Rectangle, rowSlices func(y, w int) (src, dst []byte)) {
	w, h := r.Dx(), r.Dy()
	if w <= 0 || h <= 0 {
		return
	}
	convertBand := func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			src, dst := rowSlices(y, w)
			for i := 0; i < w*4; i += 4 {
				dst[i+0] = src[i+2] // B
				dst[i+1] = src[i+1] // G
				dst[i+2] = src[i+0] // R
				dst[i+3] = src[i+3] // A
			}
		}
	}
	workers := runtime.GOMAXPROCS(0)
	if workers > 4 {
		workers = 4
	}
	// Small surfaces (bars, chips) aren't worth the goroutine handoff.
	if workers < 2 || w*h < 128*1024 {
		convertBand(r.Min.Y, r.Max.Y)
		return
	}
	var wg sync.WaitGroup
	band := (h + workers - 1) / workers
	for y0 := r.Min.Y; y0 < r.Max.Y; y0 += band {
		y1 := y0 + band
		if y1 > r.Max.Y {
			y1 = r.Max.Y
		}
		wg.Add(1)
		go func(a, b int) {
			defer wg.Done()
			convertBand(a, b)
		}(y0, y1)
	}
	wg.Wait()
}
