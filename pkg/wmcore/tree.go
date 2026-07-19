// Package wmcore is the pure layout engine of go-go-wm: a binary split tree
// with leaves holding application slots and internal nodes carrying a split
// direction and ratio, plus sticky-zone snapping, drop-zone classification,
// and workspace management.
//
// It is a direct port of the tree model in the PBUI shell prototype
// (ttmp .../sources/pbui-shell.jsx:135-169) and imports nothing X-flavored:
// leaf ids and rectangles in, rectangles out.
package wmcore

import (
	"fmt"
)

// NodeID identifies a node in a tree. IDs are strings so they survive
// serialization (query IPC, ops, future JS bindings) unchanged.
type NodeID string

// Dir is a split direction.
type Dir string

const (
	Row Dir = "row" // children side by side (A left, B right)
	Col Dir = "col" // children stacked (A top, B bottom)
)

// Kind discriminates leaf and split nodes.
type Kind string

const (
	Leaf  Kind = "leaf"
	Split Kind = "split"
)

const (
	// MinRatio and MaxRatio bound divider positions, as in the prototype
	// (clamp(f, 0.1, 0.9), pbui-shell.jsx:181).
	MinRatio = 0.1
	MaxRatio = 0.9
)

// Node is a tree node. One tagged struct rather than an interface: it
// serializes trivially and keeps the JS oracle diffable.
type Node struct {
	ID   NodeID `json:"id"`
	Kind Kind   `json:"kind"`

	// Leaf fields.
	App string `json:"app,omitempty"` // application slot (app name or client id)

	// Split fields.
	Dir   Dir     `json:"dir,omitempty"`
	Ratio float64 `json:"ratio,omitempty"`
	A     *Node   `json:"a,omitempty"`
	B     *Node   `json:"b,omitempty"`
}

// IDGen mints fresh node ids. Deterministic (a counter), so tests and the
// cross-implementation oracle can replay scripts byte-for-byte.
type IDGen struct{ n int }

func (g *IDGen) Next() NodeID {
	g.n++
	return NodeID(fmt.Sprintf("n%d", g.n))
}

// Seed advances the generator past ids already in use (e.g. after loading a
// serialized tree).
func (g *IDGen) Seed(n int) {
	if n > g.n {
		g.n = n
	}
}

// NewLeaf builds a leaf node.
func NewLeaf(id NodeID, app string) *Node {
	return &Node{ID: id, Kind: Leaf, App: app}
}

// NewSplit builds a split node.
func NewSplit(id NodeID, dir Dir, a, b *Node, ratio float64) *Node {
	return &Node{ID: id, Kind: Split, Dir: dir, A: a, B: b, Ratio: clampRatio(ratio)}
}

func clampRatio(r float64) float64 {
	if r < MinRatio {
		return MinRatio
	}
	if r > MaxRatio {
		return MaxRatio
	}
	return r
}

// Clone deep-copies a tree, keeping ids.
func (n *Node) Clone() *Node {
	if n == nil {
		return nil
	}
	c := *n
	c.A = n.A.Clone()
	c.B = n.B.Clone()
	return &c
}

// CloneFresh deep-copies a tree, assigning fresh ids from g (used by
// workspace duplication, ports cloneTree, pbui-shell.jsx:162-164).
func (n *Node) CloneFresh(g *IDGen) *Node {
	if n == nil {
		return nil
	}
	c := *n
	c.ID = g.Next()
	c.A = n.A.CloneFresh(g)
	c.B = n.B.CloneFresh(g)
	return &c
}

// Find returns the node with the given id, or nil.
func (n *Node) Find(id NodeID) *Node {
	if n == nil {
		return nil
	}
	if n.ID == id {
		return n
	}
	if n.Kind == Split {
		if f := n.A.Find(id); f != nil {
			return f
		}
		return n.B.Find(id)
	}
	return nil
}

