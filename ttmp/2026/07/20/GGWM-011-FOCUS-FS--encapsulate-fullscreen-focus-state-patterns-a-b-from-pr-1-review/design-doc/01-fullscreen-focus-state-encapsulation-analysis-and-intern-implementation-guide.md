---
Title: 'Fullscreen + focus state encapsulation: analysis and intern implementation guide'
Ticket: GGWM-011-FOCUS-FS
Status: active
Topics:
    - wm
    - concurrency
DocType: design-doc
Intent: long-term
Owners: []
RelatedFiles:
    - Path: pkg/wmx11/float.go
      Note: focusFloat/unmanageFloat/frameFocused — the float focus path
    - Path: pkg/wmx11/fullscreen.go
      Note: |-
        toggle/enter/exit/clearFullscreenFor — the fullscreen state machine
        the fullscreen state machine (enter/exit/clear/toggle)
    - Path: pkg/wmx11/input.go
      Note: focusNext + tile-click focus dispatch
    - Path: pkg/wmx11/ipc.go
      Note: fullscreen IPC case
    - Path: pkg/wmx11/manage.go
      Note: |-
        focus() — the focus state machine (RC-5/7/13 fixes live here) + relayout skip at :330
        focus() state machine + relayout skip at :330 + handleConfigureRequest
    - Path: pkg/wmx11/theme.go
      Note: setTheme repaint skips/repaints fullscreen
    - Path: pkg/wmx11/wm.go
      Note: |-
        WM struct state (focused, focusedFloat, fullscreen, fsSavedRect) + ApplyBatch/afterOp/refocusCurrent
        WM struct state (focused, focusedFloat, fullscreen) + ApplyBatch/afterOp/refocusCurrent
ExternalSources:
    - https://github.com/go-go-golems/go-go-wm/pull/1
Summary: Intern-level analysis and design for encapsulating the scattered fullscreen state and coupled focus trio in pkg/wmx11 — the two systemic patterns behind five Codex review comments (RC-5/6/7/12/13). Design-only; no implementation.
LastUpdated: 2026-07-20T18:30:00-04:00
WhatFor: The document an intern reads to understand the focus/fullscreen subsystem and to plan the encapsulation refactor that prevents the recurring focus bugs.
WhenToUse: Read the system primer first, then the pattern analysis, then the design options and phased plan.
---




# Fullscreen + focus state encapsulation: analysis and intern implementation guide

## Executive summary

`go-go-wm`'s X11 layer (`pkg/wmx11`) tracks "what has keyboard focus" and
"what is fullscreen" across **three independent struct fields** on the `WM`
struct, mutated by **seven different files** with **no single owner** for
either invariant. During PR #1's review, **five of the sixteen Codex
comments** (RC-5, RC-6, RC-7, RC-12, RC-13) were all symptoms of the same
root cause: every new code path that touches focus or geometry re-derives
what "fullscreen owns" means, and gets one detail wrong.

This document is a **design-only** deliverable (no implementation). It:

1. **Explains the subsystem** so an intern can understand what focus and
   fullscreen *are* in this WM, with file:line references to the real code.
2. **Diagnoses the two systemic patterns** (Pattern A: scattered fullscreen
   state; Pattern B: the coupled focus trio) and maps each Codex comment to
   the specific invariant violation that caused it.
3. **Proposes two design options** — a minimal `fullscreenState` helper
   (Option A) and a unified `focusState` type (Option B) — with pseudocode,
   API sketches, and tradeoffs.
4. **Gives a phased, behavior-preserving migration plan** so the WM stays
   green at every commit, plus a testing strategy and a risk register.

The refactor is **not urgent** — all five bugs are already fixed
individually in GGWM-010. This is the "fix it once, structurally" follow-up
so the next feature touching focus doesn't reintroduce the same class of
bug.

---

## Part 1 — System primer: focus and fullscreen in go-go-wm

