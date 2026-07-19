---
Title: Implementation diary
Ticket: GGWM-009-RICH-REPL
Status: active
Topics:
    - scripting
    - pbui
    - ui
    - goja
DocType: reference
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://pkg/cmds/repl.go
      Note: the A3 terminal REPL whose kernel (replapi) the rich surface reuses
    - Path: repo://pkg/apps/xapp/xapp.go
      Note: the standalone host the R2 surface runs in (Keyer/Starter extensions)
ExternalSources:
    - "wolframjs-repl prototype: /home/manuel/code/wesen/2026-05-16--js-repl-wolfram/wolframjs-repl"
Summary: Step-formatted implementation diary for the rich REPL — kernel capture strategy, uispec extensions, the cell surface, and desktop integration, with verbatim failures.
WhatFor: Continuation context; records where the implementation deviates from design-doc/01 (esp. the raw-value capture path).
WhenToUse: Read alongside design-doc/01 and the GGWM-008 diary (the keyboard substrate this inherits).
---

# Diary

## Goal

Capture the implementation of GGWM-009: the RichValue protocol
re-founded on PBUI (pkg/repl), the three uispec segment kinds, the
cell surface in its standalone (`repl --ui`) and WM-tile hosts, and
the desktop integration that makes Out[n] a live presentation.

## Step 1: Kickoff — the kernel-capture problem and its solution

The design's hardest open detail turned out to be first in line: R-D3
says "the kernel is replapi, unmodified", but `app.Evaluate` returns
results as *strings* (`ExecutionReport.Result`/`ResultJSON`), and rich
derivation plus `__pbui__()` opt-in plus `Out(n)` liveness all need the
raw JS value. Reading the pinned kernel
(go-go-goja v0.10.6-0.20260718235621) settled it without kernel
changes:

- `ExecutionReport.ResultJSON` (an `{"result": …}` envelope) exists —
  but JSON loses methods, so it cannot carry `__pbui__`.
- `app.WithRuntime(ctx, sessionID, fn)` is public — an owner-mediated
  door into the session's runtime.
- The kernel's own last-value helpers (`__ggg_repl_last_<n>__`) are
  cell-local; nothing persists the raw value.

**Decision (capture strategy).** The surface submits each input as an
expression-wrapped cell
`globalThis.__pbui_hist[N] = globalThis.__pbui_hist.last = (<source>);`
via ordinary `Evaluate`; if that fails to parse (statements: `let`,
loops), it falls back to evaluating the source verbatim with no
capture — the node-REPL wrap-then-fallback idiom. A session prelude
(also a plain Evaluate) installs `__pbui_hist`, `Out(n)`, and `$_`.
After a successful wrapped eval, one `WithRuntime` owner call exports
the captured value for derivation and invokes `__pbui__()` if present
— on the JS loop, per the uimod discipline. replapi stays byte-for-byte
unmodified; R-D3 holds.

### Prompt Context

**User prompt (verbatim):** "next ticket"

