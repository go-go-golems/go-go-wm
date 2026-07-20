---
Title: 'ui module design: spec IR, app adapters, script tiles, xgoja provider'
Ticket: GGWM-003-UI-MODULE
Status: active
Topics:
    - wm
    - pbui
    - goja
    - scripting
    - ui
DocType: design-doc
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://pkg/apps/apps.go
      Note: the Region IR and click contract the ui module emits
    - Path: repo://pkg/apps/xapp/xapp.go
      Note: the standalone window shell jsApp adapts
    - Path: repo://pkg/wmx11/builtin.go
      Note: builtin-tile pipeline extended with script tiles
    - Path: ws://go-go-goja/pkg/xgoja/providerapi/module.go
      Note: the provider contract for U4
ExternalSources: []
Summary: Design for GGWM-002's deferred P5 — the `ui` native module that lets JavaScript define full PBUI application surfaces as declarative specs which Go normalizes, renders with pkg/draw, and wires into the existing Region click contract; delivered as standalone X windows (xapp shell), WM-embedded script tiles, and an xgoja provider package.
LastUpdated: 2026-07-18T23:40:00-04:00
WhatFor: The governing design for the GGWM-003 implementation; read before touching pkg/apps/uispec, pkg/jsmod/uimod, or the script-tile registry.
WhenToUse: Implementation reference; pairs with GGWM-002 design docs for module conventions and the concurrency contract.
---

# ui module design: spec IR, app adapters, script tiles, xgoja provider

## Executive summary

GGWM-002 gave scripts control over the desktop (ops, verbs, accept,
events) but not the ability to *be* an app — to put pixels and
presentations on screen. This ticket closes that gap with a `ui` native
module: a script describes its surface as a declarative spec (rows of
text / objects / buttons / hints), Go normalizes the spec at definition
time, renders it with the existing pkg/draw widget vocabulary, and
produces the same `apps.Region` list every built-in app produces. From
the desktop's point of view a JS app is indistinguishable from a Go app:
its objects pulse during accepts, answer clicks, and carry verb menus.

Three delivery surfaces, one IR:

1. **Standalone X windows** (`app.show()`, works from `go-go-wm run`) —
   the `pkg/apps/xapp` shell already does windows, broker bridging, and
   the click contract; the ui module supplies an adapter App.
2. **WM-embedded script tiles** (`app.tile()`, works from rc.js) — a
   small registry extension to the builtin-tile pipeline; scripts render
   tiles the WM paints, placed like any app via `wm.setApp(leaf,
   "script:<name>")`.
3. **xgoja provider** (`pkg/xgojaprovider`) — packages `wm`, `pbui`, and
   `ui` as provider modules so any xgoja-generated binary can require
   them (compile-time composition, per the xgoja playbook).

## Problem statement

The JSX prototype's apps (colors, notes, files…) were ported to Go in
GGWM-001. Writing a *new* app currently means writing Go: a render
function over pkg/draw plus xapp wiring. The whole point of the
scripting ticket family is "a JS grabbag of WM lego blocks" — and app
surfaces are the biggest missing block. The constraint that shapes
everything: **goja runs on one owner loop, and render paths (xapp loop,
WM loop) must never call into it synchronously.**

## The spec IR

A script's `render()` returns rows of segments. This is data, not
widgets — the Go side owns geometry, fonts, and colors:

```js
render() {
  return [
    ui.row(ui.text("MY COLORS", { bold: true })),
    ui.row(...state.colors.map((c) => ui.object("color", c))),
    ui.row(ui.button("Add random", "add"), ui.hint("click to present")),
  ];
}
```

Normalized Go form (`pkg/apps/uispec`):

```
Spec  = []Row
Row   = []Seg
Seg   = { Kind: text|object|button|hint,
          Text, Bold, Size,              // text / hint
          Ptype, Value, Label, Doc,      // object  → Region.Object
          Action, Color }                // button  → Region.Action
```

Normalization is definition-time (the GGWM-002 rule): unknown segment
kinds, non-slug ptypes, empty button actions all throw from `render()`'s
consumer before anything is drawn. The renderer
(`uispec.Render(w, h, spec, accepting)`) lays rows out top-to-bottom
with horizontal packing and wrap, draws with the same helpers as the
built-in demos (`apps.Btn`, `apps.Chip`, `apps.Hint`, accept
highlighting in Sel/Red), and returns `(*image.RGBA, []apps.Region)`.

## The snapshot handoff (the concurrency answer)

`xapp.App.Render` must be pure and runs on the xapp loop; JS cannot be
called there. The adapter (`jsApp`) therefore never calls JS to render —
it renders **the last normalized spec snapshot**:

