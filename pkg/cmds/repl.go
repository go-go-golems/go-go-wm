package cmds

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	glazed_cmds "github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/go-go-goja/pkg/engine"
	"github.com/go-go-golems/go-go-goja/pkg/replapi"
	"github.com/rs/zerolog"

	"github.com/go-go-golems/go-go-wm/pkg/jsmod"
	"github.com/go-go-golems/go-go-wm/pkg/jsmod/pbuimod"
	"github.com/go-go-golems/go-go-wm/pkg/jsmod/uimod"
	"github.com/go-go-golems/go-go-wm/pkg/jsmod/wmmod"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/client"
)

// ReplCommand is an interactive JS session against the live desktop —
// the same modules as `run`, wrapped in go-go-goja's REPL kernel (IIFE
// cell rewriting, so `let` bindings persist across lines).
type ReplCommand struct {
	*glazed_cmds.CommandDescription
}

type replSettings struct {
	Socket    string `glazed:"socket"`
	WMSocket  string `glazed:"wm-socket"`
	NoBroker  bool   `glazed:"no-broker"`
	AllowExec bool   `glazed:"allow-exec"`
}

func NewReplCommand() (*ReplCommand, error) {
	return &ReplCommand{glazed_cmds.NewCommandDescription("repl",
		glazed_cmds.WithShort("Interactive JavaScript session against the live desktop"),
		glazed_cmds.WithLong(`A REPL with the pbui and wm modules preloaded, driving whatever WM the
sockets point at. Develop your automation here, then paste it into a
script for "go-go-wm run" or into rc.js — the API is identical.

  $ go-go-wm repl
  wm> const wm = require("wm")
  wm> wm.leaves()
  [{id: "n1", app: ""}, …]
  wm> wm.split(wm.focused(), "col", {app: "builtin:trace"})`),
		glazed_cmds.WithFlags(
			socketFlag(),
			fields.New("wm-socket", fields.TypeString, fields.WithDefault(""),
				fields.WithHelp("WM control socket (default: $GO_GO_WM_SOCKET)")),
			fields.New("no-broker", fields.TypeBool, fields.WithDefault(false),
				fields.WithHelp("skip the broker connection (wm module only)")),
			fields.New("allow-exec", fields.TypeBool, fields.WithDefault(false),
				fields.WithHelp("enable wm.exec and the exec module (run subprocesses)")),
		),
	)}, nil
}

func (c *ReplCommand) Run(ctx context.Context, vals *values.Values) error {
	s := &replSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}

	var cl *client.Client
	if !s.NoBroker {
		var err error
		cl, err = client.Connect(ctx, client.Options{
			Socket: socketOrDefault(s.Socket), Name: "repl", Roles: []string{"script"},
		})
		if err != nil {
			return err
		}
		defer func() { _ = cl.Close() }()
	}

	applyInitialTheme(ctx, s.WMSocket)
	fan := jsmod.NewEventFan(cl, 256)
	var wmOpts []wmmod.Option
	if s.AllowExec {
		wmOpts = append(wmOpts, wmmod.WithExec(""))
	}
	uiMod := uimod.New(uimod.Options{BrokerSocket: socketOrDefault(s.Socket)})
	builder := engine.NewRuntimeFactoryBuilder()
	builder.WithModules(
		engine.NativeModuleRegistrar{
			ModuleID: "pbui", ModuleName: pbuimod.ModuleName,
			Loader: pbuimod.New(cl, pbuimod.WithEventFan(fan)).Loader(),
		},
		engine.NativeModuleRegistrar{
			ModuleID: "wm", ModuleName: wmmod.ModuleName,
			Loader: wmmod.New(&wmmod.IPCBackend{Socket: s.WMSocket}, fan, wmOpts...).Loader(),
		},
		engine.NativeModuleRegistrar{
			ModuleID: "ui", ModuleName: uimod.ModuleName,
			Loader: uiMod.Loader(),
		},
	)
	if s.AllowExec {
		builder.UseModuleMiddleware(engine.MiddlewareOnly("exec"))
	}
	followThemeChanges(ctx, fan, cl, uiMod)
	factory, err := builder.Build()
	if err != nil {
		return err
	}

	app, err := replapi.NewWithConfig(ctx, factory, zerolog.Nop(), replapi.RawConfig())
	if err != nil {
		return err
	}
	defer func() { _ = app.Close(context.Background()) }()
	sess, err := app.CreateSession(ctx)
	if err != nil {
		return err
	}

	fmt.Println(`go-go-wm repl — require("wm") and require("pbui") are available. Ctrl-D exits.`)
	sc := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("wm> ")
		if !sc.Scan() {
			fmt.Println()
			return sc.Err()
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		resp, err := app.Evaluate(ctx, sess.ID, line)
		if err != nil {
			fmt.Println("error:", err)
			continue
		}
		if resp.Cell == nil {
			continue
		}
		exec := resp.Cell.Execution
		for _, ev := range exec.Console {
			fmt.Printf("  [%s] %s\n", ev.Kind, ev.Message)
		}
		if exec.Error != "" {
			fmt.Println("!", exec.Error)
			continue
		}
		if exec.Result != "" {
			fmt.Println(exec.Result)
		}
	}
}
