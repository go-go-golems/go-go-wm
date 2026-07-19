package wmcore

import "testing"

func TestMoveLeafAcrossWorkspaces(t *testing.T) {
	d := NewDesktop("editor")
	srcWS := d.Workspaces[0].ID
	firstLeaf := d.Workspaces[0].Root.ID
	// Second leaf in source so the detach path (not only-leaf) runs.
	res, err := Apply(d, Op{Op: OpSplitLeaf, Node: firstLeaf, Dir: Row, App: "terminal"})
	if err != nil {
		t.Fatal(err)
	}
	movee := res.NewLeaf
	// Destination workspace.
	res, err = Apply(d, Op{Op: OpAddWorkspace, App: "notes"})
	if err != nil {
		t.Fatal(err)
	}
	dstWS := res.NewWorkspace

	if _, err := Apply(d, Op{Op: OpMoveLeaf, Node: movee, Workspace: dstWS, Dir: Col}); err != nil {
		t.Fatalf("move-leaf: %v", err)
	}
	if d.WorkspaceByID(srcWS).Root.FindLeaf(movee) != nil {
		t.Fatal("leaf still in source workspace")
	}
	got := d.WorkspaceByID(dstWS).Root.FindLeaf(movee)
	if got == nil || got.App != "terminal" {
		t.Fatalf("leaf not grafted with app intact: %+v", got)
	}
	if d.WorkspaceByID(dstWS).Root.Dir != Col {
		t.Fatal("graft ignored dir")
	}
}

func TestMoveLeafOnlyLeafLeavesLauncher(t *testing.T) {
	d := NewDesktop("editor")
	srcWS := d.Workspaces[0].ID
	leaf := d.Workspaces[0].Root.ID
	res, err := Apply(d, Op{Op: OpAddWorkspace, App: "x"})
	if err != nil {
		t.Fatal(err)
	}
	res2, err := Apply(d, Op{Op: OpMoveLeaf, Node: leaf, Workspace: res.NewWorkspace})
	if err != nil {
		t.Fatalf("move only leaf: %v", err)
	}
	src := d.WorkspaceByID(srcWS)
	if src.Root.Kind != Leaf || src.Root.App != "" || src.Root.ID != res2.NewLeaf {
		t.Fatalf("source should hold a fresh launcher leaf, got %+v", src.Root)
	}
}

func TestMoveLeafRejections(t *testing.T) {
	d := NewDesktop("a")
	leaf := d.Workspaces[0].Root.ID
	ws := d.Workspaces[0].ID
	if _, err := Apply(d, Op{Op: OpMoveLeaf, Node: leaf, Workspace: ws}); err == nil {
		t.Fatal("same-workspace move should fail")
	}
	if _, err := Apply(d, Op{Op: OpMoveLeaf, Node: "nope", Workspace: ws}); err == nil {
		t.Fatal("unknown leaf should fail")
	}
	res, _ := Apply(d, Op{Op: OpAddWorkspace, App: "b"})
	if _, err := Apply(d, Op{Op: OpMoveLeaf, Node: leaf, Workspace: res.NewWorkspace, Target: "ghost"}); err == nil {
		t.Fatal("bad target should fail")
	}
	// Failed op must leave the desktop unchanged.
	if d.WorkspaceByID(d.Workspaces[0].ID).Root.FindLeaf(leaf) == nil {
		t.Fatal("failed move mutated the desktop")
	}
}
