package draw

import (
	"fmt"
	"image"
	"testing"
)

// These are the per-paint costs the WM pays for every frame it repaints.
// A divider drag repaints two panes per admitted tick, so multiply by two
// and compare against a 16.67ms display interval (GGWM-012).

func BenchmarkFill(b *testing.B) {
	for _, sz := range [][2]int{{1272, 664}, {1920, 1080}} {
		img := image.NewRGBA(image.Rect(0, 0, sz[0], sz[1]))
		b.Run(fmt.Sprintf("%dx%d", sz[0], sz[1]), func(b *testing.B) {
			b.SetBytes(int64(sz[0] * sz[1] * 4))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				Fill(img, img.Bounds(), Current().Pane)
			}
		})
	}
}

// BenchmarkTitleStripRender allocates a fresh image per call, which is what
// paintFrame does on every repaint. This is the cost that a title child
// window would make independent of pane size.
func BenchmarkTitleStripRender(b *testing.B) {
	for _, w := range []int{1272, 1920} {
		s := TitleStrip{Title: "an ordinary window title", Width: w, Focused: true}
		b.Run(fmt.Sprintf("w=%d", w), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = s.Render()
			}
		})
	}
}

func BenchmarkText(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 600, 40))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Text(img, 4, 20, "an ordinary window title", false, 13, Current().Ink)
	}
}

func BenchmarkTextWidth(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = TextWidth("an ordinary window title", false, 13)
	}
}

// BenchmarkTextCached is the cached path; compare with BenchmarkText.
func BenchmarkTextCached(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 600, 40))
	ClearTextCache()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		drawTextCached(img, 4, 20, "an ordinary window title", false, 13, Current().Ink)
	}
}
