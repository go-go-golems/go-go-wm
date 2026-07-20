---
Title: JavaScript API reference
Slug: js-api-reference
Topics:
- scripting
- wm
- pbui
- ui
IsTemplate: false
IsTopLevel: true
ShowPerDefault: true
SectionType: GeneralTopic
---

The complete scripting surface, in one place. Three native modules —
`wm` (layout), `pbui` (presentations), `ui` (surfaces) — available
identically at three attachment points:

| where | command | notes |
|---|---|---|
| rc file (in-process) | `go-go-wm wm --rc file.js` | only place `wm.bind` and `app.tile()` work; `wm.exec` always on |
| standalone script | `go-go-wm run [--once] file.js` | daemon without `--once`; `--allow-exec` gates exec |
| interactive | `go-go-wm repl` | same API; bindings persist across lines |
| notebook | `go-go-wm repl --ui` | the rich REPL: an X window where every result is a live presentation |

Sockets: `--socket` (broker) and `--wm-socket` (WM control), with
sensible env/XDG defaults. Scripts are also broker clients: the
filename is the client name and verb owner.

## Module `wm` — layout, themes, navigation, processes

Queries (synchronous, 2s-bounded; wire field names):

- `wm.tree()` — the full desktop: `{workspaces: [{id, name, root}],
  current}`; nodes are `{id, kind: "leaf"|"split", app?, dir?, ratio?,
  a?, b?}`.
- `wm.windows()` — `[{leaf, client, title, class, instance, workspace,
  rect, focused, floating?, leader?}]`. Floats have `floating: true`
  and an empty `leaf` (they live outside the tree).
- `wm.focused()` — focused leaf id (or undefined).
- `wm.leaves(workspace?)` — `[{id, app}]` in layout order.

Mutations (each is one op; throw on failure):

- `wm.split(leaf, dir, {ratio?, app?})` → new leaf id. dir:
  `"row"|"col"`; app: `""` (empty), `"builtin:trace"`, `"script:<n>"`.
- `wm.close(leaf)` · `wm.setApp(leaf, app)` · `wm.setRatio(split, f)`
- `wm.swap(a, b)` · `wm.moveSplit(from, target, zone)` ·
  `wm.moveLeaf(leaf, wsId, {target?, dir?})`
- `wm.apply(op)` — raw op escape hatch; `wm.apply([op, ...])` — batch:
  ops apply in order, the WM repaints once, returns results
  (`{new_leaf?, new_workspace?}` each).

Workspaces (fluent; find by id or name, create if absent):

    const ws = wm.workspace("mail");
    ws.switch(); ws.rename("Mail"); ws.clone(); ws.remove();
    ws.adopt(leafId, {dir?});     // pull a leaf in from anywhere
    ws.apply("dev");              // build a layout recipe (fresh ws only)

Navigation and themes:

- `wm.focus(target)` — leaf id or `"left"|"right"|"up"|"down"`
  (geometric) or `"next"|"prev"` (layout order); returns focused leaf.
- `wm.move(dir)` — swap focused leaf with its neighbor in dir.
- `wm.theme()` → current name; `wm.theme("dark")` — switch (repaints,
  emits `theme.changed`); `wm.themes()` → `["paper","light","dark"]`.
- `wm.float()` — toggle the focused window tiled↔floating; returns the
  new state. Dialogs/utility/splash/fixed-size windows float on their
  own (WM_TRANSIENT_FOR, window type, min==max hints).
- `wm.fullscreen()` — toggle the focused window over the whole screen
  (bars included; clients go full-bleed); workspace switches exit it.

Processes, keys, events:

- `wm.exec(cmdline)` — spawn via `sh -c`, fire-and-forget, DISPLAY
  set. rc.js: always available; run/repl: needs `--allow-exec`.
- `wm.launch(target)` — launcher-registry id (`"app:firefox"`,
  `"builtin:trace"`, `"script:x"`) or raw command line; returns the
  routed kind. `wm.launcher()` — open the Mod4+d popup.
- `wm.command({id, label, doc?, run})` — serve a launcher entry from
  this runtime. Works everywhere: rc.js hosts the callback in-process;
  standalone daemons register over the broker (launches dispatch as
  `command.invoke` events, and the entry dies with the daemon's broker
  client, like verbs). The `command` ptype gets verbs and answers
  `accept("command")` from any launcher surface.
- `wm.bind(combo, fn)` — X keybinding (`"Mod4-Shift-e"`, xgbutil
  grammar). In-process runtimes only; elsewhere throws with guidance.
- `wm.on(event, fn)` — event subscription (see the event list below).

Declarative rules and layouts (validated at definition time,
inspectable, compiled to ops on execution):

    wm.layout("dev", { split: "row", ratio: 0.62,
      a: { app: "editor" },
      b: { split: "col", a: { app: "terminal" }, b: { app: "notes" } }});
    wm.layouts();                          // the normalized plans
    wm.workspace("proj").apply("dev");     // idempotent: fresh ws only

    wm.rule({ title: /zoom/, workspace: "calls" });
    wm.rule({ class: /Slack/, workspace: "8", dir: "col" });
    wm.rule({ class: /Galculator/, float: true });   // i3 for_window floating
    wm.rule({ class: /mpv/, float: false });         // force tiling
    wm.rules();