> Goal: an intern who has never touched `pkg/wmx11` should understand the
> focus model, the fullscreen model, and where each lives, before reading
> the diagnosis.

### 1.1 What "focus" means here

`go-go-wm` is a tiling window manager with a floating overlay layer. At any
moment, **exactly one** of three things holds the keyboard:

1. **A tiled leaf** — a slot in the binary split tree (`wmcore.NodeID`).
2. **A floating frame** — a dialog/override window tracked in `WM.floats`,
   keyed by its X client window (`xproto.Window`).
3. **A fullscreen frame** — one window covering the whole screen, which may
   itself be a tile or a float.

The WM records this across three fields on the `WM` struct
(`pkg/wmx11/wm.go:140-185`):

```go
type WM struct {
    // ...
    focused      wmcore.NodeID            // the tiled leaf with focus ("" = none)
    floats       map[xproto.Window]*frame // floating frames, keyed by client
    focusedFloat xproto.Window            // 0 = the tiled world holds focus
    fullscreen   *frame                   // the one fullscreen window, nil = none
    fsSavedRect  wmcore.Rect              // float's pre-fullscreen rect
    // ...
}
```

The invariant the code *tries* to maintain is:

- If `fullscreen != nil`, it has focus (and its geometry).
- Else if `focusedFloat != 0`, that float has focus.
- Else `focused` is the focused tiled leaf.

But **nothing enforces this**. Each field is mutated independently by
different functions, and the "exactly one" rule is re-derived at every
read site. The only place that states the rule correctly is the
`frameFocused` predicate (`pkg/wmx11/float.go:303`):

```go
// frameFocused is the single visual-focus predicate: a float is focused
// when it holds focusedFloat; a tile only counts while no float does.
func (w *WM) frameFocused(f *frame) bool {
    if f.floating {
        return w.focusedFloat == f.client
    }
    return w.focused == f.leaf && w.focusedFloat == 0
}
```

This predicate is the seed of the right design (Part 3).

### 1.2 What "fullscreen" means here

Fullscreen is **shell state, not a tree property** (`pkg/wmx11/fullscreen.go:1-6`):
one window at a time covers the whole screen (bars included, i3 semantics).
The tree still owns tiled geometry — exiting fullscreen is just a relayout.

Two flavors of fullscreen frame exist, and the distinction is the source of
half the bugs:

- **Tiled fullscreen**: `f.leaf` is a real `wmcore.NodeID`; the frame is in
  `w.frames`. Exiting restores geometry from the tree via `relayout`.
- **Floating fullscreen**: `f.leaf == ""` (floats are not tree leaves); the
  frame is in `w.floats`. Exiting restores from `fsSavedRect`.

The fullscreen state machine (`pkg/wmx11/fullscreen.go`):

```
                  toggleFullscreen()
   none ─────────────────────────────► enterFullscreen(f)
   (w.fullscreen==nil)                  (w.fullscreen = f;
                                         save rect; resize to screen;
                                         stack above; emit event)
        ◄─────────────────────────────
                  toggleFullscreen()        exitFullscreen()
   fullscreen ─────────────────────────────► none
   (w.fullscreen==f)                        (restore rect or relayout;
                                            w.fullscreen = nil; emit)
```

Plus `clearFullscreenFor(f)` — drops state when the window is destroyed
(`fullscreen.go:84`), without the exit repaint.

### 1.3 The geometry ownership rule

While `w.fullscreen != nil`, **fullscreen owns its geometry**: `relayout`
explicitly skips it (`pkg/wmx11/manage.go:330`):

```go
if f == nil || f == w.fullscreen {
    continue // fullscreen owns its geometry until it exits
}
```

And `setTheme` repaints it explicitly because relayout skips it
(`pkg/wmx11/theme.go:74`):

```go
w.relayout()
if w.fullscreen != nil {
    w.paintFrame(w.fullscreen)
}
```

### 1.4 The focus dispatch sites

Focus is set from several places, each of which must respect the
fullscreen/focus invariant:

