---
Title: Implementation diary
Ticket: GGWM-008-LAUNCHER
Status: active
Topics:
    - wm
    - ui
    - pbui
DocType: reference
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://pkg/apps/builtin.go
      Note: renderLauncher — the four-button placeholder being replaced
    - Path: repo://pkg/wmx11/pbui.go
      Note: showMenu — the popup-window species the launcher overlay extends
ExternalSources:
    - "freedesktop.org Desktop Entry Specification"
Summary: Step-formatted implementation diary for the launcher subsystem (registry, popup, keyboard substrate, PBUI integration) — decisions, verbatim failures, verification per phase.
WhatFor: Continuation context for anyone extending the launcher or the frame-keyboard substrate GGWM-009 inherits.
WhenToUse: Read alongside design-doc/01; deviations from the design are recorded here first.
---

# Diary

## Goal

Capture the implementation journey of GGWM-008 (launcher): the command
registry, the Mod4+d popup, the keyboard-input-for-WM-surfaces
substrate (which GGWM-009's REPL tile inherits), and the `command`
ptype integration — with every failure recorded verbatim.

## Step 1: Kickoff — design re-read and machinery survey

GGWM-007 wrapped in the previous turn; the user's "do it" starts
GGWM-008, the next ticket in the recorded dependency chain (008 L3's
keyboard substrate gates GGWM-009 R3). Before writing code: re-read
design-doc/01 and survey the five pieces of existing machinery the
design leans on, so the new code lands on the real seams.

The survey confirmed the design's map with one nuance worth writing
down: the empty-tile surface (`renderLauncher`) is not an app that
owns state — it re-renders from scratch on every paint via
`RenderBuiltin(name, w, h, world, accepting)`. A launcher tile with a
live query string therefore needs *WM-side state* (the query lives in
the WM process, like `World` does), not a new renderer contract.

### Prompt Context

**User prompt (verbatim):** "do it"

**Assistant interpretation:** Implement GGWM-008-LAUNCHER (the next
ticket), same working discipline as GGWM-007: diary, phased commits,
live verification.

**Inferred user intent:** Keep the ticket queue moving autonomously;
the launcher was explicitly designed for implementation in this
follow-up session.

**User prompt (verbatim, mid-step):** "jeep a detailed diary and backfill (use skill to read about the format)"

**Assistant interpretation:** Keep ("jeep") a detailed diary in the
`diary` skill's strict step format, and backfill what has already
happened — including converting the GGWM-007 diary written earlier
this session to that format.

**Inferred user intent:** Diaries across tickets should be uniform and
review-grade; the skill format (Prompt Context, verbatim failures,
review instructions) is the standard.

### What I did
- Re-read GGWM-008 design-doc/01 in full (registry shape, two
  surfaces, keyboard substrate, decision records L-D1..L-D4, phases
  L1-L4).
- Surveyed the build-on machinery: `pkg/apps/builtin.go`
  (`renderLauncher`, `RenderBuiltin` dispatch, `World`, Region/Btn/
  Hint helpers), `pkg/wmx11/builtin.go` (openBuiltin frame creation,
  `paintBuiltin` → regions in frame coords, `builtinClick` →
  `apps.Resolve` contract, `builtinAction` namespace), `pkg/wmx11/
  pbui.go` (`showMenu`: override-redirect window + `blit` + click
  routing — the popup species), GGWM-007's `raiseChrome` (the popup
  must be raised with menus, above floats).
- Backfilled both diaries into the strict skill format (GGWM-007's
  four steps rewritten; this file started).

### Why
- The design names exact seams (`renderLauncher`,
  `pkg/wmx11/pbui.go`, `wm.exec`, uispec); verifying they still look
  as designed is cheap insurance — GGWM-007 changed stacking and
  focus, both of which the popup and tile touch.

### What worked
- All the design's assumed seams exist and are current; GGWM-007's
  `raiseChrome` already has the menu-above-floats slot the popup needs
  to join.

### What didn't work
- N/A (survey step).

### What I learned
- Builtin tiles are stateless renderers over WM-process state
  (`World`); the launcher tile's query state must live WM-side, not in
  `pkg/apps`. This shapes L3: the WM owns a `launcherState` per… or
  rather one global (focused-tile) query — decided in the L3 step.
- `builtinClick` resolves clicks through `apps.Resolve(accepting,
  region, button)`, which already implements the accept-answer
  contract — launcher rows that carry `command` objects get accept
  behavior for free (L4's accept mode partially falls out).

### What was tricky to build
- N/A yet.

### What warrants a second pair of eyes
- N/A yet.

### What should be done in the future
- N/A

### Code review instructions
- Nothing to review yet; start with design-doc/01 Parts II-III.

### Technical details
- Planned order (same as design): L1 pure registry (pkg/launcher) →
  L2 popup + Mod4+d → L3 frame KeyPress substrate + tile v2 → L4
  command ptype/verbs/accept + wm.command/wm.launch + i3.js. Commit
  per phase; float-smoke/rc-smoke/examples-smoke as regression gates.
