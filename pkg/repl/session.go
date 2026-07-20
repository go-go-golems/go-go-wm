package repl

import (
	"fmt"

	"github.com/go-go-golems/go-go-wm/pkg/apps/uispec"
)

// CellStatus is a cell's lifecycle state.
type CellStatus string

const (
	StatusEvaluating CellStatus = "evaluating"
	StatusDone       CellStatus = "done"
	StatusError      CellStatus = "error"
)

// Cell is one In/Out pair.
type Cell struct {
	N       int
	Input   string
	Status  CellStatus
	Console []string
	Error   string
	Result  string // kernel's string form (fallback when Value is nil)
	Value   *Value
	View    int // selected view index
	Folded  bool
}

// Session is the surface model: completed cells plus the live input
// line. Pure data — the kernel and the host live elsewhere.
type Session struct {
	Cells   []Cell
	Input   string   // the editor line
	History []string // submitted inputs, oldest first
	histPos int      // history cursor (len(History) = editing fresh)
	pending string   // input stashed while browsing history
	Scroll  int      // rows scrolled up from the bottom
}

// Submit moves the input line into a new evaluating cell and returns
// its index number (1-based, In[n]).
func (s *Session) Submit() int {
	n := len(s.Cells) + 1
	s.Cells = append(s.Cells, Cell{N: n, Input: s.Input, Status: StatusEvaluating})
	s.History = append(s.History, s.Input)
	s.histPos = len(s.History)
	s.Input = ""
	s.Scroll = 0 // snap to the bottom on submit
	return n
}

// Complete fills in cell n's outcome.
func (s *Session) Complete(n int, console []string, errText, result string, v *Value) {
	if n < 1 || n > len(s.Cells) {
		return
	}
	c := &s.Cells[n-1]
	c.Console = console
	c.Result = result
	if errText != "" {
		c.Status = StatusError
		c.Error = errText
		return
	}
	c.Status = StatusDone
	c.Value = v
}

// HistoryPrev / HistoryNext move the editor line through past inputs.
func (s *Session) HistoryPrev() {
	if len(s.History) == 0 || s.histPos == 0 {
		return
	}
	if s.histPos == len(s.History) {
		s.pending = s.Input
	}
	s.histPos--
	s.Input = s.History[s.histPos]
}

func (s *Session) HistoryNext() {
	if s.histPos >= len(s.History) {
		return
	}
	s.histPos++
	if s.histPos == len(s.History) {
		s.Input = s.pending
		return
	}
	s.Input = s.History[s.histPos]
}

// SetView selects a view on cell n (bounds-checked).
func (s *Session) SetView(n, view int) {
	if n < 1 || n > len(s.Cells) {
		return
	}
	c := &s.Cells[n-1]
	if c.Value == nil || view < 0 || view >= len(c.Value.Views) {
		return
	}
	c.View = view
}

// ToggleFold folds/unfolds cell n's output.
func (s *Session) ToggleFold(n int) {
	if n >= 1 && n <= len(s.Cells) {
		s.Cells[n-1].Folded = !s.Cells[n-1].Folded
	}
}

// ScrollBy moves the window (positive = towards older rows); the
// renderer clamps against the actual row count.
func (s *Session) ScrollBy(delta, budget int) {
	total := len(s.allRows())
	s.Scroll += delta
	maxScroll := total - budget
	if maxScroll < 0 {
		maxScroll = 0
	}
	if s.Scroll > maxScroll {
		s.Scroll = maxScroll
	}
	if s.Scroll < 0 {
		s.Scroll = 0
	}
}

// Spec renders the session, bottom-anchored: the last `budget` rows
// (minus Scroll) plus the input field, which is always visible.
func (s *Session) Spec(budget int) uispec.Spec {
	rows := s.allRows()
	if budget < 1 {
		budget = 1
	}
	if len(rows) > budget {
		end := len(rows) - s.Scroll
		if end > len(rows) {
			end = len(rows)
		}
		start := end - budget
		if start < 0 {
			start = 0
			end = minI(budget, len(rows))
		}
		windowed := make(uispec.Spec, 0, budget+2)
		if start > 0 {
			windowed = append(windowed, uispec.Row{{Kind: uispec.KindHint,
				Text: fmt.Sprintf("… %d earlier rows (PgUp scrolls)", start)}})
		}
		windowed = append(windowed, rows[start:end]...)
		rows = windowed
	}
	rows = append(rows, uispec.Row{{
		Kind: uispec.KindField, Text: s.Input, Action: "input", Focus: true,
		Doc: "In[" + fmt.Sprintf("%d", len(s.Cells)+1) + "] — Enter evaluates · ↑↓ history · PgUp/PgDn scroll",
	}})
	return rows
}