| Site | File:line | What it does |
|---|---|---|
| `focus(leaf)` | `manage.go:506` | The main focus mutator; pins to fullscreen if set |
| `focusFloat(f)` | `float.go:285` | Gives a float the keyboard; preserves `w.focused` |
| `focusNext()` | `input.go:122` | `Mod4-space` cycles tiled leaves |
| `refocusCurrent()` | `wm.go:419` | Lands focus on the current workspace after a switch |
| tile click | `input.go:166,177,187` | `focus(f.leaf)` |
| `unmanageFloat(f)` | `float.go:255` | Restores focus to `w.focused` when a float closes |

### 1.5 The workspace-switch reconciliation

Two paths reconcile after a batch of ops: `afterOp` (single op) and
`ApplyBatch` (multiple ops). Both must exit fullscreen on a workspace
switch (fullscreen is workspace-local) and refocus
(`pkg/wmx11/wm.go:393-440`):

```go
// afterOp (single op):
if op.Op == wmcore.OpSwitchWorkspace || op.Op == wmcore.OpAddWorkspace {
    w.exitFullscreen()
    w.refocusCurrent()
}

// ApplyBatch (batch) — RC-6 fix added the exitFullscreen:
if anySwitch {
    w.exitFullscreen()
    w.refocusCurrent()
}
```

---

## Part 2 — Diagnosis: the two systemic patterns

### 2.1 Pattern A — Fullscreen state is scattered, not encapsulated

`w.fullscreen` (a `*frame`) is read or written in **six files**:

| File | Sites | Role |
|---|---|---|
| `fullscreen.go` | 6 | The state machine (enter/exit/clear/toggle) |
| `manage.go` | 5 | `focus()` pins to it; `relayout` skips it; `handleConfigureRequest` ignores it (RC-12) |
| `wm.go` | 3 | `ApplyBatch`/`afterOp` exit it on switch; `refocusCurrent` |
| `theme.go` | 2 | Repaint it explicitly (relayout skips it) |
| `ipc.go` | 1 | `toggleFullscreen` IPC |
| `float.go` | 1 | `unmanageFloat` calls `clearFullscreenFor` |

There is **no method that answers "does fullscreen own this frame's
geometry/focus?"** — each site re-derives it with `w.fullscreen != nil` or
`w.fullscreen == f`, and the floating-vs-tiled distinction is re-derived
with `f.floating` / `f.leaf == ""` each time.

### 2.2 The five Codex comments, mapped to invariant violations

Every one of these is a code path that didn't respect an invariant that
*should* have been a single method call:

| Comment | File:line | Invariant violated | What happened |
|---|---|---|---|
| **RC-5** | `manage.go:506` | Fullscreen owns focus | `focus()` moved X input to a hidden tiled client under the fullscreen frame |
| **RC-6** | `wm.go:393` | Switch exits fullscreen | `ApplyBatch` didn't call `exitFullscreen` (only `afterOp` did) → focus pinned to unmapped frame |
| **RC-7** | `manage.go:521` | Float fullscreen has empty leaf | Pinning `leaf = w.fullscreen.leaf` set it to `""`, then cleared `focusedFloat` → lost the float |
| **RC-12** | `manage.go:589` | Fullscreen owns geometry | `handleConfigureRequest` honored a float's resize while fullscreen → frame shrank, stuck |
| **RC-13** | `manage.go:521` | Preserve tiled leaf under float | `focus()` cleared `w.focused` when pinning a fullscreen float → `unmanageFloat` couldn't restore |

**The pattern:** each bug is "a new code path re-derived the fullscreen
invariant and got one branch wrong." RC-7 and RC-13 are even on *the same
function* (`focus`), fixed in two separate batches, because the function
kept growing special cases.

### 2.3 Pattern B — The coupled focus trio

The focus invariant ("exactly one of {tile, float, fullscreen}") is spread
across three fields with no single mutator. Seven files touch both
`focused` and `focusedFloat`:

