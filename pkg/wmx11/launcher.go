package wmx11

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"os"
	"os/exec"
	"strings"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/keybind"
	"github.com/jezek/xgbutil/xevent"
	"github.com/jezek/xgbutil/xwindow"

	"github.com/go-go-golems/go-go-wm/pkg/apps"
	"github.com/go-go-golems/go-go-wm/pkg/draw"
	"github.com/go-go-golems/go-go-wm/pkg/launcher"
	"github.com/go-go-golems/go-go-wm/pkg/pbui"
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
	// The rich REPL (GGWM-009): a standalone surface the WM tiles like
	// any client — launchable from Mod4+d and every empty tile.
	if exe, err := os.Executable(); err == nil {
		out = append(out, launcher.Command{
			ID:    "app:go-go-wm-repl",
			Label: "repl (notebook)",
			Kind:  launcher.KindApp,
			Exec:  exe + " repl --ui",
			Doc:   "rich JavaScript REPL — results are live presentations",
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
		i := w.launcherPanel().RowAt(int(ev.EventX), int(ev.EventY))
		if i < 0 || w.launcher == nil || i >= len(w.launcher.rows) {
			return
		}
		if ev.Detail == 3 {
			// Rows are presentations: right-click opens the command's
			// verb menu (the popup closes; the menu takes over).
			cmd := w.launcher.rows[i].Command
			w.closeLauncher()
			if b := w.broker; b != nil {
				obj := commandObject(cmd)
				x, y := int(ev.RootX), int(ev.RootY)
				go func() { _, _ = b.RequestMenu(context.Background(), obj, x, y) }()
			}
			return
		}
		w.launcherActivate(i)
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

// commandObject presents a command as a typed PBUI object.
func commandObject(cmd launcher.Command) pbui.Object {
	obj, _ := pbui.NewObject("command", cmd.ID)
	obj.Label = cmd.Label
	obj.Doc = cmd.Doc
	return obj
}

// maybeAnswerCommand answers a pending command accept with cmd and
// reports whether it did — every launcher surface checks this before
// launching (accept mode: Enter/click answers instead of runs).
func (w *WM) maybeAnswerCommand(cmd launcher.Command) bool {
	if w.accepting == nil || w.broker == nil ||
		!pbui.TypeMatches(w.accepting.ptypes, "command") {
		return false
	}
	session, b, obj := w.accepting.session, w.broker, commandObject(cmd)
	go func() { _ = b.Answer(context.Background(), session, obj) }()
	return true
}

// launcherActivate launches (or, in accept mode, answers with) the row
// at index and closes the popup.
func (w *WM) launcherActivate(i int) {
	ui := w.launcher
	if ui == nil || i < 0 || i >= len(ui.rows) {
		return
	}
	cmd := ui.rows[i].Command
	w.closeLauncher()
	if w.maybeAnswerCommand(cmd) {
		return
	}
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

// dispatchScriptCommand fires a script-registered command: in-process
// registrations post straight into the rc runtime; remote (A2 daemon)
// registrations dispatch over the event bus — the owning process
// subscribed to command.invoke and fires its own callback.
func (w *WM) dispatchScriptCommand(cmd launcher.Command) {
	if fire := w.scriptCommands[cmd.ID]; fire != nil {
		fire()
		return
	}
	if rc, ok := w.remoteCmds[cmd.ID]; ok {
		w.emitEvent("command.invoke", map[string]interface{}{
			"id": cmd.ID, "owner": rc.owner,
		})
		return
	}
	log.Warn().Str("id", cmd.ID).Msg("script command has no registered runtime")
}

// remoteCmd is one A2-daemon-registered launcher entry.
type remoteCmd struct {
	def   launcher.Command
	owner string // broker client name; entries die with the client
}

// registerRemoteCommand adds/replaces a daemon-owned command (IPC
// register-command). WM loop only.
func (w *WM) registerRemoteCommand(def launcher.Command, owner string) error {
	if def.ID == "" || def.Label == "" || owner == "" {
		return fmt.Errorf("remote command needs an id, a label, and an owner")
	}
	if !strings.HasPrefix(def.ID, "script:") {
		def.ID = "script:" + def.ID
	}
	def.Kind = launcher.KindScript
	def.Exec = "" // remote commands never exec; they dispatch by event
	if w.remoteCmds == nil {
		w.remoteCmds = map[string]remoteCmd{}
	}
	w.remoteCmds[def.ID] = remoteCmd{def: def, owner: owner}
	w.syncScriptCommands()
	return nil
}

// dropRemoteCommands removes every command a disconnected client owned
// (the broker announces client.disconnected; verbs die the same way).
func (w *WM) dropRemoteCommands(owner string) {
	changed := false
	for id, rc := range w.remoteCmds {
		if rc.owner == owner {
			delete(w.remoteCmds, id)
			changed = true
		}
	}
	if changed {
		w.syncScriptCommands()
	}
}

// syncScriptCommands pushes the merged local + remote script command
// list into the registry.
func (w *WM) syncScriptCommands() {
	defs := append([]launcher.Command(nil), w.scriptCmdDefs...)
	for _, rc := range w.remoteCmds {
		defs = append(defs, rc.def)
	}
	if w.registry != nil {
		w.registry.SetStatic(launcher.KindScript, defs)
	}
}

// RegisterCommand adds a script command to the registry (wm.command in
// rc.js — the wmmod.Backend seam). fire must be a single post to the
// JS loop. Re-registering an id replaces it, like verbs.
func (b *ScriptBackend) RegisterCommand(id, label, doc string, fire func()) error {
	if id == "" || label == "" || fire == nil {
		return fmt.Errorf("script command needs an id, a label, and a run function")
	}
	done := make(chan struct{})
	b.WM.Post(func() {
		defer close(done)
		w := b.WM
		if w.scriptCommands == nil {
			w.scriptCommands = map[string]func(){}
		}
		full := "script:" + id
		w.scriptCommands[full] = fire
		var defs []launcher.Command
		replaced := false
		for _, c := range w.scriptCmdDefs {
			if c.ID == full {
				replaced = true
				continue
			}
			defs = append(defs, c)
		}
		_ = replaced
		defs = append(defs, launcher.Command{
			ID: full, Label: label, Doc: doc, Kind: launcher.KindScript,
		})
		w.scriptCmdDefs = defs
		w.syncScriptCommands()
	})
	select {
	case <-done:
		return nil
	case <-b.WM.ctx.Done():
		return fmt.Errorf("wm shutting down")
	}
}

// launchTarget resolves a wm.launch argument: a registry id launches
// through the kind router; anything else is a raw command line (the
// design's exec fallback).
func (w *WM) launchTarget(target string) (string, error) {
	if target == "" {
		return "", fmt.Errorf("launch target must be non-empty")
	}
	if cmd, ok := w.registry.Get(target); ok {
		w.launchCommand(cmd)
		return string(cmd.Kind), nil
	}
	w.execCommand(target)
	return "exec", nil
}

// commandVerbs is the WM's verb contribution for the command ptype.
func commandVerbs() []pbui.Verb {
	return []pbui.Verb{
		{ID: "command.launch", Label: "Launch", Ptypes: []string{"command"}},
		{ID: "command.edit", Label: "Edit .desktop entry", Ptypes: []string{"command"}},
	}
}

// runCommandVerb executes the command.* verbs (WM loop).
func (w *WM) runCommandVerb(verbID string, obj *pbui.Object) {
	cmd, ok := w.registry.Get(obj.StringValue())
	if !ok {
		log.Warn().Str("id", obj.StringValue()).Msg("verb on unknown command")
		return
	}
	switch verbID {
	case "command.launch":
		w.launchCommand(cmd)
	case "command.edit":
		if cmd.Src == "" {
			return // builtins/scripts have no file to edit
		}
		term := w.cfg.Spawn
		if term == "" {
			term = "xterm"
		}
		w.execCommand(term + ` -e sh -c '${EDITOR:-vi} "` + cmd.Src + `"'`)
	}
}

// --- the keyboard substrate + launcher tile (L3) ---------------------------

// handleFrameKey routes a KeyPress delivered to a frame window to the
// focused WM-rendered surface. The design rule: typed input goes to the
// focused surface; chords with a WM modifier never do (global grabs
// fire first by X semantics, and unbound chords are dropped here).
func (w *WM) handleFrameKey(f *frame, mods uint16, code xproto.Keycode) {
	if f.client != 0 {
		return
	}
	if mods&(xproto.ModMask4|xproto.ModMask1|xproto.ModMaskControl) != 0 {
		return
	}
	s := keybind.LookupString(w.X, mods, code)
	if s == "" {
		return
	}
	name := w.builtinAppOf(f)
	if strings.HasPrefix(name, scriptPrefix) {
		// The uimod seam: the key closure is a single post to the JS loop.
		if tile := w.scriptTiles[strings.TrimPrefix(name, scriptPrefix)]; tile != nil && tile.key != nil {
			tile.key(s)
		}
		return
	}
	if name == apps.AppLauncher {
		w.launcherTileKey(f, s)
	}
}

// launcherTile is one empty tile's live query state (WM-side; the
// renderer in pkg/apps is stateless). Keyed by leaf id — leaf ids are
// never reused, and syncBuiltins prunes states of vanished leaves.
type launcherTile struct {
	query string
	sel   int
}

func (w *WM) launcherTileState(leaf wmcore.NodeID) *launcherTile {
	if w.launcherTiles == nil {
		w.launcherTiles = map[wmcore.NodeID]*launcherTile{}
	}
	st := w.launcherTiles[leaf]
	if st == nil {
		st = &launcherTile{}
		w.launcherTiles[leaf] = st
	}
	return st
}

// launcherTileRows are the visible matches for a tile's query.
func (w *WM) launcherTileRows(st *launcherTile, maxRows int) []launcher.Scored {
	if w.registry == nil {
		return nil
	}
	rows := w.registry.Match(st.query)
	if len(rows) > maxRows {
		rows = rows[:maxRows]
	}
	if st.sel >= len(rows) {
		st.sel = 0
	}
	return rows
}

// launcherTileKey is the tile's keyboard: same vocabulary as the popup,
// but Enter launches into this tile.
func (w *WM) launcherTileKey(f *frame, s string) {
	st := w.launcherTileState(f.leaf)
	switch s {
	case "Return", "KP_Enter":
		rows := w.launcherTileRows(st, launcherMaxShown)
		if st.sel < len(rows) {
			if w.maybeAnswerCommand(rows[st.sel].Command) {
				return
			}
			w.launchIntoTile(f, rows[st.sel].Command)
		}
		return
	case "Up":
		if st.sel > 0 {
			st.sel--
		}
	case "Down":
		st.sel++ // clamped by launcherTileRows on render
	case "BackSpace":
		if st.query != "" {
			st.query = st.query[:len(st.query)-1]
			st.sel = 0
		}
	case "space":
		st.query += " "
	default:
		if len(s) == 1 && s[0] >= 0x20 && s[0] < 0x7f {
			st.query += s
			st.sel = 0
		} else {
			return
		}
	}
	w.paintFrame(f)
}

// launchIntoTile launches a command with this tile as the target:
// builtins take the leaf over; apps spawn (the new client lands in the
// empty leaf via placementLeaf); scripts dispatch to their runtime.
func (w *WM) launchIntoTile(f *frame, cmd launcher.Command) {
	w.registry.Bump(cmd.ID)
	delete(w.launcherTiles, f.leaf)
	switch cmd.Kind {
	case launcher.KindBuiltin:
		_, _ = w.Apply(wmcore.Op{Op: wmcore.OpSetLeafApp, Node: f.leaf, App: cmd.ID})
		w.focus(f.leaf)
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
		w.paintFrame(f)
	case launcher.KindScript:
		w.dispatchScriptCommand(cmd)
		w.paintFrame(f)
	}
	w.emitEvent("command.launched", map[string]interface{}{
		"id": cmd.ID, "label": cmd.Label, "kind": string(cmd.Kind),
		"leaf": string(f.leaf),
	})
}

// renderLauncherTile is the empty tile's surface (launcher tile v2):
// the compact panel over the shared registry, plus a hint line.
// Regions carry launchcmd: actions so rows are clickable.
func (w *WM) renderLauncherTile(f *frame, cw, ch int) (*image.RGBA, []apps.Region) {
	st := w.launcherTileState(f.leaf)
	panel := draw.LauncherPanel{
		Query:   st.query,
		Prompt:  "type to run — Enter launches here",
		Compact: true,
		Width:   cw,
		Height:  ch - 18,
	}
	rows := w.launcherTileRows(st, panel.MaxRows())
	for _, r := range rows {
		panel.Rows = append(panel.Rows, draw.LauncherRow{
			Label: r.Label, Doc: r.Doc, Tag: string(r.Kind),
			Tone: commandTone(r.Command),
		})
	}
	panel.Selected = st.sel

	img := apps.NewSurface(cw, ch)
	copyImage(img, panel.Render(), 0, 0)
	draw.Text(img, 8, ch-6, "empty tile — Mod4-Return: terminal · Mod4-d: popup · X clients land here",
		false, 10, draw.Faint)

	var regions []apps.Region
	for i, r := range panel.RowRects() {
		obj := commandObject(rows[i].Command)
		regions = append(regions, apps.Region{
			Rect: r, Action: "launchcmd:" + rows[i].ID, Object: &obj,
			Doc: rows[i].Label + " — click: launch here · right-click: verbs",
		})
	}
	return img, regions
}

// LauncherInfo is the debug/introspection view ({"q":"launcher"}).
type LauncherInfo struct {
	Open     bool     `json:"open"`
	Query    string   `json:"query"`
	Selected int      `json:"selected"`
	Rows     []string `json:"rows"` // visible command ids, in order
}

// LauncherTileInfo is the focused launcher tile's debug view
// ({"q":"launcher-tile"}).
type LauncherTileInfo struct {
	Leaf     string   `json:"leaf"`
	Query    string   `json:"query"`
	Selected int      `json:"selected"`
	Rows     []string `json:"rows"`
}

func (w *WM) launcherTileInfo() LauncherTileInfo {
	f := w.frames[w.focused]
	if f == nil || f.client != 0 || w.builtinAppOf(f) != apps.AppLauncher {
		return LauncherTileInfo{}
	}
	st := w.launcherTileState(f.leaf)
	rows := w.launcherTileRows(st, launcherMaxShown)
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return LauncherTileInfo{Leaf: string(f.leaf), Query: st.query, Selected: st.sel, Rows: ids}
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
