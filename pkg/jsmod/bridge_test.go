package jsmod

import (
	"encoding/json"
	"testing"

	"github.com/go-go-golems/go-go-wm/pkg/pbui"
)

func TestObjectRoundTrip(t *testing.T) {
	obj, err := pbui.NewObject("color", "#33302a")
	if err != nil {
		t.Fatal(err)
	}
	obj.Label = "ink"
	js := ObjectToJS(&obj)
	back, err := JSToObject(js)
	if err != nil {
		t.Fatal(err)
	}
	if back.Ptype != "color" || back.StringValue() != "#33302a" || back.Label != "ink" {
		t.Fatalf("round trip lost data: %+v", back)
	}
}

func TestJSToObjectRejectsBadShapes(t *testing.T) {
	for _, v := range []interface{}{
		nil, "string", 42, []interface{}{"a"},
		map[string]interface{}{},            // no ptype
		map[string]interface{}{"ptype": 7},  // wrong type
		map[string]interface{}{"ptype": ""}, // empty
	} {
		if _, err := JSToObject(v); err == nil {
			t.Errorf("JSToObject(%v) should fail", v)
		}
	}
}

func TestOpFromJS(t *testing.T) {
	op, err := OpFromJS(map[string]interface{}{"op": "split-leaf", "node": "l1", "dir": "row"})
	if err != nil {
		t.Fatal(err)
	}
	if op.Op != "split-leaf" || string(op.Node) != "l1" {
		t.Fatalf("op = %+v", op)
	}
	if _, err := OpFromJS(map[string]interface{}{"node": "l1"}); err == nil {
		t.Fatal("missing op name should fail")
	}
	if _, err := OpFromJS(func() {}); err == nil {
		t.Fatal("unmarshalable value should fail")
	}
}

func TestSegsToWire(t *testing.T) {
	segs, err := SegsToWire([]interface{}{
		"hello ", map[string]interface{}{"ptype": "color", "value": "#fff"}, 3.5, nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 4 || segs[0]["text"] != "hello " || segs[1]["ptype"] != "color" ||
		segs[2]["text"] != "3.5" || segs[3]["text"] != "null" {
		t.Fatalf("segs = %v", segs)
	}
	if _, err := SegsToWire([]interface{}{map[string]interface{}{"ptype": 1}}); err == nil {
		t.Fatal("bad object seg should fail")
	}
}

// FuzzBridge feeds arbitrary JSON-shaped input through the converters:
// they must reject or convert, never panic (design doc 02, test 3).
func FuzzBridge(f *testing.F) {
	f.Add([]byte(`{"ptype":"color","value":"#fff"}`))
	f.Add([]byte(`{"op":"split-leaf","ratio":1e308}`))
	f.Add([]byte(`[{"nested":{"deep":[1,2,{"ptype":null}]}}]`))
	f.Add([]byte(`{"ptype":"\\u0000","value":{"v":-0.0}}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		var v interface{}
		if err := json.Unmarshal(raw, &v); err != nil {
			return
		}
		_, _ = JSToObject(v)
		_, _ = OpFromJS(v)
		_, _ = SegsToWire([]interface{}{v})
		if m, ok := v.(map[string]interface{}); ok {
			if obj, err := JSToObject(m); err == nil {
				// Whatever converts must survive the URI bijection.
				if _, err := pbui.ObjectFromURI(pbui.ObjectToURI(obj)); err != nil {
					t.Fatalf("URI round trip failed for %+v: %v", obj, err)
				}
			}
		}
	})
}