```
float.go (9/9)  manage.go (8/8)  wm.go (3/4)  input.go (7/1)
fullscreen.go (1/1)  launcher.go (2/1)  scripting.go (0/1)
```

The consequence: `focus()` is now a 40-line function with three special
cases (tiled, floating-fullscreen, normal), each carefully clearing the
*other* fields. RC-7 and RC-13 were both "cleared the wrong field." The
function is correct *now*, after three rounds of fixes — but it's brittle:
the next feature that touches focus will likely break it again.

### 2.4 Why this keeps happening

The root cause is a **missing abstraction**: there is no type that owns
"what is focused." The `WM` struct exposes three raw fields, and every
caller is trusted to keep them consistent. There's no compile-time
guarantee that a new `focus(...)` call site respects fullscreen, and no
single place to read "what is the current focus target."

---

## Part 3 — Design options

Two options, in increasing ambition. Option A is the minimal fix for
Pattern A; Option B is the full unification of Pattern B. They compose:
Option A is a subset of Option B.

### 3.1 Option A — A `fullscreenState` helper (encapsulate Pattern A)

**Idea:** introduce a small helper type that owns the fullscreen invariant
and exposes intent-revealing methods, so call sites stop poking
`w.fullscreen` directly.

```go
// fullscreenState owns the "one window covers the screen" invariant.
// It is the only type allowed to read/write WM.fullscreen + fsSavedRect.
type fullscreenState struct {
    wm *WM // back-reference, or pass WM fields explicitly
}

// Active returns the fullscreen frame, or nil.
func (fs *fullscreenState) Active() *frame { return fs.wm.fullscreen }

// Owns reports whether f is the fullscreen frame.
func (fs *fullscreenState) Owns(f *frame) bool { return fs.wm.fullscreen == f }

// OwnsGeometry reports whether fullscreen currently owns geometry
// (i.e. is active). relayout and handleConfigureRequest check this.
func (fs *fullscreenState) OwnsGeometry() bool { return fs.wm.fullscreen != nil }

// OwnsFocus reports whether fullscreen currently owns keyboard focus.
// focus() checks this instead of poking w.fullscreen directly.
func (fs *fullscreenState) OwnsFocus() bool { return fs.wm.fullscreen != nil }

// FocusTarget returns the frame that should receive focus while
// fullscreen is active (handles the tiled-vs-floating distinction that
// RC-7/13 got wrong). Returns nil if not active.
func (fs *fullscreenState) FocusTarget() *frame {
    if fs.wm.fullscreen == nil {
        return nil
    }
    return fs.wm.fullscreen // the frame itself; callers use .client or .leaf
}

// Enter makes f fullscreen. Returns the saved rect (floats restore it).
func (fs *fullscreenState) Enter(f *frame) wmcore.Rect { /* ... */ }

// Exit leaves fullscreen and restores geometry.
func (fs *fullscreenState) Exit() { /* ... */ }

// Clear drops state when f is destroyed (no repaint).
func (fs *fullscreenState) Clear(f *frame) { /* ... */ }
```

**Migration of call sites** (mechanical, behavior-preserving):

| Current | Becomes |
|---|---|
| `w.fullscreen != nil` (geometry check) | `w.fs.OwnsGeometry()` |
| `w.fullscreen == f` | `w.fs.Owns(f)` |
| `w.fullscreen.leaf` / `.floating` / `.client` in `focus()` | `w.fs.FocusTarget()` + branch on `f.floating` |
| `w.enterFullscreen(f)` | `w.fs.Enter(f)` |
| `w.exitFullscreen()` | `w.fs.Exit()` |
| `w.clearFullscreenFor(f)` | `w.fs.Clear(f)` |

**What this prevents:** RC-5 (focus would call `OwnsFocus()`), RC-6
(switch would call `Exit()` — already does, but the pattern is uniform),
RC-12 (`handleConfigureRequest` would call `Owns(f)`), and the
floating-vs-tiled distinction lives in one place (`FocusTarget`).

