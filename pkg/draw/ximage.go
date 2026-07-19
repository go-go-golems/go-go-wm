package draw

import (
	"image"

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
	w := r.Dx()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		so := img.PixOffset(r.Min.X, y)
		do := ximg.PixOffset(r.Min.X, y)
		src := img.Pix[so : so+w*4 : so+w*4]
		dst := ximg.Pix[do : do+w*4 : do+w*4]
		for i := 0; i < w*4; i += 4 {
			dst[i+0] = src[i+2] // B
			dst[i+1] = src[i+1] // G
			dst[i+2] = src[i+0] // R
			dst[i+3] = src[i+3] // A
		}
	}
}
