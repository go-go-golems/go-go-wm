package repl

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/go-go-golems/go-go-wm/pkg/apps/uispec"
	"github.com/go-go-golems/go-go-wm/pkg/draw"
)

// Derive maps an exported JS value (float64/string/bool/nil/
// []interface{}/map[string]interface{}) to a Value. Total: misdetection
// yields a suboptimal default view, never an error; the json view is
// always present as ground truth for structured values.
func Derive(v interface{}) Value {
	switch t := v.(type) {
	case nil:
		return Value{Summary: "undefined"}
	case bool:
		return textValue("boolean", fmt.Sprintf("%v", t), t)
	case float64:
		return numberValue(t)
	case int64:
		return numberValue(float64(t))
	case string:
		if colorHexRe.MatchString(t) {
			return colorValue(t)
		}
		return stringValue(t)
	case []interface{}:
		return deriveArray(t)
	case map[string]interface{}:
		return jsonValue(t, fmt.Sprintf("Object (%d keys)", len(t)))
	default:
		return Value{Summary: fmt.Sprintf("(%T)", v)}
	}
}

var colorHexRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func rawJSON(v interface{}) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return raw
}

func textValue(ptype, text string, v interface{}) Value {
	return Value{
		Ptype: ptype, Summary: text, Raw: rawJSON(v),
		Views: []View{{Name: "text", Spec: uispec.Spec{{{Kind: uispec.KindText, Text: text}}}}},
	}
}

func numberValue(f float64) Value {
	s := trimNumStr(f)
	v := textValue("number", s, f)
	v.Input = s
	return v
}

func colorValue(hex string) Value {
	return Value{
		Ptype: "color", Summary: hex, Raw: rawJSON(hex),
		Input: fmt.Sprintf("%q", hex),
		Views: []View{
			{Name: "swatch", Spec: uispec.Spec{{
				{Kind: uispec.KindObject, Ptype: "color", Value: hex, Doc: "color " + hex},
			}}},
			{Name: "text", Spec: uispec.Spec{{{Kind: uispec.KindText, Text: hex}}}},
		},
	}
}

func stringValue(s string) Value {
	sum := s
	if len(sum) > 60 {
		sum = sum[:60] + "…"
	}
	v := Value{
		Ptype: "string", Summary: fmt.Sprintf("%q", sum), Raw: rawJSON(s),
		Input: fmt.Sprintf("%q", s),
	}
	// Long strings: show line-wrapped text capped like json.
	lines := strings.Split(s, "\n")
	if len(lines) > jsonLineCap {
		lines = lines[:jsonLineCap]
	}
	var spec uispec.Spec
	for _, l := range lines {
		spec = append(spec, uispec.Row{{Kind: uispec.KindText, Text: l}})
	}
	v.Views = []View{{Name: "text", Spec: spec}}
	return v
}

func jsonValue(v interface{}, summary string) Value {
	return Value{
		Ptype: "json", Summary: summary, Raw: rawJSON(v),
		Views: []View{{Name: "json", Spec: prettyJSONSpec(v)}},
	}
}

func prettyJSONSpec(v interface{}) uispec.Spec {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return uispec.Spec{{{Kind: uispec.KindHint, Text: "unserializable value"}}}
	}
	lines := strings.Split(string(raw), "\n")
	capped := 0
	if len(lines) > jsonLineCap {
		capped = len(lines) - jsonLineCap
		lines = lines[:jsonLineCap]
	}
	var spec uispec.Spec
	for _, l := range lines {
		spec = append(spec, uispec.Row{{Kind: uispec.KindText, Text: l, Size: 10.5}})
	}
	if capped > 0 {
		spec = append(spec, uispec.Row{{Kind: uispec.KindHint,
			Text: fmt.Sprintf("… %d more lines", capped)}})
	}
	return spec
}

// deriveArray classifies arrays: all numbers → series, flat same-key
// objects → dataset, all color hexes → palette, else json. Inspection
// is capped at inspectCap elements.
func deriveArray(arr []interface{}) Value {
	if len(arr) == 0 {
		return jsonValue(arr, "Array (empty)")
	}
	sample := arr
	if len(sample) > inspectCap {
		sample = sample[:inspectCap]
	}
	allNum, allColor, allObj := true, true, true
	for _, e := range sample {
		switch t := e.(type) {
		case float64, int64:
			allColor, allObj = false, false
		case string:
			allNum, allObj = false, false
			if !colorHexRe.MatchString(t) {
				allColor = false
			}
		case map[string]interface{}:
			allNum, allColor = false, false
		default:
			allNum, allColor, allObj = false, false, false
		}
	}
	switch {
	case allNum:
		return seriesValue(arr)
	case allColor:
		return paletteValue(arr)
	case allObj:
		if cols, ok := datasetColumns(sample); ok {
			return datasetValue(arr, cols)
		}
	}
	return jsonValue(arr, fmt.Sprintf("Array (%d items)", len(arr)))
}