**What this does NOT fix:** Pattern B — `focus()` still manually clears
`focusedFloat` and `focused`. RC-7/RC-13's class of bug (wrong field
cleared) remains possible.

### 3.2 Option B — A unified `focusState` type (encapsulate Pattern B)

**Idea:** make "what is focused" a single value, not three coordinated
fields. The `frameFocused` predicate (`float.go:303`) already shows the
right shape — promote it to the data model.

```go
// focusTarget is the single source of truth for "what has the keyboard."
// Exactly one variant is set at a time; the zero value means "nothing
// focused." This replaces the focused + focusedFloat + fullscreen trio.
type focusTarget struct {
    kind   focusKind // tile | float | fullscreen
    leaf   wmcore.NodeID   // set when kind == tile (or the tile under a fullscreen float)
    client xproto.Window   // set when kind == float or fullscreen-float
}

type focusKind uint8
const (
    focusNone focusKind = iota
    focusTile
    focusFloat
    focusFullscreen
)

// focusState owns the focus invariant. It is the only type allowed to
// mutate the focus target, and it coordinates with fullscreenState so
// the two can't disagree.
type focusState struct {
    mu     sync.Mutex // or rely on the WM loop invariant
    target focusTarget
    // preservedTile is the tiled leaf to restore when a float/fullscreen
    // closes — replaces the implicit "w.focused stays set" convention
    // that RC-13 had to re-establish by hand.
    preservedTile wmcore.NodeID
}

// Current returns the active focus target (read-only).
func (fs *focusState) Current() focusTarget { /* ... */ }

// FocusTile makes leaf the focus target, clearing any float.
func (fs *focusState) FocusTile(leaf wmcore.NodeID) { /* ... */ }

// FocusFloat makes f the focus target, preserving the current tile for
// restoration.
func (fs *focusState) FocusFloat(f *frame) { /* ... */ }

// FocusFullscreen pins focus to the fullscreen frame (tile or float),
// preserving the underlying tile.
func (fs *focusState) FocusFullscreen(f *frame) { /* ... */ }

// Restore is called by unmanageFloat / exitFullscreen to return focus
// to the preserved tile.
func (fs *focusState) Restore() { /* ... */ }

// Focused reports whether f currently has focus (replaces frameFocused).
func (fs *focusState) Focused(f *frame) bool { /* ... */ }
```

**The key win:** the "exactly one" invariant becomes a single enum value,
not three coordinated fields. A new call site can't accidentally clear the
wrong field — there's only one field. `preservedTile` makes the
restoration contract explicit (RC-13's bug was that the contract was
implicit and `focus()` violated it).

**The cost:** this is a data-model change, not just an API change. Every
read of `w.focused` / `w.focusedFloat` becomes a method call. It's the
"right" design but the larger refactor.

### 3.3 Decision record: A vs. B

| Criterion | Option A (fullscreen helper) | Option B (unified focusState) |
|---|---|---|
| Prevents RC-5/6/12 (fullscreen geometry/focus) | ✅ | ✅ (superset) |
| Prevents RC-7/13 (wrong field cleared) | ❌ | ✅ |
| Blast radius | 6 files, ~15 sites | 7 files, ~30 sites |
| Behavior-preserving per commit | Easier | Harder (data model) |
| Risk of regression | Low | Medium |
| Recommended | **As a first step** | **As the end state** |

**Recommendation:** do Option A first (low-risk, fixes the fullscreen
half), then Option B (the focus unification) as a follow-up once A is
proven. The phased plan below reflects this.

---

## Part 4 — Phased, behavior-preserving migration plan

Each phase is independently mergeable and verifiable. The WM must stay
green (tests + manual X11) after every phase.

### Phase 0 — Add regression tests for the current behavior (prerequisite)

Before refactoring, lock in the *current* (post-fix) behavior so a
refactor regression is caught immediately. These tests don't exist yet:

- `focus` pins to fullscreen tile (RC-5): fullscreen a tile, `Mod4-space`,
  assert X focus stays on the fullscreen client.
