---
Title: The rich REPL — an intern's guide to Wolfram-style values as PBUI presentations
Ticket: GGWM-009-RICH-REPL
Status: active
Topics:
    - scripting
    - pbui
    - ui
    - goja
DocType: design-doc
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://pkg/cmds/repl.go
      Note: the terminal REPL (A3) whose kernel this surface reuses
    - Path: repo://pkg/apps/uispec/uispec.go
      Note: the segment IR cells render through — and that this ticket extends
    - Path: repo://pkg/jsmod/uimod/app.go
      Note: the snapshot-handoff discipline the REPL surface inherits
    - Path: repo://pkg/pbui/object.go
      Note: the typed-object substrate rich values compile to
ExternalSources:
    - "wolframjs-repl prototype: /home/manuel/code/wesen/2026-05-16--js-repl-wolfram/wolframjs-repl (RichValue protocol, view registry, cell UI)"
Summary: Design for a notebook-style REPL surface inside the PBUI desktop — the RichValue protocol from the wolframjs-repl prototype re-founded on PBUI concepts (views become per-ptype renderers over uispec, operations become broker verbs, every Out[n] is a live presentation that answers accepts); covers the value protocol and its JS opt-in symbol, the view registry and new segment kinds, the cell surface and its keyboard/scroll needs, session and Out-history semantics, and a phased plan with the launcher ticket's keyboard substrate as an explicit dependency.
LastUpdated: 2026-07-19T15:30:00-04:00
WhatFor: The governing design for GGWM-009; read before building the value protocol, the view registry, or the cell surface.
WhenToUse: After the GGWM-002 guide (runtime model) and the GGWM-008 design (keyboard substrate); alongside the wolframjs-repl prototype's own design doc for the original browser-side architecture.
---

# The rich REPL

Two existing artifacts meet in this design. On one side, the desktop
already has a REPL: `go-go-wm repl` (attachment point A3) wraps a
go-go-goja session over the live WM, but its output is `fmt.Println` —
a color is eight characters of hex, a tree is a JSON blob. On the
other side, the wolframjs-repl prototype (see ExternalSources) worked
out, in a browser, what it takes for every evaluation result to be a
**rich value**: a typed object carrying a summary, multiple named
views, and applicable operations, rendered by a registry of view
components and manipulated through a uniform protocol.

The thesis of this ticket is that the prototype's architecture is not
browser-specific — it is PBUI's architecture, discovered independently.
The mapping is nearly mechanical, and making it explicit is the design:

| wolframjs-repl | PBUI equivalent | notes |
|---|---|---|
| `RichValue.type` | `pbui.Object.Ptype` | one slug vocabulary |
| `summary()` | `Object.Label` | the collapsed face |
| `views()` → View[] | per-ptype view registry rendering uispec rows | Part III |
| `Operation` / `transform()` | broker **verbs** owned by the REPL | right-click already works desktop-wide |
| `explain()` | `Object.Doc` + a `doc` view | mouse-doc line for free |
| `richDisplaySymbol` opt-in | `__pbui__()` / `Symbol.for("pbui.richDisplay")` | Part II |
| worker boundary serialization | JS-loop → render-snapshot boundary | the uimod discipline, unchanged |
| React renderer registry | Go view registry keyed by view type | lazy loading becomes "defer heavy views" |
| In[n]/Out[n] cells | the cell surface + `Out(n)`/`$_` bindings | Part IV |

What the desktop adds that the browser prototype could not have:
**Out[n] is not a picture of a value — it is a live presentation.** A
color result answers any process's `accept("color")`; a commit-hash
result carries the git verbs some daemon registered; another script can
`await pbui.accept("dataset")` and the user clicks Out[3]. The REPL
stops being a window you look into and becomes a producer of
first-class desktop objects.

## Part I — What exists to build on

- **The kernel.** `go-go-goja`'s replapi (used by `pkg/cmds/repl.go`)
  provides sessions, IIFE cell rewriting (top-level `let` persists
  across cells), per-cell console capture, and error strings. The rich
  REPL changes none of it; it replaces the *presentation* of results.
- **The surface discipline.** `uimod` (GGWM-003) established the
  pattern every interactive surface uses: VM-owned callables touched
  only on the JS loop; a mutex-guarded normalized snapshot; render
  hosts (WM tile or standalone window) that never call JavaScript;
  handlers that re-render and post one repaint. The REPL surface is
  the largest instance of this pattern to date, not a new pattern.
- **The IR.** `uispec` renders rows of text/hint/object/button
  segments with regions for the click contract. Cells are rows; the
  gaps (tables, swatch strips, sparklines, a text-input row) are new
  segment kinds, designed in Part III.
