package cmds

import (
	"context"

	glazed_cmds "github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/broker"
	"github.com/go-go-golems/go-go-wm/pkg/wmx11"
)

// WMCommand runs the window manager.
type WMCommand struct {
	*glazed_cmds.CommandDescription
}

type wmSettings struct {
	Display        string `glazed:"display"`
	Socket         string `glazed:"socket"`
	IPCSocket      string `glazed:"ipc-socket"`
	Spawn          string `glazed:"spawn"`
	EmbeddedBroker bool   `glazed:"embedded-broker"`
	NoBroker       bool   `glazed:"no-broker"`
	RC             string `glazed:"rc"`
}

func NewWMCommand() (*WMCommand, error) {
	return &WMCommand{glazed_cmds.NewCommandDescription("wm",
		glazed_cmds.WithShort("Run the window manager"),
		glazed_cmds.WithLong(`Takes over the X display as a reparenting tiling window manager:
binary split tree, sticky dividers (¼ ⅓ ½ ⅔ ¾), drag-⠿ swap/dock, and
workspaces, drawn in the paper-and-ink PBUI look.

Keybindings: Mod4-Return terminal · Mod4-d/s split right/below · Mod4-w
close · Mod4-space focus next · Mod4-n new workspace · Mod4-1..9 switch ·
Escape cancels accepts/menus · Mod4-Shift-q quit.

Development happens in a nested server:
  Xephyr :1 -screen 1280x800 &
  go-go-wm wm --display :1 --embedded-broker &
  DISPLAY=:1 xterm`),
		glazed_cmds.WithFlags(
			fields.New("display", fields.TypeString, fields.WithDefault(""),
				fields.WithHelp("X display (default: $DISPLAY)")),
			socketFlag(),
			fields.New("ipc-socket", fields.TypeString, fields.WithDefault(""),
				fields.WithHelp("query/control socket (default: $GO_GO_WM_SOCKET or $XDG_RUNTIME_DIR/go-go-wm.sock)")),
			fields.New("spawn", fields.TypeString, fields.WithDefault("xterm"),
				fields.WithHelp("command bound to Mod4-Return")),
			fields.New("embedded-broker", fields.TypeBool, fields.WithDefault(false),
				fields.WithHelp("run the PBUI broker inside this process (still spoken to via its socket)")),
			fields.New("no-broker", fields.TypeBool, fields.WithDefault(false),
				fields.WithHelp("run without PBUI presentations (pure WM)")),
			fields.New("rc", fields.TypeString, fields.WithDefault(""),
				fields.WithHelp("rc.js startup script run in an in-process goja runtime (wm.bind works here)")),
		),
	)}, nil
}

func (c *WMCommand) Run(ctx context.Context, vals *values.Values) error {
	s := &wmSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}

	sock := socketOrDefault(s.Socket)
	if s.EmbeddedBroker && !s.NoBroker {
		b := broker.New()
		go func() {
			if err := b.ListenAndServe(ctx, sock); err != nil {
				log.Error().Err(err).Msg("embedded broker exited")
			}
		}()
	}

	cfg := wmx11.Config{
		Display:      s.Display,
		BrokerSocket: sock,
		IPCSocket:    s.IPCSocket,
		Spawn:        s.Spawn,
		NoBroker:     s.NoBroker,
	}
	if s.RC != "" {
		rcPath := s.RC
		cfg.OnReady = func(w *wmx11.WM) { startRC(ctx, w, rcPath, sock, s.NoBroker) }
	}
	w, err := wmx11.New(cfg)
	if err != nil {
		return err
	}
	log.Info().Str("display", s.Display).Str("broker", sock).Msg("wm starting")
	return w.Run(ctx)
}
