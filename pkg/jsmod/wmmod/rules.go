package wmmod

// Rules and layout recipes (GGWM-002 P4): declarative specs are
// normalized at definition time (errors when you write them, not when
// they fire), stored as inspectable plans, and compiled to plain Ops at
// execution time. A rule is sugar over an event subscription; a layout
// is sugar over a split sequence — neither is a new WM mechanism.

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sync"

	"github.com/dop251/goja"

	"github.com/go-go-golems/go-go-wm/pkg/jsmod"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
)

// LayoutStep is one node of a normalized layout plan.
type LayoutStep struct {
	Kind  string      `json:"kind"` // "leaf" | "split"
	App   string      `json:"app,omitempty"`
	Dir   wmcore.Dir  `json:"dir,omitempty"`
	Ratio float64     `json:"ratio,omitempty"`
	A     *LayoutStep `json:"a,omitempty"`
	B     *LayoutStep `json:"b,omitempty"`
}

// Rule is a normalized placement rule.
type Rule struct {
	Title     string `json:"title"` // regexp source (case-insensitive)
	Workspace string `json:"workspace"`
	Dir       string `json:"dir,omitempty"`

	re *regexp.Regexp
}

type ruleState struct {
	mu      sync.Mutex
	layouts map[string]*LayoutStep
	rules   []*Rule
	armed   bool
}

func newRuleState() *ruleState {
	return &ruleState{layouts: map[string]*LayoutStep{}}
}

// normalizeLayout validates a raw JS spec into a LayoutStep tree.
// Definition-time failure is the contract: a bad spec throws at
// wm.layout(), never mid-application.
func normalizeLayout(v interface{}) (*LayoutStep, error) {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("layout node must be an object, got %T", v)
	}
	if app, hasApp := m["app"]; hasApp {
		s, ok := app.(string)
		if !ok {
			return nil, fmt.Errorf("layout leaf: app must be a string")
		}
		for k := range m {
			if k != "app" {
				return nil, fmt.Errorf("layout leaf: unknown key %q", k)
			}
		}
		return &LayoutStep{Kind: "leaf", App: s}, nil
	}
	splitRaw, hasSplit := m["split"]
	if !hasSplit {
		return nil, fmt.Errorf(`layout node needs "app" or "split"`)
	}
	dirStr, _ := splitRaw.(string)
	if dirStr != "row" && dirStr != "col" {
		return nil, fmt.Errorf(`layout split must be "row" or "col", got %v`, splitRaw)
	}
	step := &LayoutStep{Kind: "split", Dir: wmcore.Dir(dirStr), Ratio: 0.5}
	for k := range m {
		switch k {
		case "split", "ratio", "a", "b":
		default:
			return nil, fmt.Errorf("layout split: unknown key %q", k)
		}
	}
	if r, ok := m["ratio"]; ok {
		f, ok := r.(float64)
		if !ok {
			if i, iok := r.(int64); iok {
				f, ok = float64(i), true
			}
		}
		if !ok || f < wmcore.MinRatio || f > wmcore.MaxRatio {
			return nil, fmt.Errorf("layout ratio must be a number in [%.1f, %.1f]", wmcore.MinRatio, wmcore.MaxRatio)
		}
		step.Ratio = f
	}
	var err error
	if step.A, err = normalizeLayout(m["a"]); err != nil {
		return nil, fmt.Errorf("a: %w", err)
	}
	if step.B, err = normalizeLayout(m["b"]); err != nil {
		return nil, fmt.Errorf("b: %w", err)
	}
	return step, nil
}