**Assistant interpretation:** Implement GGWM-009-RICH-REPL (the last
designed ticket, now unblocked by GGWM-008's keyboard substrate), with
the established diary/commit discipline.

**Inferred user intent:** Finish the designed backlog; the REPL is the
capstone that exercises the whole PBUI substrate.

### What I did
- Re-read design-doc/01; surveyed `pkg/cmds/repl.go` (kernel wiring),
  `uispec.go` (segment IR + Normalize/Render), `xapp.go` (the
  standalone host: `Keyer` delivers keys, `Starter` hands the Ctx,
  `Render` is pure — the R2 surface is just an `xapp.App`).
- Dug through the pinned replapi/replsession source to find the raw-
  value seam; recorded the capture strategy above.

### Why
- The wrap-then-fallback approach keeps every kernel behavior the
  terminal REPL has (IIFE binding persistence, console capture,
  top-level await, error text) while adding capture only for the
  expression case — which is exactly the case where a rich value
  exists.

### What worked / didn't work
- N/A (survey step).

### What I learned
- goja exports reach Go as `float64/string/bool/nil/[]interface{}/
  map[string]interface{}` — derivation can be a total function over
  that closed set.

### What was tricky to build
- N/A yet (the capture decision is the trick).

### What warrants a second pair of eyes
- The double-Evaluate in the fallback path: safe because a parse error
  executes nothing, but that invariant should be stated in code.

### What should be done in the future
- N/A

### Code review instructions
- Start with this step's capture decision, then pkg/repl once built.

### Technical details
- Phases: R1 pkg/repl + uispec table/image/field + plotters (pure) →
  R2 `repl --ui` xapp surface + E2E → R3 verbs/Out-liveness/builtin
  tile → docs. R4 enrichment (outline/schema/scrape/save) deferred
  except what falls out cheaply; recorded per item.

## Step 2: R1 — the pure core (pkg/repl + uispec extensions + plotters)

The prototype's RichValue protocol became ~600 lines of pure Go:
`Value`/`View`, a total bounded `Derive` over the closed set of goja
exports, `NormalizeRich` for `__pbui__` payloads, three new uispec
segment kinds, and two golden-tested plotters. Everything green except
one genuinely embarrassing infinite loop.

### Prompt Context

**User prompt (verbatim):** (see Step 1, "next ticket")

**Commit (code):** 1c58768 — "GGWM-009: rich-value protocol, uispec table/image/field, mini-plotters"

### What I did
- `pkg/repl/value.go`: Value{Ptype, Summary, Doc, Views, Input, Raw},
  View{Name, Spec}; caps as named constants (inspect 100, table rows
  20, json lines 40, series points 200). `NormalizeRich` validates
  {ptype, summary, doc?, input?, views:[{name, rows}]} with uispec
  row/seg coordinates in errors; unknown keys rejected.
- `pkg/repl/derive.go`: color (#rrggbb) → swatch+text views; number/
  boolean/string; []number → series (sparkline/bars/table/json views);
  []same-key-objects → dataset (table/schema/json, columns sorted for
  determinism — goja map iteration is random); []colors → palette;
  everything else → json with capped pretty view.
- uispec: `table` (block segment; per-column width fit with trailing-
  column clamp, numeric right-alignment, #rrggbb cells drawn as 12px
  chips with real color-object regions, "… N more rows/columns"
  hints), `image` (blits a pre-rendered RGBA; Normalize rejects it
  from JS — R-D2), `field` (Field-surface row, block caret on Focus,
  `field:<action>` region). Table cells normalize numbers/bools to
  strings.
- `draw.Sparkline` / `draw.BarStrip` (~60 lines each): min/max
  normalize, flat-series center line, zero-crossing baseline for bars.
  Goldens: sparkline, sparkline-flat, bar-strip.
- Tests: derive table (scalars, color vs short-hex, series summary,
  dataset caps + sorted columns + ragged fallback, palette, json line
  cap), NormalizeRich error shapes, uispec table/field normalize +
  render assertions (color cells emit live regions under an active
  accept — the design's "swatch inside a table inside Out[7]" claim,
  pinned as a test).

### Why
- Deviations recorded: `View.Lazy` dropped (caps make every view cheap
  enough to render eagerly; lazy views return if profiling ever says
  so). wm.tree/wm.windows outline views deferred to R4 with the rest
  of enrichment.

### What worked
- pkg/repl and uispec suites green on first run.

### What didn't work
- The first golden run **hung until the 120s harness timeout**. Cause:
  my Bresenham `line()` recomputed `2*e` after the x-branch had
  already mutated `e`, so steep segments overshot the endpoint and the
  `x0==x1 && y0==y1` exit never fired. Fix: capture `e2 := 2*e` once
  per iteration (the textbook form exists for exactly this reason).
  Symptom to remember: a golden test that hangs is a plotter loop, not
  an I/O problem.

### What I learned
- Sorted dataset columns are not cosmetic: goja exports maps with
  random iteration order, and every downstream artifact (goldens,
  E2E asserts, user muscle memory) needs a total order.

### What was tricky to build
- Table layout under a width budget: per-column max-width fit, then a
  trailing-column clamp with an explicit "… N more columns" hint —
  silent truncation would read as complete data (the no-silent-caps
  rule).

### What warrants a second pair of eyes
- `renderTable`'s numeric-column detection treats an all-empty column
  as numeric (right-aligned); harmless but odd-looking.
- `Derive` treats int64 and float64 as the number path; goja normally
  exports float64, so the int64 arms are belt-and-braces.

### What should be done in the future
- R2: the cell surface as an xapp.App + kernel wiring + E2E.

### Code review instructions
- `pkg/repl/derive.go` top-to-bottom, then the uispec Render cases.
  Validate: `go test ./pkg/repl/ ./pkg/apps/uispec/ ./pkg/draw/`.

### Technical details
- The `field` segment intentionally matches the launcher's field look
  (Field surface + Sel block caret) but does not yet share code; the
  launcher panel predates the segment. Unification is recorded as
  cleanup, not required.
