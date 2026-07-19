// Package demoapps implements the standalone PBUI demo clients (run with
// `go-go-wm demo <name>`): the color lab, number field, and notes from the
// prototype, plus a file browser, a TODO list, and a markdown viewer. Each
// is an xapp.App — pure renderers over local state, verbs registered with
// the broker, cross-app behavior only through accept and the event bus.
package demoapps

import (
	"encoding/json"
	"fmt"
	"image"
	"math/rand"

	"github.com/go-go-golems/go-go-wm/pkg/apps"
	"github.com/go-go-golems/go-go-wm/pkg/apps/xapp"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
)

// Colors is the color lab (ports ColorsApp + the color verbs from
// actionsFor, pbui-shell.jsx:311-333 and 714-727).
type Colors struct {
	Swatches []string
	rng      *rand.Rand
}

func NewColors() *Colors {
	return &Colors{
		Swatches: []string{"#b0563f", "#d3b56a", "#9cb4c2", "#a9bda2", "#b3abc4", "#cfa08f"},
		rng:      rand.New(rand.NewSource(20260718)),
	}
}

func (c *Colors) Name() string  { return "demo-colors" }
func (c *Colors) Title() string { return "color lab" }

func (c *Colors) Verbs() []pbui.Verb {
	return []pbui.Verb{
		{ID: "color.mix", Label: "Mix with…  (accept a color)", Ptypes: []string{"color"}, Accepts: []string{"color"}},
		{ID: "color.lighten", Label: "Lighten (new swatch)", Ptypes: []string{"color"}},
		{ID: "color.remove", Label: "Remove swatch", Ptypes: []string{"color"}},
	}
}

func (c *Colors) Render(w, h int, accepting []string) (*image.RGBA, []apps.Region) {
	return apps.RenderColors(w, h, c.Swatches, accepting)
}

func (c *Colors) HandleAction(ctx xapp.Ctx, action string) {
	if action == "cmd:add-random" {
		hex := fmt.Sprintf("#%02x%02x%02x",
			80+c.rng.Intn(150), 80+c.rng.Intn(150), 80+c.rng.Intn(150))
		c.add(hex)
		ctx.Emit("color_added", map[string]string{"hex": hex, "note": "random"})
		ctx.Redraw()
	}
}

func (c *Colors) HandleVerb(ctx xapp.Ctx, verbID string, obj *pbui.Object) {
	if obj == nil {
		return
	}
	hex := obj.StringValue()
	switch verbID {
	case "color.mix":
		go func() {
			other, err := ctx.Broker.Accept(contextBg(), []string{"color"},
				"MIX "+hex+" — click another COLOR anywhere (Esc cancels)")
			if err != nil || other == nil {
				return
			}
			mixed := mixHex(hex, other.StringValue())
			ctx.Post(func() {
				c.add(mixed)
				a, _ := pbui.NewObject("color", hex)
				b, _ := pbui.NewObject("color", other.StringValue())
				m, _ := pbui.NewObject("color", mixed)
				ctx.Print(apps.Seg{Object: &a}, apps.Seg{Text: " (+) "},
					apps.Seg{Object: &b}, apps.Seg{Text: " = "}, apps.Seg{Object: &m})
				ctx.Emit("mixed", map[string]string{"a": hex, "b": other.StringValue(), "result": mixed})
				ctx.Redraw()
			})
		}()
	case "color.lighten":
		l := lighten(hex, 30)
		c.add(l)
		ctx.Emit("color_added", map[string]string{"hex": l, "note": "lighten " + hex})
		ctx.Redraw()
	case "color.remove":
		kept := c.Swatches[:0]
		for _, s := range c.Swatches {
			if s != hex {
				kept = append(kept, s)
			}
		}
		c.Swatches = kept
		ctx.Emit("color_removed", map[string]string{"hex": hex})
		ctx.Redraw()
	}
}

func (c *Colors) add(hex string) {
	for _, s := range c.Swatches {
		if s == hex {
			return
		}
	}
	c.Swatches = append(c.Swatches, hex)
}

func hexToRGB(s string) (int, int, int) {
	var r, g, b int
	if len(s) == 7 {
		_, _ = fmt.Sscanf(s[1:], "%02x%02x%02x", &r, &g, &b)
	}
	return r, g, b
}

func clamp255(x int) int {
	if x < 0 {
		return 0
	}
	if x > 255 {
		return 255
	}
	return x
}

func mixHex(a, b string) string {
	ar, ag, ab := hexToRGB(a)
	br, bg, bb := hexToRGB(b)
	return fmt.Sprintf("#%02x%02x%02x", (ar+br)/2, (ag+bg)/2, (ab+bb)/2)
}

func lighten(a string, d int) string {
	r, g, b := hexToRGB(a)
	return fmt.Sprintf("#%02x%02x%02x", clamp255(r+d), clamp255(g+d), clamp255(b+d))
}

// Numbers is the number field (ports NumbersApp + number verbs).
type Numbers struct{}

func (n *Numbers) Name() string  { return "demo-numbers" }
func (n *Numbers) Title() string { return "number field" }

func (n *Numbers) Verbs() []pbui.Verb {
	return []pbui.Verb{
		{ID: "number.multiply", Label: "Multiply by…  (accept a number)", Ptypes: []string{"number"}, Accepts: []string{"number"}},
		{ID: "number.factorize", Label: "Factorize", Ptypes: []string{"number"}},
	}
}

func (n *Numbers) Render(w, h int, accepting []string) (*image.RGBA, []apps.Region) {
	return apps.RenderNumbers(w, h, accepting)
}

func (n *Numbers) HandleAction(xapp.Ctx, string) {}

func (n *Numbers) HandleVerb(ctx xapp.Ctx, verbID string, obj *pbui.Object) {
	if obj == nil {
		return
	}
	var x float64
	_ = json.Unmarshal(obj.Value, &x)
	switch verbID {
	case "number.multiply":
		go func() {
			other, err := ctx.Broker.Accept(contextBg(), []string{"number"},
				fmt.Sprintf("MULTIPLY %v — click another NUMBER anywhere (Esc cancels)", x))
			if err != nil || other == nil {
				return
			}
			var y float64
			_ = json.Unmarshal(other.Value, &y)
			res, _ := pbui.NewObject("number", x*y)
			ctx.Print(apps.Seg{Text: fmt.Sprintf("%v x %v = ", x, y)}, apps.Seg{Object: &res})
			ctx.Emit("product", map[string]interface{}{"a": x, "b": y, "result": x * y})
		}()
	case "number.factorize":
		if x == float64(int(x)) && x >= 2 {
			ctx.Print(apps.Seg{Text: fmt.Sprintf("factors of %d: %v", int(x), factorize(int(x)))})
		}
	}
}

func factorize(n int) []int {
	var f []int
	m := n
	for d := 2; d*d <= m; d++ {
		for m%d == 0 {
			f = append(f, d)
			m /= d
		}
	}
	if m > 1 {
		f = append(f, m)
	}
	return f
}