- `focus` pins to fullscreen float without losing the tile (RC-7/13):
  fullscreen a float, `Mod4-space`, close the float, assert focus returns
  to the original tile.
- `ApplyBatch` exits fullscreen on switch (RC-6): fullscreen, switch
  workspace, assert `w.fullscreen == nil` and focus is on the new ws.
- `handleConfigureRequest` ignored while fullscreen (RC-12): fullscreen a
  float, send a ConfigureRequest, assert frame rect unchanged.

These need a WM harness (Xvfb or a mock). See Part 5.

### Phase 1 — Extract read-only `fullscreenState` helpers (Option A, read side)

Introduce `fullscreenState` with only the read methods (`Active`, `Owns`,
`OwnsGeometry`, `OwnsFocus`, `FocusTarget`). Replace every read of
`w.fullscreen` with a method call. **No behavior change** — pure
refactor. Verify with tests + `-race`.

Commit: `refactor(wmx11): extract fullscreenState read helpers`.

### Phase 2 — Move the fullscreen mutators into `fullscreenState` (Option A, write side)

Move `enterFullscreen`/`exitFullscreen`/`clearFullscreenFor`/`toggleFullscreen`
bodies into `fullscreenState` methods; the `WM` methods become thin
delegates. Verify.

Commit: `refactor(wmx11): consolidate fullscreen mutators into fullscreenState`.

### Phase 3 — Simplify `focus()` using `fullscreenState` (Option A payoff)

Now that `fullscreenState.FocusTarget()` exists, `focus()`'s fullscreen
branch becomes a single call instead of three special cases. The
floating-vs-tiled distinction lives in `FocusTarget`. Verify RC-5/7/13
tests still pass.

Commit: `refactor(wmx11): simplify focus() via fullscreenState.FocusTarget`.

### Phase 4 — Introduce `focusState` (Option B)

Replace the `focused` + `focusedFloat` fields with a single `focusState`.
Migrate all read sites (`w.focused` → `w.focus.Current().leaf`, etc.).
This is the largest phase; do it in sub-commits per file. Verify after
each.

Commit: `refactor(wmx11): unify focus state into focusState`.

### Phase 5 — Make `frameFocused` and `unmanageFloat` use `focusState`

The `frameFocused` predicate and `unmanageFloat`'s restoration become
`focusState.Focused(f)` and `focusState.Restore()`. The implicit
"preservedTile" contract becomes explicit. Verify.

Commit: `refactor(wmx11): route focus predicate + restoration through focusState`.

---

## Part 5 — Testing and validation strategy

### 5.1 The testing gap

`pkg/wmx11` currently has almost no tests — the WM logic is hard to test
without an X server. The existing tests are in `wmcore` (pure layout) and
`launcher`/`pbui` (pure Go). This refactor **needs** a WM test harness or
it's flying blind.

### 5.2 Options for a WM harness

1. **Xvfb + real Xorg** — most realistic, but slow and flaky in CI.
2. **A mock `*xgbutil.XUtil`** — fast, but `xgbutil` isn't designed for
   mocking; would need an interface seam.
3. **Extract the pure state logic** — pull `focusState`/`fullscreenState`
   out so they're testable with no X dependency (like `wmcore`). This is
   the best option and aligns with the design: the state types should be
   pure Go, with the X calls injected.

**Recommendation:** design `focusState` and `fullscreenState` as pure
state machines that emit "focus this client" / "resize this frame"
*intents*, with the X side applying them. Then the state logic tests with
no display. This is a bigger architectural lift but pays off beyond this
refactor.

### 5.3 Validation commands

```bash
# Existing tests must stay green
env GOTOOLCHAIN=go1.26.5 go test ./... -race -count=1

# Manual X11 validation (from AGENT.md)
Xephyr :1 -screen 1280x800 &
go-go-wm wm --display :1 --embedded-broker &
DISPLAY=:1 xterm
# then exercise: fullscreen tile, Mod4-space, fullscreen float, close,
# workspace switch while fullscreen
```

