package draw

import (
	"image"
	"image/color"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// directText is the pre-cache implementation, kept as the reference.
func directText(img *image.RGBA, x, y int, s string, bold bool, size float64, c color.RGBA) {
	face := Face(bold, size)
	d := font.Drawer{Dst: img, Src: image.NewUniform(c), Face: face, Dot: fixed.P(x, y)}
	d.DrawString(s)
}

// TestTextCacheMatchesDirect pins the fidelity of the glyph-run cache.
//
// The cache composites a run's glyphs into one alpha mask and blits that
// once, where the direct path blends each glyph onto the destination in
// turn. Those are identical for non-overlapping glyphs but can differ by one
// least-significant bit where antialiased glyphs overlap, because the alpha
// arithmetic rounds at a different point. One LSB is invisible and worth a
// ~7x speedup on the hottest operation in the paint path (GGWM-012), but it
// must not silently grow: this test fails if any channel drifts by more
// than 1/255.
func TestTextCacheMatchesDirect(t *testing.T) {
	cases := []struct {
		s    string
		bold bool
		size float64
	}{
		{"AN ORDINARY WINDOW TITLE", true, 11},
		{"about", false, 13},
		{"colors demo", false, 12},
		{"Wj_gy|@# MMM iii", false, 10},
		{"x", true, 10},
		{"", false, 11},
	}
	const tolerance = 1
	for _, tc := range cases {
		a := image.NewRGBA(image.Rect(0, 0, 400, 40))
		b := image.NewRGBA(image.Rect(0, 0, 400, 40))
		bg := color.RGBA{255, 255, 255, 255}
		Fill(a, a.Bounds(), bg)
		Fill(b, b.Bounds(), bg)
		ink := color.RGBA{20, 20, 20, 255}

		directText(a, 10, 25, tc.s, tc.bold, tc.size, ink)
		ClearTextCache()
		drawTextCached(b, 10, 25, tc.s, tc.bold, tc.size, ink)

		maxd := 0
		for i := range a.Pix {
			d := int(a.Pix[i]) - int(b.Pix[i])
			if d < 0 {
				d = -d
			}
			if d > maxd {
				maxd = d
			}
		}
		if maxd > tolerance {
			t.Errorf("%q bold=%v size=%v: max channel delta %d exceeds tolerance %d",
				tc.s, tc.bold, tc.size, maxd, tolerance)
		}
	}
}

// TestTextCacheAdvanceMatches keeps layout stable: callers position the next
// run from the returned advance, so it must equal the uncached measurement.
func TestTextCacheAdvanceMatches(t *testing.T) {
	for _, s := range []string{"AN ORDINARY WINDOW TITLE", "about", "x", ""} {
		img := image.NewRGBA(image.Rect(0, 0, 400, 40))
		ClearTextCache()
		got := drawTextCached(img, 10, 25, s, true, 11, color.RGBA{0, 0, 0, 255})
		want := TextWidth(s, true, 11)
		if got != want {
			t.Errorf("%q: advance %d, TextWidth %d", s, got, want)
		}
	}
}

// TestTextCacheEviction exercises the bound.
func TestTextCacheEviction(t *testing.T) {
	ClearTextCache()
	img := image.NewRGBA(image.Rect(0, 0, 200, 40))
	for i := 0; i < textCacheMax+50; i++ {
		drawTextCached(img, 0, 20, string(rune('a'+i%26))+string(rune('A'+i%26))+itoa(i), false, 11, color.RGBA{0, 0, 0, 255})
	}
	textMu.Lock()
	n := len(textCache)
	textMu.Unlock()
	if n > textCacheMax {
		t.Errorf("cache grew to %d, bound is %d", n, textCacheMax)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
