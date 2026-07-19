---
Title: PBUI WM design and implementation guide
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
    - Path: cmd/go-go-wm/main.go
      Note: empty scaffold to be wired glaze-style in Phase 0
    - Path: repo://ttmp/2026/07/18/GGWM-001-PBUI-WM--pbui-window-manager-in-go-split-tree-wm-presentation-broker-kitty-integration/sources/pbui-shell.jsx
      Note: reference prototype; all Part I line numbers refer to this file
    - Path: repo://ttmp/2026/07/18/GGWM-001-PBUI-WM--pbui-window-manager-in-go-split-tree-wm-presentation-broker-kitty-integration/sources/screenshot-01-full-shell-object-menu.png
      Note: visual reference — full shell with object menu open
    - Path: repo://ttmp/2026/07/18/GGWM-001-PBUI-WM--pbui-window-manager-in-go-split-tree-wm-presentation-broker-kitty-integration/sources/screenshot-02-accept-mode-drag-swap.png
      Note: visual reference — drag/drop preview and trace/event log
ExternalSources:
    - https://github.com/BurntSushi/wingo
    - https://github.com/jezek/xgb
    - https://github.com/BurntSushi/xgbutil
    - https://sw.kovidgoyal.net/kitty/kittens/hints/
Summary: Intern-level architecture, design, and phased implementation guide for go-go-wm — a Go X11 tiling window manager with a CLIM-style presentation/accept layer (PBUI), a broker daemon, kitty terminal integration, glazed-based CLI structure, and forward-looking hooks for goja JS scripting.
LastUpdated: 2026-07-18T21:30:00-04:00
WhatFor: The primary onboarding and implementation document for the PBUI WM project; explains every subsystem, the wire protocol, the command surface, and the build order.
WhenToUse: Read top-to-bottom before writing any code; return to specific sections (protocol, module APIs, phases) while implementing.
---


# PBUI WM design and implementation guide

## Executive Summary

We are building **go-go-wm**: a real X11 tiling window manager, in Go, that
ports the ideas of the React prototype in `sources/pbui-shell.jsx` — a
CLIM / Genera "Dynamic Windows" style shell where every visible object is a
**typed presentation** that commands can read via an **accept protocol**,
where right-clicking any object pops a **type-directed action menu**, and
where the window manager itself (tiles, workspaces) is made of presentations.

The system has three parts, all shipped **in one binary** built on the
**glazed** command framework:

1. **The window manager** (`go-go-wm wm`): a reparenting X11 WM with a binary
   split tree, Blender-style sticky resize zones, drag-to-swap/dock, and
   workspaces, drawing its own paper-and-ink frames with no compositor.
2. **The PBUI broker** (`go-go-wm broker`, also embeddable in the WM process):
   a display-server-agnostic daemon on a Unix socket that owns the accept
   state machine and the verb registry. Applications register presentations
   and verbs with it; the WM is just its most privileged client.
3. **Client tooling**: `go-go-wm present` (emit OSC 8 presentation links from
   any CLI), `go-go-wm query` (bspc-style introspection), `go-go-wm menu`
   / `go-go-wm accept` (participate from scripts), plus a Python **kitty
   kitten** that turns any kitty terminal into a full PBUI participant.

The architecture is deliberately layered so that the two most-iterated
subsystems — the layout tree and the accept protocol — are pure Go with zero
X11 dependency and test with no display, and so that a JS engine
(go-go-goja) can later drive every part of it: all mutations flow through a
single serializable **operations/event bus**, and all verbs live in a
data-driven registry rather than in switch statements.

## Problem Statement and Scope

The prototype (`sources/pbui-shell.jsx`, 868 lines) demonstrates the full
interaction model in a browser, where it is easy because every app shares one
address space. The task is to make it real on X11, where:

- the WM half (tiles/workspaces/aesthetic) maps almost mechanically onto a
  reparenting WM, but
- the presentation half becomes an **IPC protocol design problem**, because
  X11 deliberately knows nothing about objects inside client windows.

In scope: X11 WM, broker + wire protocol, Go client library and CLI helpers,
kitty integration, paper-and-ink rendering, hermetic test strategy, glazed
command surface, design hooks for later goja scripting. Out of scope for now:
Wayland (see the research doc §2), non-kitty terminal integrations, a
compositor, sound/notifications.

The companion document
`reference/01-preliminary-research-x11-presentations-kitty-testing.md`
carries the full rationale; this guide is the "how to build it".

## Part I — Understanding the prototype (read this first)

Everything we build is a port of behavior that already exists in
`sources/pbui-shell.jsx`. An intern should read that file once, with this map
in hand. Line numbers refer to the imported copy in this ticket.

Two screenshots of the running prototype are imported alongside it and are
the **visual reference for the whole project**:

- `sources/screenshot-01-full-shell-object-menu.png` — the full shell: the
  workspace strip, six tiles (color lab, number field, listener, notes,
  trace, inspector), the status/mouse-doc line, and an open object menu on a
  `<color>` swatch showing the type-directed verbs (Inspect, Mix with…,
  Lighten, Remove swatch, Collect into Notes).
- `sources/screenshot-02-accept-mode-drag-swap.png` — a ⠿ drag in progress:
  the notes tile outlined with the dashed red drop preview and the
  "⇄ swap apps" / "new tile → swap apps" drag ghost; the trace tile showing
  the sequenced event log (`accepted`, `collected`, `split_tile`) and the
  listener holding live presentation chips from previous commands.

`pkg/draw` golden files should converge on this look: paper `#e9e2d0`
background, hard 2px ink borders, flat colored title strips, hard drop
shadows, IBM Plex Mono everywhere.

### I.1 The presentation primitive: `<P>`

`P` (`sources/pbui-shell.jsx:52-73`) wraps any rendered value in a typed,
live object. Its contract:

