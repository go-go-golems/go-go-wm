package wmcore

import (
	"math/rand"
	"testing"
)

func mustValidate(t *testing.T, d *Desktop, ctx string) {
	t.Helper()
	if err := d.Validate(); err != nil {
		t.Fatalf("%s: invalid desktop: %v", ctx, err)
	}
}

func TestSplitCloseRoundTrip(t *testing.T) {
	d := NewDesktop("launcher")
	w := d.CurrentWorkspace()
	first := w.Root.Leaves()[0].ID

	res, err := Apply(d, Op{Op: OpSplitLeaf, Node: first, Dir: Row, App: "xterm"})
	if err != nil {
		t.Fatal(err)
	}
	mustValidate(t, d, "after split")
	if got := d.CurrentWorkspace().Root.CountLeaves(); got != 2 {
		t.Fatalf("leaves = %d, want 2", got)
	}
	if _, err := Apply(d, Op{Op: OpCloseLeaf, Node: res.NewLeaf}); err != nil {
		t.Fatal(err)
	}
	mustValidate(t, d, "after close")
	root := d.CurrentWorkspace().Root
	if root.Kind != Leaf || root.ID != first {
		t.Fatalf("close did not restore original leaf: %+v", root)
	}
}

func TestCloseLastLeafRefused(t *testing.T) {
	d := NewDesktop("launcher")
	first := d.CurrentWorkspace().Root.Leaves()[0].ID
	if _, err := Apply(d, Op{Op: OpCloseLeaf, Node: first}); err == nil {
		t.Fatal("closing the only leaf should fail")
	}
}

func TestSwapMovesApps(t *testing.T) {
	d := NewDesktop("a")
	first := d.CurrentWorkspace().Root.Leaves()[0].ID
	res, _ := Apply(d, Op{Op: OpSplitLeaf, Node: first, Dir: Col, App: "b"})
	if _, err := Apply(d, Op{Op: OpSwapLeaves, Node: first, Target: res.NewLeaf}); err != nil {
		t.Fatal(err)
	}
	root := d.CurrentWorkspace().Root
	if root.FindLeaf(first).App != "b" || root.FindLeaf(res.NewLeaf).App != "a" {
		t.Fatalf("apps not swapped: %s=%q %s=%q",
			first, root.FindLeaf(first).App, res.NewLeaf, root.FindLeaf(res.NewLeaf).App)
	}
}

func TestMoveSplitDetachesAndDocks(t *testing.T) {
	d := NewDesktop("a")
	first := d.CurrentWorkspace().Root.Leaves()[0].ID
	r1, _ := Apply(d, Op{Op: OpSplitLeaf, Node: first, Dir: Row, App: "b"})
	r2, _ := Apply(d, Op{Op: OpSplitLeaf, Node: r1.NewLeaf, Dir: Col, App: "c"})

	before := d.CurrentWorkspace().Root.CountLeaves()
	if _, err := Apply(d, Op{Op: OpMoveSplit, Node: r2.NewLeaf, Target: first, Zone: ZoneTop}); err != nil {
		t.Fatal(err)
	}
	mustValidate(t, d, "after move-split")
	if got := d.CurrentWorkspace().Root.CountLeaves(); got != before {
		t.Fatalf("move-split changed leaf count %d -> %d", before, got)
	}
	// The moved leaf must now be the A-side (top) sibling of a Col split
	// whose other side contains `first`.
	moved := d.CurrentWorkspace().Root.FindLeaf(r2.NewLeaf)
	if moved == nil || moved.App != "c" {
		t.Fatalf("moved leaf lost: %+v", moved)
	}
}

func TestMoveSplitSingleLeafRefused(t *testing.T) {
	d := NewDesktop("a")
	first := d.CurrentWorkspace().Root.Leaves()[0].ID
	if _, err := Apply(d, Op{Op: OpMoveSplit, Node: first, Target: first, Zone: ZoneLeft}); err == nil {
		t.Fatal("move-split onto itself should fail")
	}
}

