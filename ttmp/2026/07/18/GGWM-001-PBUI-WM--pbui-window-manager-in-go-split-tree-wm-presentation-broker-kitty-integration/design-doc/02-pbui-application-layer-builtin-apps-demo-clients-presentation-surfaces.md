---
Title: 'PBUI application layer: builtin apps, demo clients, presentation surfaces'
Ticket: GGWM-001-PBUI-WM
Status: active
Topics:
    - wm
    - pbui
    - x11
    - broker
    - kitty
DocType: design-doc
Intent: long-term
Owners: []
RelatedFiles:
    - Path: pkg/apps/apps.go
      Note: Region model and click contract
    - Path: pkg/apps/builtin.go
      Note: builtin renderers + World
    - Path: pkg/apps/demo.go
      Note: pure renderers for colors/numbers/notes
    - Path: pkg/apps/demoapps/colors.go
      Note: color lab app + color verbs
    - Path: pkg/apps/demoapps/files.go
      Note: file browser (dual-role directory regions)
    - Path: pkg/apps/demoapps/markdown.go
      Note: markdown viewer, accepts <file>
    - Path: pkg/apps/demoapps/todo.go
      Note: todo list with keyboard (Keyer)
    - Path: pkg/apps/xapp/xapp.go
      Note: client app shell
    - Path: pkg/wmx11/builtin.go
      Note: WM-side builtin wiring
    - Path: pkg/wmx11/divider.go
      Note: divider interaction states
ExternalSources: []
Summary: Design of the PBUI application layer built in the same session as the WM core - the presentation-surface framework (regions + click contract), the WM-embedded launcher/about/trace/listener/inspector, the six standalone demo clients (colors, numbers, notes, files, todo, markdown), the listener.print event-bus channel, and the divider interaction states.
LastUpdated: 2026-07-18T22:00:00-04:00
WhatFor: Explains how applications are written against the PBUI system - both WM-embedded builtins and standalone X clients - and records the design decisions behind the shared surface/region model.
WhenToUse: Read before writing a new PBUI app, changing the click contract, or moving builtin apps out of (or into) the WM process.
---



# PBUI application layer: builtin apps, demo clients, presentation surfaces

## Executive Summary

The WM core (design doc 01) gives us tiles, the broker, and the accept
protocol. This document covers the layer on top: **how applications are
written**. One framework — `pkg/apps` — serves two very different hosts:

1. **WM-embedded builtins** (`launcher`, `about`, `trace`, `listener`,
   `inspector`): tiles rendered by the WM process itself over a shared
   `World`, the direct port of the prototype's singleton apps.
2. **Standalone demo clients** (`go-go-wm demo colors|numbers|notes|files|
   todo|markdown`): plain X clients that the WM manages like any other
   window, hosted by the `pkg/apps/xapp` shell, speaking pure PBUI protocol.

Both kinds render **presentation surfaces**: an `image.RGBA` plus a list of
`Region`s — rectangles carrying either a typed `pbui.Object` (clickable per
the accept/menu contract) or a plain `Action` id (a button). Renderers are
pure functions of app state, so every app's UI is golden-PNG tested with no
X server and no broker (`pkg/apps/apps_test.go`).

All of this was implemented and verified live in Xvfb during the same
session (see `reference/02-investigation-diary.md`, entry 2, and
`various/build-screenshots/08…12-*.png`).

## Problem Statement

The prototype's apps were React components sharing one address space; its
`APPS{}` table, `World` singleton, and `<P>` click behavior came for free.
On X11 we need:

- a way for the WM to render *its own* tiles with live presentations
  (trace/listener/inspector are views of WM-process state);
- a way for *external processes* to be first-class PBUI apps;
- one click contract for both, so a color chip behaves identically in the
  WM-drawn listener and in the standalone color lab;
- a channel by which any app can print into the listener transcript.

## Architecture

```
                 pure (golden-testable)              hosted
        ┌──────────────────────────────┐   ┌───────────────────────────┐
        │ pkg/apps                     │   │ WM process (pkg/wmx11)    │
        │  Region / Resolve (contract) │◄──┤  builtin.go: World wiring, │
        │  Btn / Chip / Hint widgets   │   │  frame regions, click     │
        │  RenderBuiltin (5 builtins)  │   │  routing, listener cmds   │
        │  RenderColors/Numbers/Notes  │   └───────────────────────────┘
        └──────────────┬───────────────┘   ┌───────────────────────────┐
                       │                   │ demo processes            │
        ┌──────────────▼───────────────┐   │ pkg/apps/xapp: X window,  │
        │ pkg/apps/demoapps            │◄──┤ event loop, broker bridge,│
        │  Colors Numbers Notes        │   │ keyboard (Keyer), hover   │
        │  Files Todo Markdown         │   └───────────────────────────┘
        └──────────────────────────────┘
```

### The Region model and click contract

```go
type Region struct {
    Rect   image.Rectangle
    Object *pbui.Object // presentation (accept/menu contract applies)
    Action string       // plain button ("cmd:sum") or primary action
    Doc    string       // mouse-doc line on hover
}

func Resolve(accepting []string, r *Region, button int) Click
```

