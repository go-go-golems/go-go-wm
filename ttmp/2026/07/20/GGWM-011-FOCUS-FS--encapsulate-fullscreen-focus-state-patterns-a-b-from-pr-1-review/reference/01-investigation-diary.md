---
Title: Investigation diary
Ticket: GGWM-011-FOCUS-FS
Status: active
Topics:
    - wm
    - concurrency
DocType: reference
Intent: long-term
Owners: []
RelatedFiles:
    - Path: pkg/wmx11/wm.go
      Note: WM struct state (focused, focusedFloat, fullscreen, fsSavedRect) + ApplyBatch/afterOp/refocusCurrent
    - Path: pkg/wmx11/fullscreen.go
      Note: toggle/enter/exit/clearFullscreenFor — the fullscreen state machine
    - Path: pkg/wmx11/manage.go
      Note: focus() — the focus state machine (RC-5/7/13 fixes live here)
    - Path: pkg/wmx11/float.go
      Note: focusFloat/unmanageFloat/frameFocused — the float focus path
    - Path: pkg/wmx11/theme.go
      Note: setTheme repaint skips/repaints fullscreen
    - Path: pkg/wmx11/input.go
      Note: focusNext + tile-click focus dispatch
---

# Diary

## Goal

Analyze and design (not implement) the encapsulation of fullscreen + focus
state in `pkg/wmx11`, the two systemic patterns identified during PR #1's
Codex review. Produce an intern-ready analysis/design/implementation guide,
store it in this ticket, and upload to reMarkable.

## Step 1: Evidence-gathering and pattern mapping

The user asked to create a ticket for the refactoring identified in
GGWM-010 Step 5 (Patterns A & B), analyze it in depth, and write an intern
guide — but explicitly **not to implement it**. So this is a design-only
deliverable. I read every fullscreen and focus touchpoint in `pkg/wmx11` to
ground the analysis in real file:line references.

### Prompt Context

**User prompt (verbatim):** "Did ou create a ticket for the refactoring? If not, do so. then Create a detailed analysis / design / implementation guide that is for a new intern, explaining all the parts of the system needed to understand what it is, with prose paragraphs and bullet point sand pseudocode and diagrams and api references and file references. It should be very clear and technical. Store in the ticket and the nupload to remarkable."

**Follow-up (verbatim):** "do not implement it though, just analyze and brainstorm and desigin it out"

**Assistant interpretation:** Create a dedicated ticket for the fullscreen/focus encapsulation refactor, do a deep analysis, write an intern guide, upload to reMarkable — design only, no code changes.

**Inferred user intent:** Capture the systemic refactor as a first-class design artifact (separate from the PR-fix ticket GGWM-010) so an intern can pick it up later, with enough depth that they understand the whole focus/fullscreen subsystem before touching it.

### What I did
- Created ticket `GGWM-011-FOCUS-FS` with a design-doc and a diary.
- Read the full state surface: `WM` struct fields (`focused`, `focusedFloat`, `fullscreen`, `fsSavedRect`) at `wm.go:140-185`; the fullscreen state machine (`fullscreen.go`); the focus state machine (`manage.go:505-545`); the float focus path (`float.go:255-310`); the relayout skip (`manage.go:330`); the batch/afterOp/refocus path (`wm.go:358-440`); the theme repaint (`theme.go:68-78`); the input dispatch (`input.go`).
- Mapped every Codex comment (RC-5/6/7/12/13) to the specific invariant violation that caused it.
- Designed two encapsulation options (a `fullscreenState` helper and a unified `focusState`) with pseudocode, and a phased migration that keeps the tree green at each step.

### Why
Five of the 16 Codex comments on PR #1 were all symptoms of the same root cause: fullscreen and focus state are scattered across 7 files with no single owner, so every new code path re-derives the "fullscreen owns focus+geometry" invariant and gets one wrong. A design doc that an intern can read top-to-bottom is the right deliverable — the refactor itself is larger than a PR-fix batch and should be deliberate.

