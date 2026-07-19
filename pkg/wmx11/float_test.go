package wmx11

import (
	"testing"

	"github.com/jezek/xgb/xproto"

	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
)

func boolp(b bool) *bool { return &b }

// The decision table from the design doc: rule override →
// WM_TRANSIENT_FOR → window type → fixed size.
func TestFloatDecision(t *testing.T) {
	cases := []struct {
		name      string
		rule      *bool
		leader    uint32
		types     []string
		fixedSize bool
		want      bool
	}{
		{name: "plain window tiles", want: false},
		{name: "transient floats", leader: 42, want: true},
		{name: "dialog type floats", types: []string{"_NET_WM_WINDOW_TYPE_DIALOG"}, want: true},
		{name: "utility type floats", types: []string{"_NET_WM_WINDOW_TYPE_UTILITY"}, want: true},
		{name: "splash type floats", types: []string{"_NET_WM_WINDOW_TYPE_SPLASH"}, want: true},
		{name: "toolbar type floats", types: []string{"_NET_WM_WINDOW_TYPE_TOOLBAR"}, want: true},
		{name: "normal type tiles", types: []string{"_NET_WM_WINDOW_TYPE_NORMAL"}, want: false},
		{name: "mixed list floats on any float type",
			types: []string{"_NET_WM_WINDOW_TYPE_NORMAL", "_NET_WM_WINDOW_TYPE_DIALOG"}, want: true},
		{name: "fixed size floats", fixedSize: true, want: true},
		{name: "rule true forces float", rule: boolp(true), want: true},
		{name: "rule false beats transient", rule: boolp(false), leader: 42, want: false},
		{name: "rule false beats dialog type",
			rule: boolp(false), types: []string{"_NET_WM_WINDOW_TYPE_DIALOG"}, want: false},
		{name: "rule true beats normal type",
			rule: boolp(true), types: []string{"_NET_WM_WINDOW_TYPE_NORMAL"}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := floatDecision(tc.rule, xproto.Window(tc.leader), tc.types, tc.fixedSize)
			if got != tc.want {
				t.Fatalf("floatDecision = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFloatRuleMatching(t *testing.T) {
	w := &WM{}
	if err := w.SetFloatRules([]FloatRule{
		{Class: "Galculator", Float: true},
		{Title: "^mpv$", Float: false},
	}); err != nil {
		t.Fatal(err)
	}
	if v := w.floatRuleVerdict("Calculator", "Galculator", "galculator"); v == nil || !*v {
		t.Fatalf("class rule should force float, got %v", v)
	}
	// Class matches the instance part too (i3 semantics).
	if v := w.floatRuleVerdict("x", "Other", "galculator"); v == nil || !*v {
		t.Fatalf("instance match should force float, got %v", v)
	}
	if v := w.floatRuleVerdict("mpv", "mpv", "mpv"); v == nil || *v {
		t.Fatalf("title rule should force tile, got %v", v)
	}
	if v := w.floatRuleVerdict("emacs", "Emacs", "emacs"); v != nil {
		t.Fatalf("no rule should match, got %v", *v)
	}
	if err := w.SetFloatRules([]FloatRule{{Class: "(", Float: true}}); err == nil {
		t.Fatal("invalid regexp must reject the set")
	}
	if err := w.SetFloatRules([]FloatRule{{Float: true}}); err == nil {
		t.Fatal("pattern-less rule must reject the set")
	}
}

func TestClampFloatRect(t *testing.T) {
	w := &WM{area: wmcore.Rect{X: 4, Y: 28, W: 1592, H: 816}}
	// A rect dragged far right keeps 40px of strip on screen.
	r := w.clampFloatRect(wmcore.Rect{X: 4000, Y: 100, W: 400, H: 300})
	if r.X != w.area.X+w.area.W-40 {
		t.Fatalf("right clamp: X = %d", r.X)
	}
	// Never above the work area (the strip must stay grabbable).
	r = w.clampFloatRect(wmcore.Rect{X: 100, Y: -500, W: 400, H: 300})
	if r.Y != w.area.Y {
		t.Fatalf("top clamp: Y = %d", r.Y)
	}
	// A rect inside the area is untouched.
	in := wmcore.Rect{X: 200, Y: 200, W: 400, H: 300}
	if got := w.clampFloatRect(in); got != in {
		t.Fatalf("interior rect changed: %+v", got)
	}
}
