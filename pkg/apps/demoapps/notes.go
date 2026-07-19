package demoapps

import (
	"context"
	"image"

	"github.com/go-go-golems/go-go-wm/pkg/apps"
	"github.com/go-go-golems/go-go-wm/pkg/apps/xapp"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
)

func contextBg() context.Context { return context.Background() }

// Notes is the notes/clipboard app (ports NotesApp, pbui-shell.jsx:350-374):
// collected objects remain live presentations.
type Notes struct {
	Items []apps.Note
	next  int
}

func NewNotes() *Notes { return &Notes{} }

func (n *Notes) Name() string  { return "demo-notes" }
func (n *Notes) Title() string { return "notes / clipboard" }

func (n *Notes) Verbs() []pbui.Verb {
	return []pbui.Verb{
		{ID: "any.collect", Label: "Collect into Notes", Ptypes: []string{"any"}},
		{ID: "note.remove", Label: "Remove note", Ptypes: []string{"note"}},
	}
}

func (n *Notes) Render(w, h int, accepting []string) (*image.RGBA, []apps.Region) {
	return apps.RenderNotes(w, h, n.Items, accepting)
}

func (n *Notes) HandleAction(ctx xapp.Ctx, action string) {
	if action != "cmd:collect" {
		return
	}
	go func() {
		obj, err := ctx.Broker.Accept(contextBg(), []string{"any"},
			"COLLECT — click ANY presentation, any tile (Esc cancels)")
		if err != nil || obj == nil {
			return
		}
		ctx.Post(func() {
			n.collect(ctx, *obj)
			ctx.Redraw()
		})
	}()
}

func (n *Notes) HandleVerb(ctx xapp.Ctx, verbID string, obj *pbui.Object) {
	if obj == nil {
		return
	}
	switch verbID {
	case "any.collect":
		n.collect(ctx, *obj)
		ctx.Redraw()
	case "note.remove":
		id := obj.StringValue()
		kept := n.Items[:0]
		for _, it := range n.Items {
			if itID := intToStr(it.ID); itID != id {
				kept = append(kept, it)
			}
		}
		n.Items = kept
		ctx.Emit("note_removed", map[string]string{"id": id})
		ctx.Redraw()
	}
}

func (n *Notes) collect(ctx xapp.Ctx, obj pbui.Object) {
	n.next++
	n.Items = append(n.Items, apps.Note{ID: n.next, Obj: obj})
	ctx.Emit("collected", map[string]interface{}{
		"ptype": obj.Ptype, "value": obj.StringValue(),
		"note": "collected object stays LIVE — it can still be accepted and menued here",
	})
}

func intToStr(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	p := len(buf)
	for i > 0 {
		p--
		buf[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		buf[p] = '-'
	}
	return string(buf[p:])
}