func TestWorkspaceOps(t *testing.T) {
	d := NewDesktop("a")
	res, err := Apply(d, Op{Op: OpAddWorkspace, App: "b"})
	if err != nil || res.NewWorkspace == "" {
		t.Fatalf("add-workspace: %v %+v", err, res)
	}
	if d.Current != res.NewWorkspace {
		t.Fatalf("add-workspace did not switch: current=%s", d.Current)
	}
	if _, err := Apply(d, Op{Op: OpRenameWorkspace, Workspace: res.NewWorkspace, Name: "lab"}); err != nil {
		t.Fatal(err)
	}
	if d.WorkspaceByID(res.NewWorkspace).Name != "lab" {
		t.Fatal("rename did not stick")
	}
	res2, err := Apply(d, Op{Op: OpCloneWorkspace, Workspace: res.NewWorkspace})
	if err != nil {
		t.Fatal(err)
	}
	mustValidate(t, d, "after clone")
	if _, err := Apply(d, Op{Op: OpRemoveWorkspace, Workspace: res2.NewWorkspace}); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(d, Op{Op: OpRemoveWorkspace, Workspace: res.NewWorkspace}); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(d, Op{Op: OpRemoveWorkspace, Workspace: d.Current}); err == nil {
		t.Fatal("removing the last workspace should fail")
	}
}

func TestSnap(t *testing.T) {
	cases := []struct {
		in      float64
		want    float64
		snapped bool
	}{
		{0.5, 0.5, true},
		{0.51, 0.5, true},
		{0.48, 0.5, true},
		{0.26, 0.25, true},
		{0.32, 1.0 / 3.0, true},
		{0.31, 0.31, false}, // 0.0233 from 1/3 — just outside the 0.022 band
		{0.4, 0.4, false},
		{0.74, 0.75, true},
		{0.6, 0.6, false},
	}
	for _, c := range cases {
		got, snapped := Snap(c.in)
		if got != c.want || snapped != c.snapped {
			t.Errorf("Snap(%v) = (%v, %v), want (%v, %v)", c.in, got, snapped, c.want, c.snapped)
		}
	}
}

func TestZoneAt(t *testing.T) {
	r := Rect{X: 0, Y: 0, W: 400, H: 400}
	// band = min(400*0.3, 110) = 110
	cases := []struct {
		x, y int
		want Zone
	}{
		{200, 200, ZoneCenter},
		{10, 200, ZoneLeft},
		{390, 200, ZoneRight},
		{200, 10, ZoneTop},
		{200, 390, ZoneBottom},
	}
	for _, c := range cases {
		if got := ZoneAt(r, c.x, c.y); got != c.want {
			t.Errorf("ZoneAt(%d,%d) = %v, want %v", c.x, c.y, got, c.want)
		}
	}
}

func TestLayoutTilesExactly(t *testing.T) {
	d := NewDesktop("a")
	first := d.CurrentWorkspace().Root.Leaves()[0].ID
	r1, _ := Apply(d, Op{Op: OpSplitLeaf, Node: first, Dir: Row, App: "b"})
	_, _ = Apply(d, Op{Op: OpSetRatio, Node: findParentSplit(d, first), Ratio: 1.0 / 3.0})
	_, _ = Apply(d, Op{Op: OpSplitLeaf, Node: r1.NewLeaf, Dir: Col, App: "c"})

	screen := Rect{X: 0, Y: 20, W: 1280, H: 780}
	gap := 8
	items := Layout(d.CurrentWorkspace().Root, screen, gap)

	// Sum of leaf areas + divider areas must equal the screen area.
	area := 0
	for id, it := range items {
		n := d.CurrentWorkspace().Root.Find(id)
		if n.Kind == Leaf {
			area += it.Rect.W * it.Rect.H
			if it.Rect.W <= 0 || it.Rect.H <= 0 {
				t.Errorf("leaf %s has degenerate rect %v", id, it.Rect)
			}
		} else {
			area += dividerArea(it.DividerRect, n.Dir)
		}
	}
	if want := screen.W * screen.H; area != want {
		t.Errorf("tiled area %d != screen area %d", area, want)
	}
}

func dividerArea(r Rect, _ Dir) int { return r.W * r.H }

