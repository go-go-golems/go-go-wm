package cmds

import (
	"context"
	"net/http"
	"net/http/pprof"
	"os"
	"time"

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
	Theme          string `glazed:"theme"`
	NoDefaultBinds bool   `glazed:"no-default-binds"`
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
			fields.New("theme", fields.TypeString, fields.WithDefault(""),
				fields.WithHelp("initial theme: paper (default), light, dark — switchable live via wm.theme()")),
			fields.New("no-default-binds", fields.TypeBool, fields.WithDefault(false),
				fields.WithHelp("skip built-in keybindings so an rc.js config owns the keyboard (Escape stays)")),
		),
	)}, nil
}

func (c *WMCommand) Run(ctx context.Context, vals *values.Values) error {
	s := &wmSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}

	// GO_GO_WM_PPROF=localhost:6060 serves net/http/pprof for profiling
	// paint/layout work (flamegraphs via `go tool pprof`). Handlers are
	// registered on a dedicated mux (not DefaultServeMux) and the server
	// has a read-header timeout to limit Slowloris-style exposure.
	if addr := os.Getenv("GO_GO_WM_PPROF"); addr != "" {
		go func() {
			mux := http.NewServeMux()
			mux.HandleFunc("/debug/pprof/", pprof.Index)
			mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
			mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
			mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
			mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
			srv := &http.Server{
				Addr:              addr,
				Handler:           mux,
				ReadHeaderTimeout: 5 * time.Second,
				// WriteTimeout stays 0: /debug/pprof/profile and /trace run
				// for the requested capture duration.
			}
			log.Info().Str("addr", addr).Msg("pprof listening")
			if err := srv.ListenAndServe(); err != nil {
				log.Warn().Err(err).Msg("pprof server failed")
			}
		}()
	}

	sock := socketOrDefault(s.Socket)

	// Publish the session's sockets into the WM process environment so
	// everything it spawns — terminals, launcher apps, wm.exec children,
	// and their grandchildren — reaches THIS desktop's broker and
	// control socket. Without this, tools run inside the session
	// (go-go-wm scrape/menu/accept, clicked pbui:// links) fall back to
	// the default socket paths, which an embedded broker on a custom
	// --socket is not using.
	if !s.NoBroker {
		_ = os.Setenv("PBUI_SOCKET", sock)
	}
	ipcSock := s.IPCSocket
	if ipcSock == "" {
		ipcSock = wmx11.DefaultIPCSocketPath()
	}
	_ = os.Setenv("GO_GO_WM_SOCKET", ipcSock)
	if s.Display != "" {
		_ = os.Setenv("DISPLAY", s.Display)
	}

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
		Theme:        s.Theme,

		NoDefaultBinds: s.NoDefaultBinds,
	}
	// Capsules (GGWM-013 M5): the WM pointer does not exist yet, so the
	// spawner late-binds it; OnReady fills it before any verb can fire.
	var wmRef *wmx11.WM
	cfg.SpawnCapsule = capsuleSpawner(ctx, func() *wmx11.WM { return wmRef }, s.Display, sock, s.NoBroker)
	rcPath := s.RC
	cfg.OnReady = func(w *wmx11.WM) {
		wmRef = w
		if rcPath != "" {
			startRC(ctx, w, rcPath, sock, s.Display, s.NoBroker)
		}
	}
	w, err := wmx11.New(cfg)
	if err != nil {
		return err
	}
	log.Info().Str("display", s.Display).Str("broker", sock).Msg("wm starting")
	return w.Run(ctx)
}