`Resolve` ports the prototype's `<P>` click rules (pbui-shell.jsx:63-68):

1. **Right click (button 3)**: always the object menu.
2. **Left click** with a pending accept matching `Object.Ptype`: answer it.
3. Otherwise, the region's `Action` (its *primary action* — a launcher
   button, a directory's "navigate", a todo's "toggle").
4. Otherwise, the object menu.

A region may carry *both* Object and Action — a directory row navigates on
left click yet still menus on right click, exactly like `onActivate` in the
prototype.

### WM-embedded builtins (`pkg/wmx11/builtin.go`)

- Leaves with `App == ""` (launcher) or `App == "builtin:<name>"` get a
  frame window with **no client** (`frame.client == 0`); `paintFrame`
  renders the strip plus `apps.RenderBuiltin(...)` content and stores the
  returned regions on the frame for hit-testing.
- `syncBuiltins()` reconciles builtin frames after every op, so a closed
  client's lone leaf becomes a launcher again automatically.
- **World**: `apps.World{Trace, Listener, Inspected}` lives in the WM. The
  WM subscribes to the broker event bus (`watchEvents`); every event
  becomes a trace row (a live `<event>` presentation). This makes the
  trace tile literally a view of the system's op/event stream — the same
  stream `go-go-wm query events` serves, and the future goja hook surface.
- **Listener commands** (`Describe…`, `Sum…`, `Pick color…`) run accepts on
  goroutines and post results back to the loop; results are printed as live
  presentation chips (`Sum…` is the prototype's two-accept flow verbatim).
- **Inspector**: the WM owns the `any.inspect` verb; `describeObject` ports
  the prototype's `describe()` (color → rgb/luminance, number →
  prime/factors).
- New-client placement only reuses **launcher** leaves (`App == ""`);
  builtin app tiles are never stolen (`placementLeaf`).

### The xapp shell (`pkg/apps/xapp`)

What a standalone app implements:

```go
type App interface {
    Name() string          // broker client name = verb owner
    Title() string         // WM_NAME → title strip
    Verbs() []pbui.Verb
    Render(w, h int, accepting []string) (*image.RGBA, []apps.Region)
    HandleAction(ctx Ctx, action string)
    HandleVerb(ctx Ctx, verbID string, obj *pbui.Object)
}
type Keyer interface{ HandleKey(ctx Ctx, key string) } // optional
```

