package demoapps

import (
	"image"

	"github.com/go-go-golems/go-go-wm/pkg/apps"
	"github.com/go-go-golems/go-go-wm/pkg/apps/xapp"
	"github.com/go-go-golems/go-go-wm/pkg/draw"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
)

// Todo is a TODO list whose items are <todo> presentations. Type to fill
// the input line, Enter adds; left click toggles done (primary action);
// right click menus (toggle / remove / promote from any object). "Add from
// accept…" turns any presentation on screen into a task — collect a color,
// a file, a git commit.
type Todo struct {
	Items []todoItem
	Input string
	next  int
}

type todoItem struct {
	id   int
	text string
	done bool
}

func NewTodo() *Todo { return &Todo{} }

func (t *Todo) Name() string  { return "demo-todo" }
func (t *Todo) Title() string { return "todo list" }

func (t *Todo) Verbs() []pbui.Verb {
	return []pbui.Verb{
		{ID: "todo.toggle", Label: "Toggle done", Ptypes: []string{"todo"}},
		{ID: "todo.remove", Label: "Remove", Ptypes: []string{"todo"}},
		{ID: "any.todo", Label: "Make a TODO of this", Ptypes: []string{"any"}},
	}
}

func (t *Todo) Render(w, h int, accepting []string) (*image.RGBA, []apps.Region) {
	img := apps.NewSurface(w, h)
	var regions []apps.Region

	// Input line.
	field := image.Rect(8, 8, w-130, 30)
	draw.Fill(img, field, draw.Current().Field)
	draw.Border(img, field, 2, draw.Current().Ink)
	text := t.Input
	if text == "" {
		draw.Text(img, 14, 23, "type a task, Enter adds", false, 11, draw.Current().Faint)
	} else {
		draw.Text(img, 14, 23, text+"_", false, 11, draw.Current().Ink)
	}
	r := apps.Btn(img, w-118, 8, "From accept…", draw.Current().Mustard)
	regions = append(regions, apps.Region{Rect: r, Action: "cmd:from-accept",
		Doc: "accept ANY presentation and make a task of it"})

	y := 44
	for _, it := range t.Items {
		if y > h-22 {
			break
		}
		box := image.Rect(10, y+2, 24, y+16)
		draw.Fill(img, box, draw.Current().Field)
		draw.Border(img, box, 2, draw.Current().Ink)
		if it.done {
			draw.Fill(img, box.Inset(4), draw.Current().Sage)
		}
		tone := draw.Current().Ink
		if it.done {
			tone = draw.Current().Faint
		}
		draw.Text(img, 32, y+14, it.text, false, 11.5, tone)
		if it.done {
			tw := draw.TextWidth(it.text, false, 11.5)
			draw.Fill(img, image.Rect(32, y+9, 32+tw, y+10), draw.Current().Faint)
		}
		obj, _ := pbui.NewObject("todo", it.id)
		obj.Label = it.text
		hl := len(accepting) > 0 && pbui.TypeMatches(accepting, "todo")
		row := image.Rect(8, y, w-8, y+18)
		if hl {
			draw.Border(img, row, 1, draw.Current().Red)
		}
		regions = append(regions, apps.Region{
			Rect: row, Object: &obj, Action: "toggle:" + intToStr(it.id),
			Doc: "todo: " + it.text + " — L: toggle done · R: menu",
		})
		y += 22
	}
	if len(t.Items) == 0 {
		apps.Hint(img, 8, y+12, "no tasks yet")
	}
	return img, regions
}

func (t *Todo) HandleAction(ctx xapp.Ctx, action string) {
	switch {
	case action == "cmd:from-accept":
		go func() {
			obj, err := ctx.Broker.Accept(contextBg(), []string{"any"},
				"TODO — click ANY presentation to make a task of it (Esc cancels)")
			if err != nil || obj == nil {
				return
			}
			ctx.Post(func() {
				t.add("handle <" + obj.Ptype + "> " + obj.StringValue())
				ctx.Emit("todo_added", map[string]string{"from": obj.Ptype})
				ctx.Redraw()
			})
		}()
	case len(action) > 7 && action[:7] == "toggle:":
		t.toggle(action[7:])
		ctx.Redraw()
	}
}

func (t *Todo) HandleVerb(ctx xapp.Ctx, verbID string, obj *pbui.Object) {
	if obj == nil {
		return
	}
	switch verbID {
	case "todo.toggle":
		t.toggle(obj.StringValue())
	case "todo.remove":
		id := obj.StringValue()
		kept := t.Items[:0]
		for _, it := range t.Items {
			if intToStr(it.id) != id {
				kept = append(kept, it)
			}
		}
		t.Items = kept
	case "any.todo":
		t.add("handle <" + obj.Ptype + "> " + obj.StringValue())
	}
	ctx.Redraw()
}

func (t *Todo) HandleKey(ctx xapp.Ctx, key string) {
	switch key {
	case "Return", "KP_Enter":
		if t.Input != "" {
			t.add(t.Input)
			t.Input = ""
			ctx.Emit("todo_added", map[string]string{"via": "keyboard"})
		}
	case "BackSpace":
		if len(t.Input) > 0 {
			t.Input = t.Input[:len(t.Input)-1]
		}
	case "space":
		t.Input += " "
	default:
		if len(key) == 1 {
			t.Input += key
		}
	}
	ctx.Redraw()
}

func (t *Todo) add(text string) {
	t.next++
	t.Items = append(t.Items, todoItem{id: t.next, text: text})
}

func (t *Todo) toggle(id string) {
	for i := range t.Items {
		if intToStr(t.Items[i].id) == id {
			t.Items[i].done = !t.Items[i].done
		}
	}
}
