// Package wmx11 is the thin X11 shell of go-go-wm: a reparenting window
// manager that translates X events into wmcore operations and wmcore
// geometry into ConfigureWindow calls, draws its frames with pkg/draw, and
// participates in the PBUI protocol as the broker's most privileged client.
//
// Design disciplines (see the ticket design doc, §III.4):
//   - all WM state is owned by the run loop; external inputs (broker
//     messages, IPC queries, timers) are posted as closures via Post
//   - all layout mutations flow through wmcore.Apply (ops-as-data)
//   - every line here is expensive to test, so anything with logic lives
//     in wmcore/draw/pbui instead
package wmx11

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/xcursor"
	"github.com/jezek/xgbutil/xevent"
	"github.com/jezek/xgbutil/xwindow"

	"github.com/go-go-golems/go-go-wm/pkg/apps"
	"github.com/go-go-golems/go-go-wm/pkg/draw"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/client"
	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
)

// Gap is the divider thickness in pixels.
const Gap = 8

// Config configures a WM run.
type Config struct {
	Display      string // e.g. ":1"; empty → $DISPLAY
	BrokerSocket string // pbui broker socket; empty → default path
	IPCSocket    string // query/control socket; empty → default path
	Spawn        string // command for the "spawn terminal" keybinding
	NoBroker     bool   // do not connect to the broker (pure-WM mode)
	Theme        string // initial theme name; empty → draw's default ("paper")

	// NoDefaultBinds skips the built-in keybindings (Mod4-Return, splits,
	// Mod4-1..9, Mod4-Shift-q, …) so an rc.js config can own the keyboard
	// without double-fired combos. Escape (accept/menu cancel) is kept.
	NoDefaultBinds bool

	// OnReady runs once, after the WM owns the display, sockets, and
	// broker connection, immediately before the event loop starts
	// consuming. Posted closures queue until the loop runs, so OnReady
	// may hand the WM to code (like the rc.js runtime) that talks back
	// through Post — but it must not wait for those posts itself.
	OnReady func(w *WM)
}

// DefaultIPCSocketPath returns $GO_GO_WM_SOCKET or $XDG_RUNTIME_DIR/go-go-wm.sock.
func DefaultIPCSocketPath() string {
	if p := os.Getenv("GO_GO_WM_SOCKET"); p != "" {
		return p
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "go-go-wm.sock")
	}
	return fmt.Sprintf("/tmp/go-go-wm-%d.sock", os.Getuid())
}

// frame is one managed client: the client window reparented into a frame
// window we own and draw.
type frame struct {
	leaf     wmcore.NodeID
	client   xproto.Window // 0 for builtin tiles (WM-rendered content)
	win      *xwindow.Window
	title    string
	class    string        // WM_CLASS class part (rule matching, "Slack")
	instance string        // WM_CLASS instance part ("slack")
	rect     wmcore.Rect   // current frame rect (screen coords)
	regions  []apps.Region // builtin tiles: clickable presentation regions
}

type acceptState struct {
	session string
	ptypes  []string
	prompt  string
}

type menuState struct {
	obj   pbui.Object
	verbs []pbui.Verb
	win   *xwindow.Window
	model draw.Menu
	hover int
	x, y  int
}

// WM is the window manager.
type WM struct {
	X        *xgbutil.XUtil
	cfg      Config
	desktop  *wmcore.Desktop
	frames   map[wmcore.NodeID]*frame // leaf id → frame
	byClient map[xproto.Window]*frame // client window → frame
	byFrame  map[xproto.Window]*frame // frame window → frame
	focused  wmcore.NodeID

	screen wmcore.Rect // full root geometry
	area   wmcore.Rect // screen minus bars

	topBar    *xwindow.Window
	bottomBar *xwindow.Window
	overlay   *xwindow.Window // drop preview
	menu      *menuState
	dividers  map[wmcore.NodeID]*dividerWin // split id → divider window

	accepting *acceptState
	mouseDoc  string

	drag *dragState

	world *apps.World // state behind the embedded trace/listener/inspector

	scriptTiles map[string]*scriptTile // "script:<name>" renderers (rc.js apps)

	broker *client.Client // nil in pure-WM mode
	ops    chan func()
	ctx    context.Context
	cancel context.CancelFunc
}