- **Identity**: a presentation is a pair `(ptype, value)` — e.g.
  `("color", "#b0563f")`, `("number", 42)`, `("tile", "n7")`.
- **Left click**: if an accept for a matching ptype is pending → resolve the
  accept with this object. Else if the presentation has a primary action
  (`onActivate`) → run it. Else → open the object menu.
- **Right click**: always the object menu.
- **Hover**: writes a one-line description plus click documentation into the
  **mouse-doc line** at the bottom of the screen.
- **Accept highlight**: while an accept is pending, matching presentations get
  a pulsing red outline (`.pres.acceptable`, line 779).

`Pres` (`sources/pbui-shell.jsx:77-89`) is the *default face* for a
`(ptype, value)` — used when an app re-presents an object it did not
originate (a color collected into notes still shows a swatch). This is the
key to composability: values travel, faces are recomputed.

### I.2 The accept protocol

`accept(ptype, prompt)` (`sources/pbui-shell.jsx:676-689`) returns a Promise:

1. Entering accept mode sets a global `accepting = {ptype, prompt, resolve}`.
2. The shell shows a red **ACCEPTING banner** at the top (line 791-795) and
   every matching presentation — *in any tile, in any workspace* — becomes
   clickable-to-answer.
3. Clicking a matching presentation resolves the promise with
   `{ptype, value}`; Escape resolves it with `null` (line 684-689).

Commands are then written as straight-line async code, e.g. the listener's
Sum command (`sources/pbui-shell.jsx:389-397`): accept a number, accept
another number, print the result — where the result is *itself* a live
`<number>` presentation.

The crucial property to preserve in the port: **accept is global and
modal-but-non-blocking** — the user can switch workspaces mid-accept, open
menus are closed, and only one accept session exists at a time.

### I.3 Type-directed action menus

`actionsFor(ptype, value)` (`sources/pbui-shell.jsx:712-760`) is a central
table mapping a ptype to verbs: every object gets "Inspect"; colors get
"Mix with… (accept a color)", "Lighten", "Remove swatch"; numbers get
"Multiply by…"; tiles get "Split ⬌/⬍", "Swap app with… (accept a tile)",
"Close"; workspaces get "Switch to", "Rename", "Duplicate", "Delete"; most
things get "Collect into Notes". Note that verbs freely *start new accepts* —
menus and accept compose.

In the port this table becomes the broker's **verb registry**, keyed by
ptype, populated by the WM and by client apps at registration time.

### I.4 The window manager model

The layout is an immutable binary tree (`sources/pbui-shell.jsx:135-169`):

```
node := leaf{id, app}
      | split{id, dir: row|col, a: node, b: node, ratio: 0..1}
```

Operations, all pure tree→tree functions:

- `updateNode`, `removeLeaf`, `findLeaf`, `countLeaves`, `cloneTree`
  (lines 140-164). `removeLeaf` replaces a split by the surviving sibling —
  "close a tile, its sibling absorbs the space".
- `splitLeaf(id, dir)` (line 574): replace a leaf with a split of itself and
  a new "launcher" leaf.
- `swapTiles(a, b)` (line 583): exchange the `app` fields of two leaves —
  state travels because it lives in the world, not the tile.
- `moveSplit(from, target, zone)` (lines 594-607): the ⠿ edge-drop. Detach
  the source leaf, then split the target on the given side with it. A move,
  not a copy; refuses if detaching is impossible (single-leaf tree).
- **Sticky snapping** (lines 166-169): divider drags snap to
  `[¼, ⅓, ½, ⅔, ¾]` within a `0.022` band; the divider recolors when hot /
  dragging / snapped (line 192).
- **Drop zones** (`zoneFor`, lines 612-621): pointer position inside a tile
  maps to `center` (swap) or `left/right/top/bottom` (split-dock) using a
  band of `min(30% of min dimension, 110px)`.

Workspaces (`sources/pbui-shell.jsx:656-673`) are named trees with
add/remove/clone; the workspace chips, and the tiles themselves, are
presentations (`ptype "workspace"`, `ptype "tile"`) with their own verbs.

### I.5 Apps and the World

Apps are **singletons**: `World` (`sources/pbui-shell.jsx:95-123`) holds all
app state (colors, notes, listener transcript, trace, inspector target);
opening the same app in two tiles yields two live views of one state. Apps
are registered in a flat table `APPS{}` (lines 522-531) as
`{title, color, component}` — porting an app means adding an entry and
speaking `<P ptype value>`.

The **trace app** (lines 458-480) matters more than it looks: every mutation
logs a typed event (`split_tile`, `accepted`, `mixed`, `workspace_added` …)
and the events are themselves presentations. In the Go system this becomes
the event bus — and later, the JS extension surface.

## Part II — System architecture

### II.1 The big picture

```
                        ┌──────────────────────────────────────────────┐
                        │              go-go-wm (one binary)           │
                        │                                              │
   X11 server ◄────────►│  wm-x11 ──► wm-core (pure tree)              │
   (Xorg/Xephyr/Xvfb)   │    │  ▲        │                             │
                        │    │  └─ render │ geometry                   │
                        │    ▼            ▼                            │
                        │  draw (image.RGBA, paper-and-ink theme)      │
                        │    │                                         │
                        │    │ broker client (Unix socket)             │
                        └────┼─────────────────────────────────────────┘
                             │
              ┌──────────────▼──────────────┐
              │   pbui broker (daemon or    │   accept sessions,
              │   embedded in wm process)   │   verb registry,
              │   $XDG_RUNTIME_DIR/pbui.sock│   presentation registry,
              └──┬───────────┬──────────┬───┘   event fan-out
                 │           │          │
        ┌────────▼───┐  ┌────▼─────┐  ┌─▼──────────────┐
        │ kitty      │  │ pbui     │  │ any Go/script  │
        │ accept     │  │ present/ │  │ app using the  │
        │ kitten (py)│  │ menu CLI │  │ client library │
        └────────────┘  └──────────┘  └────────────────┘
```

