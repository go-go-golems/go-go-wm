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

// NormalizeRich validates a __pbui__() payload (an exported JS map used
// as display metadata: ptype, summary, views, ...) and wraps the evaluated
// value as the rich object's payload. The descriptor carries display data
// only; the EXPORTED VALUE becomes Value.Raw, because Raw is what flows
// into pbui.Object.Value for accepts and verbs. Storing the descriptor in
// Raw (as before) sent {ptype, summary, views, ...} to downstream consumers
// instead of the matrix's actual data (Codex review RC-4).
func NormalizeRich(value interface{}, descriptor map[string]interface{}) (Value, error) {
	m := descriptor
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
	// The payload is the evaluated VALUE, not the display descriptor.
	// If the value cannot be marshaled (functions, undefined), leave Raw
	// empty so downstream consumers get an empty payload rather than the
	// wrong object.
	if value != nil {
		if raw, err := json.Marshal(value); err == nil {
			v.Raw = raw
		}
	}
	return v, nil
}

// jsonUnmarshal is split out for session.go (avoids a second import
// site accumulating).
func jsonUnmarshal(raw json.RawMessage, out interface{}) error {
	return json.Unmarshal(raw, out)
}
