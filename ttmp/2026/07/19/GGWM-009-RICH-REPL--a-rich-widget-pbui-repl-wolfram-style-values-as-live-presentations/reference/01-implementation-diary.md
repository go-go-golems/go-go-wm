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
