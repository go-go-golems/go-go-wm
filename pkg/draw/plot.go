package draw

import (
	"image"
	"image/color"
	"math"
)

// Mini-plotters (GGWM-009): small pre-rendered strips for the uispec
// image segment. Deliberately narrow — sparklines and bar strips, not a
// plotting system (R-D2/R-D4: heavy visuals arrive as external
// processes presenting plot objects).

// Sparkline draws vals as a polyline over the Field surface. Values are
// normalized to the value range; a flat series draws a centered line.
func Sparkline(vals []float64, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	Fill(img, img.Bounds(), Field)
	Border(img, img.Bounds(), 1, Faint)
	if len(vals) == 0 || w < 8 || h < 8 {
		return img
	}
	lo, hi := minMax(vals)
	span := hi - lo
	px := func(i int) int {
		if len(vals) == 1 {
			return w / 2
		}
		return 3 + i*(w-7)/(len(vals)-1)
	}
	py := func(v float64) int {
		if span == 0 {
			return h / 2
		}
		return h - 4 - int(float64(h-8)*(v-lo)/span)
	}
	for i := 1; i < len(vals); i++ {
		line(img, px(i-1), py(vals[i-1]), px(i), py(vals[i]), Blue)
	}
	// End dot.
	last := len(vals) - 1
	Fill(img, image.Rect(px(last)-1, py(vals[last])-1, px(last)+2, py(vals[last])+2), Ink)
	return img
}

// BarStrip draws vals as vertical bars. Negative values hang below a
// zero baseline when the range crosses zero.
func BarStrip(vals []float64, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	Fill(img, img.Bounds(), Field)
	Border(img, img.Bounds(), 1, Faint)
	if len(vals) == 0 || w < 8 || h < 8 {
		return img
	}
	lo, hi := minMax(vals)
	if lo > 0 {
		lo = 0
	}
	if hi < 0 {
		hi = 0
	}
	span := hi - lo
	if span == 0 {
		span = 1
	}
	zeroY := 4 + int(float64(h-8)*hi/span)
	bw := (w - 6) / len(vals)
	if bw < 1 {
		bw = 1
	}
	gap := 1
	if bw <= 2 {
		gap = 0
	}
	for i, v := range vals {
		x := 3 + i*bw
		vy := 4 + int(float64(h-8)*(hi-v)/span)
		r := image.Rect(x, vy, x+bw-gap, zeroY)
		if v < 0 {
			r = image.Rect(x, zeroY, x+bw-gap, vy)
		}
		if r.Dy() == 0 {
			r.Max.Y++
		}
		Fill(img, r, Sage)
	}
	// Zero baseline.
	Fill(img, image.Rect(2, zeroY, w-2, zeroY+1), Faint)
	return img
}

func minMax(vals []float64) (float64, float64) {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, v := range vals {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	return lo, hi
}

// line draws a 1px Bresenham segment.
func line(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	dx, dy := absInt(x1-x0), -absInt(y1-y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	e := dx + dy
	for {
		Fill(img, image.Rect(x0, y0, x0+1, y0+1), c)
		if x0 == x1 && y0 == y1 {
			return
		}
		// e2 must be captured before either branch mutates e, or steep
		// lines overshoot the endpoint and never terminate.
		e2 := 2 * e
		if e2 >= dy {
			e += dy
			x0 += sx
		}
		if e2 <= dx {
			e += dx
			y0 += sy
		}
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