// FindLeaf returns the leaf with the given id, or nil (ports findLeaf).
func (n *Node) FindLeaf(id NodeID) *Node {
	f := n.Find(id)
	if f != nil && f.Kind == Leaf {
		return f
	}
	return nil
}

// Leaves returns all leaves in traversal order (A before B).
func (n *Node) Leaves() []*Node {
	var out []*Node
	n.walk(func(x *Node) {
		if x.Kind == Leaf {
			out = append(out, x)
		}
	})
	return out
}

// CountLeaves ports countLeaves (pbui-shell.jsx:161).
func (n *Node) CountLeaves() int {
	if n == nil {
		return 0
	}
	if n.Kind == Leaf {
		return 1
	}
	return n.A.CountLeaves() + n.B.CountLeaves()
}

func (n *Node) walk(f func(*Node)) {
	if n == nil {
		return
	}
	f(n)
	if n.Kind == Split {
		n.A.walk(f)
		n.B.walk(f)
	}
}

// Validate checks structural invariants: splits have exactly two children
// with a valid direction and in-bounds ratio, leaves have none, and ids are
// unique. Property tests run this after every operation.
func (n *Node) Validate() error {
	if n == nil {
		return fmt.Errorf("nil tree")
	}
	seen := map[NodeID]bool{}
	var check func(x *Node) error
	check = func(x *Node) error {
		if x.ID == "" {
			return fmt.Errorf("node with empty id")
		}
		if seen[x.ID] {
			return fmt.Errorf("duplicate node id %q", x.ID)
		}
		seen[x.ID] = true
		switch x.Kind {
		case Leaf:
			if x.A != nil || x.B != nil {
				return fmt.Errorf("leaf %q has children", x.ID)
			}
		case Split:
			if x.A == nil || x.B == nil {
				return fmt.Errorf("split %q missing a child", x.ID)
			}
			if x.Dir != Row && x.Dir != Col {
				return fmt.Errorf("split %q has invalid dir %q", x.ID, x.Dir)
			}
			if x.Ratio < MinRatio || x.Ratio > MaxRatio {
				return fmt.Errorf("split %q ratio %v out of bounds", x.ID, x.Ratio)
			}
			if err := check(x.A); err != nil {
				return err
			}
			return check(x.B)
		default:
			return fmt.Errorf("node %q has invalid kind %q", x.ID, x.Kind)
		}
		return nil
	}
	return check(n)
}

// --- mutations -------------------------------------------------------------
//
// All mutations return a new tree (persistent-style, ports the prototype's
// immutable updates) and an error instead of silently doing nothing.

// update replaces the node with the given id via fn (ports updateNode).
func update(n *Node, id NodeID, fn func(*Node) *Node) *Node {
	if n == nil {
		return nil
	}
	if n.ID == id {
		return fn(n)
	}
	if n.Kind == Split {
		a, b := update(n.A, id, fn), update(n.B, id, fn)
		if a != n.A || b != n.B {
			c := *n
			c.A, c.B = a, b
			return &c
		}
	}
	return n
}

// removeLeaf detaches the leaf with the given id; the sibling absorbs the
// space (ports removeLeaf, pbui-shell.jsx:148-156). Returns the tree
// unchanged if id is the root or absent.
func removeLeaf(n *Node, id NodeID) *Node {
	if n == nil || n.Kind != Split {
		return n
	}
	if n.A.ID == id {
		return n.B
	}
	if n.B.ID == id {
		return n.A
	}
	a, b := removeLeaf(n.A, id), removeLeaf(n.B, id)
	if a != n.A || b != n.B {
		c := *n
		c.A, c.B = a, b
		return &c
	}
	return n
}