Dependency direction is strict and enforced by package layout:

```
wmcore ◄── wmx11 ──► draw
              │
              ▼
           pbui (core types) ◄── broker ◄── client ◄── present/scrape/kitty
```

`pbui` (core types) and `wmcore` import nothing project-internal. The broker
imports only `pbui`. Only `wmx11` imports X libraries.

### II.2 Repository layout (target)

The repo is the freshly scaffolded go-go-golems binary at the workspace root
(`go.mod` = `github.com/go-go-golems/go-go-wm`, empty `cmd/go-go-wm/main.go`,
placeholder `pkg/`). Target layout:

```
cmd/go-go-wm/main.go          — root cobra command, glazed help system, logging
pkg/pbui/                     — ptypes, Object, Verb, wire messages, framing
pkg/pbui/broker/              — accept state machine, registries, socket daemon
pkg/pbui/client/              — Go client: Connect, Present, Accept, RegisterVerbs
pkg/pbui/present/             — OSC 8 emission helpers (terminal marking)
pkg/pbui/scrape/              — regex/parser translators for plain text
pkg/wmcore/                   — split tree, snapping, workspaces, layout
pkg/wmx11/                    — xgb/xgbutil shell: events, frames, EWMH, grabs
pkg/draw/                     — theme + widgets rendered to image.RGBA; golden-testable
pkg/draw/fonts/               — embedded IBM Plex Mono TTFs (go:embed)
pkg/apps/                     — built-in PBUI apps (listener, notes, inspector…) later
pkg/cmds/                     — glazed command definitions (one file per command)
kitty/pbui_accept.py          — custom kitten (embedded via go:embed, installed on demand)
kitty/open-actions.conf       — snippet installed by `go-go-wm kitty install`
pkg/doc/                      — embedded help topics for the glazed help system
```

### II.3 The single binary and its glazed command surface

Everything is subcommands of one binary (per the project decision: no
separate daemons to install; the broker is a subcommand, and can also run
embedded in the WM process).

| Command | Kind | Purpose |
|---|---|---|
| `go-go-wm wm` | bare | run the window manager (flags: `--display`, `--embedded-broker`, `--socket`, `--workspaces`) |
| `go-go-wm broker` | bare | run the broker standalone (`--socket`) |
| `go-go-wm query tree` | glazed | dump the layout tree(s) as structured rows |
| `go-go-wm query windows` | glazed | managed clients: id, class, tile, workspace |
| `go-go-wm query events --follow` | glazed | subscribe to the event bus, one row per event |
| `go-go-wm present <ptype> <value> [text]` | bare | print an OSC 8 pbui link to stdout |
| `go-go-wm accept <ptype> --prompt …` | bare | start an accept session, print the accepted object as JSON |
| `go-go-wm answer <ptype> <value>` | bare | answer the pending accept (used by the kitten) |
| `go-go-wm menu <uri>` | bare | ask the broker to pop the verb menu for `pbui://…` |
| `go-go-wm scrape [--rules …]` | bare | stdin→stdout filter injecting pbui links into plain text |
| `go-go-wm kitty install` | bare | write the kitten + open_actions snippet into the kitty config dir |

Query commands are **glazed commands** so we get `--output json/yaml/table`,
field selection, and templating for free — the WM's introspection surface is
then scriptable from day one, which is both the test harness (§Part V) and
the precursor of the JS API.

Command skeleton (current glazed API, v1.0.5+ — import paths matter, see
`glazed/pkg/cmds/…`):

```go
// pkg/cmds/query_tree.go
type QueryTreeCommand struct{ *cmds.CommandDescription }

type QueryTreeSettings struct {
    Socket    string `glazed:"socket"`
    Workspace string `glazed:"workspace"`
}

func NewQueryTreeCommand() (*QueryTreeCommand, error) {
    glazedSection, err := settings.NewGlazedSchema()
    if err != nil { return nil, err }
    return &QueryTreeCommand{cmds.NewCommandDescription("tree",
        cmds.WithShort("Dump the layout tree as rows"),
        cmds.WithFlags(
            fields.New("socket", fields.TypeString,
                fields.WithDefault(""), fields.WithHelp("broker/wm socket (default: $XDG_RUNTIME_DIR/pbui.sock)")),
            fields.New("workspace", fields.TypeString,
                fields.WithDefault(""), fields.WithHelp("restrict to one workspace")),
        ),
        cmds.WithSections(glazedSection),
    )}, nil
}

func (c *QueryTreeCommand) RunIntoGlazeProcessor(
    ctx context.Context, vals *values.Values, gp middlewares.Processor,
) error {
    s := &QueryTreeSettings{}
    if err := values.DecodeSectionInto(vals, schema.DefaultSlug, s); err != nil { return err }
    cl, err := client.Connect(ctx, s.Socket)
    if err != nil { return err }
    defer cl.Close()
    tree, err := cl.QueryTree(ctx, s.Workspace)
    if err != nil { return err }
    for _, n := range tree.Flatten() {
        _ = gp.AddRow(ctx, types.NewRow(
            types.MRP("workspace", n.Workspace), types.MRP("id", n.ID),
            types.MRP("kind", n.Kind), types.MRP("dir", n.Dir),
            types.MRP("ratio", n.Ratio), types.MRP("app", n.App),
            types.MRP("rect", n.Rect.String()),
        ))
    }
    return nil
}
```

`cmd/go-go-wm/main.go` follows the glaze pattern: build root cobra command,
`logging.AddLoggingSectionToRootCommand`, create the help system, load
`pkg/doc` topics, `help_cmd.SetupCobraRootCommand`. The repo scaffold already
carries logcopter wiring (`logcopter_generate.go`).