// allRows builds every cell's rows (unwindowed).
func (s *Session) allRows() uispec.Spec {
	var out uispec.Spec
	for i := range s.Cells {
		out = append(out, s.cellRows(&s.Cells[i])...)
	}
	return out
}

func (s *Session) cellRows(c *Cell) uispec.Spec {
	var out uispec.Spec
	out = append(out, uispec.Row{
		{Kind: uispec.KindHint, Text: fmt.Sprintf("In[%d]", c.N)},
		{Kind: uispec.KindText, Text: c.Input, Bold: true},
	})
	for _, line := range c.Console {
		out = append(out, uispec.Row{{Kind: uispec.KindHint, Text: "  " + line}})
	}
	switch c.Status {
	case StatusEvaluating:
		out = append(out, uispec.Row{{Kind: uispec.KindHint, Text: "  evaluating…"}})
	case StatusError:
		out = append(out, uispec.Row{
			{Kind: uispec.KindText, Text: "! " + c.Error, Size: 10.5},
		})
	case StatusDone:
		out = append(out, s.outRows(c)...)
	}
	return out
}

// outRows renders Out[n]: the presentation chip (real ptype — it
// answers accepts), the fold toggle, the view switcher, and the
// selected view's rows.
func (s *Session) outRows(c *Cell) uispec.Spec {
	v := c.Value
	if v == nil {
		// Statement fallback: the kernel's string form only.
		if c.Result == "" || c.Result == "undefined" {
			return nil
		}
		return uispec.Spec{{
			{Kind: uispec.KindHint, Text: fmt.Sprintf("Out[%d]", c.N)},
			{Kind: uispec.KindText, Text: c.Result},
		}}
	}
	head := uispec.Row{{Kind: uispec.KindHint, Text: fmt.Sprintf("Out[%d]", c.N)}}
	if v.Ptype != "" {
		head = append(head, uispec.Seg{
			Kind: uispec.KindObject, Ptype: v.Ptype, Value: rawAny(v),
			Label: v.Summary, Doc: v.Doc,
		})
	} else {
		head = append(head, uispec.Seg{Kind: uispec.KindText, Text: v.Summary})
	}
	fold := "fold"
	if c.Folded {
		fold = "unfold"
	}
	head = append(head, uispec.Seg{Kind: uispec.KindButton, Text: fold,
		Action: fmt.Sprintf("fold:%d", c.N), Doc: "collapse/expand this output"})
	out := uispec.Spec{head}
	if c.Folded || len(v.Views) == 0 {
		return out
	}
	if len(v.Views) > 1 {
		var switcher uispec.Row
		for i, view := range v.Views {
			name := view.Name
			if i == c.View {
				name = "· " + name
			}
			switcher = append(switcher, uispec.Seg{Kind: uispec.KindButton, Text: name,
				Action: fmt.Sprintf("view:%d:%d", c.N, i), Color: "sage",
				Doc: "switch Out[" + fmt.Sprintf("%d", c.N) + "] to the " + view.Name + " view"})
		}
		out = append(out, switcher)
	}
	sel := c.View
	if sel >= len(v.Views) {
		sel = 0
	}
	out = append(out, v.Views[sel].Spec...)
	return out
}

// rawAny decodes Value.Raw for the object segment payload (object
// segments marshal their Value; round-tripping keeps it JSON-clean).
func rawAny(v *Value) interface{} {
	var out interface{}
	if len(v.Raw) > 0 {
		if err := jsonUnmarshal(v.Raw, &out); err == nil {
			return out
		}
	}
	return v.Summary
}

func minI(a, b int) int {
	if a < b {
		return a
	}
	return b
}
