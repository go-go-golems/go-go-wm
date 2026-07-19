// Package repl is the rich-value core of the notebook REPL (GGWM-009):
// the wolframjs-repl prototype's RichValue protocol re-founded on PBUI.
// A Value is a typed object (real ptypes — R-D1) with a one-line
// summary and ordered views rendered over uispec. This package is pure:
// no goja, no X, no kernel — derivation works on exported Go values,
// and __pbui__ opt-in payloads arrive as plain maps.
package repl

import (
	"encoding/json"
	"fmt"

	"github.com/go-go-golems/go-go-wm/pkg/apps/uispec"
)

// Value is one evaluation result, presentation-ready.
type Value struct {
	Ptype   string // "" = not a presentation (undefined, functions): summary only
	Summary string // the collapsed face: "Dataset (120 rows × 4 cols)"
	Doc     string // hover line
	Views   []View // ordered; first is the default
	Input   string // re-evaluable input form ("copy as input")
	Raw     json.RawMessage
}

// View is one named rendering of a value.
type View struct {
	Name string
	Spec uispec.Spec
}

// Caps: derivation and views are bounded so an accidental huge value
// costs a bounded render (design risk "big values").
const (
	inspectCap   = 100 // elements examined for type detection
	tableRowCap  = 20  // rows shown before "… N more"
	jsonLineCap  = 40  // pretty-json lines
	seriesPtsCap = 200 // points fed to plotters
)

// NormalizeRich validates a __pbui__() payload (an exported JS map)
// into a Value. Shape: {ptype, summary, doc?, input?, views:
// [{name, rows}]} where rows is ui.row(...) data (uispec.Normalize
// rules, so errors carry row/seg coordinates).
func NormalizeRich(raw interface{}) (Value, error) {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return Value{}, fmt.Errorf("__pbui__ must return an object, got %T", raw)
	}
	for k := range m {
		switch k {
		case "ptype", "summary", "doc", "input", "views":
		default:
			return Value{}, fmt.Errorf("__pbui__: unknown key %q", k)
		}
	}
	v := Value{}
	v.Ptype, _ = m["ptype"].(string)
	v.Summary, _ = m["summary"].(string)
	v.Doc, _ = m["doc"].(string)
	v.Input, _ = m["input"].(string)
	if v.Summary == "" {
		return Value{}, fmt.Errorf("__pbui__: summary must be a non-empty string")
	}
	views, ok := m["views"].([]interface{})
	if !ok || len(views) == 0 {
		return Value{}, fmt.Errorf("__pbui__: views must be a non-empty array")
	}
	for i, rawView := range views {
		vm, ok := rawView.(map[string]interface{})
		if !ok {
			return Value{}, fmt.Errorf("__pbui__ view %d: must be an object", i)
		}
		name, _ := vm["name"].(string)
		if name == "" {
			return Value{}, fmt.Errorf("__pbui__ view %d: name must be non-empty", i)
		}
		spec, err := uispec.Normalize(vm["rows"])
		if err != nil {
			return Value{}, fmt.Errorf("__pbui__ view %q: %w", name, err)
		}
		v.Views = append(v.Views, View{Name: name, Spec: spec})
	}
	if raw, err := json.Marshal(m); err == nil {
		v.Raw = raw
	}
	return v, nil
}
