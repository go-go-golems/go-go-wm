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
	case OpAddWorkspace:
		app := op.App
		if app == "" {
			app = "launcher"
		}
		res.NewWorkspace = d.AddWorkspace(app).ID
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
