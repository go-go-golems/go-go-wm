package cmds

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	glazed_cmds "github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/client"
)

// AcceptCommand starts an accept session and prints the accepted object.
type AcceptCommand struct {
	*glazed_cmds.CommandDescription
}

type acceptSettings struct {
	Socket string   `glazed:"socket"`
	Ptypes []string `glazed:"ptype"`
	Prompt string   `glazed:"prompt"`
}

func NewAcceptCommand() (*AcceptCommand, error) {
	return &AcceptCommand{glazed_cmds.NewCommandDescription("accept",
		glazed_cmds.WithShort("Accept a typed object from anywhere on screen"),
		glazed_cmds.WithLong(`Starts an accept session on the broker and blocks until some client
answers with a matching object (printed as JSON on stdout), or the session
is cancelled (exit code 1, nothing printed).

Example:
  go-go-wm accept --ptype color --prompt "PICK — click a COLOR"`),
		glazed_cmds.WithFlags(
			socketFlag(),
			fields.New("ptype", fields.TypeStringList,
				fields.WithDefault([]string{"any"}),
				fields.WithHelp("acceptable presentation types (\"any\" matches everything)")),
			fields.New("prompt", fields.TypeString,
				fields.WithDefault("accept…"),
				fields.WithHelp("prompt shown in the ACCEPTING banner")),
		),
	)}, nil
}

func (c *AcceptCommand) Run(ctx context.Context, vals *values.Values) error {
	s := &acceptSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}
	cl, err := client.Connect(ctx, client.Options{Socket: socketOrDefault(s.Socket), Name: "cli-accept"})
	if err != nil {
		return err
	}
	defer func() { _ = cl.Close() }()
	obj, err := cl.Accept(ctx, s.Ptypes, s.Prompt)
	if err != nil {
		return err
	}
	if obj == nil {
		fmt.Fprintln(os.Stderr, "accept cancelled")
		os.Exit(1)
	}
	out, _ := json.Marshal(obj)
	fmt.Println(string(out))
	return nil
}

// AnswerCommand answers the pending accept session (used by pickers such as
// the kitty kitten).
type AnswerCommand struct {
	*glazed_cmds.CommandDescription
}

type answerSettings struct {
	Socket  string `glazed:"socket"`
	Session string `glazed:"session"`
	Ptype   string `glazed:"ptype"`
	Value   string `glazed:"value"`
	URI     string `glazed:"uri"`
}

func NewAnswerCommand() (*AnswerCommand, error) {
	return &AnswerCommand{glazed_cmds.NewCommandDescription("answer",
		glazed_cmds.WithShort("Answer the pending accept session with an object"),
		glazed_cmds.WithLong(`Resolves the pending accept session. Give either --ptype and --value, or a
pbui:// --uri (as carried by OSC 8 hyperlinks).

Examples:
  go-go-wm answer --ptype color --value '#b0563f'
  go-go-wm answer --uri 'pbui://color/%23b0563f'`),
		glazed_cmds.WithFlags(
			socketFlag(),
			fields.New("session", fields.TypeString, fields.WithDefault(""),
				fields.WithHelp("session id (default: whatever session is pending)")),
			fields.New("ptype", fields.TypeString, fields.WithDefault(""),
				fields.WithHelp("presentation type of the answer")),
			fields.New("value", fields.TypeString, fields.WithDefault(""),
				fields.WithHelp("string value of the answer")),
			fields.New("uri", fields.TypeString, fields.WithDefault(""),
				fields.WithHelp("pbui:// URI carrying the answer (alternative to --ptype/--value)")),
		),
	)}, nil
}

func (c *AnswerCommand) Run(ctx context.Context, vals *values.Values) error {
	s := &answerSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}
	var obj pbui.Object
	var err error
	switch {
	case s.URI != "":
		obj, err = pbui.ObjectFromURI(strings.TrimSpace(s.URI))
		if err != nil {
			return err
		}
	case s.Ptype != "":
		obj, err = pbui.NewObject(s.Ptype, s.Value)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("answer needs --uri or --ptype/--value")
	}
	cl, err := client.Connect(ctx, client.Options{Socket: socketOrDefault(s.Socket), Name: "cli-answer", Roles: []string{"picker"}})
	if err != nil {
		return err
	}
	defer func() { _ = cl.Close() }()
	return cl.Answer(ctx, s.Session, obj)
}