## Part III — Subsystem designs

### III.1 `pkg/pbui` — the object model and wire protocol

This package is the contract everything else depends on. It contains **no
I/O** — just types and encoding.

```go
// A typed presentation value. Ptype is an open string namespace
// ("color", "number", "file", "git-commit", "tile", "workspace", …).
type Object struct {
    Ptype string          `json:"ptype"`
    Value json.RawMessage `json:"value"`          // typed payload, ptype-defined shape
    Label string          `json:"label,omitempty"` // display face fallback
    Doc   string          `json:"doc,omitempty"`   // mouse-doc line text
}

// A verb in the type-directed action table.
type Verb struct {
    ID      string   `json:"id"`      // "color.mix", "tile.split-right"
    Label   string   `json:"label"`   // "Mix with…  (accept a color)"
    Ptypes  []string `json:"ptypes"`  // which ptypes it applies to; ["any"] allowed
    Accepts []string `json:"accepts,omitempty"` // ptypes this verb will accept() when run
    Owner   string   `json:"-"`       // filled by broker: which client registered it
}
```

**Ptype matching** ports `typeMatches` (`sources/pbui-shell.jsx:44-45`):
`"any"` matches everything; otherwise exact string match against a list.
Keep it this dumb until a real need for subtyping appears; CLIM had a type
lattice and we explicitly defer that (see Open Questions).

**URI form** (used by kitty/OSC 8 and `go-go-wm menu`):
`pbui://<ptype>/<url-encoded-value>` for scalar values;
`pbui://<ptype>/?v=<base64url(json)>` for structured ones. One function pair,
`ObjectToURI` / `ObjectFromURI`, owns this bijection — nothing else parses
URIs.

**Wire protocol.** Newline-delimited JSON frames over a Unix stream socket
(`$XDG_RUNTIME_DIR/pbui.sock` by default). NDJSON first; the framing is
isolated behind a `Codec` interface so deterministic CBOR can replace it
later without touching handlers.

Messages (all carry `"t"` — the message type — and a client-scoped `"seq"`
for request/response pairing):

```
client → broker
  hello        {name, roles: ["app"|"wm"|"picker"], protocol: 1}
  register     {presentations?: bool, verbs: [Verb]}
  accept.start {ptype: [..], prompt}            → accept.result later
  accept.answer{session, object: Object}         (any client may answer)
  accept.cancel{session}
  verb.invoke  {verb_id, object: Object}
  menu.request {object: Object, x, y}            (ask WM to pop a menu)
  doc.hover    {text}                            (feed the mouse-doc line)
  query.*      {…}                               (tree, windows, verbs…)
  event.emit   {type, data}                      (apps add to the trace)

broker → client
  accept.mode  {session, ptype: [..], prompt}    broadcast: highlight matches
  accept.clear {session, reason: done|cancelled}
  accept.result{session, object: Object | null}  to the requester only
  verb.run     {verb_id, object}                 to the verb's owner
  menu.show    {object, verbs: [Verb], x, y}     to the WM only
  event        {seq, type, data, source}         event-bus fan-out to subscribers
  error        {seq, code, msg}
```

The accept flow, end to end (compare `sources/pbui-shell.jsx:676-689`):

```
app A ── accept.start {ptype:[color], prompt:"MIX #b0563f — click a COLOR"} ─► broker
broker ── accept.mode ─► ALL clients (WM draws banner; kitty kitten may arm;
                                      GUI apps outline matching swatches)
user clicks a swatch in app B (or picks a hyperlink in kitty)
app B ── accept.answer {session, object:{ptype:color, value:"#9cb4c2"}} ─► broker
broker ── accept.result {object} ─► app A          (its accept() returns)
broker ── accept.clear ─► ALL clients               (highlights drop, banner clears)
```

Broker rules that make this safe:

1. **One session at a time.** A new `accept.start` while one is pending
   cancels the old one (matching the prototype, which overwrites
   `accepting`). Sessions carry ids so stale answers are rejected.
2. **Escape anywhere cancels**: the WM translates the Escape grab into
   `accept.cancel`.
3. **Disconnect of the requester cancels its session; disconnect of any
   client mid-answer is ignored.**
4. The broker never trusts clients: malformed frames get `error` and, on
   repeat, disconnect; the decoder is fuzz-tested (§Part V).

### III.2 `pkg/pbui/broker` — the daemon

A single goroutine owns all state (registries + the current accept session);
connection goroutines only decode frames and post them to it over a channel.
This "post to the loop" shape is deliberate — it is the same discipline as
the X event loop, and it is exactly the shape a goja runtime needs later
(JS is single-threaded; everything that wants to call into JS must already
be funneled through one loop).

```go
type Broker struct {
    ops     chan func(*state)      // all mutations posted here
    clients map[ClientID]*conn     // writers are per-conn goroutines with send queues
}
type state struct {
    verbs    map[string][]pbui.Verb   // ptype → verbs (the actionsFor table)
    session  *acceptSession           // nil when idle
    eventSeq uint64
    subs     map[ClientID]EventFilter // event-bus subscriptions
}
```

Embedded mode: `go-go-wm wm --embedded-broker` runs `broker.New().Serve(l)`
in-process and the WM connects over the same socket API (loopback through
the Unix socket, *not* a Go function call) — so the WM never gets a secret
side channel and the protocol stays honest.

The **event bus** is the broker's second job: every mutation in the system
(WM ops, accepts, verb runs, app events) becomes a sequenced event, exactly
like the prototype's trace (`sources/pbui-shell.jsx:113, 451-480`). Events
are the trace app's feed, the test harness's assertion stream
(`go-go-wm query events --follow`), and later the JS hook surface
(`on("tile.split", fn)`).

### III.3 `pkg/wmcore` — the pure layout engine

