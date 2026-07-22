package wmcore

import "testing"

// TestBuildIndexMatchesFind pins the invariant that makes the O(n^2) removal
// safe: for every node the layout produces, the index and Find agree.
func TestBuildIndexMatchesFind(t *testing.T) {
	d := NewDesktop("term")
	for i := 0; i < 4; i++ {
		ws := d.CurrentWorkspace()
		if ws == nil {
			t.Fatal("no current workspace")
		}
		dir := Row
		if i%2 == 1 {
			dir = Col
		}
		leaf := ws.Root.Leaves()[0]
		if _, err := Apply(d, Op{Op: OpSplitLeaf, Node: leaf.ID, Dir: dir, App: "term"}); err != nil {
			t.Fatalf("split %d: %v", i, err)
		}
	}

	ws := d.CurrentWorkspace()
	idx := BuildIndex(ws.Root)
	items := Layout(ws.Root, Rect{X: 0, Y: 0, W: 1280, H: 800}, 6)

	if len(items) != len(idx) {
		t.Fatalf("layout produced %d items but index has %d nodes", len(items), len(idx))
	}
	for id := range items {
		want := ws.Root.Find(id)
		got := idx[id]
		if want == nil {
			t.Fatalf("Find(%s) returned nil for a laid-out node", id)
		}
		if got != want {
			t.Fatalf("index[%s] = %p, Find = %p", id, got, want)
		}
		if got.Kind != want.Kind || got.Dir != want.Dir {
			t.Fatalf("index[%s] disagrees with Find on kind/dir", id)
		}
	}
	if idx["nope"] != nil || ws.Root.Find("nope") != nil {
		t.Fatal("unknown id resolved")
	}
}
