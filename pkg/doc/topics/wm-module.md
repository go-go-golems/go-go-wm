---
Title: The wm scripting module
Slug: wm-module
Topics:
- scripting
- wm
IsTemplate: false
IsTopLevel: true
ShowPerDefault: true
SectionType: GeneralTopic
---

Scripts loaded by `go-go-wm run`, `go-go-wm repl`, or `go-go-wm wm --rc`
get the `wm` native module: layout queries and mutations against the
running window manager. Every mutation compiles to the same serializable
ops the keyboard and mouse produce — there is no second mutation path.

## Queries

- `wm.tree()` — the full desktop: `{workspaces: [{id, name, root}], current}`.
  Nodes are `{id, kind: "leaf"|"split", app?, dir?, ratio?, a?, b?}`.
- `wm.windows()` — managed windows: `[{leaf, client, title, workspace, rect, focused}]`.
- `wm.focused()` — the focused leaf id, or null.
- `wm.leaves(workspace?)` — `[{id, app}]` in layout order (default: current workspace).

Queries are synchronous with a 2-second deadline; a stuck WM throws
instead of hanging.

## Mutations

- `wm.split(leaf, dir?, {ratio?, app?})` — split a leaf ("row" default);
  returns the new leaf id.
- `wm.close(leaf)`, `wm.setApp(leaf, app)`, `wm.setRatio(split, ratio)`
- `wm.swap(a, b)`, `wm.moveSplit(from, target, zone)`
- `wm.moveLeaf(leaf, workspaceId, {target?, dir?})` — move across
  workspaces; the leaf keeps its id, so its window survives.
- `wm.apply(op)` — the escape hatch: any raw op object
  (`{op: "split-leaf", node: "n1", dir: "row"}`).

## Workspaces

`wm.workspace(name)` resolves by id or name, creating the workspace when
absent. The returned handle owns the resolved id:

    const ws = wm.workspace("mail");
    ws.switch(); ws.rename("Mail"); ws.clone(); ws.remove();
    ws.adopt(leafId, { dir: "col" });   // pull a leaf in from anywhere
    ws.apply("dev");                    // build a wm.layout recipe (below)

## Events and keys

- `wm.on(event, fn)` — subscribe to the desktop event bus (`"*"` for
  everything). `fn({event, data, source, seq})`. Every op is an event.
- `wm.bind(combo, fn)` — keybindings, e.g. `"Mod4-e"`. Only available in
  the in-process runtime (`go-go-wm wm --rc rc.js`); standalone scripts
  get an error saying so.

## Themes, focus, exec (GGWM-004)

- `wm.theme()` → current theme name; `wm.theme("dark")` switches the
  whole desktop (repaint + `theme.changed` broker event, so standalone
  script apps follow). `wm.themes()` → `["paper", "light", "dark"]`.
- `wm.focus(target)` — a leaf id, or `left|right|up|down` (geometric,
  i3-style), or `next|prev` (layout order). Returns the focused leaf.
- `wm.move(dir)` — swap the focused leaf with its neighbor in that
  direction (an ordinary swap-leaves op under the hood).
- `wm.exec(cmdline)` — spawn a process (`sh -c`), i3's `exec`.
  Fire-and-forget. Always available in rc.js; `run`/`repl` need
  `--allow-exec`.

## Rules and layouts (declarative)

Both are normalized at definition time — a bad spec throws when you
write it, never when it fires — and inspectable before execution:

    wm.layout("dev", {
      split: "row", ratio: 0.62,
      a: { app: "editor" },
      b: { split: "col", a: { app: "terminal" }, b: { app: "notes" } },
    });
    wm.layouts();                       // the normalized plans
    wm.workspace("proj").apply("dev");  // builds only on a fresh workspace

    wm.rule({ title: /zoom/, workspace: "calls" });
    wm.rule({ class: /Slack/, workspace: "8" });   // i3 assign [class=...]
    wm.rules();                         // normalized: [{title, workspace}]

A rule watches `window.managed` and moves matching windows with a
`move-leaf` op — sugar over the event bus, not a new WM mechanism.
Rules match on `title` and/or `class` (WM_CLASS class or instance);
every present pattern must match. First matching rule wins; matching
is case-insensitive.

## See also

`glaze help pbui-module` for presentations, accept, and verbs.
