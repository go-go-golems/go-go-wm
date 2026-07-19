package cmds

import (
	"context"
	"fmt"

	glazed_cmds "github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/go-go-wm/pkg/apps/demoapps"
	"github.com/go-go-golems/go-go-wm/pkg/apps/xapp"
)

// DemoCommand launches one of the demo PBUI apps as an X client.
type DemoCommand struct {
	*glazed_cmds.CommandDescription
}

type demoSettings struct {
	Display string `glazed:"display"`
	Socket  string `glazed:"socket"`
	App     string `glazed:"app"`
	Path    string `glazed:"path"`
}

func NewDemoCommand() (*DemoCommand, error) {
	return &DemoCommand{glazed_cmds.NewCommandDescription("demo",
		glazed_cmds.WithShort("Run a demo PBUI application"),
		glazed_cmds.WithLong(`Launches one of the demo apps as a regular X client that speaks the PBUI
protocol: colors, numbers, notes, files, todo, markdown.

Cross-app flows to try:
  · markdown: Open… → click a row in the files app
  · notes: Collect… → click a color, a number, a file — anything
  · right-click a number → Multiply by… → click another number

Example:
  go-go-wm demo --app colors --display :1
  go-go-wm demo --app markdown --path README.md`),
		glazed_cmds.WithFlags(
			fields.New("app", fields.TypeChoice,
				fields.WithChoices("colors", "numbers", "notes", "files", "todo", "markdown"),
				fields.WithRequired(true),
				fields.WithHelp("which demo app to run")),
			fields.New("display", fields.TypeString, fields.WithDefault(""),
				fields.WithHelp("X display (default: $DISPLAY)")),
			socketFlag(),
			fields.New("path", fields.TypeString, fields.WithDefault(""),
				fields.WithHelp("start path (files: directory · markdown: file)")),
		),
	)}, nil
}

func (c *DemoCommand) Run(ctx context.Context, vals *values.Values) error {
	s := &demoSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}
	var app xapp.App
	switch s.App {
	case "colors":
		app = demoapps.NewColors()
	case "numbers":
		app = &demoapps.Numbers{}
	case "notes":
		app = demoapps.NewNotes()
	case "files":
		app = demoapps.NewFiles(s.Path)
	case "todo":
		app = demoapps.NewTodo()
	case "markdown":
		app = demoapps.NewMarkdown(s.Path)
	default:
		return fmt.Errorf("unknown demo app %q", s.App)
	}
	return xapp.Run(ctx, s.Display, socketOrDefault(s.Socket), app)
}
