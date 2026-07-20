package repl

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-go-golems/go-go-wm/pkg/apps/uispec"
)

func viewNames(v Value) []string {
	out := make([]string, len(v.Views))
	for i, view := range v.Views {
		out[i] = view.Name
	}
	return out
}

func TestDeriveScalars(t *testing.T) {
	if v := Derive(nil); v.Ptype != "" || v.Summary != "undefined" {
		t.Fatalf("nil: %+v", v)
	}
	if v := Derive(42.0); v.Ptype != "number" || v.Summary != "42" || v.Input != "42" {
		t.Fatalf("number: %+v", v)
	}
	if v := Derive(3.5); v.Summary != "3.5" {
		t.Fatalf("float: %+v", v)
	}
	if v := Derive(true); v.Ptype != "boolean" || v.Summary != "true" {
		t.Fatalf("bool: %+v", v)
	}
}

func TestDeriveColor(t *testing.T) {
	v := Derive("#aa5533")
	if v.Ptype != "color" || v.Summary != "#aa5533" {
		t.Fatalf("color: %+v", v)
	}
	if viewNames(v)[0] != "swatch" {
		t.Fatalf("default view must be swatch: %v", viewNames(v))
	}
	seg := v.Views[0].Spec[0][0]
	if seg.Kind != uispec.KindObject || seg.Ptype != "color" {
		t.Fatalf("swatch must be a live color object: %+v", seg)
	}
	// Non-color strings stay strings.
	if v := Derive("#aa55"); v.Ptype != "string" {
		t.Fatalf("short hex must be a string: %+v", v)
	}
}

func TestDeriveSeries(t *testing.T) {
	arr := []interface{}{1.0, 2.0, 5.0, 3.0}
	v := Derive(arr)
	if v.Ptype != "series" {
		t.Fatalf("series: %+v", v)
	}
	if v.Summary != "Series (4 points, 1 … 5)" {
		t.Fatalf("summary: %q", v.Summary)
	}
	names := viewNames(v)
	if names[0] != "sparkline" || names[len(names)-1] != "json" {
		t.Fatalf("views: %v", names)
	}
	if v.Views[0].Spec[0][0].Img == nil {
		t.Fatal("sparkline view must carry an image segment")
	}
}

func TestDeriveDatasetAndCaps(t *testing.T) {
	var arr []interface{}
	for i := 0; i < 50; i++ {
		arr = append(arr, map[string]interface{}{
			"name": "row", "n": float64(i), "tone": "#aabbcc",
		})
	}
	v := Derive(arr)
	if v.Ptype != "dataset" || v.Summary != "Dataset (50 rows × 3 cols)" {
		t.Fatalf("dataset: %+v", v)
	}
	table := v.Views[0].Spec[0][0]
	if table.Kind != uispec.KindTable || len(table.Cells) != tableRowCap || table.More != 30 {
		t.Fatalf("table cap: rows=%d more=%d", len(table.Cells), table.More)
	}
	if got := table.Columns; strings.Join(got, ",") != "n,name,tone" {
		t.Fatalf("columns must be sorted: %v", got)
	}
	// schema view present.
	if viewNames(v)[1] != "schema" {
		t.Fatalf("views: %v", viewNames(v))
	}
	// Ragged objects fall back to json.
	ragged := []interface{}{
		map[string]interface{}{"a": 1.0},
		map[string]interface{}{"b": 2.0},
	}
	if v := Derive(ragged); v.Ptype != "json" {
		t.Fatalf("ragged must be json: %+v", v)
	}
}

func TestDerivePalette(t *testing.T) {
	v := Derive([]interface{}{"#ff0000", "#00ff00"})
	if v.Ptype != "palette" || len(v.Views[0].Spec[0]) != 2 {
		t.Fatalf("palette: %+v", v)
	}
}

func TestDeriveJSONCapped(t *testing.T) {
	big := map[string]interface{}{}
	for i := 0; i < 100; i++ {
		big[strings.Repeat("k", 3)+trimNumStr(float64(i))] = float64(i)
	}
	v := Derive(big)
	if v.Ptype != "json" {
		t.Fatalf("json: %+v", v)
	}
	spec := v.Views[0].Spec
	last := spec[len(spec)-1][0]
	if last.Kind != uispec.KindHint || !strings.Contains(last.Text, "more lines") {
		t.Fatalf("json view must cap with a more-lines hint, last=%+v", last)
	}
	if len(spec) > jsonLineCap+1 {
		t.Fatalf("json view too tall: %d rows", len(spec))
	}
}

func TestNormalizeRich(t *testing.T) {
	good := map[string]interface{}{
		"ptype": "matrix", "summary": "2×2 matrix", "input": "m",
		"views": []interface{}{
			map[string]interface{}{"name": "grid", "rows": []interface{}{
				[]interface{}{map[string]interface{}{"kind": "text", "text": "1 2"}},
			}},
		},
	}
	// The value (the matrix data) becomes Raw, not the descriptor.
	matrix := [][]int{{1, 2}, {3, 4}}
	v, err := NormalizeRich(matrix, good)
	if err != nil {
		t.Fatal(err)
	}
	if v.Ptype != "matrix" || len(v.Views) != 1 || v.Views[0].Name != "grid" {
		t.Fatalf("rich: %+v", v)
	}
	// RC-4: Raw must be the exported VALUE, not the descriptor.
	var got [][]int
	if err := json.Unmarshal(v.Raw, &got); err != nil {
		t.Fatalf("Raw is not the matrix value: %v", err)
	}
	if len(got) != 2 || got[0][0] != 1 || got[1][1] != 4 {
		t.Fatalf("Raw payload wrong: %+v", got)
	}
	// Missing summary rejected.
	bad := map[string]interface{}{"ptype": "x", "views": good["views"]}
	if _, err := NormalizeRich(matrix, bad); err == nil || !strings.Contains(err.Error(), "summary") {
		t.Fatalf("missing summary: %v", err)
	}
	// Bad view rows carry coordinates.
	bad2 := map[string]interface{}{
		"summary": "s",
		"views": []interface{}{map[string]interface{}{"name": "v", "rows": []interface{}{
			[]interface{}{map[string]interface{}{"kind": "nope"}},
		}}},
	}
	if _, err := NormalizeRich(matrix, bad2); err == nil || !strings.Contains(err.Error(), `view "v"`) {
		t.Fatalf("bad rows: %v", err)
	}
	// Unknown top-level key rejected.
	bad3 := map[string]interface{}{"summary": "s", "views": good["views"], "extra": 1}
	if _, err := NormalizeRich(matrix, bad3); err == nil || !strings.Contains(err.Error(), "extra") {
		t.Fatalf("unknown key: %v", err)
	}
}
