package wmx11

import (
	"image/color"
	"os/exec"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/keybind"
	"github.com/jezek/xgbutil/xevent"
	"github.com/jezek/xgbutil/xwindow"

	"github.com/go-go-golems/go-go-wm/pkg/apps"
	"github.com/go-go-golems/go-go-wm/pkg/draw"
	"github.com/go-go-golems/go-go-wm/pkg/launcher"
	"github.com/go-go-golems/go-go-wm/pkg/wmcore"
)

// The launcher popup (GGWM-008 L2): an override-redirect overlay over
// the shared command registry. It takes input focus directly while
// open (popup keyboard is the easy case — the frame-tile substrate is
// L3); global Mod4 grabs still fire first by X grab semantics.

const (
	launcherPopupW   = 640
	launcherPopupH   = 420
	launcherMaxShown = 14
)

// launcherUI is the open popup's state.
type launcherUI struct {
	win   *xwindow.Window
	w, h  int
	query string
	sel   int
	rows  []launcher.Scored // visible slice (≤ launcherMaxShown)
}

// setupLauncher builds the registry and registers the builtin source.
func (w *WM) setupLauncher() {
	w.registry = launcher.New()
	w.registry.SetStatic(launcher.KindBuiltin, builtinCommands())
	go w.registry.Refresh() // first scan off-loop; ready by first open
}

// builtinCommands is the WM's own launchable vocabulary.
func builtinCommands() []launcher.Command {
	var out []launcher.Command
	for _, name := range []string{apps.AppAbout, apps.AppTrace, apps.AppListener, apps.AppInspector} {
		out = append(out, launcher.Command{
			ID:    "builtin:" + name,
			Label: apps.BuiltinTitle(name),
			Kind:  launcher.KindBuiltin,
			Doc:   "open the " + apps.BuiltinTitle(name) + " tile",
		})
	}
	return out
}

// toggleLauncher opens or closes the popup (Mod4+d).
func (w *WM) toggleLauncher() {
	if w.launcher != nil {
		w.closeLauncher()
		return
	}
	w.openLauncher()
}

func (w *WM) openLauncher() {
	if w.launcher != nil || w.registry == nil {
		return
	}
	w.registry.Refresh()
	pw, ph := launcherPopupW, launcherPopupH
	if pw > w.screen.W-20 {
		pw = w.screen.W - 20
	}
	if ph > w.screen.H-20 {
		ph = w.screen.H - 20
	}
	x := (w.screen.W - pw) / 2
	y := (w.screen.H - ph) / 3

	win, err := xwindow.Generate(w.X)
	if err != nil {
		return
	}
	err = win.CreateChecked(w.X.RootWin(), x, y, pw, ph,
		xproto.CwBackPixel|xproto.CwOverrideRedirect|xproto.CwEventMask,
		uint32(pixel(draw.Pane)), 1,
		xproto.EventMaskButtonPress|xproto.EventMaskExposure|
			xproto.EventMaskKeyPress)
	if err != nil {
		return
	}
	ui := &launcherUI{win: win, w: pw, h: ph}
	w.launcher = ui
	w.refreshLauncherRows()

	xevent.KeyPressFun(func(_ *xgbutil.XUtil, ev xevent.KeyPressEvent) {
		w.launcherKey(ev.State, ev.Detail)
	}).Connect(w.X, win.Id)
	xevent.ButtonPressFun(func(_ *xgbutil.XUtil, ev xevent.ButtonPressEvent) {
		if i := w.launcherPanel().RowAt(int(ev.EventX), int(ev.EventY)); i >= 0 {
			w.launcherActivate(i)
		}
	}).Connect(w.X, win.Id)
	xevent.ExposeFun(func(_ *xgbutil.XUtil, ev xevent.ExposeEvent) {
		if w.launcher != nil && ev.Count == 0 {
			w.paintLauncher()
		}
	}).Connect(w.X, win.Id)

	win.Map()
	win.Stack(xproto.StackModeAbove)
	win.Focus() // popup keyboard: direct input focus while open
	w.paintLauncher()
	w.setMouseDoc("launcher — type to filter · ↑↓ select · Enter launch · Esc close")
}

func (w *WM) closeLauncher() {
	ui := w.launcher
	if ui == nil {
		return
	}
	w.launcher = nil
	xevent.Detach(w.X, ui.win.Id)
	ui.win.Destroy()
	w.restoreInputFocus()
	w.setMouseDoc("")
}

// restoreInputFocus gives the keyboard back to whatever holds the
// focus registers (used when a popup that stole focus closes).
func (w *WM) restoreInputFocus() {
	if pf := w.floats[w.focusedFloat]; pf != nil {
		xwindow.New(w.X, pf.client).Focus()
		return
	}
	if f := w.frames[w.focused]; f != nil {
		if f.client != 0 {
			xwindow.New(w.X, f.client).Focus()
		} else {
			f.win.Focus()
		}
	}
}

// refreshLauncherRows recomputes the visible matches for the query.
func (w *WM) refreshLauncherRows() {
	ui := w.launcher
	if ui == nil {
		return
	}
	rows := w.registry.Match(ui.query)
	if len(rows) > launcherMaxShown {
		rows = rows[:launcherMaxShown]
	}
	ui.rows = rows
	if ui.sel >= len(rows) {
		ui.sel = 0
	}
}

