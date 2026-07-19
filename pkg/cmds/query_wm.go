package cmds

import (
	"context"
	"encoding/json"
	"fmt"

	glazed_cmds "github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/middlewares"
	"github.com/go-go-golems/glazed/pkg/settings"
	"github.com/go-go-golems/glazed/pkg/types"
	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
	"github.com/go-go-golems/go-go-wm/pkg/wmx11"
)

func ipcSocketFlag() *fields.Definition {
	return fields.New("wm-socket", fields.TypeString, fields.WithDefault(""),
		fields.WithHelp("WM control socket (default: $GO_GO_WM_SOCKET or $XDG_RUNTIME_DIR/go-go-wm.sock)"))
}

type ipcEnvelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error string          `json:"error"`
}

func wmQuery(socket, q string, out interface{}) error {
	var env ipcEnvelope
	if err := wmx11.QueryIPC(socket, map[string]string{"q": q}, &env); err != nil {
		return err
	}
	if !env.OK {
		return fmt.Errorf("wm: %s", env.Error)
	}
	return json.Unmarshal(env.Data, out)
}

// QueryTreeCommand dumps the layout tree(s) as rows.
type QueryTreeCommand struct {
	*glazed_cmds.CommandDescription
}

type queryTreeSettings struct {
	WMSocket  string `glazed:"wm-socket"`
	Workspace string `glazed:"workspace"`
}

func NewQueryTreeCommand() (*QueryTreeCommand, error) {
	glazedSection, err := settings.NewGlazedSchema()
	if err != nil {
		return nil, err
	}
	return &QueryTreeCommand{glazed_cmds.NewCommandDescription("tree",
		glazed_cmds.WithShort("Dump the WM's layout tree as rows"),
		glazed_cmds.WithFlags(
			ipcSocketFlag(),
			fields.New("workspace", fields.TypeString, fields.WithDefault(""),
				fields.WithHelp("restrict to one workspace id")),
		),
		glazed_cmds.WithSections(glazedSection),
	)}, nil
}

func (c *QueryTreeCommand) RunIntoGlazeProcessor(ctx context.Context, vals *values.Values, gp middlewares.Processor) error {
	s := &queryTreeSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}
	var desk struct {
		Workspaces []wmcore.Workspace `json:"workspaces"`
		Current    string             `json:"current"`
	}
	if err := wmQuery(s.WMSocket, "tree", &desk); err != nil {
		return err
	}
	for _, ws := range desk.Workspaces {
		if s.Workspace != "" && ws.ID != s.Workspace {
			continue
		}
		var walk func(n *wmcore.Node, depth int) error
		walk = func(n *wmcore.Node, depth int) error {
			if n == nil {
				return nil
			}
			row := types.NewRow(
				types.MRP("workspace", ws.ID),
				types.MRP("ws_name", ws.Name),
				types.MRP("current", ws.ID == desk.Current),
				types.MRP("id", string(n.ID)),
				types.MRP("kind", string(n.Kind)),
				types.MRP("depth", depth),
				types.MRP("dir", string(n.Dir)),
				types.MRP("ratio", n.Ratio),
				types.MRP("app", n.App),
			)
			if err := gp.AddRow(ctx, row); err != nil {
				return err
			}
			if err := walk(n.A, depth+1); err != nil {
				return err
			}
			return walk(n.B, depth+1)
		}
		if err := walk(ws.Root, 0); err != nil {
			return err
		}
	}
	return nil
}

// QueryWindowsCommand lists managed clients.
type QueryWindowsCommand struct {
	*glazed_cmds.CommandDescription
}

type queryWindowsSettings struct {
	WMSocket string `glazed:"wm-socket"`
}

func NewQueryWindowsCommand() (*QueryWindowsCommand, error) {
	glazedSection, err := settings.NewGlazedSchema()
	if err != nil {
		return nil, err
	}
	return &QueryWindowsCommand{glazed_cmds.NewCommandDescription("windows",
		glazed_cmds.WithShort("List managed client windows"),
		glazed_cmds.WithFlags(ipcSocketFlag()),
		glazed_cmds.WithSections(glazedSection),
	)}, nil
}

func (c *QueryWindowsCommand) RunIntoGlazeProcessor(ctx context.Context, vals *values.Values, gp middlewares.Processor) error {
	s := &queryWindowsSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}
	var wins []wmx11.WindowInfo
	if err := wmQuery(s.WMSocket, "windows", &wins); err != nil {
		return err
	}
	for _, wi := range wins {
		if err := gp.AddRow(ctx, types.NewRow(
			types.MRP("leaf", wi.Leaf),
			types.MRP("client", fmt.Sprintf("0x%x", wi.Client)),
			types.MRP("title", wi.Title),
			types.MRP("workspace", wi.Workspace),
			types.MRP("rect", wi.Rect),
			types.MRP("focused", wi.Focused),
		)); err != nil {
			return err
		}
	}
	return nil
}
