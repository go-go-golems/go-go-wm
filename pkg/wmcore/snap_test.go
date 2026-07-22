package wmcore

import "testing"

// TestSnapDeadZone pins the cost of snapping in pointer travel.
//
// A divider snaps while |ratio - snapPoint| < Stick. The band is symmetric,
// so a pointer crossing it enters at one edge and leaves at the other: the
// divider holds still for 2*Stick worth of travel. At the original 0.022 that
// was 56px on a 1272px split, which users read as the drag having broken
// rather than as stickiness (GGWM-012 Step 18).
func TestSnapDeadZone(t *testing.T) {
	r := Rect{X: 4, Y: 28, W: 1272, H: 664}
	// Sweep across the 1/2 snap point one pixel at a time.
	// Count only runs where Snap actually engaged. Near the extremes the
	// ratio is clamped, which also holds the divider still but is a
	// different mechanism and not what this test is about.
	held, maxHeld := 0, 0
	prev := -1.0
	for x := r.X; x < r.X+r.W; x++ {
		f := RatioForPointer(r, Row, x, 0)
		s, snapped := Snap(f)
		if snapped && s == prev {
			held++
			if held > maxHeld {
				maxHeld = held
			}
		} else {
			held = 0
		}
		prev = s
	}
	wantMax := int(2*Stick*float64(r.W)) + 2 // +2 for rounding at both edges
	if maxHeld > wantMax {
		t.Errorf("divider held still for %d px; expected at most ~%d px (2*Stick*width)", maxHeld, wantMax)
	}
	t.Logf("Stick=%.3f  widest dead zone on a %dpx split: %d px", Stick, r.W, maxHeld)
}

// TestSnapStillSnaps guards against tuning the stickiness away entirely.
func TestSnapStillSnaps(t *testing.T) {
	for _, target := range Snaps {
		if got, ok := Snap(target); !ok || got != target {
			t.Errorf("Snap(%v) = %v, %v; want exact snap", target, got, ok)
		}
	}
	if _, ok := Snap(0.42); ok {
		t.Error("Snap(0.42) should not snap; 0.42 is not near a snap point")
	}
}
