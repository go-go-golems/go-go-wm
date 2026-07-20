package wmcore

import "testing"

// buildTestTree makes editor | (trace / listener): a row split whose B
// side is a column split — the golden.js shape.
func buildTestTree(t *testing.T) (*Desktop, NodeID, NodeID, NodeID) {
	t.Helper()
	d := NewDesktop("editor")
	root := d.CurrentWorkspace().Root
	res, err := Apply(d, Op{Op: OpSplitLeaf, Node: root.ID, Dir: Row, App: "trace"})
	if err != nil {
		t.Fatal(err)
	}
	right := res.NewLeaf
	res2, err := Apply(d, Op{Op: OpSplitLeaf, Node: right, Dir: Col, App: "listener"})
	if err != nil {
		t.Fatal(err)
	}
	return d, root.ID, right, res2.NewLeaf
}

func TestNeighborLeafDirections(t *testing.T) {
	d, editor, trace, listener := buildTestTree(t)
	ws := d.CurrentWorkspace()
	area := Rect{X: 0, Y: 0, W: 1200, H: 800}

	cases := []struct {
		from NodeID
		dir  string
		want NodeID
	}{
		{trace, "left", editor},    // top-right → left
		{listener, "left", editor}, // bottom-right → left
		{editor, "right", trace},   // exact tie on distance and cross → topmost wins
		{trace, "down", listener},  // top-right → below
		{listener, "up", trace},    // bottom-right → above
		{editor, "left", ""},       // workspace edge
		{trace, "up", ""},          // top edge
		{listener, "down", ""},     // bottom edge
		{editor, "up", ""},         // nothing above
		{trace, "sideways", ""},    // bad direction
		{"missing", "left", ""},    // unknown source
	}
	for _, c := range cases {
		got := NeighborLeaf(ws.Root, area, 8, c.from, c.dir)
		if got != c.want {
			t.Errorf("NeighborLeaf(%s, %s) = %q, want %q", c.from, c.dir, got, c.want)
		}
	}
}

func TestNeighborLeafPicksNearestWithCrossOverlap(t *testing.T) {
	// editor | (trace / listener), focus editor: both right-side leaves
	// overlap editor's vertical band; the tie on horizontal distance must
	// break toward the one whose center is nearer editor's center.
	d, editor, trace, listener := buildTestTree(t)
	ws := d.CurrentWorkspace()
	area := Rect{X: 0, Y: 0, W: 1200, H: 800}
	// Shrink the top-right pane: listener's center is now closer to
	// editor's center, so "right" should pick listener.
	split := findSplitOf(ws.Root, trace)
	if split == "" {
		t.Fatal("no parent split for trace")
	}
	if _, err := Apply(d, Op{Op: OpSetRatio, Node: split, Ratio: 0.2}); err != nil {
		t.Fatal(err)
	}
	if got := NeighborLeaf(ws.Root, area, 8, editor, "right"); got != listener {
		t.Errorf("right neighbor after reshape = %q, want %q (listener)", got, listener)
	}
}

func findSplitOf(root *Node, leaf NodeID) NodeID {
	var out NodeID
	var walk func(n *Node)
	walk = func(n *Node) {
		if n == nil || n.Kind != Split {
			return
		}
		if (n.A != nil && n.A.ID == leaf) || (n.B != nil && n.B.ID == leaf) {
			out = n.ID
			return
		}
		walk(n.A)
		walk(n.B)
	}
	walk(root)
	return out
}
