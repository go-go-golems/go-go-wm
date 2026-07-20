package wmcore

// Directional navigation (GGWM-004 H3): i3-style `focus left` needs a
// geometric answer, not a tree answer — the leaf whose rectangle sits
// next to the focused one on screen, which the split tree does not
// directly encode. NeighborLeaf works on the same Layout() geometry the
// WM paints from, so what you see is what you focus.

// NeighborLeaf returns the leaf nearest to `from` in direction dir
// ("left" | "right" | "up" | "down"), or "" when the workspace edge is
// reached. Candidates must overlap the source on the cross axis (else
// "left" could jump diagonally), and among those the smallest
// edge-to-edge distance wins, with the cross-axis center distance as
// the tie-breaker.
func NeighborLeaf(root *Node, area Rect, gap int, from NodeID, dir string) NodeID {
	items := Layout(root, area, gap)
	src, ok := items[from]
	if !ok {
		return ""
	}
	var best NodeID
	var bestRect Rect
	bestDist, bestCross := 1<<30, 1<<30
	for id, item := range items {
		n := root.Find(id)
		if n == nil || n.Kind != Leaf || id == from {
			continue
		}
		r := item.Rect
		var dist, cross int
		switch dir {
		case "left":
			if r.X+r.W > src.Rect.X || !overlaps(r.Y, r.H, src.Rect.Y, src.Rect.H) {
				continue
			}
			dist = src.Rect.X - (r.X + r.W)
			cross = centerDelta(r.Y, r.H, src.Rect.Y, src.Rect.H)
		case "right":
			if r.X < src.Rect.X+src.Rect.W || !overlaps(r.Y, r.H, src.Rect.Y, src.Rect.H) {
				continue
			}
			dist = r.X - (src.Rect.X + src.Rect.W)
			cross = centerDelta(r.Y, r.H, src.Rect.Y, src.Rect.H)
		case "up":
			if r.Y+r.H > src.Rect.Y || !overlaps(r.X, r.W, src.Rect.X, src.Rect.W) {
				continue
			}
			dist = src.Rect.Y - (r.Y + r.H)
			cross = centerDelta(r.X, r.W, src.Rect.X, src.Rect.W)
		case "down":
			if r.Y < src.Rect.Y+src.Rect.H || !overlaps(r.X, r.W, src.Rect.X, src.Rect.W) {
				continue
			}
			dist = r.Y - (src.Rect.Y + src.Rect.H)
			cross = centerDelta(r.X, r.W, src.Rect.X, src.Rect.W)
		default:
			return ""
		}
		better := dist < bestDist ||
			(dist == bestDist && cross < bestCross) ||
			// Full tie: topmost, then leftmost — deterministic regardless
			// of map iteration order.
			(dist == bestDist && cross == bestCross &&
				(best == "" || r.Y < bestRect.Y || (r.Y == bestRect.Y && r.X < bestRect.X)))
		if better {
			best, bestRect, bestDist, bestCross = id, r, dist, cross
		}
	}
	return best
}

// overlaps reports whether [a, a+aw) and [b, b+bw) intersect.
func overlaps(a, aw, b, bw int) bool { return a < b+bw && b < a+aw }

func centerDelta(a, aw, b, bw int) int {
	d := (a + aw/2) - (b + bw/2)
	if d < 0 {
		d = -d
	}
	return d
}
