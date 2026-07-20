package cmds

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	glazed_cmds "github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/ewmh"
	"github.com/jezek/xgbutil/icccm"
	"github.com/jezek/xgbutil/xevent"
	"github.com/jezek/xgbutil/xprop"
	"github.com/jezek/xgbutil/xwindow"
)

// TestwinCommand maps a plain X window with precisely controlled float
// signals — the GGWM-007 test client. Nothing standard can easily set
// WM_TRANSIENT_FOR or min==max size hints on demand; this can.
type TestwinCommand struct {
	*glazed_cmds.CommandDescription
}

type testwinSettings struct {
	Display      string `glazed:"display"`
	Title        string `glazed:"title"`
	Class        string `glazed:"class"`
	Instance     string `glazed:"instance"`
	Type         string `glazed:"type"`
	TransientFor string `glazed:"transient-for"`
	Fixed        string `glazed:"fixed"`
	Size         string `glazed:"size"`
}

func NewTestwinCommand() (*TestwinCommand, error) {
	return &TestwinCommand{glazed_cmds.NewCommandDescription("testwin",
		glazed_cmds.WithShort("Map a test window with controlled float signals (WM testing)"),
		glazed_cmds.WithLong(`Maps a plain X window whose float-relevant properties are set exactly
as requested: _NET_WM_WINDOW_TYPE, WM_TRANSIENT_FOR, and fixed-size
WM_NORMAL_HINTS (min == max). Prints its own window id on stdout so
tests can chain transients. Speaks WM_DELETE_WINDOW (exits politely
when the WM asks it to close).

Examples:
  go-go-wm testwin --display :5 --type dialog --title "save as"
  go-go-wm testwin --display :5 --fixed 300x120 --class Galculator
  go-go-wm testwin --display :5 --transient-for 0x1a00003`),
		glazed_cmds.WithFlags(
			fields.New("display", fields.TypeString, fields.WithDefault(""),
				fields.WithHelp("X display (default: $DISPLAY)")),
			fields.New("title", fields.TypeString, fields.WithDefault("testwin"),
				fields.WithHelp("WM_NAME / _NET_WM_NAME")),
			fields.New("class", fields.TypeString, fields.WithDefault("Testwin"),
				fields.WithHelp("WM_CLASS class part")),
			fields.New("instance", fields.TypeString, fields.WithDefault("testwin"),
				fields.WithHelp("WM_CLASS instance part")),
			fields.New("type", fields.TypeChoice,
				fields.WithChoices("normal", "dialog", "utility", "splash", "toolbar"),
				fields.WithDefault("normal"),
				fields.WithHelp("_NET_WM_WINDOW_TYPE to declare")),
			fields.New("transient-for", fields.TypeString, fields.WithDefault(""),
				fields.WithHelp("leader window id for WM_TRANSIENT_FOR (0x… or decimal)")),
			fields.New("fixed", fields.TypeString, fields.WithDefault(""),
				fields.WithHelp("WxH: set min == max size hints (fixed-size window)")),
			fields.New("size", fields.TypeString, fields.WithDefault("400x300"),
				fields.WithHelp("WxH: requested window geometry")),
		),
	)}, nil
}

func parseWxH(s string) (int, int, error) {
	parts := strings.SplitN(strings.ToLower(s), "x", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected WxH, got %q", s)
	}
	w, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	h, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}
	return w, h, nil
}

func (c *TestwinCommand) Run(ctx context.Context, vals *values.Values) error {
	s := &testwinSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}
	var X *xgbutil.XUtil
	var err error
	if s.Display != "" {
		X, err = xgbutil.NewConnDisplay(s.Display)
	} else {
		X, err = xgbutil.NewConn()
	}
	if err != nil {
		return err
	}

	width, height, err := parseWxH(s.Size)
	if err != nil {
		return fmt.Errorf("--size: %w", err)
	}
	win, err := xwindow.Generate(X)
	if err != nil {
		return err
	}
	if err := win.CreateChecked(X.RootWin(), 0, 0, width, height,
		xproto.CwBackPixel, 0xffe8e0d8); err != nil {
		return err
	}

	_ = icccm.WmNameSet(X, win.Id, s.Title)
	_ = ewmh.WmNameSet(X, win.Id, s.Title)
	_ = icccm.WmClassSet(X, win.Id, &icccm.WmClass{Instance: s.Instance, Class: s.Class})
	if s.Type != "normal" {
		_ = ewmh.WmWindowTypeSet(X, win.Id,
			[]string{"_NET_WM_WINDOW_TYPE_" + strings.ToUpper(s.Type)})
	}
	if s.TransientFor != "" {
		id, err := strconv.ParseUint(strings.TrimPrefix(s.TransientFor, "0x"),
			pick(strings.HasPrefix(s.TransientFor, "0x"), 16, 10), 32)
		if err != nil {
			return fmt.Errorf("--transient-for: %w", err)
		}
		_ = icccm.WmTransientForSet(X, win.Id, xproto.Window(id))
	}
	if s.Fixed != "" {
		fw, fh, err := parseWxH(s.Fixed)
		if err != nil {
			return fmt.Errorf("--fixed: %w", err)
		}
		_ = icccm.WmNormalHintsSet(X, win.Id, &icccm.NormalHints{
			Flags:    icccm.SizeHintPMinSize | icccm.SizeHintPMaxSize,
			MinWidth: uint(fw), MinHeight: uint(fh),
			MaxWidth: uint(fw), MaxHeight: uint(fh),
		})
		win.Resize(fw, fh)
	}

	// Speak WM_DELETE_WINDOW so the WM's close button exercises the
	// polite path instead of Kill.
	_ = icccm.WmProtocolsSet(X, win.Id, []string{"WM_DELETE_WINDOW"})
	xevent.ClientMessageFun(func(_ *xgbutil.XUtil, ev xevent.ClientMessageEvent) {
		if name, err := xprop.AtomName(X, xproto.Atom(ev.Data.Data32[0])); err == nil &&
			name == "WM_DELETE_WINDOW" {
			xevent.Quit(X)
		}
	}).Connect(X, win.Id)

	win.Map()
	// The id, for chaining (--transient-for) and for assertions.
	fmt.Printf("0x%x %d\n", win.Id, win.Id)

	go func() {
		<-ctx.Done()
		xevent.Quit(X)
		// xevent.Main blocks in a connection read; only closing the
		// connection actually unblocks it on signal-driven shutdown.
		X.Conn().Close()
	}()
	xevent.Main(X)
	return nil
}

func pick(cond bool, a, b int) int {
	if cond {
		return a
	}
	return b
}
