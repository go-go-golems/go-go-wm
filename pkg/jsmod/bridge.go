// Package jsmod holds the goja-facing bindings of go-go-wm: conversion
// between wire types and plain JavaScript values (this file), the pbui
// native module (pbuimod), and the wm native module (wmmod).
//
// Everything here follows the concurrency contract of the GGWM-002 design
// docs: bridge functions are pure data transforms and may run anywhere,
// but the values they produce are handed to goja only on the VM's owner
// loop.
package jsmod

import (
	"encoding/json"
	"fmt"

	"github.com/go-go-golems/go-go-wm/pkg/pbui"
	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
)

// ObjectToJS converts a wire Object into the plain map shape scripts see:
// {ptype, value, label?, doc?}. The JSON value payload is decoded so JS
// sees numbers/objects/strings, not a JSON string.
func ObjectToJS(o *pbui.Object) map[string]interface{} {
	if o == nil {
		return nil
	}
	m := map[string]interface{}{"ptype": o.Ptype}
	var v interface{}
	if len(o.Value) > 0 && json.Unmarshal(o.Value, &v) == nil {
		m["value"] = v
	} else {
		m["value"] = nil
	}
	if o.Label != "" {
		m["label"] = o.Label
	}
	if o.Doc != "" {
		m["doc"] = o.Doc
	}
	return m
}

// JSToObject converts a script-supplied value into a wire Object. It
// accepts the map shape produced by ObjectToJS and pbui.object(), and it
// never panics on hostile input — malformed shapes return an error.
func JSToObject(v interface{}) (pbui.Object, error) {
	m, ok := v.(map[string]interface{})
	if !ok {
		return pbui.Object{}, fmt.Errorf("object must be {ptype, value}, got %T", v)
	}
	ptype, ok := m["ptype"].(string)
	if !ok || ptype == "" {
		return pbui.Object{}, fmt.Errorf("object.ptype must be a non-empty string")
	}
	obj, err := pbui.NewObject(ptype, m["value"])
	if err != nil {
		return pbui.Object{}, err
	}
	if label, ok := m["label"].(string); ok {
		obj.Label = label
	}
	if doc, ok := m["doc"].(string); ok {
		obj.Doc = doc
	}
	return obj, nil
}

// OpFromJS converts a script-supplied value into a wmcore.Op. The op name
// is validated here (normalize-then-fail-early); operand validation stays
// in wmcore.Apply, which is the single authority.
func OpFromJS(v interface{}) (wmcore.Op, error) {
	var op wmcore.Op
	raw, err := json.Marshal(v)
	if err != nil {
		return op, fmt.Errorf("op is not JSON-shaped: %w", err)
	}
	if err := json.Unmarshal(raw, &op); err != nil {
		return op, fmt.Errorf("op shape: %w", err)
	}
	if op.Op == "" {
		return op, fmt.Errorf("op.op must be a non-empty op name")
	}
	return op, nil
}

// SegsToWire converts pbui.print(...) varargs into the listener.print segs
// convention (see pkg/apps/xapp): strings become {text}, object maps
// become {ptype, value}.
func SegsToWire(args []interface{}) ([]map[string]interface{}, error) {
	segs := make([]map[string]interface{}, 0, len(args))
	for i, a := range args {
		switch t := a.(type) {
		case string:
			segs = append(segs, map[string]interface{}{"text": t})
		case map[string]interface{}:
			obj, err := JSToObject(t)
			if err != nil {
				return nil, fmt.Errorf("print arg %d: %w", i, err)
			}
			segs = append(segs, map[string]interface{}{"ptype": obj.Ptype, "value": obj.StringValue()})
		case nil:
			segs = append(segs, map[string]interface{}{"text": "null"})
		default:
			segs = append(segs, map[string]interface{}{"text": fmt.Sprint(t)})
		}
	}
	return segs, nil
}

// EventToJS converts a broker event message into the {event, data, source,
// seq} shape handed to pbui.on / wm.on handlers.
func EventToJS(m *pbui.Msg) map[string]interface{} {
	out := map[string]interface{}{
		"event":  m.Event,
		"source": m.Source,
		"seq":    m.EventSeq,
	}
	var data interface{}
	if len(m.Data) > 0 && json.Unmarshal(m.Data, &data) == nil {
		out["data"] = data
	} else {
		out["data"] = nil
	}
	return out
}
