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
	"image"
	"os"
	"path/filepath"
	"time"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/xcursor"
	"github.com/jezek/xgbutil/xevent"
	"github.com/jezek/xgbutil/xgraphics"
	"github.com/jezek/xgbutil/xwindow"

	"github.com/go-go-golems/go-go-wm/pkg/apps"
	"github.com/go-go-golems/go-go-wm/pkg/draw"
	"github.com/go-go-golems/go-go-wm/pkg/launcher"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
	"github.com/go-go-golems/go-go-wm/pkg/pbui/client"
	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
	"github.com/go-go-golems/go-go-wm/pkg/xshm"
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

	// Floating overlay layer (GGWM-007). A floating frame has leaf == ""
	// and never appears in the wmcore tree; it belongs to workspace ws
	// and is tracked in WM.floats instead of WM.frames.
	floating               bool
	leader                 xproto.Window // WM_TRANSIENT_FOR target, 0 if none
	ws                     string        // owning workspace id (floats only)
	minW, minH, maxW, maxH int           // WM_NORMAL_HINTS bounds, 0 = unbounded

	// Paint buffers, reused between paints and dropped on resize/unmap
	// (per-paint allocation made GC ~30% of the profile, GGWM-005).
	// surf is the zero-copy MIT-SHM shared pixmap (GGWM-006); ximg is
	// the PutImage fallback. Either way the pixel content doubles as
	// the window's background pixmap, so Expose is server-side.
	img  *image.RGBA
	ximg *xgraphics.Image
	surf *xshm.Surface // front: currently installed as the window background
	// back is the second shared pixmap. The server composites the window
	// from whichever pixmap is installed as its background, and nothing
	// synchronises that against our writes — so writing into the installed
	// one is a tear. Rendering into the spare and then swapping the
	// background attribute makes each frame appear whole (GGWM-012 Step 20).
	back *xshm.Surface

	// mapped mirrors what the server has been told, so relayout can skip
	// the Map/Unmap request when visibility did not change. Zero value is
	// "unknown", which forces the first pass to issue the request — the
	// safe default for a frame adopted at startup (GGWM-012).
	mapped mapState

	// resizedAt is set when relayout commits a size change (MoveResize) and
	// cleared when paintFrame issues the matching repair. Between those two
	// moments the server displays the frame at its NEW size filled from a
	// background pixmap still holding the PREVIOUS chrome — title buttons at
	// the old right edge. The gap is the duration of that visible artifact
	// (GGWM-012 Step 23); prevW/prevH make the log line self-describing.
	resizedAt    time.Time
	prevW, prevH int
}

// mapState is a tri-state so that "never told the server anything" is
// distinguishable from "told it to unmap".
type mapState uint8

const (
	mapUnknown mapState = iota
	mapMapped
	mapUnmapped
)

// dropBuffers releases the frame's paint buffers (and their server
// pixmaps). Call whenever the frame leaves the screen or is destroyed.
// The background-pixmap attribute holds a server-side reference that
// would keep the shm pages alive, so it is reset to a plain pixel
// first.
func (f *frame) dropBuffers() {
	if f.surf != nil {
		xproto.ChangeWindowAttributes(f.surf.X.Conn(), f.win.Id,
			xproto.CwBackPixel, []uint32{uint32(pixel(draw.Current().Pane))})
		f.surf.Destroy()
		f.surf = nil
	}
	if f.back != nil {
		f.back.Destroy()
		f.back = nil
	}
	if f.ximg != nil {
		f.ximg.Destroy()
		f.ximg = nil
	}
	f.img = nil
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
	fstate   focusState               // unified focus owner (Option B; single source of truth)

	floats     map[xproto.Window]*frame // client → floating frame (GGWM-007)
	floatRules []compiledFloatRule      // scripting-layer float overrides

	fullscreen  *frame          // the one fullscreen window, nil = none
	fs          fullscreenState // read-side owner of the fullscreen invariant (Phase 1)
	fsSavedRect wmcore.Rect     // float's pre-fullscreen rect (tiles restore from the tree)

	screen wmcore.Rect // full root geometry
	area   wmcore.Rect // screen minus bars

	topBar       *xwindow.Window
	bottomBar    *xwindow.Window
	topBarImg    *xgraphics.Image // cached bar surfaces (blitCached)
	bottomBarImg *xgraphics.Image
	overlay      *xwindow.Window // drop preview
	menu         *menuState
	dividers     map[wmcore.NodeID]*dividerWin // split id → divider window

	accepting *acceptState
	mouseDoc  string

	registry       *launcher.Registry              // the command registry (GGWM-008)
	launcher       *launcherUI                     // the open popup, nil when closed
	launcherTiles  map[wmcore.NodeID]*launcherTile // empty-tile query states (L3)
	scriptCommands map[string]func()               // "script:<id>" → JS-loop post (L4)
	scriptCmdDefs  []launcher.Command              // their registry entries
	remoteCmds     map[string]remoteCmd            // A2 daemon commands, by id

	drag *dragState

	// idxScratch is the reusable node index for one reconciliation pass.
	// Reused rather than allocated because a fresh map costs more than the
	// Root.Find scans it replaces for small trees (GGWM-012).
	idxScratch wmcore.Index

	// perf counts the work the reconciler and paint path do, so resize
	// performance claims can be settled with numbers instead of opinion
	// (GGWM-012). Read it with the "perf" IPC query.
	perf perfCounters

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
		floats:   map[xproto.Window]*frame{},
		ops:      make(chan func(), 256),
		ctx:      ctx,
		cancel:   cancel,
	}
	w.fs.wm = w     // back-reference for the fullscreenState read helpers
	w.fstate.wm = w // back-reference for the focusState (Option B)
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
	w.setupLauncher()
	w.manageExisting()

	if err := w.startIPC(); err != nil {
		return err
	}
	log.Info().Bool("shared_pixmaps", xshm.Available(w.X)).Msg("frame upload path")
	if !w.cfg.NoBroker {
		w.connectBroker() // best-effort; retries are the user's problem for now
	}

	w.syncBuiltins()
	w.relayout()
	w.refocusCurrent() // boot: land focus on the first tile

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
	root.Change(xproto.CwBackPixel, uint32(pixel(draw.Current().Paper)))
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
	res, err := applyOne(w, op)
	if err != nil {
		return res, err
	}
	w.afterOp(op)
	return res, err
}