A direct port of `sources/pbui-shell.jsx:135-169` and the mutation set at
lines 571-673, as immutable-ish pure functions with string IDs:

```go
type NodeID string
type Dir uint8 // Row, Col

type Node struct {           // one struct, tagged — simpler to serialize than an interface
    ID    NodeID
    Kind  Kind               // Leaf | Split
    App   string             // Leaf: app/client slot id
    Dir   Dir                // Split
    Ratio float64            // Split, clamped [0.1, 0.9]
    A, B  *Node              // Split
}

type Workspace struct { ID, Name string; Root *Node }
type Desktop  struct { Workspaces []Workspace; Current string }

// Pure operations (each returns a new tree; errors instead of silent no-ops):
func SplitLeaf(root *Node, id NodeID, dir Dir, newApp string) (*Node, error)
func CloseLeaf(root *Node, id NodeID) (*Node, error)
func SetRatio(root *Node, id NodeID, ratio float64) (*Node, error)
func SwapLeaves(root *Node, a, b NodeID) (*Node, error)
func MoveSplit(root *Node, from, target NodeID, zone Zone) (*Node, error)
func SetLeafApp(root *Node, id NodeID, app string) (*Node, error)

// Geometry: tree in, rectangles out. Gap = divider thickness.
func Layout(root *Node, r Rect, gap int) map[NodeID]Rect
// Sticky snapping, ports SNAPS/STICK/snapFrac (jsx:166-169):
func Snap(f float64) (frac float64, snapped bool)   // snaps to ¼ ⅓ ½ ⅔ ¾ within 0.022
// Drop zones, ports zoneFor (jsx:612-621):
func ZoneAt(r Rect, x, y int) Zone                  // Center | Left | Right | Top | Bottom
```

Two additions the prototype didn't need:

