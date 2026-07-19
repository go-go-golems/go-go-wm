package uispec

import (
	"encoding/json"
	"strings"
	"testing"
)

func row(segs ...interface{}) []interface{} { return segs }

func seg(kv ...interface{}) map[string]interface{} {
	m := map[string]interface{}{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

func TestNormalizeAcceptsTheFullVocabulary(t *testing.T) {
	spec, err := Normalize([]interface{}{
		row(seg("kind", "text", "text", "TITLE", "bold", true, "size", 14.0)),
		row(
			seg("kind", "object", "ptype", "color", "value", "#b0563f", "doc", "a color"),
			seg("kind", "object", "ptype", "word", "value", "hello", "label", "hi"),
		),
		row(
			seg("kind", "button", "text", "Add", "action", "add", "color", "rose"),
			seg("kind", "hint", "text", "click things"),
		),
		row(), // spacer
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(spec) != 4 || len(spec[1]) != 2 || spec[0][0].Bold != true {
		t.Fatalf("spec = %+v", spec)
	}
	if spec[2][0].Action != "add" || spec[2][0].Color != "rose" {
		t.Fatalf("button seg = %+v", spec[2][0])
	}
}

func TestNormalizeRejectsHostileShapes(t *testing.T) {
	cases := []struct {
		name string
		raw  interface{}
		want string
	}{
		{"not-array", "nope", "array of rows"},
		{"row-not-array", []interface{}{"x"}, "ui.row"},
		{"seg-not-map", []interface{}{row("x")}, "segment must be"},
		{"unknown-kind", []interface{}{row(seg("kind", "sparkle"))}, "unknown segment kind"},
		{"unknown-key", []interface{}{row(seg("kind", "text", "text", "a", "frob", 1))}, "unknown segment key"},
		{"bad-ptype", []interface{}{row(seg("kind", "object", "ptype", "no spaces", "value", 1))}, "slug"},
		{"button-no-action", []interface{}{row(seg("kind", "button", "text", "x"))}, "action name"},
		{"button-bad-color", []interface{}{row(seg("kind", "button", "text", "x", "action", "a", "color", "plaid"))}, "unknown"},
		{"size-out-of-range", []interface{}{row(seg("kind", "text", "text", "x", "size", 200.0))}, "out of range"},
	}
	for _, c := range cases {
		_, err := Normalize(c.raw)
		if err == nil {
			t.Errorf("%s: accepted", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q lacks %q", c.name, err, c.want)
		}
	}
}

func TestRenderEmitsRegionsWithTheClickContract(t *testing.T) {
	spec, err := Normalize([]interface{}{
		row(seg("kind", "text", "text", "COLORS", "bold", true)),
		row(
			seg("kind", "object", "ptype", "color", "value", "#b0563f"),
			seg("kind", "object", "ptype", "color", "value", "#5a7a58"),
		),
		row(seg("kind", "button", "text", "Add", "action", "add")),
	})
	if err != nil {
		t.Fatal(err)
	}
	img, regions := Render(400, 300, spec, nil)
	if img.Bounds().Dx() != 400 {
		t.Fatal("surface size wrong")
	}
	var objects, actions int
	for _, r := range regions {
		if r.Rect.Empty() {
			t.Fatalf("empty region rect: %+v", r)
		}
		if r.Object != nil {
			objects++
			if r.Object.Ptype != "color" {
				t.Fatalf("object region ptype: %+v", r.Object)
			}
		}
		if r.Action != "" {
			actions++
			if r.Action != "cmd:add" {
				t.Fatalf("action = %q, want cmd:add", r.Action)
			}
		}
	}
	if objects != 2 || actions != 1 {
		t.Fatalf("regions: %d objects, %d actions (want 2, 1)", objects, actions)
	}
	// Regions must not overlap (layout sanity).
	for i := range regions {
		for j := i + 1; j < len(regions); j++ {
			if regions[i].Rect.Overlaps(regions[j].Rect) {
				t.Fatalf("regions %d and %d overlap: %v %v", i, j, regions[i].Rect, regions[j].Rect)
			}
		}
	}
}

func TestRenderWrapsInsteadOfOverflowing(t *testing.T) {
	rowSegs := make([]interface{}, 0, 20)
	for i := 0; i < 20; i++ {
		rowSegs = append(rowSegs, seg("kind", "object", "ptype", "word", "value", "wrapped-word"))
	}
	spec, err := Normalize([]interface{}{rowSegs})
	if err != nil {
		t.Fatal(err)
	}
	_, regions := Render(300, 600, spec, nil)
	if len(regions) != 20 {
		t.Fatalf("regions = %d, want 20", len(regions))
	}
	for _, r := range regions {
		if r.Rect.Max.X > 300 {
			t.Fatalf("region overflows surface: %v", r.Rect)
		}
	}
}

func TestSpecIsJSONStable(t *testing.T) {
	// The spec is data; it must survive a JSON round trip untouched
	// (script tiles hand snapshots across goroutines as values).
	spec, _ := Normalize([]interface{}{
		row(seg("kind", "object", "ptype", "color", "value", "#fff", "label", "w")),
	})
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	var back Spec
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back[0][0].Ptype != "color" || back[0][0].Label != "w" {
		t.Fatalf("round trip lost data: %+v", back)
	}
}
