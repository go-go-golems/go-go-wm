package wmcore

import "fmt"

// Rect is a pixel rectangle (X11 convention: origin top-left).
type Rect struct {
	X, Y, W, H int
}

func (r Rect) String() string { return fmt.Sprintf("%dx%d+%d+%d", r.W, r.H, r.X, r.Y) }

// Contains reports whether the point lies inside the rectangle.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// Snapping: dividers stick at these fractions within a Stick-wide band,
// Blender-style (ports SNAPS/STICK/snapFrac, pbui-shell.jsx:166-169).
var Snaps = []float64{0.25, 1.0 / 3.0, 0.5, 2.0 / 3.0, 0.75}

const Stick = 0.022

// Snap returns the (possibly snapped) fraction and whether it snapped.
func Snap(f float64) (float64, bool) {
	for _, s := range Snaps {
		d := f - s
		if d < 0 {
			d = -d
		}
		if d < Stick {
			return s, true
		}
	}
	return f, false
}

// Zone classifies where inside a tile a drag hovers: the center swaps apps,
// the edges split-dock (ports zoneFor, pbui-shell.jsx:612-621).
type Zone string

const (
	ZoneCenter Zone = "center"
	ZoneLeft   Zone = "left"
	ZoneRight  Zone = "right"
	ZoneTop    Zone = "top"
	ZoneBottom Zone = "bottom"
)

// ZoneAt maps a pointer position inside r to a drop zone. The edge band is
// min(30% of the smaller dimension, 110px), as in the prototype.
func ZoneAt(r Rect, x, y int) Zone {
	dl, dr := x-r.X, r.X+r.W-x
	dt, db := y-r.Y, r.Y+r.H-y
	band := r.W
	if r.H < band {
		band = r.H
	}
	band = band * 3 / 10
	if band > 110 {
		band = 110
	}
	m := min4(dl, dr, dt, db)
	if m > band {
		return ZoneCenter
	}
	switch m {
	case dl:
		return ZoneLeft
	case dr:
		return ZoneRight
	case dt:
		return ZoneTop
	default:
		return ZoneBottom
	}
}

func min4(a, b, c, d int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	if d < m {
		m = d
	}
	return m
}

// LayoutItem describes one laid-out node.
type LayoutItem struct {
	Rect Rect
	// For splits, DividerRect is the gap strip between the children (the
	// grabbable divider); zero for leaves.
	DividerRect Rect
}

// Layout computes pixel rectangles for every node in the tree. A split
// divides its rect at Ratio along Dir, reserving gap pixels for the divider
// between the children; leaves tile the workspace rect exactly minus
// dividers. The browser's flexbox did this implicitly for the prototype.
func Layout(root *Node, r Rect, gap int) map[NodeID]LayoutItem {
	out := map[NodeID]LayoutItem{}
	layoutInto(root, r, gap, out)
	return out
}

func layoutInto(n *Node, r Rect, gap int, out map[NodeID]LayoutItem) {
	if n == nil {
		return
	}
	item := LayoutItem{Rect: r}
	if n.Kind == Leaf {
		out[n.ID] = item
		return
	}
	if n.Dir == Row {
		avail := r.W - gap
		if avail < 2 {
			avail = 2
		}
		wa := int(float64(avail) * n.Ratio)
		if wa < 1 {
			wa = 1
		}
		if wa > avail-1 {
			wa = avail - 1
		}
		item.DividerRect = Rect{X: r.X + wa, Y: r.Y, W: gap, H: r.H}
		out[n.ID] = item
		layoutInto(n.A, Rect{X: r.X, Y: r.Y, W: wa, H: r.H}, gap, out)
		layoutInto(n.B, Rect{X: r.X + wa + gap, Y: r.Y, W: avail - wa, H: r.H}, gap, out)
		return
	}
	avail := r.H - gap
	if avail < 2 {
		avail = 2
	}
	ha := int(float64(avail) * n.Ratio)
	if ha < 1 {
		ha = 1
	}
	if ha > avail-1 {
		ha = avail - 1
	}
	item.DividerRect = Rect{X: r.X, Y: r.Y + ha, W: r.W, H: gap}
	out[n.ID] = item
	layoutInto(n.A, Rect{X: r.X, Y: r.Y, W: r.W, H: ha}, gap, out)
	layoutInto(n.B, Rect{X: r.X, Y: r.Y + ha + gap, W: r.W, H: avail - ha}, gap, out)
}

// RatioForPointer converts a pointer position over a split's rect into a
// ratio (used by divider drags), clamped to the ratio bounds.
func RatioForPointer(splitRect Rect, dir Dir, x, y int) float64 {
	var f float64
	if dir == Row {
		if splitRect.W == 0 {
			return 0.5
		}
		f = float64(x-splitRect.X) / float64(splitRect.W)
	} else {
		if splitRect.H == 0 {
			return 0.5
		}
		f = float64(y-splitRect.Y) / float64(splitRect.H)
	}
	return clampRatio(f)
}