- **`Layout` is explicit** (the browser's flexbox did it for free). The rule:
  a split divides its rect at `ratio` along `Dir`, minus `gap` pixels for
  the divider; recurse. Property: rects of all leaves tile the workspace
  rect exactly, minus dividers.
- **Ops as data.** Every mutation also exists as a serializable `Op`
  (`{op:"split-leaf", id, dir, app}` …) with `Apply(desktop, Op)`. The WM,
  the query IPC, the record/replay tests, *and the future JS bindings* all
  speak Ops — one vocabulary, three consumers. This is decision D5 below.

### III.4 `pkg/wmx11` — the thin X shell

Modeled on wingo; stack is `github.com/jezek/xgb` +
`github.com/BurntSushi/xgbutil` (`xevent`, `ewmh`, `icccm`, `keybind`,
`mousebind`, `xgraphics`, `xwindow`).

Responsibilities, and nothing more:

1. **Become the WM**: select `SubstructureRedirect|SubstructureNotify` on the
   root (fails if another WM runs), claim `WM_S0`, advertise EWMH support
   (`_NET_SUPPORTED`, `_NET_SUPPORTING_WM_CHECK`).
2. **Reparent**: on `MapRequest`, create a frame window, reparent the client
   into it below a title strip, register the pair, assign it to the focused
   leaf (or split per policy). On `DestroyNotify`/`UnmapNotify`, close the
   leaf. Honor `WM_DELETE_WINDOW` / `WM_TAKE_FOCUS` per ICCCM.
3. **Apply geometry**: after any tree change,
   `for id, rect := range wmcore.Layout(...)` → `ConfigureWindow` each frame,
   then repaint title strips.
4. **Frames are presentations**: the title strip renders (via `pkg/draw`) the
   ⠿ grip, app title, and ⬌ ⬍ ✕ buttons; clicks map to Ops or accepts
   (`ptype "tile"`), mirroring `TileView` (`sources/pbui-shell.jsx:225-278`).
5. **Interactive drags**: divider drag and ⠿ drag are pointer grabs; motion
   events run through `wmcore.Snap` / `wmcore.ZoneAt`; the drop preview is a
   stippled/rubber-band overlay (no compositor), ports the dashed zone
   overlay of `sources/pbui-shell.jsx:244-254`.
6. **Bars**: one strut dock window at the top (workspace chips + ACCEPTING
   banner) and one at the bottom (status + mouse-doc line), contents from
   `pkg/draw`, data from the broker (`accept.mode`, `doc.hover`).
7. **EWMH bookkeeping**: `_NET_CURRENT_DESKTOP`, `_NET_WM_DESKTOP`,
   `_NET_CLIENT_LIST`, `_NET_ACTIVE_WINDOW`, struts — so pagers, bars, and
   `wmctrl` interoperate.

The event loop is one goroutine. External inputs (broker messages, query
requests) are posted into it:

```go
for {
    select {
    case ev := <-xEvents:      handleX(ev)        // MapRequest, ButtonPress, Expose…
    case msg := <-brokerMsgs:  handleBroker(msg)  // accept.mode → repaint banner…
    case fn := <-posted:       fn()               // ops from query IPC / later JS
    }
}
```

Unmanaged clients degrade gracefully: any plain X window is still a leaf in
the tree and an opaque `<window>` presentation with generic verbs (close,
move-to-workspace, swap) — the CLIM "foreign object" stance.

### III.5 `pkg/draw` — paper and ink

Pure rendering into `image.RGBA` (xgbutil's `xgraphics.Image` embeds one, so
upload is cheap). The palette ports `C` (`sources/pbui-shell.jsx:30-36`):
paper `#e9e2d0`, pane `#f5f0e3`, ink `#33302a`, red `#b0563f`, mustard
`#d3b56a`, etc. Fonts: IBM Plex Mono TTFs embedded with `go:embed`, rendered
via `golang.org/x/image/font/opentype` — no fontconfig, fully deterministic.

Widget set (each a pure `func(spec) *image.RGBA` or a draw-into function):
`TitleStrip{Title, Color, Buttons, Focused}`, `Banner{Prompt, Ptypes}`,
`StatusLine{Mode, Doc, Counts}`, `Menu{Header, Items, Hover}`,
`WorkspaceChip{Name, Current}`, `DividerStyle(mode)`. Hard rules: flat fills,
1–2px strokes, hard shadows (`3px 3px 0 ink`), no anti-aliased decoration
beyond glyphs, no gradients, no rounding. Golden-PNG tested (§Part V).

### III.6 `pkg/pbui/client`, `present`, `scrape` — the participation kit

```go
// client: what an app links against (also used by the WM itself)
c, _ := client.Connect(ctx, socket)           // does hello
c.RegisterVerbs(ctx, []pbui.Verb{...})        // contribute to the action table
obj, err := c.Accept(ctx, []string{"color"}, "MIX — click a COLOR")  // blocks
c.Answer(ctx, session, obj)                   // resolve someone's accept
c.OnVerb(func(v pbui.Verb, o pbui.Object) {...})
c.Events(ctx, filter)                          // event-bus subscription (channel)
```

`present` emits the OSC 8 face for terminals:

```go
func Link(o pbui.Object, text string) string  // ESC]8;;pbui://…ESC\ text ESC]8;;ESC\
```

and backs `go-go-wm present color '#b0563f' '▉ #b0563f'` — the one-liner
that makes any shell script a PBUI app. `scrape` is the stdin→stdout filter
with a rule table (hex colors, git SHAs, absolute paths, PIDs, IPs → ptypes),
the terminal analog of CLIM presentation translators; `git log | go-go-wm
scrape` lights up commits.

### III.7 Kitty integration

Three small artifacts, all installed by `go-go-wm kitty install` (files are
`go:embed`-ded in the binary so the single-binary story holds):

1. **`pbui_accept.py`** (custom kitten): invoked via kitty remote control
   when an accept starts while a kitty window is focused
   (`kitten @ kitten pbui_accept.py --ptype color --session S1`). It scans
   screen lines for OSC 8 hyperlinks with `pbui://<matching-ptype>/…` URIs,
   overlays hint labels (hints-kitten style), and on selection execs
   `go-go-wm answer <ptype> <value> --session S1`. Pure-Python core
   (`scan(lines) -> [candidate]`) kept free of kitty's boss API for unit
   testing.
2. **`open-actions.conf` snippet**: `protocol pbui` → `launch go-go-wm menu
   ${URL}` — plain click on any pbui link pops the WM-drawn verb menu. This
   matches the prototype's left-click-falls-through-to-menu convention, which
   conveniently sidesteps kitty's lack of button-differentiated link
   handling.
3. **Broker-side kitty adapter**: on `accept.mode`, if the focused client is
   a kitty window (WM knows via `WM_CLASS` + kitty's `--listen-on` socket
   discovered from its env/config), the broker's kitty module issues the
   remote-control call to arm the kitten; on `accept.clear` it dismisses it.

Known, accepted gaps (research doc §4): no hover mouse-doc from inside
kitty; graphics-protocol faces (real swatches in the scrollback) are a
later nicety.

## Part IV — Design decisions

### Decision D1: implement the WM from scratch in Go (not bspwm config, not StumpWM)

- **Context:** Three viable paths (research doc §2): script bspwm, extend
  StumpWM/McCLIM, or write our own.
- **Options considered:** bspwm+bar (fastest WM, but frames/look and
  tile-as-presentation are out of reach), StumpWM+McCLIM (real CLIM, but a
  Lisp toolchain and a codebase the team doesn't live in), from-scratch Go.
- **Decision:** From-scratch Go on `jezek/xgb` + `BurntSushi/xgbutil`,
  wingo as the reference codebase.
- **Rationale:** The frames *are* the aesthetic and the tiles must be
  first-class presentations, which requires owning the frame drawing; the
  team's tooling (glazed, goja, go-go-golems infra) is Go; wingo proves
  feasibility; the broker was going to be Go regardless.
- **Consequences:** We own ICCCM/EWMH edge cases (focus, struts, xrandr);
  the Go X ecosystem is maintenance-mode, so occasional hand-written
  requests. Mitigated by keeping `wmx11` minimal and stealing liberally
  from wingo. Must validate against awkward clients (Java, GIMP) early.
- **Status:** accepted

### Decision D2: broker is display-agnostic; the WM is just a client

- **Context:** Accept must work across the WM, kitty, and arbitrary CLIs;
  something must own the session state.
- **Options considered:** accept logic inside the WM (fewer moving parts,
  but every participant then needs X and the WM becomes the protocol);
  a separate broker process/loop with the WM as a peer client.
- **Decision:** Separate broker component speaking only the Unix-socket
  protocol; runnable standalone (`go-go-wm broker`) or embedded
  (`--embedded-broker`), but even embedded, the WM talks to it through the
  socket, never through function calls.
- **Rationale:** Symmetry (kitten, CLI, WM are peers), testability (full
  accept matrix with zero display), and honesty (no privileged side channel
  means the protocol can't silently rot).
- **Consequences:** A serialization boundary inside one process (negligible
  cost at human-interaction rates); protocol versioning from day one
  (`hello.protocol`).
- **Status:** accepted

### Decision D3: one binary, glazed commands for every entry point

- **Context:** Project preference: implement in steps, all in the same
  binary, using the glazed framework.
- **Options considered:** separate `pbui-wm`/`pbui-broker`/`pbui` binaries
  (unix-y, but N things to install and version-skew between them); one
  binary with subcommands.
- **Decision:** Single `go-go-wm` binary; `wm`/`broker`/`present`/`answer`/
  `menu`/`scrape`/`kitty` as bare commands, all `query` commands as glazed
  commands; kitten and config snippets `go:embed`-ded and materialized by
  `kitty install`.
- **Rationale:** Zero skew (the kitten's caller and the broker are the same
  build), glazed gives structured output on the whole introspection surface
  for free, and the go-go-golems scaffold (help system, logging, logcopter,
  goreleaser) is already wired for one binary.
- **Consequences:** Binary links X libs even when used as a CLI helper —
  irrelevant since xgb is pure Go and connects lazily. Startup cost of the
  helper path must stay small (`present` must not touch X or the socket).
- **Status:** accepted

### Decision D4: NDJSON wire format behind a Codec seam

- **Context:** The broker protocol needs framing; deterministic CBOR (from
  the SGEP prototype) is attractive but heavier to debug from Python/shell.
- **Options considered:** NDJSON; deterministic CBOR; protobuf.
- **Decision:** NDJSON frames now; `Codec` interface so CBOR can be swapped
  in; protocol number in `hello`.
- **Rationale:** The kitten is Python and test fixtures are hand-written;
  `socat`-ability is worth more than compactness at these message rates.
  Fuzzing targets the decoder either way.
- **Consequences:** Values must stay JSON-representable (they already are —
  the prototype's values are strings/numbers/ids). Revisit only if the
  event bus gets high-volume.
- **Status:** accepted

### Decision D5: all mutations are serializable Ops on an event bus (the goja hook)

- **Context:** go-go-goja JS scripting comes later; the goal is "a JS
  grabbag of WM lego blocks". Retrofitting scriptability onto method calls
  scattered through an event loop is painful.
- **Options considered:** direct method calls now + bindings later;
  ops-as-data now.
- **Decision:** From day one: (a) every layout mutation is a serializable
  `Op` handled by `wmcore.Apply`; (b) every state change emits a sequenced
  event on the broker bus; (c) verbs are registry entries (data + owner),
  not switch arms; (d) both the WM loop and the broker loop accept posted
  closures/messages rather than being called from other goroutines.
- **Rationale:** A goja runtime then needs only three bindings —
  `apply(op)`, `on(eventType, fn)`, `registerVerb(desc, fn)` — plus a
  `client.Accept` wrapper, and it slots into the existing posted-function
  channels (goja is single-threaded; the loops already serialize).
  Meanwhile the same Op/event vocabulary is what the query CLI and the
  record/replay tests use, so the pattern pays for itself before JS exists.
- **Consequences:** Slight ceremony (an Op struct per mutation); the Op set
  becomes a compatibility surface, so name ops carefully (they mirror the
  prototype's trace event names, `sources/pbui-shell.jsx:451-457`).
- **Status:** accepted

### Decision D6: flat string ptypes, open namespace

- **Context:** CLIM had a full presentation-type lattice with parametrized
  types and inheritance.
- **Decision:** Ptypes are plain strings; matching is exact-or-"any"
  (port of `sources/pbui-shell.jsx:44-45`); anyone may mint new ptypes.
- **Rationale:** The prototype demonstrates the whole UX without subtyping;
  a lattice is speculative machinery; an open namespace is what lets a
  random shell script invent `<deploy-target>` today.
- **Consequences:** No "integer is-a number" niceties; verbs listing
  multiple ptypes cover most real cases. Revisit with evidence (see Open
  Questions).
- **Status:** accepted

## Part V — Testing strategy

The layering exists to make this cheap. Full rationale in the research doc
§6; the operational summary:

1. **`wmcore`** — table-driven unit tests plus property tests over random Op
   sequences: leaf count conserved by swap; `MoveSplit` = detach+attach
   (count preserved); ratios always in `[0.1, 0.9]`; no dangling IDs;
   `CloseLeaf` yields a valid binary tree; `Layout` rects tile exactly.
   **Cross-implementation oracle**: a small runner executes random Op
   scripts against `sources/pbui-shell.jsx`'s tree functions (node) and
   `wmcore` (Go) and diffs serialized trees — the prototype is the reference
   implementation of its own port.
2. **`broker`** — table-driven session tests with in-process fake clients:
   accept/answer, cancel, requester disconnect, answer after clear, two
   `accept.start` racing; all under `-race`; `go test -fuzz` on the frame
   decoder (it eats untrusted bytes).
3. **`draw`** — golden PNGs per widget/state (embedded font ⇒
   deterministic); `go test -update` regenerates.
4. **`wmx11`** — Xephyr interactively (`Xephyr :1 -screen 1280x800`), Xvfb in
   CI; assertions via `go-go-wm query tree/windows` (ask the WM what it
   believes) plus EWMH property checks via xprop/xgbutil; synthetic input
   via xdotool/XTEST; ffmpeg-record the Xvfb screen on mysterious failures.
5. **kitty** — isolated instance (`KITTY_CONFIG_DIRECTORY`, `--listen-on`,
   `allow_remote_control`, `LIBGL_ALWAYS_SOFTWARE=1` headless); drive with
   `kitten @ send-text` / `get-text` / `send-key`; the kitten's scanner is
   pure Python unit-tested on fake screens; OSC 8 emission tested under a
   `creack/pty` pty with no terminal emulator at all.
6. **Exactly one E2E smoke test**: Xvfb + WM + broker on a tmpdir socket +
   isolated kitty; a script prints a `<color>` link; an accept is started;
   the kitten is driven by injected keys; assert the broker delivered the
   right object. Singular, paranoidly maintained.

Hermeticity rule for every test: own your `DISPLAY`, `XDG_RUNTIME_DIR`,
`PBUI_SOCKET`, `FONTCONFIG_FILE`, `KITTY_CONFIG_DIRECTORY`, and `HOME`.

## Part VI — Phased implementation plan

Each phase ends runnable and demoable. File paths are the targets from §II.2.

**Phase 0 — scaffolding (½ day).** Wire `cmd/go-go-wm/main.go` like glaze
(root command, logging section, help system, `pkg/doc`); add deps (`xgb`,
`xgbutil`, glazed); `go-go-wm --help` works. *Demo: help output.*

**Phase 1 — `pkg/wmcore` (1–2 days).** Tree, Ops, `Apply`, `Layout`, `Snap`,
`ZoneAt`; property tests; the JS-oracle runner against the prototype.
*Demo: `go test ./pkg/wmcore/...` green; a tiny `layout-dump` debug command
printing rects for a scripted tree.*

**Phase 2 — `pkg/pbui` + broker (2–3 days).** Types, URI codec, NDJSON
codec, broker state machine, client library; `go-go-wm broker`,
`go-go-wm accept`, `go-go-wm answer`, `go-go-wm query events --follow`.
*Demo: two terminals accept/answer through the broker, events streaming as
glazed rows. No X anywhere yet.*

**Phase 3 — `pkg/draw` (1–2 days).** Palette, embedded Plex Mono, widgets,
golden tests. *Demo: `go-go-wm draw-preview --widget menu` writes a PNG
(debug command, kept — it's the golden-file generator).*

**Phase 4 — `pkg/wmx11` minimal WM (1–2 weeks, the long pole).**
Redirect+reparent, frames with title strips, `Layout` application, focus,
`WM_DELETE_WINDOW`, keybindings (split/close/focus), divider drag with
snapping, ⠿ drag with zones and stippled preview, workspaces + EWMH,
bars. Develop entirely in Xephyr. Add `query tree/windows` backed by the
WM's posted-function channel. *Demo: daily-drivable-in-Xephyr WM with three
xterms; `go-go-wm query tree --output json`.*

**Phase 5 — WM ⇄ broker (2–3 days).** WM connects as privileged client:
ACCEPTING banner from `accept.mode`, Escape → cancel, `menu.show` rendering
(override-redirect window, `draw.Menu`), tile/workspace/window ptypes and
verbs registered with the broker, mouse-doc line fed by `doc.hover` and WM
hovers. *Demo: right-click a tile title → paper-and-ink menu → "Swap app
with…" → click another tile — the prototype's flow, on X.*

**Phase 6 — terminal participation (3–4 days).** `present`, `scrape`,
`kitty install`, the kitten, open_actions routing. *Demo: the E2E scenario —
`git log | go-go-wm scrape` in kitty, accept a `<git-commit>` from the WM,
pick it with hint keys.*

**Phase 7 — built-in apps + polish (ongoing).** Port listener / notes /
inspector / trace as PBUI clients (each is now just a broker client with a
window — they double as protocol exercisers); the E2E smoke test; awkward-
client smoke tests (Java, GIMP); xrandr (one tree per output).

**Phase 8 (later, separate ticket) — go-go-goja.** Expose `apply(op)` /
`on(event, fn)` / `registerVerb` / `accept` through go-go-goja's module
system (`go-go-goja/pkg/engine`, `modules.NativeModule`); a `go-go-wm repl`
command. The design work was already done in D5 — this phase should be
bindings, not surgery.

## Part VII — Risks, alternatives, open questions

**Risks.**

- *ICCCM/EWMH edge cases* (focus models, transients, struts, fullscreen)
  are the classic WM time sink — mitigated by wingo as a crib, awkward-client
  smoke tests from Phase 4 on, and by not promising daily-driver status.
- *Go X ecosystem staleness*: xgb/xgbutil are maintenance-mode; budget for
  reading protocol docs. Wingo's continued function is the canary.
- *Kitty API drift*: kittens use kitty-internal Python APIs; pin a known
  kitty version in CI and keep the kitten's core pure.
- *Scope creep toward CLIM*: the type lattice, presentation histories, and
  command processors are rabbit holes; D6 and the phase plan are the fence.

**Alternatives already adjudicated** are in Part IV; the standing fallback
if `wmx11` stalls is running `wmcore`+broker against bspwm via `bspc`
(the tree models are compatible) at the cost of the frames-as-presentations
story.

**Open questions.**

1. Ptype refinement: do we eventually need parameterized types
   (`number in [0,255]`) for accept filtering, or do verb-side checks
   suffice? Decide only when a real verb needs it.
2. Multi-accept ("accept N objects then finish") — the prototype does
   sequential single accepts (Sum, `sources/pbui-shell.jsx:389-397`);
   sequential is probably fine forever, but the session model doesn't
   preclude a `count` field.
3. Presentation persistence: should collected notes survive WM restarts
   (world state on disk)? Leaning yes via a tiny state file, Phase 7.
4. Security posture of the socket: `$XDG_RUNTIME_DIR` perms are the
   boundary for now; revisit if the protocol ever crosses users/machines.
5. Focus-follows-mouse vs click-to-focus for accept ergonomics (clicking to
   answer must not steal focus destructively) — resolve by feel in Phase 5.

## References

- `sources/pbui-shell.jsx` — the reference prototype (all line numbers above).
- `reference/01-preliminary-research-x11-presentations-kitty-testing.md` —
  full research and rationale, including the Wayland caveat and the testing
  deep-dive.
- `cmd/go-go-wm/main.go`, `pkg/` — current (empty) scaffold to be filled per
  §II.2.
- glazed command conventions: `glazed/pkg/cmds`, `glazed/pkg/cmds/fields`,
  `glazed/pkg/cmds/values`, `glazed/pkg/settings`, `glazed/pkg/cli`,
  `glazed/pkg/help` (see the glazed-command-authoring skill for the
  canonical skeleton used in §II.3).
- go-go-goja module system (Phase 8): `go-go-goja/pkg/engine`,
  `go-go-goja/pkg/xgoja`.
- External: wingo, jezek/xgb, BurntSushi/xgbutil, bspwm, EWMH/ICCCM specs,
  kitty hints kitten / open_actions / remote control, OSC 8 spec, CLIM 2
  spec — URLs collected in the research doc §7.
