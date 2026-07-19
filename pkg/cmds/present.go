package cmds

import (
	"context"
	"fmt"

	glazed_cmds "github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"

	"github.com/go-go-golems/go-go-wm/pkg/pbui"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/client"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/present"
)

// PresentCommand prints an OSC 8 pbui link — the one-liner that makes any
// shell script a PBUI app. It touches neither X nor the broker socket.
type PresentCommand struct {
	*glazed_cmds.CommandDescription
}

type presentSettings struct {
	Ptype string `glazed:"ptype"`
	Value string `glazed:"value"`
	Text  string `glazed:"text"`
	NL    bool   `glazed:"newline"`
}

func NewPresentCommand() (*PresentCommand, error) {
	return &PresentCommand{glazed_cmds.NewCommandDescription("present",
		glazed_cmds.WithShort("Print an OSC 8 presentation link"),
		glazed_cmds.WithLong(`Prints text wrapped in an OSC 8 hyperlink whose URI is a typed pbui object.

Examples:
  go-go-wm present --ptype color --value '#b0563f'
  go-go-wm present --ptype file --value ./README.md --text 'the readme'
  echo "deploy $(go-go-wm present --ptype deploy-target --value prod --text prod)"`),
		glazed_cmds.WithFlags(
			fields.New("ptype", fields.TypeString, fields.WithRequired(true),
				fields.WithHelp("presentation type")),
			fields.New("value", fields.TypeString, fields.WithRequired(true),
				fields.WithHelp("string value")),
			fields.New("text", fields.TypeString, fields.WithDefault(""),
				fields.WithHelp("visible face (default: a ptype-appropriate face)")),
			fields.New("newline", fields.TypeBool, fields.WithDefault(true),
				fields.WithHelp("print a trailing newline")),
		),
	)}, nil
}

func (c *PresentCommand) Run(ctx context.Context, vals *values.Values) error {
	s := &presentSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}
	obj, err := pbui.NewObject(s.Ptype, s.Value)
	if err != nil {
		return err
	}
	text := s.Text
	if text == "" {
		text = present.Face(obj)
	}
	if s.NL {
		fmt.Println(present.Link(obj, text))
	} else {
		fmt.Print(present.Link(obj, text))
	}
	return nil
}

// MenuCommand asks the broker to pop the verb menu for a pbui:// URI (wired
// into kitty's open_actions). Without a WM connected it prints the verbs.
type MenuCommand struct {
	*glazed_cmds.CommandDescription
}

type menuSettings struct {
	Socket  string `glazed:"socket"`
	URI     string `glazed:"uri"`
	X       int    `glazed:"x"`
	Y       int    `glazed:"y"`
	Display string `glazed:"display"`
}

func NewMenuCommand() (*MenuCommand, error) {
	return &MenuCommand{glazed_cmds.NewCommandDescription("menu",
		glazed_cmds.WithShort("Pop the type-directed action menu for a pbui:// object"),
		glazed_cmds.WithLong(`Asks the WM to pop the verb menu for a pbui:// object at the pointer
(the default), or at an explicit --x/--y. Wired into kitty's
open_actions so clicking a scraped pbui:// link opens the menu where
the cursor is.`),
		glazed_cmds.WithFlags(
			socketFlag(),
			fields.New("uri", fields.TypeString, fields.WithRequired(true),
				fields.WithHelp("pbui:// URI of the object")),
			fields.New("x", fields.TypeInteger, fields.WithDefault(-1),
				fields.WithHelp("screen x (default: pointer position)")),
			fields.New("y", fields.TypeInteger, fields.WithDefault(-1),
				fields.WithHelp("screen y (default: pointer position)")),
			fields.New("display", fields.TypeString, fields.WithDefault(""),
				fields.WithHelp("X display for the pointer query (default: $DISPLAY)")),
		),
	)}, nil
}

func (c *MenuCommand) Run(ctx context.Context, vals *values.Values) error {
	s := &menuSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}
	obj, err := pbui.ObjectFromURI(s.URI)
	if err != nil {
		return err
	}
	// Unset position (-1) → pop the menu at the pointer, which is what a
	// clicked link wants. Explicit --x/--y still work.
	x, y := s.X, s.Y
	if x < 0 || y < 0 {
		if px, py, ok := pointerPosition(s.Display); ok {
			x, y = px, py
		} else {
			x, y = 0, 0
		}
	}
	cl, err := client.Connect(ctx, client.Options{Socket: socketOrDefault(s.Socket), Name: "cli-menu"})
	if err != nil {
		return err
	}
	defer func() { _ = cl.Close() }()
	verbs, err := cl.RequestMenu(ctx, obj, x, y)
	if err != nil {
		return err
	}
	// verbs == nil means a WM took over and will render the menu.
	for _, v := range verbs {
		fmt.Printf("%s\t%s\n", v.ID, v.Label)
	}
	return nil
}

// pointerPosition returns the root pointer coordinates on display (or
// $DISPLAY when empty). ok is false if X is unreachable — the caller
// then falls back to a corner.
func pointerPosition(display string) (int, int, bool) {
	var X *xgbutil.XUtil
	var err error
	if display != "" {
		X, err = xgbutil.NewConnDisplay(display)
	} else {
		X, err = xgbutil.NewConn()
	}
	if err != nil {
		return 0, 0, false
	}
	defer X.Conn().Close()
	reply, err := xproto.QueryPointer(X.Conn(), X.RootWin()).Reply()
	if err != nil {
		return 0, 0, false
	}
	return int(reply.RootX), int(reply.RootY), true
}