// normalizeRule validates {title, workspace, dir?}. title may be a string
// (Go regexp, compiled case-insensitive) or a JS RegExp.
func normalizeRule(vm *goja.Runtime, arg goja.Value) (*Rule, error) {
	obj, ok := arg.(*goja.Object)
	if !ok {
		return nil, fmt.Errorf("rule must be an object")
	}
	r := &Rule{}
	for _, k := range obj.Keys() {
		switch k {
		case "title", "workspace", "dir":
		default:
			return nil, fmt.Errorf("rule: unknown key %q", k)
		}
	}
	if tv := obj.Get("title"); tv != nil && !goja.IsUndefined(tv) && !goja.IsNull(tv) {
		if s, ok := tv.Export().(string); ok {
			r.Title = s
		} else if to, ok := tv.(*goja.Object); ok {
			// A JS RegExp: use its source (flags beyond i are ignored —
			// matching is always case-insensitive).
			if src := to.Get("source"); src != nil && !goja.IsUndefined(src) {
				r.Title = src.String()
			}
		}
	}
	if r.Title == "" {
		return nil, fmt.Errorf("rule.title must be a non-empty pattern")
	}
	re, err := regexp.Compile("(?i)" + r.Title)
	if err != nil {
		return nil, fmt.Errorf("rule.title: %w", err)
	}
	r.re = re
	r.Workspace, _ = obj.Get("workspace").Export().(string)
	if r.Workspace == "" {
		return nil, fmt.Errorf("rule.workspace must be a non-empty name")
	}
	if d := obj.Get("dir"); d != nil && !goja.IsUndefined(d) && !goja.IsNull(d) {
		if s := d.String(); s != "" {
			if s != "row" && s != "col" {
				return nil, fmt.Errorf(`rule.dir must be "row" or "col"`)
			}
			r.Dir = s
		}
	}
	return r, nil
}

// ensureWorkspace resolves a workspace by id or name, creating (and
// naming) it when absent. Pure backend calls — usable from any goroutine.
func (m *Module) ensureWorkspace(ctx context.Context, name string) (string, error) {
	d, err := m.backend.Tree(ctx)
	if err != nil {
		return "", err
	}
	for i := range d.Workspaces {
		if d.Workspaces[i].ID == name || d.Workspaces[i].Name == name {
			return d.Workspaces[i].ID, nil
		}
	}
	res, err := m.backend.Apply(ctx, wmcore.Op{Op: wmcore.OpAddWorkspace})
	if err != nil {
		return "", err
	}
	if _, err := m.backend.Apply(ctx, wmcore.Op{Op: wmcore.OpRenameWorkspace, Workspace: res.NewWorkspace, Name: name}); err != nil {
		return "", err
	}
	return res.NewWorkspace, nil
}

// applyLayout compiles a plan onto a workspace. It only builds on a
// fresh workspace (a single empty leaf) — anything else is a no-op
// returning false, which is what makes layout recipes idempotent.
func (m *Module) applyLayout(ctx context.Context, wsID string, plan *LayoutStep) (bool, error) {
	d, err := m.backend.Tree(ctx)
	if err != nil {
		return false, err
	}
	ws := d.WorkspaceByID(wsID)
	if ws == nil {
		return false, fmt.Errorf("no workspace %q", wsID)
	}
	if ws.Root.Kind != wmcore.Leaf || ws.Root.App != "" {
		return false, nil // already populated
	}
	return true, m.compileLayout(ctx, wmcore.NodeID(ws.Root.ID), plan)
}

// compileLayout walks the plan top-down: split first (the target is
// still a leaf then), set the ratio on the split the Result implies,
// recurse into both sides.
func (m *Module) compileLayout(ctx context.Context, target wmcore.NodeID, step *LayoutStep) error {
	if step.Kind == "leaf" {
		if step.App == "" {
			return nil
		}
		_, err := m.backend.Apply(ctx, wmcore.Op{Op: wmcore.OpSetLeafApp, Node: target, App: step.App})
		return err
	}
	res, err := m.backend.Apply(ctx, wmcore.Op{Op: wmcore.OpSplitLeaf, Node: target, Dir: step.Dir})
	if err != nil {
		return err
	}
	if step.Ratio != 0.5 {
		d, err := m.backend.Tree(ctx)
		if err != nil {
			return err
		}
		if parent := findParentSplit(d, res.NewLeaf); parent != "" {
			if _, err := m.backend.Apply(ctx, wmcore.Op{Op: wmcore.OpSetRatio, Node: parent, Ratio: step.Ratio}); err != nil {
				return err
			}
		}
	}
	if err := m.compileLayout(ctx, target, step.A); err != nil {
		return err
	}
	return m.compileLayout(ctx, res.NewLeaf, step.B)
}