// SplitLeaf replaces leaf id with a split of itself (side A) and a fresh
// leaf running newApp (side B). Ports splitLeaf (pbui-shell.jsx:574-577).
func SplitLeaf(root *Node, id NodeID, dir Dir, newApp string, g *IDGen) (*Node, NodeID, error) {
	if root.FindLeaf(id) == nil {
		return root, "", fmt.Errorf("split-leaf: no leaf %q", id)
	}
	newLeaf := NewLeaf(g.Next(), newApp)
	out := update(root, id, func(n *Node) *Node {
		return NewSplit(g.Next(), dir, n, newLeaf, 0.5)
	})
	return out, newLeaf.ID, nil
}

// CloseLeaf removes leaf id; its sibling reclaims the space. Refuses to
// close the last leaf.
func CloseLeaf(root *Node, id NodeID) (*Node, error) {
	if root.FindLeaf(id) == nil {
		return root, fmt.Errorf("close-leaf: no leaf %q", id)
	}
	if root.Kind == Leaf {
		return root, fmt.Errorf("close-leaf: cannot close the only leaf")
	}
	out := removeLeaf(root, id)
	if out.FindLeaf(id) != nil {
		return root, fmt.Errorf("close-leaf: could not detach %q", id)
	}
	return out, nil
}

// SetRatio sets the divider position of split id, clamped and pre-snapped by
// the caller if desired.
func SetRatio(root *Node, id NodeID, ratio float64) (*Node, error) {
	n := root.Find(id)
	if n == nil || n.Kind != Split {
		return root, fmt.Errorf("set-ratio: no split %q", id)
	}
	out := update(root, id, func(x *Node) *Node {
		c := *x
		c.Ratio = clampRatio(ratio)
		return &c
	})
	return out, nil
}

// SetLeafApp changes the app slot of leaf id (ports setLeafApp).
func SetLeafApp(root *Node, id NodeID, app string) (*Node, error) {
	if root.FindLeaf(id) == nil {
		return root, fmt.Errorf("set-leaf-app: no leaf %q", id)
	}
	out := update(root, id, func(x *Node) *Node {
		c := *x
		c.App = app
		return &c
	})
	return out, nil
}

// SwapLeaves exchanges the app slots of two leaves; state travels with the
// app because it lives in the world, not the tile (ports swapTiles,
// pbui-shell.jsx:583-590).
func SwapLeaves(root *Node, a, b NodeID) (*Node, error) {
	la, lb := root.FindLeaf(a), root.FindLeaf(b)
	if la == nil || lb == nil {
		return root, fmt.Errorf("swap-leaves: missing leaf (%q, %q)", a, b)
	}
	appA, appB := la.App, lb.App
	out := update(root, a, func(x *Node) *Node { c := *x; c.App = appB; return &c })
	out = update(out, b, func(x *Node) *Node { c := *x; c.App = appA; return &c })
	return out, nil
}

// MoveSplit re-docks leaf from as a new split of leaf target on the given
// side; the source tile closes and its sibling reclaims the space — a move,
// not a copy. Ports moveSplit (pbui-shell.jsx:594-607).
func MoveSplit(root *Node, from, target NodeID, zone Zone, g *IDGen) (*Node, error) {
	if from == target {
		return root, fmt.Errorf("move-split: source and target are the same leaf")
	}
	src := root.FindLeaf(from)
	if src == nil {
		return root, fmt.Errorf("move-split: no leaf %q", from)
	}
	if root.FindLeaf(target) == nil {
		return root, fmt.Errorf("move-split: no leaf %q", target)
	}
	detached := removeLeaf(root, from)
	if detached.FindLeaf(from) != nil {
		return root, fmt.Errorf("move-split: could not detach %q (single-leaf tree)", from)
	}
	srcCopy := src.Clone()
	var dir Dir
	if zone == ZoneLeft || zone == ZoneRight {
		dir = Row
	} else {
		dir = Col
	}
	before := zone == ZoneLeft || zone == ZoneTop
	out := update(detached, target, func(n *Node) *Node {
		if before {
			return NewSplit(g.Next(), dir, srcCopy, n, 0.5)
		}
		return NewSplit(g.Next(), dir, n, srcCopy, 0.5)
	})
	return out, nil
}
