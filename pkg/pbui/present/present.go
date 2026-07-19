// Package present emits PBUI presentations for terminals: OSC 8 hyperlinks
// whose URI is a pbui:// object. The visible text is the presentation's
// face; the URI carries {ptype, value}. In a dumb terminal it degrades to
// plain text, in any OSC 8 terminal it is clickable, and in a kitty wired
// with the pbui kitten it is a live presentation.
package present

import (
	"fmt"

	"github.com/go-go-golems/go-go-wm/pkg/pbui"
)

const (
	osc8Open  = "\x1b]8;;%s\x1b\\"
	osc8Close = "\x1b]8;;\x1b\\"
)

// Link renders text as an OSC 8 hyperlink carrying o.
func Link(o pbui.Object, text string) string {
	return fmt.Sprintf(osc8Open, pbui.ObjectToURI(o)) + text + osc8Close
}

// Face returns a default visible face for an object when the caller has
// none (ports the Pres default faces, pbui-shell.jsx:77-89).
func Face(o pbui.Object) string {
	v := o.StringValue()
	switch o.Ptype {
	case "color":
		return "▉ " + v
	default:
		if o.Label != "" {
			return o.Label
		}
		return v
	}
}