```
JS loop:                          xapp/WM loop:
  action/verb/key handler runs      click → Region → action name
  render() → raw rows        ◄──────  posted to JS loop
  export → normalize (pure Go)
  snapshot = rows  ──────────►      setRows(snapshot); Redraw()
```

- Handlers (`actions`, `verbs`, `onKey`) run on the JS loop via
  `Owner.Post` — rule 6 of the GGWM-002 concurrency contract.
- After every handler, the module re-runs `render()`, exports the raw
  rows (on the JS loop), normalizes them (pure Go, any goroutine), and
  posts the snapshot to the render side.
- Render sides read the snapshot under a mutex. No loop ever waits on
  another.

A render error (throw, bad spec) becomes a `script.error` event and the
previous snapshot stays on screen — a broken render never blanks a tile.

## Module API

```js
const ui = require("ui");
const app = ui.app({
  name: "js-colors",            // broker client name / verb owner
  title: "JS COLORS",
  render() { return [...rows]; },
  actions: { add() { ... } },   // button actions by name
  verbs: [                      // optional; same shape as pbui.verb
    { id: "color.shout", label: "Shout", ptypes: ["color"],
      run(obj) { ... } },
  ],
  onKey(key) { ... },           // optional keyboard hook
});

app.show();          // standalone X window (run scripts; daemon mode)
app.tile();          // register as "script:js-colors" (rc.js only)
                     //   place it: wm.setApp(leaf, app.tile())
app.refresh();       // re-render outside a handler (e.g. from pbui.on)
```

Builders `ui.row/text/object/button/hint` are data-only (usable in any
profile). `show()` starts the xapp shell on a goroutine; `tile()`
registers with the WM's script-tile registry via a `TileHost` seam and
returns the app string for placement.

## Decision records

**U-D1: declarative spec, not a canvas API.** A canvas (`ui.rect`,
`ui.text(x,y)`) would push layout, hit-testing, and accept-highlight
logic into every script. The spec keeps the click contract and the look
in Go — scripts cannot get them wrong. Consequence: less pixel freedom;
the escape hatch (custom drawing) is deferred. *Accepted.*

**U-D2: snapshot handoff instead of synchronous JS render.** Calling JS
from Render would couple the render loops to the VM loop (deadline
management, deadlock risk — exactly what the GGWM-002 contract forbids).
Snapshots make Render pure at the cost of one frame of staleness after
external state changes, which `app.refresh()` covers. *Accepted.*

**U-D3: script tiles ride the builtin pipeline.** Tiles named
`script:<name>` join `""`/`launcher`/`builtin:*` in `isBuiltinLeaf`; the
WM consults a registry (`map[name]→render/action fns`) at paint and
click time. The registry stores *Go closures over snapshots*, so the WM
loop never sees JS. Registration flows through `wmx11.ScriptBackend`
(the existing A1 seam) — `run` scripts get a clear error pointing at
rc.js. *Accepted.*

**U-D4: provider modules connect lazily and degrade.** In xgoja
binaries there is no `run.go` to own the broker connection, and setup
must not fail just because no broker is up. Each provider module
resolves sockets from config JSON (falling back to `$PBUI_SOCKET` /
`$GO_GO_WM_SOCKET`) and connects on first use; without a broker the
data-only surface keeps working and everything else throws the standard
"not connected" error. *Accepted.*

## Phases

- **U1 — `pkg/apps/uispec`**: IR structs, `Normalize` (hostile-input
  tests), `Render` (region assertions; reuses apps helpers). No goja.
- **U2 — `pkg/jsmod/uimod`**: builders, `ui.app`, the jsApp adapter
  over xapp, action/verb/key dispatch, `app.refresh`; example
  `js-colors.js`; live Xvfb verification (click the button, accept a
  color from the JS app).
- **U3 — script tiles**: wmx11 registry + paint/click routing +
  `ScriptBackend` TileHost; `rc-tile.js` example; rc-smoke extension.
- **U4 — xgoja provider**: `pkg/xgojaprovider` registering wm/pbui/ui;
  registry-level test (no X).

## Testing

uispec is the testable core (pure functions): normalization rejections,
layout/region properties (every object seg yields a Region with the
right Object; buttons yield Actions; wrap never overlaps). uimod E2E
runs in Xvfb via the examples (self-asserting through the event bus and
`query verbs`); script tiles piggyback on rc-smoke. The provider test
registers the package into a real `providerapi.ProviderRegistry` and
instantiates every module loader.

## Open questions

- Scroll/viewport for long specs (trace-style tiles) — deferred with
  the same status as builtin trace scrolling (GGWM-001 backlog).
- A `ui.input()` text-field segment (needs focus routing) — deferred;
  `onKey` covers listener-style input lines for now.
- Per-app themes — out of scope; the paper-and-ink look is the look.