// armRules starts the Go-side window.managed watcher once.
func (m *Module) armRules() error {
	m.ruleState.mu.Lock()
	armed := m.ruleState.armed
	m.ruleState.armed = true
	m.ruleState.mu.Unlock()
	if armed {
		return nil
	}
	if m.fan == nil {
		return fmt.Errorf("rules need a broker connection (events carry window.managed)")
	}
	m.fan.SubscribeGo("window.managed", func(msg *pbui.Msg) {
		var data struct {
			Leaf  string `json:"leaf"`
			Title string `json:"title"`
		}
		if err := json.Unmarshal(msg.Data, &data); err != nil || data.Leaf == "" {
			return
		}
		m.ruleState.mu.Lock()
		rules := append([]*Rule(nil), m.ruleState.rules...)
		m.ruleState.mu.Unlock()
		for _, r := range rules {
			if !r.re.MatchString(data.Title) {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
			wsID, err := m.ensureWorkspace(ctx, r.Workspace)
			if err == nil {
				op := wmcore.Op{Op: wmcore.OpMoveLeaf, Node: wmcore.NodeID(data.Leaf), Workspace: wsID}
				if r.Dir != "" {
					op.Dir = wmcore.Dir(r.Dir)
				}
				_, err = m.backend.Apply(ctx, op)
			}
			cancel()
			if err != nil {
				jsmod.EmitScriptError(nil, "wm.rule:"+r.Workspace, err, nil)
			}
			return // first matching rule wins
		}
	})
	return m.fan.EnsurePump(context.Background())
}

// installRuleExports adds layout/layouts/rule/rules to the module exports.
func (m *Module) installRuleExports(vm *goja.Runtime, set func(string, interface{})) {
	// wm.layout(name, spec): normalize and store a recipe.
	set("layout", func(call goja.FunctionCall) goja.Value {
		name := call.Argument(0).String()
		if name == "" {
			panic(vm.ToValue("wm.layout: name must be non-empty"))
		}
		plan, err := normalizeLayout(call.Argument(1).Export())
		if err != nil {
			panic(vm.ToValue("wm.layout: " + err.Error()))
		}
		m.ruleState.mu.Lock()
		m.ruleState.layouts[name] = plan
		m.ruleState.mu.Unlock()
		return goja.Undefined()
	})
	// wm.layouts(): the normalized plans, for inspection and tests.
	set("layouts", func(goja.FunctionCall) goja.Value {
		m.ruleState.mu.Lock()
		defer m.ruleState.mu.Unlock()
		plain, err := jsmod.ToPlain(m.ruleState.layouts)
		if err != nil {
			panic(vm.ToValue("wm.layouts: " + err.Error()))
		}
		return vm.ToValue(plain)
	})
	// wm.rule({title, workspace, dir?}): normalize, store, arm the watcher.
	set("rule", func(call goja.FunctionCall) goja.Value {
		r, err := normalizeRule(vm, call.Argument(0))
		if err != nil {
			panic(vm.ToValue("wm.rule: " + err.Error()))
		}
		if err := m.armRules(); err != nil {
			panic(vm.ToValue("wm.rule: " + err.Error()))
		}
		m.ruleState.mu.Lock()
		m.ruleState.rules = append(m.ruleState.rules, r)
		m.ruleState.mu.Unlock()
		return goja.Undefined()
	})
	// wm.rules(): normalized rules.
	set("rules", func(goja.FunctionCall) goja.Value {
		m.ruleState.mu.Lock()
		defer m.ruleState.mu.Unlock()
		plain, err := jsmod.ToPlain(m.ruleState.rules)
		if err != nil {
			panic(vm.ToValue("wm.rules: " + err.Error()))
		}
		return vm.ToValue(plain)
	})
}