---

## Part 6 — Risks, alternatives, and open questions

### 6.1 Risks

- **Hot path.** Focus dispatch runs on every keypress and window event.
  Adding a method call (even inlined) per site is fine, but a mutex on
  `focusState` could contend if not careful. The WM is single-threaded for
  X events (the `ops` channel), so a mutex may be unnecessary — confirm
  the threading model before adding one.
- **Behavior preservation.** The current code is correct (after 3 fix
  rounds). A refactor that subtly changes an edge case (e.g. focus
  restoration order) could regress without tests. Phase 0 is non-negotiable.
- **Scope creep.** Option B tempts a full "extract pure state" rewrite.
  Keep the phases separate; don't let B drag in the rendering pipeline.

### 6.2 Alternatives considered

- **Do nothing.** The bugs are fixed. Rejected: the pattern will recur on
  the next focus-touching feature; the code is now genuinely hard to read.
- **Just add comments.** Rejected: comments don't prevent the next
  contributor from poking `w.fullscreen` directly. The point is to make the
  wrong thing impossible, not just discouraged.
- **A linter rule.** Could add a `//nolint`-style guard, but Go has no
  clean "don't touch this field" mechanism. Encapsulation is the Go-idiomatic
  answer.

### 6.3 Open questions

- **Is the WM single-threaded for focus mutations?** If the `ops` channel
  serializes all X-event handling, `focusState` needs no mutex and the
  design simplifies. Confirm by auditing `xevent` wiring in `wm.go`.
- **Should `fullscreenState` own the event emission** (`window.fullscreen`),
  or stay with the WM? Currently `enterFullscreen`/`exitFullscreen` emit.
  Moving emission into the helper centralizes it but couples it to the
  broker.
- **Workspace-local fullscreen.** RC-6 showed fullscreen is
  workspace-local (switching exits it). Should `fullscreenState` know
  about workspaces, or should the switch path call `Exit()` explicitly
  (as now)? The latter is simpler; the former is more encapsulated.

---

## Part 7 — References

### 7.1 Key files

| File | Role |
|---|---|
| `pkg/wmx11/wm.go:140-185` | `WM` struct: the three state fields |
| `pkg/wmx11/fullscreen.go` | The fullscreen state machine |
| `pkg/wmx11/manage.go:506-545` | `focus()` — the focus state machine |
| `pkg/wmx11/manage.go:330` | `relayout` skips fullscreen |
| `pkg/wmx11/manage.go:582-601` | `handleConfigureRequest` (RC-12) |
| `pkg/wmx11/float.go:255-310` | `unmanageFloat`/`focusFloat`/`frameFocused` |
| `pkg/wmx11/wm.go:358-440` | `ApplyBatch`/`afterOp`/`refocusCurrent` |
| `pkg/wmx11/theme.go:68-78` | `setTheme` fullscreen repaint |
| `pkg/wmx11/input.go:62-135` | `focusNext` + focus dispatch |

### 7.2 The Codex comments this addresses

| Comment | File:line | Fixed in (GGWM-010) | This ticket prevents by |
|---|---|---|---|
| RC-5 | `manage.go:506` | commit `8d9e905` | `fullscreenState.OwnsFocus()` |
| RC-6 | `wm.go:393` | commit `8127f27` | uniform `Exit()` on switch |
| RC-7 | `manage.go:521` | commit `8127f27` | `FocusTarget()` centralizes float-vs-tile |
| RC-12 | `manage.go:589` | commit `7d023fa` | `fullscreenState.Owns(f)` |
| RC-13 | `manage.go:521` | commit `7d023fa` | `focusState.preservedTile` (Option B) |

### 7.3 Related tickets

- `GGWM-010-PR1-REVIEW` — the PR-fix ticket where Patterns A & B were
  identified (Step 5 of its diary). This ticket is the structural follow-up.
