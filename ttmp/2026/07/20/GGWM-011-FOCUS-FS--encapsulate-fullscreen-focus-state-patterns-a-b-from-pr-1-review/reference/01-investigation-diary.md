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
