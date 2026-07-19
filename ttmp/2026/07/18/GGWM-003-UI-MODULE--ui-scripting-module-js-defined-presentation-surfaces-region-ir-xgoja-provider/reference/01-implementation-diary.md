---
Title: Implementation diary
Ticket: GGWM-003-UI-MODULE
Status: active
Topics:
    - wm
    - pbui
    - goja
    - scripting
    - ui
DocType: reference
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://pkg/apps/uispec/uispec.go
      Note: the spec IR + renderer built in entry 1
    - Path: repo://pkg/jsmod/uimod/app.go
      Note: the snapshot-handoff adapter built in entry 1
    - Path: repo://pkg/wmx11/scripttiles.go
      Note: the WM script-tile registry built in entry 1
    - Path: repo://pkg/xgojaprovider/provider.go
      Note: the xgoja provider built in entry 1
ExternalSources: []
Summary: Chronological diary of the GGWM-003 build — the uispec IR and renderer, the ui module with its snapshot-handoff adapters (standalone windows and WM script tiles), and the xgoja provider, with the live-verification transcripts and pixel-level accept-highlight proof.
LastUpdated: 2026-07-19T00:20:00-04:00
WhatFor: Continuation context for the ui module; records what was built, how it was verified, and the traps.
WhenToUse: Read with design-doc/01 before extending scripted surfaces (inputs, scrolling, themes).
---

# Implementation diary

## Goal

Execute GGWM-003 (GGWM-002's deferred P5): the `ui` native module —
JS-defined presentation surfaces emitting the Region IR — plus the WM
script-tile registry and the xgoja provider package.

## Entry 1 — 2026-07-18/19: U1–U4 in one sitting

### What was done, in order

1. **U1 — `pkg/apps/uispec`**: the Seg/Row/Spec IR, `Normalize`
   (definition-time validation: unknown kinds/keys, non-slug ptypes,
   empty button actions, out-of-range sizes all throw with row/seg
   coordinates), and `Render` (top-to-bottom rows, left-to-right packing
   with wrap, `apps.Chip`/`apps.Btn`/hint drawing, accept highlighting,
   object→`Region.Object` and button→`Region.Action` emission). Pure
   package; 5 test functions including region-overlap and wrap
   assertions and a JSON-stability check.
2. **U2 — `pkg/jsmod/uimod`**: data-only builders (`row/text/object/
   button/hint` — permissive; Normalize is the gate), and `ui.app(def)`.
   The definition is parsed VM-side once (render fn, actions map, verbs
   with `run` handlers, optional `onKey`); the first snapshot is
   produced inside `ui.app` (legal: loader code runs on the JS loop).
   `app.show()` runs the xapp shell on a goroutine over a `jsXApp`
   adapter whose `Render` only reads the mutex-guarded snapshot;
   `HandleAction`/`HandleVerb`/`HandleKey` post to the JS loop, where
   the handler runs, `render()` re-runs, the new spec normalizes, and a
   repaint posts back. Render errors keep the previous frame and emit
   `script.error`. Added a tiny `xapp.Starter` extension (Started(ctx)
   once at boot) so the adapter owns a redraw hook before any click —
   that is what `app.refresh()` uses.
3. **U3 — script tiles**: `pkg/wmx11/scripttiles.go` — a
   `map[name]scriptTile` registry on the WM; `ScriptBackend.RegisterTile
   / RepaintTile` (the uimod.TileHost seam); `isBuiltinLeaf` accepts
   `script:*`; `paintBuiltin` branches to the registry (placeholder
   surface for unregistered names); `builtinAction` routes the whole
   action namespace of a script tile to its JS app; lavender title
   strips titled `<name> (js)`. `app.tile()` returns the app string so
   placement is ordinary (`wm.split(…, {app: app.tile()})`).
4. **U4 — `pkg/xgojaprovider`**: provider package (id `go-go-wm`)
   registering pbui/wm/ui via `providerapi.Register`. Lazy loaders:
   setup performs no I/O; first require() dials (config JSON `socket`/
   `wm_socket`/`display`/`name`, env fallbacks); a dead broker degrades
   pbui to data-only instead of failing the runtime. 5 registry-level
   tests, no X.
5. Wiring: run.go/repl.go get the ui module (broker socket passed
   through), rc.go gets it with `TileHost: ScriptBackend`. Help topic
   `ui-module`; cookbook entries 8–9; examples-smoke extended (WM stage
   now boots with `--rc rc-tile.js`; asserts the scripted tile is in the
   tree and js-colors' verb is on the broker) — **all 7 fixtures PASS**.

### Live verification (Xvfb :78, screenshots in various/)

- Screenshot 01: `js-colors.js` as a real X window (chips, three toned
  buttons) beside the WM-painted `js-counter (js)` tile from
  `rc-tile.js`.
- Screenshot 02: after xdotool clicks — counter shows `count: 2`,
  number chip `2`, `last change: inc` (WM loop → JS loop → snapshot →
  repaint round trip), js-colors gained a random chip; mouse-doc showed
  the button doc.
- Screenshot 03 + pixel proof: with a CLI `accept --ptype color`
  pending, the JS app's chips flip to Sel background and red borders —
  pixel counts over the chip row: Sel 0→3091, red 81→825 — and clicking
  chip `#b0563f` **answered the accept** (`{"ptype":"color","value":
  "#b0563f"}`, exit 0). A JS-defined app is a full accept participant.

### What worked

- The snapshot handoff produced zero concurrency drama: both render
  hosts (xapp window, WM tile) reuse the same jsAppState unchanged.
- The provider tests passed on the first run — the lazy/degrade shape
  fell straight out of U-D4.

### What didn't work / traps

- The accept highlight looked absent in downscaled screenshots (Sel vs
  PaneAlt differ mostly in the blue channel); pixel sampling settled it.
  Lesson: assert colors programmatically, not by eyeball.
- Frame titles: `paintFrame` re-derives `f.title` from `BuiltinTitle`
  on every paint, silently overwriting what `openBuiltin` set — script
  tiles needed the branch *there*, not only at open time.
- The pkill self-match rule from GGWM-002 held: even `pgrep -af
  "accept --socket"` in a compound command kills the shell (exit 144).
  Kill patterns must live in a script file invoked by nothing but its
  path.

### Code review instructions

Read `uispec.go` Normalize/Render first (the validation table and the
region emission), then `uimod/app.go` against Part VI of the GGWM-002
guide (check: no VM touch outside posted closures; snapshot mutex never
held across a JS call), then `scripttiles.go` (registry closures must
stay VM-free), then `xgojaprovider/provider.go` (lazy loader once-ness).
Run `go test ./pkg/apps/uispec/ ./pkg/xgojaprovider/` and
`GO_GO_WM_BIN=<bin> scripts/examples-smoke.sh`.

## Related

- design-doc/01 — the governing design (decisions U-D1..U-D4).
- GGWM-002 reference/01 — the scripting-layer diary this continues.