// launcherPanel builds the draw model from the current state.
func (w *WM) launcherPanel() draw.LauncherPanel {
	ui := w.launcher
	rows := make([]draw.LauncherRow, len(ui.rows))
	for i, r := range ui.rows {
		rows[i] = draw.LauncherRow{
			Label: r.Label, Doc: r.Doc, Tag: string(r.Kind),
			Tone: commandTone(r.Command),
		}
	}
	return draw.LauncherPanel{
		Query: ui.query, Prompt: "run — type to filter",
		Rows: rows, Selected: ui.sel,
		Width: ui.w, Height: ui.h,
	}
}

// commandTone gives an entry its stable accent color (the leafColor
// trick: hash the ID into the palette).
func commandTone(c launcher.Command) color.RGBA {
	switch c.Kind {
	case launcher.KindBuiltin:
		return apps.BuiltinColor(builtinName(c.ID))
	case launcher.KindScript:
		return draw.Lavender
	}
	return draw.AppColor(leafColor(wmcore.NodeID(c.ID)))
}

func (w *WM) paintLauncher() {
	if w.launcher == nil {
		return
	}
	w.blit(w.launcher.win, w.launcherPanel().Render())
}

// launcherKey handles a KeyPress delivered to the popup window.
// Chords with the WM modifier never reach the surface (design rule);
// global grabs already fired for bound combos.
func (w *WM) launcherKey(mods uint16, code xproto.Keycode) {
	ui := w.launcher
	if ui == nil {
		return
	}
	if mods&(xproto.ModMask4|xproto.ModMask1|xproto.ModMaskControl) != 0 {
		return
	}
	s := keybind.LookupString(w.X, mods, code)
	switch s {
	case "Escape":
		w.closeLauncher()
		return
	case "Return", "KP_Enter":
		w.launcherActivate(ui.sel)
		return
	case "Up":
		if ui.sel > 0 {
			ui.sel--
		}
	case "Down":
		if ui.sel < len(ui.rows)-1 {
			ui.sel++
		}
	case "BackSpace":
		if ui.query != "" {
			ui.query = ui.query[:len(ui.query)-1]
			ui.sel = 0
			w.refreshLauncherRows()
		}
	case "space":
		ui.query += " "
		w.refreshLauncherRows()
	default:
		if len(s) == 1 && s[0] >= 0x20 && s[0] < 0x7f {
			ui.query += s
			ui.sel = 0
			w.refreshLauncherRows()
		} else {
			return // dead keys, modifiers, function keys: ignore
		}
	}
	w.paintLauncher()
}

// launcherActivate launches the row at index and closes the popup.
func (w *WM) launcherActivate(i int) {
	ui := w.launcher
	if ui == nil || i < 0 || i >= len(ui.rows) {
		return
	}
	cmd := ui.rows[i].Command
	w.closeLauncher()
	w.launchCommand(cmd)
}

// launchCommand routes a launch by kind (the registry's Bump feeds
// frecency either way).
func (w *WM) launchCommand(cmd launcher.Command) {
	w.registry.Bump(cmd.ID)
	switch cmd.Kind {
	case launcher.KindApp:
		cmdline := cmd.Exec
		if cmd.Terminal {
			term := w.cfg.Spawn
			if term == "" {
				term = "xterm"
			}
			cmdline = term + " -e " + cmd.Exec
		}
		w.execCommand(cmdline)
	case launcher.KindBuiltin:
		w.launchBuiltin(cmd.ID)
	case launcher.KindScript:
		w.dispatchScriptCommand(cmd)
	}
	w.emitEvent("command.launched", map[string]interface{}{
		"id": cmd.ID, "label": cmd.Label, "kind": string(cmd.Kind),
	})
}

// launchBuiltin puts a builtin app on screen via the same placement
// rule clients use: reuse the current workspace's empty leaf if there
// is one, else split the focused (or first) leaf. Works on a fresh
// boot where nothing holds focus yet.
func (w *WM) launchBuiltin(app string) {
	leafID := w.placementLeaf()
	if leafID == "" {
		return
	}
	_, _ = w.Apply(wmcore.Op{Op: wmcore.OpSetLeafApp, Node: leafID, App: app})
	w.focus(leafID)
}

// execCommand spawns a shell command detached, DISPLAY forced — the
// one process-spawning path (shared with the terminal keybinding).
func (w *WM) execCommand(cmdline string) {
	c := exec.Command("sh", "-c", cmdline)
	if w.cfg.Display != "" {
		c.Env = append(c.Environ(), "DISPLAY="+w.cfg.Display)
	}
	if err := c.Start(); err != nil {
		log.Warn().Err(err).Str("cmd", cmdline).Msg("launch failed")
		return
	}
	go func() { _ = c.Wait() }()
}

// dispatchScriptCommand is the L4 seam (script-registered commands run
// on their owning JS runtime).
func (w *WM) dispatchScriptCommand(cmd launcher.Command) {
	log.Warn().Str("id", cmd.ID).Msg("script command dispatch not wired yet (L4)")
}

// LauncherInfo is the debug/introspection view ({"q":"launcher"}).
type LauncherInfo struct {
	Open     bool     `json:"open"`
	Query    string   `json:"query"`
	Selected int      `json:"selected"`
	Rows     []string `json:"rows"` // visible command ids, in order
}

func (w *WM) launcherInfo() LauncherInfo {
	ui := w.launcher
	if ui == nil {
		return LauncherInfo{}
	}
	rows := make([]string, len(ui.rows))
	for i, r := range ui.rows {
		rows[i] = r.ID
	}
	return LauncherInfo{Open: true, Query: ui.query, Selected: ui.sel, Rows: rows}
}