Workspace rules watch `window.managed`; `float` rules are pushed down
to the WM and decide at map time (a `float: false` beats even a
dialog's own signals). A rule needs a workspace and/or a float field.
`title` and/or `class` (string = Go regexp, or JS RegExp;
case-insensitive; class matches WM_CLASS class or instance); all
present patterns must match; first match wins.

## Module `pbui` — presentations, accepts, verbs

Data-only: `pbui.object(ptype, value)` · `pbui.uri(obj)` /
`pbui.parse(uri)` · `pbui.link(obj, text)` (OSC 8). Ptypes are slugs.

Accept (the desktop-wide typed prompt):

    const c = await pbui.accept("color", "PICK — click any color");
    // null = cancelled (a normal outcome, never a rejection)

Answering side: `pbui.onAcceptMode(fn)` / `pbui.onAcceptClear(fn)`,
`pbui.answer(session, obj)`, `pbui.cancel(session?)`.

Verbs (menu entries served by this process, desktop-wide):

    pbui.verb({ id: "git.checkout", label: "Checkout",
                ptypes: ["git-commit"], accepts: [] },
              async obj => { ... });

Definition-time validation; re-registering an id replaces it.

Output and events: `pbui.print(...segs)` (strings + live objects into
the listener) · `pbui.emit(event, data)` · `pbui.on(event, fn)`
(`"*"` for all; bounded delivery, drops are reported as
`script.error`) · `pbui.hover(text)` · `pbui.menu(obj, x?, y?)`.

## Module `ui` — JS-defined surfaces

Builders (data-only): `ui.row(...segs)` · `ui.text(s, {bold?, size?})`
· `ui.hint(s)` · `ui.object(ptype, value, {label?, doc?})` ·
`ui.button(label, action, {color?, doc?})` (tones:
rose|blue|mint|mustard|lavender|sage).

    const app = ui.app({
      name: "my-app",              // broker client name / verb owner
      title: "MY APP",
      render() { return [ui.row(...)]; },   // re-run after every handler
      actions: { add() { ... } },
      verbs: [{ id, label, ptypes, accepts?, run(obj) { ... } }],
      onKey(key) { ... },
    });
    app.show();      // standalone X window (any runtime)
    app.tile();      // WM-painted tile (rc.js only) → "script:<name>"
    app.refresh();   // re-render outside a handler (timers, events)

`onKey` fires for both surfaces: standalone windows always did; tiles
receive typed keys when focused (GGWM-008's frame keyboard substrate —
chords with the WM modifier never reach the app).

Specs validate when produced (bad shapes throw with row/seg
coordinates); a throwing render keeps the previous frame and emits
`script.error`. Render hosts never execute JavaScript — handlers run
on the JS loop and post snapshots.

## The rich REPL (`repl --ui`)

`wm`, `pbui`, and `ui` are pre-bound — type `wm.tree()` directly (the
terminal REPL keeps explicit `require`, matching pasted scripts).
Every evaluation result derives a typed presentation: colors become
swatches (they answer desktop accepts), numeric arrays become series
(sparkline/bars/table views), arrays of same-shaped objects become
datasets (table/schema views), everything else gets a capped json
view. `Out(n)` and `$_` return the raw JS values. Right-click any
Out chip for `repl.use` / `repl.copy-input` plus whatever verbs the
desktop serves for that ptype.

Any object can override derivation with a `__pbui__()` method:

    class Matrix {
      __pbui__() {
        return { ptype: "matrix", summary: "2×2 matrix",
                 input: "Matrix.from(...)",
                 views: [{ name: "grid", rows: [ui.row(...)] }] };
      }
    }

Views use the ui.row vocabulary plus `{kind:"table", columns, cells,
more?}` and `{kind:"field", text, action, focus?}` segments; a
throwing or invalid `__pbui__` falls back to derivation with the
error shown as a hint. Each cell emits `repl.cell-done {n, input,
error, console, ptype?, summary?}`.

## Events (the bus vocabulary)

Every op is emitted under its op name: `split-leaf`, `close-leaf`,
`set-ratio`, `set-leaf-app`, `swap-leaves`, `move-split`, `move-leaf`,
`add-workspace`, `remove-workspace`, `rename-workspace`,
`clone-workspace`, `switch-workspace`. Plus: `window.managed`
`{leaf?, title, class, instance, workspace, floating?, leader?}`,
`close_tile`, `split_tile`, `window.float-closed {client, title,
class}`, `window.float-toggled {client, floating, leaf?}`,
`command.launched {id, label, kind, leaf?}`, `command.invoke {id,
owner}` (the A2 dispatch), `window.fullscreen {on, title, leaf?|client?}`,
`theme.changed {theme}`, `accept.started` /
`accept.answered` / `accept.cleared`, `listener.print`,
`verb.invoked`, `op.rejected`, `script.error`, `repl.cell-done`. Handlers receive
`{event, data, source, seq}`.

## Gotchas

- Query results use wire field names (`new_workspace`, not
  `NewWorkspace`).
- `pbui.accept` cancellation resolves `null` — check for it.
- In REPL probes and tests, prefer `var` over top-level `let` when
  something external must read the global.
- A slow event handler drops oldest-first; design handlers to be one
  post, not a computation.
- Errors in handlers become `script.error` events (see the trace
  tile), never process crashes.

## See also

`glaze help wm-module`, `pbui-module`, `ui-module` (per-module deep
dives) · `glaze help user-guide` · `glaze help getting-started`.
