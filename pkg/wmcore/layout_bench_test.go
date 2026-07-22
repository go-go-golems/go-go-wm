package wmcore

import (
	"fmt"
	"testing"
)

// buildTree grows a workspace to n leaves by repeatedly splitting, giving a
// realistic (unbalanced) shape rather than a perfect binary tree.
func buildTree(tb testing.TB, n int) *Workspace {
	tb.Helper()
	d := NewDesktop("term")
	for i := 1; i < n; i++ {
		ws := d.CurrentWorkspace()
		leaves := ws.Root.Leaves()
		dir := Row
		if i%2 == 1 {
			dir = Col
		}
		leaf := leaves[i%len(leaves)]
		if _, err := Apply(d, Op{Op: OpSplitLeaf, Node: leaf.ID, Dir: dir, App: "term"}); err != nil {
			tb.Fatalf("split: %v", err)
		}
	}
	return d.CurrentWorkspace()
}

var screen = Rect{X: 0, Y: 0, W: 1920, H: 1080}

// BenchmarkLayout is the per-tick cost the divider drag used to pay twice.
func BenchmarkLayout(b *testing.B) {
	for _, n := range []int{2, 4, 8, 16, 32} {
		ws := buildTree(b, n)
		b.Run(fmt.Sprintf("leaves=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = Layout(ws.Root, screen, 6)
			}
		})
	}
}

// BenchmarkFindAll models the OLD reconciliation inner loop: a Root.Find per
// layout item. Compare against BenchmarkIndexAll to see the O(n^2) -> O(n)
// change that GGWM-012 removed from relayoutPaint and syncDividers.
func BenchmarkFindAll(b *testing.B) {
	for _, n := range []int{2, 4, 8, 16, 32} {
		ws := buildTree(b, n)
		items := Layout(ws.Root, screen, 6)
		b.Run(fmt.Sprintf("leaves=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				for id := range items {
					_ = ws.Root.Find(id)
				}
			}
		})
	}
}

// BenchmarkIndexAll is the NEW shape: build the index once, then look up.
func BenchmarkIndexAll(b *testing.B) {
	for _, n := range []int{2, 4, 8, 16, 32} {
		ws := buildTree(b, n)
		items := Layout(ws.Root, screen, 6)
		b.Run(fmt.Sprintf("leaves=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				idx := BuildIndex(ws.Root)
				for id := range items {
					_ = idx[id]
				}
			}
		})
	}
}

// BenchmarkNeighborLeaf is event-rate, not frame-rate, but shares the same
// Layout + Find shape and is worth a baseline.
func BenchmarkNeighborLeaf(b *testing.B) {
	ws := buildTree(b, 16)
	from := ws.Root.Leaves()[0].ID
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = NeighborLeaf(ws.Root, screen, 6, from, "right")
	}
}

// BenchmarkIndexReuseAll is the shipped shape: one scratch map per WM,
// refilled per pass. Compare with BenchmarkFindAll (old) and
// BenchmarkIndexAll (allocating).
func BenchmarkIndexReuseAll(b *testing.B) {
	for _, n := range []int{2, 4, 8, 16, 32} {
		ws := buildTree(b, n)
		items := Layout(ws.Root, screen, 6)
		scratch := Index{}
		b.Run(fmt.Sprintf("leaves=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				idx := BuildIndexInto(ws.Root, scratch)
				for id := range items {
					_ = idx[id]
				}
			}
		})
	}
}