The shell owns: the X window (plain client — the WM frames it), the
event loop (same MainPing pattern as the WM), broker handlers
(accept.mode/clear → highlight repaint, verb.run → HandleVerb), the click
contract, hover → `doc.hover` (which is why hovering a swatch updates the
WM's mouse-doc line), and keyboard delivery for `Keyer` apps
(`keybind.LookupString`).

`Ctx.Print(segs...)` publishes a `listener.print` event whose payload is a
list of text/object segments; the WM's event watcher appends it to the
listener transcript **with the objects still live**. This is how the color
lab's mix result lands in the listener as clickable chips.

### The six demo apps (`pkg/apps/demoapps`)

| App | Ptypes presented | Verbs owned | Notable |
|---|---|---|---|
| colors | `<color>` swatches | color.mix / lighten / remove | mix runs its own accept — verbs compose with accept |
| numbers | `<number>` 2–61, primes shaded | number.multiply / factorize | listener's Sum… picks these up |
| notes | `<note>` ids + collected objects | any.collect / note.remove | collected objects stay live (accept/menu still work) |
| files | `<file>` / `<directory>` rows | dir.browse / file.head | dirs: navigate on L, menu on R (dual-role region) |
| todo | `<todo>` items | todo.toggle / remove / any.todo | keyboard input (Keyer); "From accept…" turns any object into a task |
| markdown | `<file>` chip of loaded doc | file.view | **Open… accepts a `<file>`** — answered by clicking a file-browser row |

The `file.view` verb means every `<file>` menu system-wide grows a "View as
markdown" entry while the viewer runs — the CLIM "verbs are contributed by
whoever implements them" property, working across processes.

### Divider interaction states (`pkg/wmx11/divider.go`)

Dividers are real windows in the split gaps with the prototype's four
states (pbui-shell.jsx:171-196): idle paper + dotted grip, hot paneAlt,
dragging sage, **snapped mustard** (the ¼ ⅓ ½ ⅔ ¾ flash), plus
row/col-resize cursors. Snap feedback is driven from `dividerMotion` via
`wmcore.Snap`. Verified live: drag to 0.336 → snapped to exactly ⅓ with the
mustard flash (`various/build-screenshots/12-divider-states-hot-drag-snap.png`).

## Design Decisions

### Decision A1: one Region model for builtins and clients

- **Context:** The same UI (chips, buttons, swatches) appears in WM-drawn
  tiles and client windows; two click-behavior implementations would drift.
- **Options considered:** separate WM-internal widget system vs. shared
  pure surface model.
- **Decision:** `pkg/apps.Region` + `Resolve` used by both hosts; renderers
  pure, hosts thin.
- **Rationale:** The contract (accept > activate > menu, right-click menus)
  is *the* product invariant; encoding it once makes it golden- and
  unit-testable (`TestRegionsAndResolve`) with zero X.
- **Consequences:** Hosts must shift region coordinates (builtin content
  offsets by strip height); regions are rebuilt on every render (fine at
  human rates).
- **Status:** accepted

### Decision A2: trace/listener/inspector live in the WM process

- **Context:** User requirement ("embed the trace + listener + inspector
  into the WM itself"); they are views of system-wide state.
- **Options considered:** separate demo clients (symmetric but the trace
  would need its own event subscription and the listener transcript would
  live outside the shell); WM-embedded views of a World.
- **Decision:** Embedded. The World lives in the WM; the trace feed *is*
  the broker event bus the WM already subscribes to.
- **Rationale:** Matches the Genera model (the Listener is part of the
  shell); zero extra processes for the core experience; the event bus
  remains the single source of truth (external `query events` sees exactly
  what the trace tile shows).
- **Consequences:** Builtin UI code paths run inside the WM loop —
  accept-running commands must hop through goroutines + Post (implemented
  in `listenerCommand`/`sumCommand`). Listener text-eval (the `3+4` input
  line) needs WM-side keyboard routing — deferred, tracked as a task.
- **Status:** accepted

### Decision A3: listener.print as an event-bus channel

- **Context:** Any app must be able to print into the listener, including
  live objects.
- **Options considered:** a dedicated broker message type; reusing the
  event bus with a conventional event name.
- **Decision:** `listener.print` events with `{segs: [{text}|{ptype,value}]}`.
- **Rationale:** No protocol change; prints are naturally part of the
  trace; any future listener implementation (or a second listener!) can
  subscribe identically.
- **Consequences:** Print segments are re-hydrated into Objects WM-side
  (`parsePrintSegs`); values restricted to JSON scalars for now.
- **Status:** accepted

### Decision A4: demo apps as one binary subcommand, not separate binaries

- **Context:** Single-binary project decision (design doc 01, D3).
- **Decision:** `go-go-wm demo --app <name>` with a `TypeChoice` flag; app
  state lives per-process.
- **Consequences:** Two instances of `demo colors` are two *different*
  worlds (unlike the prototype's singletons). Acceptable: the singleton
  property is preserved where it matters (the WM builtins); a shared-state
  variant would need broker-side state or a registry, tracked as an open
  question.
- **Status:** accepted

## Verified behavior (evidence)

All on Xvfb `:77`, screenshots in `various/build-screenshots/`:

1. `07-builtin-launcher-tile.png` — launcher tile with builtin buttons.
2. `08-full-pbui-desktop-six-tiles.png` — listener + trace + inspector
   (embedded) alongside color lab + file browser + markdown viewer
   (clients); trace shows real `client.connected` / `verbs.registered` /
   `window.managed` events.
3. `09-accepting-file-banner.png` / `10-markdown-via-accept-color-picked.png`
   — markdown's Open… accepted a `<file>` answered by a file-browser row
   (README.md rendered); listener shows `picked ▉#b0563f` with a live chip
   after Pick color… was answered by a color-lab swatch. Three processes,
   one accept protocol.
4. `11-todo-keyboard-and-tile-menu.png` — todo task added via real typed
   keys (`todo_added via=keyboard` in trace); tile title click opened the
   object menu (and a menu row click split the tile — the contract working
   end to end).
5. `12-divider-states-hot-drag-snap.png` — divider hot/drag/snap states.

## Implementation Plan (remaining)

1. Listener eval line (`3+4` → live `<number>`): WM-side key routing to the
   focused builtin tile.
2. Scrollback/scrolling for trace and listener (currently tail-only).
3. Shared-state demo apps (broker-registered world?) — decide only if a
   real need appears.
4. Markdown viewer: render inline `pbui://` links and bare URLs as `<url>`
   presentations; kitty graphics faces later.
5. Golden tests for files/todo/markdown renderers (colors/numbers/notes and
   all builtins are covered).

## Open Questions

1. Should builtin tiles be selectable from the title strip (the prototype's
   dropdown)? Currently launcher-buttons only.
2. Should `any.collect` target a *specific* notes instance when several
   run? (Verb routing is by owner name; duplicate names currently race.)
3. Presentation persistence for notes/todo across restarts (ties into
   design doc 01, open question 3).

## References

- `design-doc/01-pbui-wm-design-and-implementation-guide.md` — system
  architecture, wire protocol, decision records D1–D6.
- `sources/pbui-shell.jsx` — the prototype; app ports reference:
  ColorsApp 311-333, NumbersApp 335-348, NotesApp 350-374, ListenerApp
  376-439, InspectorApp 441-449, TraceApp 458-480, actionsFor 712-760.
- `reference/02-investigation-diary.md` entry 2 — build narrative,
  including what failed.
- Code: `pkg/apps/`, `pkg/apps/xapp/`, `pkg/apps/demoapps/`,
  `pkg/wmx11/builtin.go`, `pkg/wmx11/divider.go`, `pkg/cmds/demo.go`.