- **The keyboard substrate.** Typing into a WM-painted tile is
  designed in GGWM-008 (frame KeyPress routing to the focused
  surface). This ticket depends on it and must not re-invent it; the
  standalone-window variant (xapp already delivers keys) works
  without it, which conveniently orders the phases.

## Part II — The value protocol

### Go side

```go
// pkg/repl/value.go
type Value struct {
    Ptype   string   // the pbui vocabulary: "color", "number", "dataset", …
    Summary string   // one-line face: "Dataset (120 rows × 4 cols)"
    Doc     string   // hover line
    Views   []View   // ordered; first is the default
    Input   string   // re-evaluable input form ("copy as input")
    Raw     json.RawMessage // the Object.Value payload
}

type View struct {
    Name string          // "table", "swatches", "json", "sparkline", "text"
    Spec uispec.Spec     // pre-rendered rows (small views)
    Lazy func() uispec.Spec // large views render on first expand
}
```

### Derivation (the default path)

Most results are plain JS values; a total function maps them:

```
derive(v):
    string matching #rrggbb            → color   (swatch view, text view)
    number                             → number  (text view; stats view for arrays)
    array of numbers                   → series  (sparkline, table, json)
    array of flat objects (same keys)  → dataset (table, schema, json)
    object/array (other)               → json    (tree view, json view)
    wm.tree()/windows() shapes         → wm.tree / wm.windows (outline view)
    string                             → string  (text; scraped for pbui:// links)
    null/undefined/function            → text summary only
```

Detection is heuristic and bounded (inspect ≤ first 100 elements);
misdetection costs a suboptimal default view, never an error, and the
json view is always present as ground truth.

### Opt-in (the rich path)

Any JS object can override derivation, mirroring the prototype's
well-known symbol:

```js
class Matrix {
  __pbui__() {
    return {
      ptype: "matrix",
      summary: `${this.rows}×${this.cols} matrix`,
      views: [
        { name: "grid",  rows: this.toGridRows() },   // ui.row(...) data
        { name: "latex", text: this.toLatex() },
      ],
      input: `Matrix.from(${JSON.stringify(this.data)})`,
    };
  }
}
```

`__pbui__()` runs on the JS loop during result capture (never during
render), its output is normalized exactly like `ui.app` render output
— definition-time errors with coordinates — and a throwing `__pbui__`
falls back to derivation with the error shown as a hint row. The same
protocol is how future domain engines (a dataframe library, a units
library — the prototype's `dataset`/`quantity` packages have Go or JS
analogues) plug in without touching the REPL.

## Part III — Views and the uispec extensions

The view registry is Go-side, keyed by view name, each producing
uispec rows from view data. The prototype's fourteen React renderers
reduce to six that matter on this desktop, plus the ones uispec cannot
express yet — which defines the uispec extension list precisely:

| view | needs from uispec | status |
|---|---|---|
| text, json (pretty) | text/hint segments | exists |
| swatches (colors) | object segments | exists |
| table | a `table` segment: columns, right-aligned numerics, row cap with "… N more" | **new seg kind** |
| tree/outline (json, wm.tree) | indent + expand/collapse toggles | rows + buttons (exists), state in the surface |
| sparkline / bars | an `image` segment carrying a pre-rendered `image.RGBA` strip | **new seg kind** |
| input row (the editor line) | a `field` segment: editable text + cursor | **new seg kind**, shared with the launcher query line |

The `image` segment is the deliberately narrow answer to "what about
plots": Go-side mini-plotters (sparkline, bar strip — `pkg/draw`
primitives, ~60 lines each, golden-tested) render into small RGBA
strips that travel through the normal snapshot path. Vega-class
plotting is explicitly out of scope; if it ever matters, it arrives as
an external process presenting `plot` objects, not as a uispec
capability.

Every view's object segments carry real ptypes, so the click contract
comes for free — including the crucial one: **a color swatch inside a
table inside Out[7] answers a desktop accept**, because it is just an
object segment like any other.

## Part IV — The cell surface

### Model

```go
type Cell struct {
    N       int          // In/Out index
    Input   string       // source text
    Status  cellStatus   // editing | evaluating | done | error
    Console []string     // captured prints
    Value   *repl.Value  // nil until done
    View    int          // selected view index (per cell)
    Folded  bool
}
type Session struct {
    Cells   []Cell
    Scroll  int          // first visible row (surface-level scrolling)
    kernel  *replapi.Session
}
```

