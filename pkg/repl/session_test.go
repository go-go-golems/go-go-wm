package repl

import (
	"strings"
	"testing"

	"github.com/go-go-golems/go-go-wm/pkg/apps/uispec"
)

func specText(spec uispec.Spec) string {
	var b strings.Builder
	for _, row := range spec {
		for _, seg := range row {
			b.WriteString(seg.Text)
			b.WriteString(seg.Label)
			b.WriteString("|")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func TestSessionSubmitCompleteSpec(t *testing.T) {
	s := &Session{Input: "1+1"}
	n := s.Submit()
	if n != 1 || s.Input != "" || s.Cells[0].Status != StatusEvaluating {
		t.Fatalf("submit: %+v", s)
	}
	spec := s.Spec(100)
	if !strings.Contains(specText(spec), "evaluating") {
		t.Fatalf("pending cell must show evaluating: %s", specText(spec))
	}
	v := Derive(2.0)
	s.Complete(1, []string{"log line"}, "", "2", &v)
	text := specText(s.Spec(100))
	for _, want := range []string{"In[1]", "1+1", "log line", "Out[1]", "2"} {
		if !strings.Contains(text, want) {
			t.Fatalf("spec missing %q:\n%s", want, text)
		}
	}
	// The Out chip is a real presentation.
	found := false
	for _, row := range s.Spec(100) {
		for _, seg := range row {
			if seg.Kind == uispec.KindObject && seg.Ptype == "number" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("Out row must carry a live number object")
	}
	// The input field is always last and focused.
	last := s.Spec(100)[len(s.Spec(100))-1][0]
	if last.Kind != uispec.KindField || !last.Focus {
		t.Fatalf("last row must be the focused field: %+v", last)
	}
}

func TestSessionErrorAndStatementFallback(t *testing.T) {
	s := &Session{Input: "nope("}
	s.Submit()
	s.Complete(1, nil, "SyntaxError: unexpected end", "", nil)
	if !strings.Contains(specText(s.Spec(100)), "! SyntaxError") {
		t.Fatal("error cells must show the error")
	}
	s.Input = "let x = 5"
	s.Submit()
	s.Complete(2, nil, "", "undefined", nil)
	if strings.Contains(specText(s.Spec(100)), "Out[2]") {
		t.Fatal("undefined statement results must not render an Out row")
	}
}

func TestSessionHistory(t *testing.T) {
	s := &Session{}
	for _, in := range []string{"a", "b"} {
		s.Input = in
		s.Submit()
	}
	s.Input = "draft"
	s.HistoryPrev()
	if s.Input != "b" {
		t.Fatalf("prev: %q", s.Input)
	}
	s.HistoryPrev()
	if s.Input != "a" {
		t.Fatalf("prev prev: %q", s.Input)
	}
	s.HistoryPrev() // clamped
	if s.Input != "a" {
		t.Fatalf("clamp: %q", s.Input)
	}
	s.HistoryNext()
	s.HistoryNext()
	if s.Input != "draft" {
		t.Fatalf("returning forward must restore the draft: %q", s.Input)
	}
}

func TestSessionViewsAndFold(t *testing.T) {
	s := &Session{Input: "[1,2,3]"}
	s.Submit()
	v := Derive([]interface{}{1.0, 2.0, 3.0})
	s.Complete(1, nil, "", "", &v)
	text := specText(s.Spec(100))
	if !strings.Contains(text, "· sparkline") {
		t.Fatalf("view switcher must mark the selected view:\n%s", text)
	}
	s.SetView(1, 3) // json
	if s.Cells[0].View != 3 {
		t.Fatalf("view: %d", s.Cells[0].View)
	}
	s.SetView(1, 99) // out of range ignored
	if s.Cells[0].View != 3 {
		t.Fatal("oob view must be ignored")
	}
	rows := len(s.Spec(100))
	s.ToggleFold(1)
	if got := len(s.Spec(100)); got >= rows {
		t.Fatalf("folding must shrink the spec: %d → %d", rows, got)
	}
}

func TestSessionScrollWindow(t *testing.T) {
	s := &Session{}
	for i := 0; i < 30; i++ {
		s.Input = "x"
		s.Submit()
		s.Complete(i+1, nil, "", "1", nil)
	}
	spec := s.Spec(10)
	// budget rows + earlier-rows hint + field.
	if len(spec) > 12 {
		t.Fatalf("window too big: %d", len(spec))
	}
	if !strings.Contains(specText(spec), "earlier rows") {
		t.Fatal("window must show the earlier-rows hint")
	}
	// Bottom-anchored: the last cell is visible, the first is not.
	text := specText(spec)
	if !strings.Contains(text, "In[30]") || strings.Contains(text, "In[1]|") {
		t.Fatalf("window anchor wrong:\n%s", text)
	}
	s.ScrollBy(1000, 10)
	if !strings.Contains(specText(s.Spec(10)), "In[1]") {
		t.Fatal("scrolled to top must show In[1]")
	}
	s.ScrollBy(-2000, 10)
	if !strings.Contains(specText(s.Spec(10)), "In[30]") {
		t.Fatal("scroll clamps back to the bottom")
	}
}