func seriesValue(arr []interface{}) Value {
	vals := make([]float64, 0, len(arr))
	for _, e := range arr {
		switch t := e.(type) {
		case float64:
			vals = append(vals, t)
		case int64:
			vals = append(vals, float64(t))
		}
	}
	lo, hi := minMaxF(vals)
	pts := vals
	if len(pts) > seriesPtsCap {
		pts = pts[:seriesPtsCap]
	}

	// Table view: index/value pairs, capped.
	cells := make([][]string, 0, tableRowCap)
	for i, f := range vals {
		if i >= tableRowCap {
			break
		}
		cells = append(cells, []string{fmt.Sprintf("%d", i), trimNumStr(f)})
	}
	more := len(vals) - len(cells)

	return Value{
		Ptype:   "series",
		Summary: fmt.Sprintf("Series (%d points, %s … %s)", len(vals), trimNumStr(lo), trimNumStr(hi)),
		Raw:     rawJSON(arr),
		Views: []View{
			{Name: "sparkline", Spec: uispec.Spec{
				{{Kind: uispec.KindImage, Img: draw.Sparkline(pts, 280, 48)}},
				{{Kind: uispec.KindHint, Text: fmt.Sprintf("min %s · max %s · n=%d",
					trimNumStr(lo), trimNumStr(hi), len(vals))}},
			}},
			{Name: "bars", Spec: uispec.Spec{
				{{Kind: uispec.KindImage, Img: draw.BarStrip(pts, 280, 48)}},
			}},
			{Name: "table", Spec: uispec.Spec{{
				{Kind: uispec.KindTable, Columns: []string{"i", "value"}, Cells: cells, More: more},
			}}},
			{Name: "json", Spec: prettyJSONSpec(arr)},
		},
	}
}

func paletteValue(arr []interface{}) Value {
	var row uispec.Row
	for i, e := range arr {
		if i >= tableRowCap {
			break
		}
		hex, _ := e.(string)
		row = append(row, uispec.Seg{Kind: uispec.KindObject, Ptype: "color", Value: hex, Doc: "color " + hex})
	}
	spec := uispec.Spec{row}
	if len(arr) > tableRowCap {
		spec = append(spec, uispec.Row{{Kind: uispec.KindHint,
			Text: fmt.Sprintf("… %d more", len(arr)-tableRowCap)}})
	}
	return Value{
		Ptype:   "palette",
		Summary: fmt.Sprintf("Palette (%d colors)", len(arr)),
		Raw:     rawJSON(arr),
		Views: []View{
			{Name: "swatches", Spec: spec},
			{Name: "json", Spec: prettyJSONSpec(arr)},
		},
	}
}

// datasetColumns reports whether every sampled object has the same key
// set, returning the sorted column list.
func datasetColumns(sample []interface{}) ([]string, bool) {
	first, ok := sample[0].(map[string]interface{})
	if !ok || len(first) == 0 {
		return nil, false
	}
	cols := make([]string, 0, len(first))
	for k := range first {
		cols = append(cols, k)
	}
	sort.Strings(cols)
	for _, e := range sample[1:] {
		m, ok := e.(map[string]interface{})
		if !ok || len(m) != len(cols) {
			return nil, false
		}
		for _, c := range cols {
			if _, has := m[c]; !has {
				return nil, false
			}
		}
	}
	return cols, true
}

func datasetValue(arr []interface{}, cols []string) Value {
	cells := make([][]string, 0, tableRowCap)
	for i, e := range arr {
		if i >= tableRowCap {
			break
		}
		m, _ := e.(map[string]interface{})
		row := make([]string, len(cols))
		for c, col := range cols {
			row[c] = cellString(m[col])
		}
		cells = append(cells, row)
	}
	more := len(arr) - len(cells)

	// Schema view: column name + observed type of the first row.
	schemaCells := make([][]string, 0, len(cols))
	if first, ok := arr[0].(map[string]interface{}); ok {
		for _, c := range cols {
			schemaCells = append(schemaCells, []string{c, jsTypeName(first[c])})
		}
	}

	return Value{
		Ptype:   "dataset",
		Summary: fmt.Sprintf("Dataset (%d rows × %d cols)", len(arr), len(cols)),
		Raw:     rawJSON(arr),
		Views: []View{
			{Name: "table", Spec: uispec.Spec{{
				{Kind: uispec.KindTable, Columns: cols, Cells: cells, More: more},
			}}},
			{Name: "schema", Spec: uispec.Spec{{
				{Kind: uispec.KindTable, Columns: []string{"column", "type"}, Cells: schemaCells},
			}}},
			{Name: "json", Spec: prettyJSONSpec(arr)},
		},
	}
}

func cellString(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return trimNumStr(t)
	case bool:
		return fmt.Sprintf("%v", t)
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		s := string(raw)
		if len(s) > 40 {
			s = s[:40] + "…"
		}
		return s
	}
}

func jsTypeName(v interface{}) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case float64, int64:
		return "number"
	case bool:
		return "boolean"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	}
	return "unknown"
}

func trimNumStr(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%g", f)
}

func minMaxF(vals []float64) (float64, float64) {
	if len(vals) == 0 {
		return 0, 0
	}
	lo, hi := vals[0], vals[0]
	for _, v := range vals[1:] {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	return lo, hi
}