// New connects to X and initializes (but does not run) the WM.
func New(cfg Config) (*WM, error) {
	var X *xgbutil.XUtil
	var err error
	if cfg.Display != "" {
		X, err = xgbutil.NewConnDisplay(cfg.Display)
	} else {
		X, err = xgbutil.NewConn()
	}
	if err != nil {
		return nil, fmt.Errorf("wmx11: connect: %w", err)
	}
	if cfg.Theme != "" {
		// Before the loop starts, so becomeWM paints the right root pixel.
		if terr := draw.SetTheme(cfg.Theme); terr != nil {
			return nil, terr
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &WM{
		X:        X,
		cfg:      cfg,
		desktop:  wmcore.NewDesktop(""),
		world:    apps.NewWorld(),
		frames:   map[wmcore.NodeID]*frame{},
		byClient: map[xproto.Window]*frame{},
		byFrame:  map[xproto.Window]*frame{},
		ops:      make(chan func(), 256),
		ctx:      ctx,
		cancel:   cancel,
	}
	return w, nil
}

// Post schedules fn on the WM loop (safe from any goroutine).
func (w *WM) Post(fn func()) {
	select {
	case w.ops <- fn:
	case <-w.ctx.Done():
	}
}

// Run takes over the display and runs until the context is cancelled or the
// X connection dies.
func (w *WM) Run(ctx context.Context) error {
	defer w.cancel()

	if err := w.becomeWM(); err != nil {
		return err
	}
	w.setupScreen()
	if err := w.setupBars(); err != nil {
		return err
	}
	w.setupEWMH()
	w.setupInput()
	w.manageExisting()

	if err := w.startIPC(); err != nil {
		return err
	}
	if !w.cfg.NoBroker {
		w.connectBroker() // best-effort; retries are the user's problem for now
	}

	w.syncBuiltins()
	w.relayout()

	if w.cfg.OnReady != nil {
		w.cfg.OnReady(w)
	}

	pingBefore, pingAfter, pingQuit := xevent.MainPing(w.X)
	for {
		select {
		case <-pingBefore:
			<-pingAfter
		case fn := <-w.ops:
			fn()
		case <-ctx.Done():
			xevent.Quit(w.X)
			return nil
		case <-pingQuit:
			return nil
		}
	}
}

// becomeWM selects SubstructureRedirect on the root — the "there can be
// only one" handshake. Fails if another WM runs.
func (w *WM) becomeWM() error {
	root := xwindow.New(w.X, w.X.RootWin())
	err := root.Listen(
		xproto.EventMaskSubstructureRedirect,
		xproto.EventMaskSubstructureNotify,
		xproto.EventMaskButtonPress,
		xproto.EventMaskFocusChange,
	)
	if err != nil {
		return fmt.Errorf("wmx11: another window manager is running? %w", err)
	}
	// Paper-colored root with a real pointer cursor: a bare X server
	// defines no root cursor at all (the xsetroot -cursor_name left_ptr
	// step every WM performs). Frames and clients that set no cursor of
	// their own inherit this one.
	if cursor, err := xcursor.CreateCursor(w.X, xcursor.LeftPtr); err == nil {
		root.Change(xproto.CwCursor, uint32(cursor))
	}
	root.Change(xproto.CwBackPixel, uint32(pixel(draw.Paper)))
	root.ClearAll()
	return nil
}

func (w *WM) setupScreen() {
	g := xwindow.RootGeometry(w.X)
	w.screen = wmcore.Rect{X: 0, Y: 0, W: g.Width(), H: g.Height()}
	w.area = wmcore.Rect{
		X: w.screen.X + Gap/2, Y: w.screen.Y + draw.BarH + Gap/2,
		W: w.screen.W - Gap, H: w.screen.H - 2*draw.BarH - Gap,
	}
}

// leafColor picks a stable strip color for a leaf.
func leafColor(id wmcore.NodeID) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	return int(h.Sum32())
}

// Apply runs a layout op through wmcore and refreshes the world.
func (w *WM) Apply(op wmcore.Op) (wmcore.Result, error) {
	res, err := Apply(w, op)
	return res, err
}

// Apply is split out so the IPC layer can call it too.
func Apply(w *WM, op wmcore.Op) (wmcore.Result, error) {
	res, err := wmcore.Apply(w.desktop, op)
	if err != nil {
		w.emitEvent("op.rejected", map[string]interface{}{"op": op.Op, "error": err.Error()})
		return res, err
	}
	w.emitEvent(op.Op, map[string]interface{}{
		"node": string(op.Node), "target": string(op.Target),
		"workspace": op.Workspace, "zone": string(op.Zone),
	})
	w.afterOp(op)
	return res, err
}

// afterOp reconciles X state with the desktop after a successful op.
func (w *WM) afterOp(op wmcore.Op) {
	switch op.Op {
	case wmcore.OpCloseLeaf, wmcore.OpRemoveWorkspace:
		w.reapOrphanFrames()
	}
	w.syncBuiltins()
	w.relayout()
	w.updateEWMH()
	w.paintBars()
	// A workspace switch that leaves focus on a hidden leaf strands
	// keyboard navigation (directional focus is workspace-local); land
	// on the first framed leaf of the new workspace, like i3.
	if op.Op == wmcore.OpSwitchWorkspace {
		if ws := w.desktop.CurrentWorkspace(); ws != nil && ws.Root.FindLeaf(w.focused) == nil {
			for _, l := range ws.Root.Leaves() {
				if _, ok := w.frames[l.ID]; ok {
					w.focus(l.ID)
					break
				}
			}
		}
	}
}

func (w *WM) emitEvent(event string, data map[string]interface{}) {
	if w.broker == nil {
		return
	}
	b := w.broker
	go func() { _ = b.Emit(context.Background(), event, data) }()
}

// Shutdown stops the WM loop.
func (w *WM) Shutdown() { w.cancel(); xevent.Quit(w.X) }