### What I learned
- The focus invariant is: exactly one of {tiled leaf, float, fullscreen} holds keyboard focus, tracked across THREE fields (`focused`, `focusedFloat`, `fullscreen`) with no single mutator. Every bug (RC-7, RC-13) was "the wrong field got cleared."
- The fullscreen invariant is: while `w.fullscreen != nil`, it owns geometry (relayout skips it) and focus (navigation pins to it). Every bug (RC-5, RC-6, RC-12) was a code path that didn't check or didn't respect this.
- `frameFocused` (`float.go:303`) is the only place that correctly expresses the "exactly one" predicate — it's the seed of the right design.

### What was tricky to build
- Designing a migration that doesn't break the WM mid-refactor: the focus/fullscreen code is on the hot path (every keypress, every window event), so the refactor must be behavior-preserving at each commit. The phased plan (extract read-only helpers first, then consolidate mutators) keeps each step verifiable by the existing tests + manual X11 runs.

### What warrants a second pair of eyes
- The `focusState` unification (Option B) is the more ambitious design — it changes the data model, not just the API. Whether to do Option A (fullscreen helper only) or Option B (full unification) is a judgment call worth confirming before an intern starts.
- The interaction between fullscreen and workspace switches (RC-6) is subtle: fullscreen is workspace-local (switching away exits it). The design must encode that, not just "fullscreen owns focus."

### What should be done in the future
- Implement the phased plan from the design doc (this ticket is design-only).
- Add the missing regression tests listed in the guide (fullscreen focus pin, float configure while fullscreen, focus restoration after fullscreen float closes).

### Code review instructions
- Read `design-doc/01-...md` top to bottom.
- Cross-check every file:line claim against the source.

### Technical details
- Ticket: `GGWM-011-FOCUS-FS` at `ttmp/2026/07/20/GGWM-011-FOCUS-FS--...`.
- Source under analysis: `pkg/wmx11/{wm,fullscreen,manage,float,theme,input,ipc}.go`.
- This is design-only: no source files were modified.

## Step 2: Implemented Phase 0 (regression tests) + Phase 1 (fullscreenState read helpers)

The user asked to implement Phase 0 and Phase 1. Phase 0 needed a test
harness; the design doc flagged that pkg/wmx11 has almost no tests and the
WM logic is hard to test without an X server. I resolved this by extracting
the pure, display-free decision logic behind the RC fixes into testable
helpers (matching the existing floatDecision pattern), then wiring the
production code to call them — so the tests exercise the live logic.

### Prompt Context

**User prompt (verbatim):** "phase 0 + 1"

**Assistant interpretation:** Implement Phase 0 (regression tests for RC-5/6/7/12/13) and Phase 1 (extract read-only fullscreenState helpers) of the GGWM-011 design.

**Inferred user intent:** Start the structural refactor: lock in current behavior with tests, then begin the encapsulation by routing all fullscreen reads through one type.

### What I did
- **Phase 0 (commit `f`-prefixed, then `2e16ddd` parent):** created `pkg/wmx11/focus_state.go` with three pure decision helpers — `computeFocusDecision` (RC-5/7/13), `shouldHonorFloatConfigure` (RC-12), `shouldExitFullscreenOnSwitch` (RC-6). Wired `focus()`, `handleConfigureRequest()`, and `ApplyBatch()` to consult them. Added `focus_state_test.go` with 6 tests covering all five scenarios + a no-fullscreen cross-check. Checked off task `x3yh`.
- **Phase 1 (commit `2e16ddd`):** introduced the `fullscreenState` type with read-only methods (`Active`, `Owns`, `OwnsGeometry`, `OwnsFocus`, `FocusTarget`). Added an `fs fullscreenState` field to `WM` (back-referenced to `w` in the constructor). Migrated every `w.fullscreen` read in `fullscreen.go`, `manage.go`, `theme.go`, and `focus_state.go` to route through `w.fs.*`. Added `newTestWM()` to wire the back-reference in tests. Checked off task `cet6`.

