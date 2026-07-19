package pbui

import (
	"encoding/json"
	"testing"
)

func TestURIRoundTripScalar(t *testing.T) {
	cases := []struct {
		ptype string
		value interface{}
		uri   string
	}{
		{"color", "#b0563f", "pbui://color/%23b0563f"},
		{"file", "/etc/hosts", "pbui://file/%2Fetc%2Fhosts"},
		{"git-commit", "8d6d02f", "pbui://git-commit/8d6d02f"},
	}
	for _, c := range cases {
		o, err := NewObject(c.ptype, c.value)
		if err != nil {
			t.Fatal(err)
		}
		uri := ObjectToURI(o)
		if uri != c.uri {
			t.Errorf("ObjectToURI(%s %v) = %q, want %q", c.ptype, c.value, uri, c.uri)
		}
		back, err := ObjectFromURI(uri)
		if err != nil {
			t.Fatalf("ObjectFromURI(%q): %v", uri, err)
		}
		if back.Ptype != c.ptype || back.StringValue() != c.value {
			t.Errorf("round trip: got (%s, %s)", back.Ptype, back.StringValue())
		}
	}
}

func TestURIRoundTripStructured(t *testing.T) {
	o, _ := NewObject("point", map[string]int{"x": 3, "y": 4})
	uri := ObjectToURI(o)
	back, err := ObjectFromURI(uri)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]int
	if err := json.Unmarshal(back.Value, &m); err != nil {
		t.Fatal(err)
	}
	if m["x"] != 3 || m["y"] != 4 {
		t.Fatalf("structured round trip: %v", m)
	}
}

func TestObjectFromURIRejectsGarbage(t *testing.T) {
	for _, uri := range []string{
		"http://example.com",
		"pbui://",
		"not a uri at all \x00",
		"pbui:///novalue",
	} {
		if _, err := ObjectFromURI(uri); err == nil {
			t.Errorf("ObjectFromURI(%q) should fail", uri)
		}
	}
}

func TestTypeMatches(t *testing.T) {
	if !TypeMatches([]string{"any"}, "color") {
		t.Error("any should match color")
	}
	if !TypeMatches([]string{"color", "number"}, "number") {
		t.Error("list membership should match")
	}
	if TypeMatches([]string{"color"}, "number") {
		t.Error("color should not match number")
	}
}

func FuzzDecodeFrame(f *testing.F) {
	f.Add([]byte(`{"t":"hello","name":"x","roles":["app"],"protocol":1}`))
	f.Add([]byte(`{"t":"accept.start","seq":1,"ptypes":["color"],"prompt":"p"}`))
	f.Add([]byte(`{"t":"accept.answer","object":{"ptype":"color","value":"#fff"}}`))
	f.Add([]byte(`{`))
	f.Add([]byte(``))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, line []byte) {
		m, err := DecodeFrame(line)
		if err == nil && m.T == "" {
			t.Fatal("frame with empty t must error")
		}
	})
}
