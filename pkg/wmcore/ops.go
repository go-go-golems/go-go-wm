package wmcore

import (
	"encoding/json"
	"fmt"
)

// Op is a serializable layout mutation. Every change to a Desktop flows
// through Apply, so the WM event loop, the query IPC, record/replay tests,
// and the future goja bindings all speak one vocabulary. Op names mirror the
// prototype's trace event names (pbui-shell.jsx:451-457).
type Op struct {
	Op string `json:"op"`

	// Common operands; which are used depends on Op.
	Workspace string  `json:"workspace,omitempty"` // workspace id
	Node      NodeID  `json:"node,omitempty"`      // primary node operand
	Target    NodeID  `json:"target,omitempty"`    // secondary node operand
	Dir       Dir     `json:"dir,omitempty"`
	Zone      Zone    `json:"zone,omitempty"`
	Ratio     float64 `json:"ratio,omitempty"`
	App       string  `json:"app,omitempty"`
	Name      string  `json:"name,omitempty"`
}

// Op names.
const (
	OpSplitLeaf       = "split-leaf"       // Node, Dir, App(new leaf's app)
	OpCloseLeaf       = "close-leaf"       // Node
	OpSetRatio        = "set-ratio"        // Node(split), Ratio
	OpSetLeafApp      = "set-leaf-app"     // Node, App
	OpSwapLeaves      = "swap-leaves"      // Node, Target
	OpMoveSplit       = "move-split"       // Node(from), Target, Zone
	OpMoveLeaf        = "move-leaf"        // Node, Workspace(dst), Target?, Dir?
	OpAddWorkspace    = "add-workspace"    // App(first leaf's app)
	OpRemoveWorkspace = "remove-workspace" // Workspace
	OpRenameWorkspace = "rename-workspace" // Workspace, Name
	OpCloneWorkspace  = "clone-workspace"  // Workspace
	OpSwitchWorkspace = "switch-workspace" // Workspace
)

// Result reports what an Op produced (ids minted by the operation).
type Result struct {
	NewLeaf      NodeID `json:"new_leaf,omitempty"`
	NewWorkspace string `json:"new_workspace,omitempty"`
}

// Apply executes op against d in place (workspace trees are replaced
// persistently, desktop bookkeeping mutates). Node-addressed ops locate
// their workspace automatically unless op.Workspace pins one.
func Apply(d *Desktop, op Op) (Result, error) {
	var res Result

	// Resolve the workspace for node-addressed ops.
	wsFor := func(id NodeID) (*Workspace, error) {
		if op.Workspace != "" {
			w := d.WorkspaceByID(op.Workspace)
			if w == nil {
				return nil, fmt.Errorf("%s: no workspace %q", op.Op, op.Workspace)
			}
			return w, nil
		}
		if w := d.FindLeafWorkspace(id); w != nil {
			return w, nil
		}
		// Split nodes are not leaves; scan all workspaces.
		for i := range d.Workspaces {
			if d.Workspaces[i].Root.Find(id) != nil {
				return &d.Workspaces[i], nil
			}
		}
		return nil, fmt.Errorf("%s: node %q not found in any workspace", op.Op, id)
	}

	switch op.Op {
	case OpSplitLeaf:
		w, err := wsFor(op.Node)
		if err != nil {
			return res, err
		}
		root, newLeaf, err := SplitLeaf(w.Root, op.Node, op.Dir, op.App, &d.gen)
		if err != nil {
			return res, err
		}
		w.Root, res.NewLeaf = root, newLeaf
	case OpCloseLeaf:
		w, err := wsFor(op.Node)
		if err != nil {
			return res, err
		}
		root, err := CloseLeaf(w.Root, op.Node)
		if err != nil {
			return res, err
		}
		w.Root = root
	case OpSetRatio:
		w, err := wsFor(op.Node)
		if err != nil {
			return res, err
		}
		root, err := SetRatio(w.Root, op.Node, op.Ratio)
		if err != nil {
			return res, err
		}
		w.Root = root
	case OpSetLeafApp:
		w, err := wsFor(op.Node)
		if err != nil {
			return res, err
		}
		root, err := SetLeafApp(w.Root, op.Node, op.App)
		if err != nil {
			return res, err
		}
		w.Root = root
	case OpSwapLeaves:
		w, err := wsFor(op.Node)
		if err != nil {
			return res, err
		}
		root, err := SwapLeaves(w.Root, op.Node, op.Target)
		if err != nil {
			return res, err
		}
		w.Root = root
	case OpMoveSplit:
		w, err := wsFor(op.Node)
		if err != nil {
			return res, err
		}
		root, err := MoveSplit(w.Root, op.Node, op.Target, op.Zone, &d.gen)
		if err != nil {
			return res, err
		}
		w.Root = root
	case OpMoveLeaf:
		// Cross-workspace move: op.Node is the leaf, op.Workspace names
		// the *destination*, op.Target optionally picks the leaf to split
		// there (empty = wrap the destination root), op.Dir the split
		// direction (empty = row). The leaf keeps its id so window frames
		// survive the move.
		src := d.FindLeafWorkspace(op.Node)
		if src == nil {
			return res, fmt.Errorf("move-leaf: leaf %q not found", op.Node)
		}
		dst := d.WorkspaceByID(op.Workspace)
		if dst == nil {
			return res, fmt.Errorf("move-leaf: no workspace %q", op.Workspace)
		}
		if src.ID == dst.ID {
			return res, fmt.Errorf("move-leaf: %q is already in %s", op.Node, dst.ID)
		}
		dir := op.Dir
		if dir == "" {
			dir = Row
		}
		// Validate the graft point first so failure leaves d unchanged.
		if op.Target != "" && dst.Root.FindLeaf(op.Target) == nil {
			return res, fmt.Errorf("move-leaf: no target leaf %q in %s", op.Target, dst.ID)
		}
		var moved *Node
		if src.Root.Kind == Leaf {
			// Moving the only leaf: the source workspace keeps a fresh
			// launcher leaf instead of going empty.
			moved = src.Root.Clone()
			src.Root = NewLeaf(d.gen.Next(), "")
			res.NewLeaf = src.Root.ID
		} else {
			shrunk, detached, err := DetachLeaf(src.Root, op.Node)
			if err != nil {
				return res, err
			}
			src.Root, moved = shrunk, detached
		}
		grafted, err := GraftLeaf(dst.Root, moved, op.Target, dir, &d.gen)
		if err != nil {
			return res, err
		}
		dst.Root = grafted
	case OpAddWorkspace:
		// Empty app = the launcher tile (the WM's empty-tile convention);
		// new workspaces open on a launcher unless the op names an app.
		res.NewWorkspace = d.AddWorkspace(op.App).ID
	case OpRemoveWorkspace:
		return res, d.RemoveWorkspace(op.Workspace)
	case OpRenameWorkspace:
		return res, d.RenameWorkspace(op.Workspace, op.Name)
	case OpCloneWorkspace:
		w, err := d.CloneWorkspace(op.Workspace)
		if err != nil {
			return res, err
		}
		res.NewWorkspace = w.ID
	case OpSwitchWorkspace:
		return res, d.SwitchWorkspace(op.Workspace)
	default:
		return res, fmt.Errorf("unknown op %q", op.Op)
	}
	return res, nil
}

// Serialize renders the desktop as deterministic JSON (used by the query
// IPC and the cross-implementation oracle).
func (d *Desktop) Serialize() ([]byte, error) {
	return json.MarshalIndent(struct {
		Workspaces []Workspace `json:"workspaces"`
		Current    string      `json:"current"`
	}{d.Workspaces, d.Current}, "", "  ")
}