### Why
The decision-helper extraction (Phase 0) is the lowest-risk way to bridge the test-harness gap: it doesn't require Xvfb or a mock seam, it matches the existing `floatDecision` test style, and it makes the RC invariants unit-testable. Phase 1 then builds on that by making `fullscreenState` the single reader of `w.fullscreen`, so future code can't re-derive the fullscreen invariant wrong.

### What worked
- All 6 Phase 0 tests pass under `-race`; they would fail against the pre-fix code (the decisions encode the fixed behavior).
- The `fullscreenState` back-pointer pattern is clean for production (initialized once in the constructor); tests use `newTestWM()`.
- The grep audit confirms zero bare `w.fullscreen` reads remain outside `fullscreenState` itself.

### What didn't work
- First Phase 1 build: the Phase 0 tests constructed bare `&WM{}` without setting `w.fs.wm = w`, so `computeFocusDecision` → `w.fs.FocusTarget()` nil-deref'd. Fixed by adding `newTestWM()` and migrating the test constructors.
- First lint run after Phase 0: the `switch dec.kind` in `focus()` was non-exhaustive (missing `focusNone`/`focusTile`/`focusFloat`). Fixed by adding explicit cases.

### What I learned
- The `floatDecision` test pattern (bare `&WM{}` + pure function) is the established way to test wmx11 logic without a display — the decision-helper extraction fits it naturally.
- A back-pointer (`fs.wm = w`) is the cleanest way to give a value-type helper access to the WM without changing every call site to pass `w`.

### What was tricky to build
- Making the `focus()` switch exhaustive without changing behavior: `focusNone`/`focusFloat` are unreachable from `computeFocusDecision` but must be handled (as no-ops falling through to the tiled path) to satisfy `exhaustive`.
- The RC-13 test asserts a *negative* contract (the decision must not carry a leaf that would clobber the preserved tile) plus the positive (w.focused stays intact) — because the preservation is implemented by the caller not assigning, not by the decision returning a value.

### What warrants a second pair of eyes
- The `focusNone`/`focusFloat` no-op fall-through in `focus()`: confirm these are genuinely unreachable from `computeFocusDecision` (they are — no fullscreen → `focusTile`; fullscreen → a `focusFullscreen*` kind), so the fall-through is just exhaustiveness hygiene.
- The back-pointer: confirm no production path constructs a `WM` outside `NewWM` (only `NewWM` and `newTestWM` do, both set `fs.wm`).

### What should be done in the future
- Phase 2: move the fullscreen mutators (`enter/exit/clear/toggle`) into `fullscreenState` methods.
- Phase 3: simplify `focus()` now that `FocusTarget()` centralizes the float-vs-tile distinction.
- Add an Xvfb-based smoke test for the full keypress path (the decision tests cover the logic but not the X wiring).

### Code review instructions
- Review commits: Phase 0 (`focus_state.go` + `focus_state_test.go` + wiring in `manage.go`/`wm.go`) and Phase 1 (`2e16ddd`).
- Validate: `env GOTOOLCHAIN=go1.26.5 go test ./... -race`, `golangci-lint run ./...`, `gosec -exclude=G101,G304,G301,G306,G204 ./...`.
- Grep audit: `grep -rn 'w\.fullscreen\b' pkg/wmx11/*.go | grep -v '_test\|//\|fullscreenEventData\|w\.fullscreen =\|fs\.wm\.fullscreen'` → empty.

### Technical details
- Phase 0 commit: `focus_state.go` + `focus_state_test.go` + wiring (manage.go, wm.go).
- Phase 1 commit: `2e16ddd`.
- Verification: build OK, 15+ packages pass `-race`, lint 0 issues, gosec 0 issues (3 justified `#nosec`).
- Tasks checked: `x3yh` (Phase 0), `cet6` (Phase 1).