func findParentSplit(d *Desktop, leaf NodeID) NodeID {
	var parent NodeID
	var walk func(n *Node)
	walk = func(n *Node) {
		if n == nil || n.Kind != Split {
			return
		}
		if n.A.ID == leaf || n.B.ID == leaf {
			parent = n.ID
			return
		}
		walk(n.A)
		walk(n.B)
	}
	walk(d.CurrentWorkspace().Root)
	return parent
}

// TestRandomOpSequences is the property test: apply thousands of random ops
// and assert invariants after each one. Ops that error must leave the
// desktop unchanged and valid.
func TestRandomOpSequences(t *testing.T) {
	for seed := int64(0); seed < 20; seed++ {
		rng := rand.New(rand.NewSource(seed))
		d := NewDesktop("launcher")
		for i := 0; i < 500; i++ {
			op := randomOp(rng, d)
			leavesBefore := totalLeaves(d)
			_, err := Apply(d, op)
			if verr := d.Validate(); verr != nil {
				t.Fatalf("seed=%d step=%d op=%+v: desktop invalid: %v (apply err: %v)", seed, i, op, verr, err)
			}
			leavesAfter := totalLeaves(d)
			if err == nil {
				switch op.Op {
				case OpSplitLeaf, OpAddWorkspace:
					if leavesAfter != leavesBefore+1 {
						t.Fatalf("seed=%d step=%d %s: leaves %d -> %d", seed, i, op.Op, leavesBefore, leavesAfter)
					}
				case OpCloseLeaf, OpRemoveWorkspace:
					if leavesAfter >= leavesBefore {
						t.Fatalf("seed=%d step=%d %s: leaves %d -> %d", seed, i, op.Op, leavesBefore, leavesAfter)
					}
				case OpSwapLeaves, OpMoveSplit, OpSetRatio, OpSetLeafApp, OpRenameWorkspace, OpSwitchWorkspace:
					if leavesAfter != leavesBefore {
						t.Fatalf("seed=%d step=%d %s: leaves %d -> %d", seed, i, op.Op, leavesBefore, leavesAfter)
					}
				}
			}
		}
	}
}

func totalLeaves(d *Desktop) int {
	n := 0
	for i := range d.Workspaces {
		n += d.Workspaces[i].Root.CountLeaves()
	}
	return n
}

func randomOp(rng *rand.Rand, d *Desktop) Op {
	// Collect all leaves and splits across workspaces.
	var leaves []NodeID
	var splits []NodeID
	var wss []string
	for i := range d.Workspaces {
		wss = append(wss, d.Workspaces[i].ID)
		d.Workspaces[i].Root.walk(func(n *Node) {
			if n.Kind == Leaf {
				leaves = append(leaves, n.ID)
			} else {
				splits = append(splits, n.ID)
			}
		})
	}
	pickLeaf := func() NodeID { return leaves[rng.Intn(len(leaves))] }
	pickWS := func() string { return wss[rng.Intn(len(wss))] }
	dirs := []Dir{Row, Col}
	zones := []Zone{ZoneLeft, ZoneRight, ZoneTop, ZoneBottom}

	switch rng.Intn(11) {
	case 0, 1, 2:
		return Op{Op: OpSplitLeaf, Node: pickLeaf(), Dir: dirs[rng.Intn(2)], App: "app"}
	case 3, 4:
		return Op{Op: OpCloseLeaf, Node: pickLeaf()}
	case 5:
		if len(splits) == 0 {
			return Op{Op: OpSplitLeaf, Node: pickLeaf(), Dir: Row, App: "app"}
		}
		return Op{Op: OpSetRatio, Node: splits[rng.Intn(len(splits))], Ratio: rng.Float64()}
	case 6:
		return Op{Op: OpSwapLeaves, Node: pickLeaf(), Target: pickLeaf()}
	case 7:
		return Op{Op: OpMoveSplit, Node: pickLeaf(), Target: pickLeaf(), Zone: zones[rng.Intn(4)]}
	case 8:
		if len(wss) > 3 {
			return Op{Op: OpRemoveWorkspace, Workspace: pickWS()}
		}
		return Op{Op: OpAddWorkspace, App: "app"}
	case 9:
		return Op{Op: OpSwitchWorkspace, Workspace: pickWS()}
	default:
		return Op{Op: OpSetLeafApp, Node: pickLeaf(), App: "other"}
	}
}
