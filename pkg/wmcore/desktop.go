package wmcore

import (
	"encoding/json"
	"fmt"
)

// Workspace is a named tree (ports the spaces array, pbui-shell.jsx:536-548).
type Workspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Root *Node  `json:"root"`
}

// Desktop is the full layout state: all workspaces plus the current one.
type Desktop struct {
	Workspaces []Workspace `json:"workspaces"`
	Current    string      `json:"current"` // workspace id
	gen        IDGen
	wsSeq      int
}

// NewDesktop creates a desktop with one workspace holding a single leaf.
func NewDesktop(firstApp string) *Desktop {
	d := &Desktop{}
	ws := Workspace{ID: d.nextWSID(), Name: "ws-1", Root: NewLeaf(d.gen.Next(), firstApp)}
	d.Workspaces = []Workspace{ws}
	d.Current = ws.ID
	return d
}

// Gen exposes the id generator (the X shell mints leaf ids for new clients).
func (d *Desktop) Gen() *IDGen { return &d.gen }

func (d *Desktop) nextWSID() string {
	d.wsSeq++
	return fmt.Sprintf("ws%d", d.wsSeq)
}

// CurrentWorkspace returns the active workspace (never nil for a valid desktop).
func (d *Desktop) CurrentWorkspace() *Workspace {
	if w := d.WorkspaceByID(d.Current); w != nil {
		return w
	}
	if len(d.Workspaces) > 0 {
		return &d.Workspaces[0]
	}
	return nil
}

// WorkspaceByID returns the workspace with the given id, or nil.
func (d *Desktop) WorkspaceByID(id string) *Workspace {
	for i := range d.Workspaces {
		if d.Workspaces[i].ID == id {
			return &d.Workspaces[i]
		}
	}
	return nil
}

// FindLeafWorkspace returns the workspace containing leaf id, or nil.
func (d *Desktop) FindLeafWorkspace(id NodeID) *Workspace {
	for i := range d.Workspaces {
		if d.Workspaces[i].Root.FindLeaf(id) != nil {
			return &d.Workspaces[i]
		}
	}
	return nil
}

// AddWorkspace appends a new single-leaf workspace and switches to it
// (ports addSpace, pbui-shell.jsx:657-661).
func (d *Desktop) AddWorkspace(app string) *Workspace {
	ws := Workspace{
		ID:   d.nextWSID(),
		Name: fmt.Sprintf("ws-%d", len(d.Workspaces)+1),
		Root: NewLeaf(d.gen.Next(), app),
	}
	d.Workspaces = append(d.Workspaces, ws)
	d.Current = ws.ID
	return d.WorkspaceByID(ws.ID)
}

// RemoveWorkspace deletes a workspace; refuses to delete the last one.
func (d *Desktop) RemoveWorkspace(id string) error {
	if len(d.Workspaces) < 2 {
		return fmt.Errorf("remove-workspace: cannot remove the last workspace")
	}
	idx := -1
	for i := range d.Workspaces {
		if d.Workspaces[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("remove-workspace: no workspace %q", id)
	}
	d.Workspaces = append(d.Workspaces[:idx], d.Workspaces[idx+1:]...)
	if d.Current == id {
		d.Current = d.Workspaces[0].ID
	}
	return nil
}

// RenameWorkspace sets a workspace's display name.
func (d *Desktop) RenameWorkspace(id, name string) error {
	w := d.WorkspaceByID(id)
	if w == nil {
		return fmt.Errorf("rename-workspace: no workspace %q", id)
	}
	w.Name = name
	return nil
}

// CloneWorkspace duplicates a workspace with fresh node ids and switches to
// it (ports cloneSpace, pbui-shell.jsx:668-673).
func (d *Desktop) CloneWorkspace(id string) (*Workspace, error) {
	src := d.WorkspaceByID(id)
	if src == nil {
		return nil, fmt.Errorf("clone-workspace: no workspace %q", id)
	}
	ws := Workspace{
		ID:   d.nextWSID(),
		Name: src.Name + "'",
		Root: src.Root.CloneFresh(&d.gen),
	}
	d.Workspaces = append(d.Workspaces, ws)
	d.Current = ws.ID
	return d.WorkspaceByID(ws.ID), nil
}

// SwitchWorkspace makes id current.
func (d *Desktop) SwitchWorkspace(id string) error {
	if d.WorkspaceByID(id) == nil {
		return fmt.Errorf("switch-workspace: no workspace %q", id)
	}
	d.Current = id
	return nil
}

// Validate checks every workspace tree plus desktop-level invariants
// (unique leaf ids across workspaces, current points at a real workspace).
func (d *Desktop) Validate() error {
	if len(d.Workspaces) == 0 {
		return fmt.Errorf("desktop has no workspaces")
	}
	if d.WorkspaceByID(d.Current) == nil {
		return fmt.Errorf("current workspace %q does not exist", d.Current)
	}
	seen := map[NodeID]string{}
	for i := range d.Workspaces {
		w := &d.Workspaces[i]
		if err := w.Root.Validate(); err != nil {
			return fmt.Errorf("workspace %q: %w", w.ID, err)
		}
		for _, l := range w.Root.Leaves() {
			if other, ok := seen[l.ID]; ok {
				return fmt.Errorf("leaf %q appears in workspaces %q and %q", l.ID, other, w.ID)
			}
			seen[l.ID] = w.ID
		}
	}
	return nil
}

// DeserializeDesktop is the inverse of Serialize: it decodes the JSON and
// reseeds the id generators past every id in use, so a deserialized
// desktop is safe to Apply further ops to (fresh ids never collide).
func DeserializeDesktop(raw []byte) (*Desktop, error) {
	var d Desktop
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("desktop: %w", err)
	}
	for i := range d.Workspaces {
		var n int
		if _, err := fmt.Sscanf(d.Workspaces[i].ID, "ws%d", &n); err == nil && n > d.wsSeq {
			d.wsSeq = n
		}
		d.Workspaces[i].Root.Walk(func(node *Node) {
			var m int
			if _, err := fmt.Sscanf(string(node.ID), "n%d", &m); err == nil {
				d.gen.Seed(m)
			}
		})
	}
	return &d, nil
}