// applyOne mutates the tree and emits the op event, without X-side
// reconciliation — the shared half of Apply and ApplyBatch.
func applyOne(w *WM, op wmcore.Op) (wmcore.Result, error) {
	res, err := wmcore.Apply(w.desktop, op)
	if err != nil {
		w.emitEvent("op.rejected", map[string]interface{}{"op": op.Op, "error": err.Error()})
		return res, err
	}
	w.emitEvent(op.Op, map[string]interface{}{
		"node": string(op.Node), "target": string(op.Target),
		"workspace": op.Workspace, "zone": string(op.Zone),
	})
	return res, nil
}

// ApplyBatch applies ops in order with ONE reconciliation pass at the
// end (GGWM-006 batch-boot design): a burst like "create workspaces
// 1..9" stops paying a full-screen relayout+paint per op — only the
// final state is ever painted. Stops at the first failing op; results
// cover the applied prefix. WM loop only (callers post).
func (w *WM) ApplyBatch(ops []wmcore.Op) ([]wmcore.Result, error) {
	results := make([]wmcore.Result, 0, len(ops))
	anyReap, anySwitch := false, false
	var err error
	for i := range ops {
		var res wmcore.Result
		res, err = applyOne(w, ops[i])
		if err != nil {
			err = fmt.Errorf("batch op %d (%s): %w", i, ops[i].Op, err)
			break
		}
		results = append(results, res)
		switch ops[i].Op {
		case wmcore.OpCloseLeaf, wmcore.OpRemoveWorkspace:
			anyReap = true
		case wmcore.OpSwitchWorkspace, wmcore.OpAddWorkspace:
			anySwitch = true
		}
	}
	// One reconcile for the whole burst — the same steps afterOp runs
	// per op.
	if anyReap {
		w.reapOrphanFrames()
	}
	w.syncBuiltins()
	w.relayout()
	w.updateEWMH()
	w.paintBars()
	if anySwitch {
		// Mirror afterOp: a workspace switch must exit fullscreen (the
		// old fullscreen frame is unmapped by relayout, so leaving
		// w.fullscreen set strands focus on it — refocusCurrent would
		// pin focus back to the old leaf, leaving the new workspace
		// without usable keyboard input) (Codex review RC-6). The
		// decision is display-free and unit-tested.
		if w.shouldExitFullscreenOnSwitch(anySwitch) {
			w.exitFullscreen()
		}
		w.refocusCurrent()
	}
	return results, err
}

// afterOp reconciles X state with the desktop after a successful op.
func (w *WM) afterOp(op wmcore.Op) {
	defer func(t0 time.Time) {
		log.Debug().Dur("ms", time.Since(t0)).Str("op", op.Op).Msg("afterOp")
	}(time.Now())
	switch op.Op {
	case wmcore.OpCloseLeaf, wmcore.OpRemoveWorkspace:
		w.reapOrphanFrames()
	}
	w.syncBuiltins()
	w.relayout()
	w.updateEWMH()
	w.paintBars()
	// A workspace switch that leaves focus on a hidden leaf (or a hidden
	// float) strands keyboard navigation; land on the first framed leaf
	// of the new workspace, like i3. AddWorkspace switches Current too.
	// Fullscreen is workspace-local in the simplest way: switching away
	// exits it (predictable; per-workspace fullscreen is not state worth
	// keeping).
	if op.Op == wmcore.OpSwitchWorkspace || op.Op == wmcore.OpAddWorkspace {
		w.exitFullscreen()
		w.refocusCurrent()
	}
}

// refocusCurrent moves focus onto the current workspace if it is not
// already there.
func (w *WM) refocusCurrent() {
	ws := w.desktop.CurrentWorkspace()
	if ws == nil {
		return
	}
	// A workspace switch can leave focusedFloat pointing at a float that
	// is now hidden; keyboard input would go to an unmapped window.
	if pf := w.floats[w.fstate.FocusedFloat()]; pf != nil && pf.ws != w.desktop.Current {
		w.fstate.ClearFloat()
		if w.frames[w.fstate.FocusedLeaf()] != nil {
			w.focus(w.fstate.FocusedLeaf())
		}
	}
	if ws.Root.FindLeaf(w.fstate.FocusedLeaf()) != nil {
		return
	}
	for _, l := range ws.Root.Leaves() {
		if _, ok := w.frames[l.ID]; ok {
			w.focus(l.ID)
			break
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
func (w *WM) Shutdown() {
	// Persist any pending frecency state so a burst of launches right
	// before quit is not lost (Codex review RC-9).
	if w.registry != nil {
		w.registry.Flush()
	}
	w.cancel()
	xevent.Quit(w.X)
}
