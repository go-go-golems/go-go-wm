package apps

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// TestGoldenDelta reports how far the current renderer is from the committed
// goldens. Diagnostic only: run with -run TestGoldenDelta -v after a failing
// golden test to size the change before deciding whether to regenerate.
func TestGoldenDelta(t *testing.T) {
	for _, name := range []string{"builtin-about", "demo-colors", "builtin-launcher", "demo-notes"} {
		gp := filepath.Join("testdata", name+".png")
		np := gp + ".got.png"
		if _, err := os.Stat(np); err != nil {
			t.Logf("%-20s no .got.png (test passed)", name)
			continue
		}
		a, err := load(gp)
		if err != nil {
			t.Fatalf("%s: %v", gp, err)
		}
		b, err := load(np)
		if err != nil {
			t.Fatalf("%s: %v", np, err)
		}
		if a.Bounds() != b.Bounds() {
			t.Errorf("%s: bounds differ %v vs %v", name, a.Bounds(), b.Bounds())
			continue
		}
		var diffPx, maxDelta int
		minX, minY := 1<<30, 1<<30
		maxX, maxY := -1, -1
		bounds := a.Bounds()
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				ar, ag, ab, aa := a.At(x, y).RGBA()
				br, bg, bb, ba := b.At(x, y).RGBA()
				d := absd(ar, br) + absd(ag, bg) + absd(ab, bb) + absd(aa, ba)
				if d == 0 {
					continue
				}
				diffPx++
				for _, c := range []int{absd(ar, br), absd(ag, bg), absd(ab, bb), absd(aa, ba)} {
					if c>>8 > maxDelta {
						maxDelta = c >> 8
					}
				}
				if x < minX {
					minX = x
				}
				if y < minY {
					minY = y
				}
				if x > maxX {
					maxX = x
				}
				if y > maxY {
					maxY = y
				}
			}
		}
		total := bounds.Dx() * bounds.Dy()
		t.Logf("%-20s diff=%d/%d px (%.4f%%) maxdelta=%d bbox=(%d,%d)-(%d,%d)",
			name, diffPx, total, 100*float64(diffPx)/float64(total), maxDelta, minX, minY, maxX, maxY)
	}
}

func absd(a, b uint32) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func load(p string) (image.Image, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return png.Decode(f)
}
