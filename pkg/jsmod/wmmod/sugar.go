package wmmod

import (
	"fmt"

	"github.com/dop251/goja"
	"github.com/go-go-golems/go-go-goja/pkg/runtimebridge"

	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
)

func runtimebridgeLookup(vm *goja.Runtime) (runtimebridge.RuntimeServices, bool) {
	return runtimebridge.Lookup(vm)
}

// split compiles wm.split(leaf, dir?, {ratio?, app?}) into split-leaf
// (+ set-ratio on the new parent split, + set-leaf-app) and returns the
// new leaf id.
func (m *Module) split(vm *goja.Runtime, call goja.FunctionCall) string {
	leaf := nodeArg(vm, call, 0, "wm.split: leaf id")
	dir := wmcore.Row
	if a := call.Argument(1); !goja.IsUndefined(a) && !goja.IsNull(a) && a.String() != "" {
		switch a.String() {
		case "row":
			dir = wmcore.Row
		case "col":
			dir = wmcore.Col
		default:
			panic(vm.ToValue(`wm.split: dir must be "row" or "col"`))
		}
	}
	var ratio float64
	app := ""
	if opts, ok := call.Argument(2).Export().(map[string]interface{}); ok {
		if r, ok := opts["ratio"].(float64); ok {
			ratio = r
		}
		if r, ok := opts["ratio"].(int64); ok {
			ratio = float64(r)
		}
		app, _ = opts["app"].(string)
	}

	res := m.apply(vm, "split", wmcore.Op{Op: wmcore.OpSplitLeaf, Node: leaf, Dir: dir, App: app})
	newLeaf := res.NewLeaf

	if ratio != 0 {
		// split-leaf reports the new leaf, not the new parent split; find
		// the split whose child the new leaf is, then set its ratio.
		d := m.tree(vm)
		if parent := findParentSplit(d, newLeaf); parent != "" {
			m.apply(vm, "split(ratio)", wmcore.Op{Op: wmcore.OpSetRatio, Node: parent, Ratio: ratio})
		}
	}
	return string(newLeaf)
}

func findParentSplit(d *wmcore.Desktop, child wmcore.NodeID) wmcore.NodeID {
	var found wmcore.NodeID
	var walk func(n *wmcore.Node)
	walk = func(n *wmcore.Node) {
		if n == nil || n.Kind != wmcore.Split || found != "" {
			return
		}
		if (n.A != nil && n.A.ID == child) || (n.B != nil && n.B.ID == child) {
			found = n.ID
			return
		}
		walk(n.A)
		walk(n.B)
	}
	for i := range d.Workspaces {
		walk(d.Workspaces[i].Root)
		if found != "" {
			break
		}
	}
	return found
}

// workspaceObj implements wm.workspace(name): resolve by id or name, or
// create on first use. The returned object owns the resolved id (state in
// Go, methods compile to single Ops).
func (m *Module) workspaceObj(vm *goja.Runtime, name string) goja.Value {
	if name == "" {
		panic(vm.ToValue("wm.workspace: name must be non-empty"))
	}
	d := m.tree(vm)
	id := ""
	for i := range d.Workspaces {
		if d.Workspaces[i].ID == name || d.Workspaces[i].Name == name {
			id = d.Workspaces[i].ID
			break
		}
	}
	created := false
	if id == "" {
		res := m.apply(vm, "workspace(create)", wmcore.Op{Op: wmcore.OpAddWorkspace})
		id = res.NewWorkspace
		m.apply(vm, "workspace(rename)", wmcore.Op{Op: wmcore.OpRenameWorkspace, Workspace: id, Name: name})
		created = true
	}

	obj := vm.NewObject()
	mustSetObj(obj, "id", id)
	mustSetObj(obj, "name", name)
	mustSetObj(obj, "created", created)
	mustSetObj(obj, "switch", func(goja.FunctionCall) goja.Value {
		m.apply(vm, "workspace.switch", wmcore.Op{Op: wmcore.OpSwitchWorkspace, Workspace: id})
		return obj
	})
	mustSetObj(obj, "rename", func(call goja.FunctionCall) goja.Value {
		m.apply(vm, "workspace.rename", wmcore.Op{Op: wmcore.OpRenameWorkspace, Workspace: id, Name: call.Argument(0).String()})
		return obj
	})
	mustSetObj(obj, "remove", func(goja.FunctionCall) goja.Value {
		m.apply(vm, "workspace.remove", wmcore.Op{Op: wmcore.OpRemoveWorkspace, Workspace: id})
		return goja.Undefined()
	})
	mustSetObj(obj, "clone", func(goja.FunctionCall) goja.Value {
		res := m.apply(vm, "workspace.clone", wmcore.Op{Op: wmcore.OpCloneWorkspace, Workspace: id})
		return vm.ToValue(res.NewWorkspace)
	})
	// adopt(leaf, {target?, dir?}) — pull a leaf from wherever it lives.
	mustSetObj(obj, "adopt", func(call goja.FunctionCall) goja.Value {
		op := wmcore.Op{Op: wmcore.OpMoveLeaf, Node: nodeArg(vm, call, 0, "workspace.adopt: leaf"), Workspace: id}
		if opts, ok := call.Argument(1).Export().(map[string]interface{}); ok {
			if t, _ := opts["target"].(string); t != "" {
				op.Target = wmcore.NodeID(t)
			}
			if dir, _ := opts["dir"].(string); dir != "" {
				op.Dir = wmcore.Dir(dir)
			}
		}
		m.apply(vm, "workspace.adopt", op)
		return obj
	})
	return obj
}

func mustSetObj(o *goja.Object, name string, v interface{}) {
	if err := o.Set(name, v); err != nil {
		panic(fmt.Errorf("wmmod: set %s: %w", name, err))
	}
}
