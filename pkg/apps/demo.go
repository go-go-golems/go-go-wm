package apps

import (
	"fmt"
	"image"

	"github.com/go-go-golems/go-go-wm/pkg/draw"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
)

// Renderers for the standalone demo clients (go-go-wm demo <name>). Pure
// functions of app state — the xapp shell owns the window and the broker.

// RenderColors ports ColorsApp (pbui-shell.jsx:311-333): a grid of <color>
// swatches plus an add button.
func RenderColors(w, h int, colors []string, accepting []string) (*image.RGBA, []Region) {
	img := NewSurface(w, h)
	var regions []Region
	y := Hint(img, 8, 16, "swatches are <color> presentations · right-click one → Mix with… then")
	y = Hint(img, 8, y+12, "click ANY color anywhere (listener chips, notes, other tiles)")
	y += 10
	x := 8
	for _, hex := range colors {
		card := image.Rect(x, y, x+56, y+52)
		if x+60 > w {
			x = 8
			y += 60
			card = image.Rect(x, y, x+56, y+52)
		}
		hl := len(accepting) > 0 && pbui.TypeMatches(accepting, "color")
		bg := draw.PaneAlt
		if hl {
			bg = draw.Sel
		}
		draw.Fill(img, card, bg)
		draw.Border(img, card, 2, draw.Ink)
		if hl {
			draw.Border(img, card.Inset(-2), 2, draw.Red)
		}
		draw.Fill(img, image.Rect(x+5, y+5, x+51, y+33), parseHex(hex))
		draw.Border(img, image.Rect(x+5, y+5, x+51, y+33), 1, draw.Ink)
		draw.Text(img, x+5, y+46, hex, false, 9, draw.Ink)
		obj, _ := pbui.NewObject("color", hex)
		regions = append(regions, Region{Rect: card, Object: &obj, Doc: "color " + hex})
		x += 62
	}
	y += 62
	r := Btn(img, 8, y, "Add random swatch", draw.Rose)
	regions = append(regions, Region{Rect: r, Action: "cmd:add-random", Doc: "add a random swatch"})
	return img, regions
}

// RenderNumbers ports NumbersApp (pbui-shell.jsx:335-348): 2..61 as
// <number> presentations, primes shaded.
func RenderNumbers(w, h int, accepting []string) (*image.RGBA, []Region) {
	img := NewSurface(w, h)
	var regions []Region
	y := Hint(img, 8, 16, "a field of <number> presentations (primes shaded) · right-click one:")
	y = Hint(img, 8, y+12, "factorize, multiply-with… · the listener's Sum… picks them up")
	y += 10
	x := 8
	hl := len(accepting) > 0 && pbui.TypeMatches(accepting, "number")
	for n := 2; n <= 61; n++ {
		label := fmt.Sprintf("%d", n)
		bw := 30
		if x+bw > w-8 {
			x = 8
			y += 24
		}
		r := image.Rect(x, y, x+bw, y+20)
		bg := draw.PaneAlt
		bold := false
		if isPrime(n) {
			bg = draw.Sage
			bold = true
		}
		if hl {
			bg = draw.Sel
		}
		draw.Fill(img, r, bg)
		draw.Border(img, r, 1, draw.Ink)
		if hl {
			draw.Border(img, r.Inset(-1), 1, draw.Red)
		}
		tw := draw.TextWidth(label, bold, 11)
		draw.Text(img, x+(bw-tw)/2, y+14, label, bold, 11, draw.Ink)
		obj, _ := pbui.NewObject("number", n)
		doc := "number " + label
		if isPrime(n) {
			doc += " (prime)"
		}
		regions = append(regions, Region{Rect: r, Object: &obj, Doc: doc})
		x += bw + 4
	}
	return img, regions
}

// Note is one collected object.
type Note struct {
	ID  int
	Obj pbui.Object
}

// RenderNotes ports NotesApp (pbui-shell.jsx:350-374): Collect… plus the
// list of collected, still-live presentations.
func RenderNotes(w, h int, notes []Note, accepting []string) (*image.RGBA, []Region) {
	img := NewSurface(w, h)
	var regions []Region
	r := Btn(img, 8, 8, "Collect…  (accept anything)", draw.Mustard)
	regions = append(regions, Region{Rect: r, Action: "cmd:collect",
		Doc: "accept ANY presentation — any tile — and keep it here, live"})
	y := Hint(img, 8, 48, "collected objects remain LIVE: a collected color still mixes,")
	y = Hint(img, 8, y+12, "a collected number still sums")
	y += 12
	if len(notes) == 0 {
		Hint(img, 8, y+10, "Nothing collected yet.")
		return img, regions
	}
	for _, n := range notes {
		idLabel := fmt.Sprintf("#%d", n.ID)
		idObj, _ := pbui.NewObject("note", n.ID)
		idObj.Label = "note " + idLabel
		idW := draw.TextWidth(idLabel, false, 10) + 6
		idR := image.Rect(8, y, 8+idW, y+16)
		draw.Text(img, 11, y+12, idLabel, false, 10, draw.Faint)
		for xx := 10; xx < 8+idW-2; xx += 3 {
			draw.Fill(img, image.Rect(xx, y+14, xx+1, y+15), draw.Faint)
		}
		regions = append(regions, Region{Rect: idR, Object: &idObj, Doc: "note " + idLabel + " — remove via menu"})
		tw := draw.Text(img, 8+idW+6, y+12, "<"+n.Obj.Ptype+">", false, 10, draw.Faint)
		hl := len(accepting) > 0 && pbui.TypeMatches(accepting, n.Obj.Ptype)
		obj := n.Obj
		chipR := Chip(img, 8+idW+6+tw+6, y-2, obj, hl)
		regions = append(regions, Region{Rect: chipR, Object: &obj,
			Doc: "<" + obj.Ptype + "> " + obj.StringValue() + " — still live"})
		y += 22
		if y > h-20 {
			break
		}
	}
	return img, regions
}

func isPrime(n int) bool {
	if n < 2 {
		return false
	}
	for i := 2; i*i <= n; i++ {
		if n%i == 0 {
			return false
		}
	}
	return true
}