Rendering maps cells to rows: an `In[n]` field row (or static text for
completed cells), console lines as hints, then the selected view's
rows with a view-switcher row (buttons named after the views — the
prototype's tab bar, as uispec buttons). Evaluation follows the
promise discipline from GGWM-002: the cell enters `evaluating`, the
kernel call runs via the owner, and settlement posts the re-render —
the surface never blocks.

Scrolling is owned by the session (`Scroll`), not by uispec: the
renderer takes a row window. This resolves the GGWM-003 "no scrolling"
deferral in the narrowest way that works, and script tiles get the
same mechanism later if they want it (wheel events already reach
frames; PageUp/Down arrive via the keyboard substrate).

### Out-history and liveness

`Out(n)` and `$_` are bindings in the session runtime returning the
*raw JS value* (not the Value record) — history is for computing.
The REPL registers itself as the verb owner for its results: every
Out object's menu carries `repl.use` ("insert `Out(n)` into the
current input"), `repl.copy-input` (the `Input` form), and
`repl.re-eval`; type-specific verbs (color, git-commit, …) appear
automatically because the menu is assembled by the broker from the
ptype — nothing REPL-specific there. Values printed with real ptypes
answer accepts for as long as the session lives; closing the session
withdraws the verbs (broker disconnect semantics, same as any daemon).

### Where it runs

One implementation, three hosts, in order of arrival:

1. `go-go-wm repl --ui` — a standalone xapp window (keys already
   work; no WM required beyond any X server). This is R2's vehicle.
2. `builtin:repl` — a WM tile via the rc runtime (needs the GGWM-008
   keyboard substrate).
3. The A3 terminal REPL remains untouched as the plain fallback.

## Part V — Decision records

**R-D1 — Results are real ptypes, not a wrapper type.** A color result
is ptype `color`, not `repl.value` wrapping color — so it answers
accepts and inherits desktop verbs with zero adapter code. The REPL's
own operations ride separate verbs it registers. Consequence: REPL
sessions participate in the desktop identically to any daemon script.
*proposed.*

**R-D2 — Views render Go-side over uispec; JS supplies data.** The
prototype ran renderers in React; here render hosts must stay VM-free
(the uimod law), so `__pbui__` returns data and Go renders it.
Consequence: heavy custom visuals need either the image segment or a
standalone presenting process; that boundary is explicit, not
accidental. *proposed.*

**R-D3 — The kernel is replapi, unmodified.** Cell persistence
semantics (IIFE rewriting), error capture, and session lifecycle are
proven since GGWM-002; the rich REPL is a *presentation* of that
kernel. Anything needing kernel changes (completion, interrupt) is
listed as an open question, not smuggled in. *proposed.*

**R-D4 — Three new uispec segment kinds (`table`, `image`, `field`),
no general widget system.** Each has a definition-time validator and a
renderer; `field` is shared with the launcher. The prototype's
`InteractiveView` (sliders re-evaluating expressions) becomes buttons +
re-eval verbs in v1 — a slider segment is future work with a real use
case attached. *proposed.*

## Part VI — Phases

- **R1 — protocol + derivation + views (pure).** `pkg/repl` Value/
  derive/`__pbui__` normalization; uispec `table`/`image` segments +
  sparkline/bar plotters; table-driven tests, golden images. No X, no
  kernel.
- **R2 — the standalone surface.** `repl --ui`: cell model, field
  segment + editing (xapp keys), evaluation wiring, view switcher,
  scrolling. E2E under Xvfb: type `[1,2,3]`, Enter, assert a table
  view exists via screenshot regions; type a color literal, run a CLI
  `accept --ptype color`, click the swatch, assert the answer.
- **R3 — desktop integration.** Verb registration (`repl.use`,
  `repl.copy-input`), Out/`$_` bindings, accept participation tests,
  `builtin:repl` tile once GGWM-008 L3 lands.
- **R4 — enrichment.** wm.tree outline view, dataset schema view,
  string link-scraping (`pkg/pbui/scrape`), session save/replay
  (cells are already data).

## Risks and open questions

- **Completion and interrupt** need kernel support (replapi exposes
  neither today). Both are quality-of-life, neither blocks R1–R3;
  they are the first candidates for an upstream go-go-goja request.
- **Big values.** `derive` must cap inspection and views must cap rows
  (with "… N more" affordances); an accidental `wm.tree()` on a large
  desktop should cost a bounded render, and the cap policy needs a
  test.
- **Session-scoped liveness** may surprise: Out[3] answers accepts
  only while the session runs. The alternative (persisting values to
  the broker) is a real design with real costs; deferred until the
  surprise is observed in practice.
- **Editor depth.** The field segment starts as single-line with
  history (Up/Down through inputs); multi-line editing (Shift-Enter)
  is bounded scope creep to watch — the prototype used CodeMirror,
  and nothing here should try to become CodeMirror.
- The launcher's accept-mode popup and the REPL's picker use cases
  overlap (noted in GGWM-008); if both mature, the accept prompt
  should become one shared surface.
