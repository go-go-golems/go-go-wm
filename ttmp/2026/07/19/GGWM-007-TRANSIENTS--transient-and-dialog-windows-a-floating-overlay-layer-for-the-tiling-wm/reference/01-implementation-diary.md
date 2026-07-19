---
Title: Implementation diary
Ticket: GGWM-007-TRANSIENTS
Status: active
Topics:
    - wm
    - ui
DocType: reference
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://pkg/cmds/testwin.go
      Note: the purpose-built test client
    - Path: repo://pkg/wmx11/float.go
      Note: |-
        the float layer this diary narrates
        implementation narrated by this diary
    - Path: repo://scripts/float-smoke.sh
      Note: the six-stage E2E fixture that validated every step
ExternalSources: []
Summary: Step-formatted implementation diary for the floating-transients layer — decisions made against the design doc, verbatim failures, and how each phase was verified.
LastUpdated: 2026-07-19T19:00:00-04:00
WhatFor: Continuation context for anyone extending the float layer; records deviations from design-doc/01.
WhenToUse: Read alongside design-doc/01; the diary records what the implementation actually did where it differs.
---

# Diary

## Goal

Capture the implementation journey of GGWM-007 (floating/transient
windows): the plan, the deliberate design deviations, everything that
broke along the way with exact symptoms, and how each phase was
verified — so a future session can extend the float layer without
re-deriving any of it.

## Step 1: Plan, one deliberate deviation, and a blast-radius survey

Implementation began from design-doc/01 (phases T1–T4). Before writing
code, two things were settled: the record type for floats, and a
complete list of the places in the WM that implicitly assume "every
managed window is a tile."

The design sketches a dedicated `floatWin` struct. Reality argued
otherwise: the lookup maps (`byClient`, `byFrame`), the frame-event
dispatch (`connectFrameEvents` routes by `byFrame`), the Expose fast
path, and `dropBuffers` are all typed on `*frame`. A second struct
would either duplicate that machinery or force an interface through
the paint hot path.

### Prompt Context

**User prompt (verbatim):** "Implement the next ticket, keep a detailed diary as you work, commit at appropriate itnervals"

**Assistant interpretation:** Implement GGWM-007-TRANSIENTS (the next
ticket by number and dependency order), maintaining a diary and
committing at phase boundaries.

**Inferred user intent:** Continue the GGWM ticket queue autonomously
with the same documentation discipline as previous tickets; the diary
is the continuation artifact for future sessions.

### What I did
- Re-read design-doc/01 and the GGWM-007 memory note.
- Decided the deviation: `frame` gains `floating bool`, `leader`,
  `ws`, and size-hint bounds; floats are frames with `leaf == ""`
  tracked in a sibling map `WM.floats[client]*frame`. The design's
  actual constraint — floats never touch the wmcore tree — holds.
- Enumerated every `range w.frames` / `w.frames[...]` site before
  coding (`rg -n "range w.frames|w.frames\["`). Float-blind spots
  needing explicit handling: `repaintAllFrames` (accept highlight),
  `setTheme` back-pixel loop, `updateEWMH` (`_NET_WM_DESKTOP` derives
  from leaf), `windowsSnapshot`, `relayoutPaint` (workspace
  hide/show), `closeFocused` (Mod4-w), `refocusCurrent` (stale
  focusedFloat), `handleUnmapNotify`/`handleConfigureRequest`.

### Why
- Frame reuse buys the Expose/buffer/shm machinery and the GGWM-004
  client-event discipline for free; a parallel struct would have
  re-created the zombie-frame bug class in a second type.
- The survey-first approach comes from GGWM-004/005 experience: the
  bugs in this codebase live in "the site nobody re-checked."

### What worked
- The survey was exhaustive — every one of those sites did need a
  change, and no float-blind site surfaced later that was not on the
  list.

### What didn't work
- N/A (planning step).

### What I learned
- The maps are the real type system of this WM: what `byFrame` maps to
  decides what can receive events, more than any struct name.

### What was tricky to build
- Nothing yet; the trick was resisting the design doc's struct.

### What warrants a second pair of eyes
- The claim that F-D1's *intent* survives the amendment (tree purity,
  not struct identity). Part VII of the design doc records it.

### What should be done in the future
- N/A

### Code review instructions
- Read the top comment of `pkg/wmx11/float.go`, then design-doc/01
  Part VII (as-built notes).

### Technical details
- Detection precedence (pure function `floatDecision`):
  rule override → WM_TRANSIENT_FOR → `_NET_WM_WINDOW_TYPE` ∈
  {DIALOG, UTILITY, SPLASH, TOOLBAR, MENU…NOTIFICATION} → min==max
  size hints.

## Step 2: T1/T2/T4 — the WM-side float layer, live-verified

One commit delivers detection, lifecycle, focus, stacking, geometry,
dragging, and (ahead of schedule) the T4 toggle — T4 collapsed into
T1/T2 because `liftTile`/`sinkFloat` turned out to be thin
recombinations of `placementLeaf` and unmanage's tree handling. The
layer was proven live on Xvfb with a new purpose-built test client
before committing.

The two real bugs of the session both surfaced here, and neither was
in the float code itself: a test client that would not die, and a
pre-existing focus hole that floats made observable.

### Prompt Context

**User prompt (verbatim):** (see Step 1)

**Commit (code):** 234e5ca — "GGWM-007: floating overlay layer — detection, lifecycle, focus, stacking, toggle"

### What I did
- New `pkg/wmx11/float.go`: `floatDecision`, `fetchFloatProps`
  (WM_TRANSIENT_FOR, EWMH types, size hints, requested geometry),
  `SetFloatRules`/`floatRuleVerdict`, `manageFloat`/`unmanageFloat`,
  `focusFloat`/`frameFocused`/`raiseChrome`, `syncFloats`,
  `configureFloat`, `clampFloatRect`, `toggleFloat`/`liftTile`/
  `sinkFloat`.
- `manage()` now reads title/class first and takes the float exit
  before leaf allocation; shared client wiring factored into
  `wireClient` (lifecycle connects + click-to-focus grab, float-aware).
- Float drag: `dragState` kind "float" with pointer offset; move-only,
  no repaint needed (content rides in the background pixmap).
- `draw.TitleStrip{Float: true}` renders close-only (golden
  `title-strip-float` added); floats skip the accept-highlight branch.
- IPC: `set-float-rules`, `float`; `WindowInfo` gains
  `floating`/`leader`; `windowsSnapshot` appends float rows.
- All survey sites from Step 1 patched.
- New `pkg/cmds/testwin.go` (`go-go-wm testwin`): maps a window with
  exact float signals (`--type dialog|utility|splash|toolbar`,
  `--transient-for 0x…`, `--fixed WxH`, `--class/--instance/--title`),
  prints its window id, speaks WM_DELETE_WINDOW.
- New `scripts/float-smoke.sh` (six stages), plus unit tests
  `float_test.go` (decision table, rule matching, clamp).

### Why
- T1+T2 together because lifecycle and interaction interlock (teardown
  paths depend on drag/focus state); live E2E before commit because
  the input-path bug family of GGWM-004 taught that this layer cannot
  be trusted from unit tests alone.

### What worked
- Frame reuse: Expose handling, buffer caching, shm upload, and the
  client-window event discipline applied to floats with zero new code.
- `scripts/float-smoke.sh` all six stages green after the two fixes
  below: dialog floats with leaf count unchanged; fixed-size floats;
  teardown on kill; both rule directions; workspace round trip;
  toggle alternation.

### What didn't work
- **Stage 3 "zombie float" on the first smoke run.** Output:
  `FAIL: killed float still present (zombie)` with both testwins still
  in `windows`. The WM was innocent: `kill`ed testwin processes stayed
  alive (`kill -0` succeeded 1s after SIGTERM in a manual repro).
  Cause: glazed cancels the command context on SIGTERM, and the
  shutdown goroutine called only `xevent.Quit(X)` — which sets a flag
  checked *after the next event*; `xevent.Main` sat in a blocking
  read forever. Fix: also `X.Conn().Close()` in the ctx.Done
  goroutine; the read error terminates the loop.
- **A focus hole exposed by the toggle stage.** After
  `{"q":"op","op":{"op":"add-workspace"}}`, the toggle reported
  `false` first (a float was still "hot") — `afterOp` refocused only
  on `switch-workspace`, but `add-workspace` switches Current too
  (documented prototype semantic), so a hidden float kept
  `focusedFloat` and the keyboard. Fix: `afterOp` refocuses on both,
  matching what `ApplyBatch` already did via `anySwitch`.
- First build error: `w.liftTile(f) (no value) used as value` —
  toggleFloat's tuple return; trivial restructure.

### What I learned
- `xevent.Quit` is not a shutdown primitive; only closing the X
  connection unblocks a parked `xevent.Main`. Any future X test
  client needs the same two-line goroutine.
- Latent focus bugs become visible the moment a second focus register
  exists; the smoke suite's *incidental* assertions (toggle direction)
  caught what a targeted test would have missed.

### What was tricky to build
- **The two-register focus system.** `focused` (leaf) + `focusedFloat`
  (client) with the invariant "exactly one is hot". Symptoms of
  getting it wrong ranged from double highlights to keyboard-to-
  hidden-window. Approach: a single visual predicate
  (`frameFocused(f)`) that all paints read, and explicit register
  maintenance at every mutation site — `focus()` clears the float
  register (navigation = back to tiles), `focusFloat` repaints the
  still-`focused` tile to drop its highlight, `unmanageFloat` hands
  focus back, `refocusCurrent` clears a register pointing at a hidden
  float.
- **Strip reachability.** A float placed or dragged off-screen is
  mouse-unrecoverable; `clampFloatRect` guarantees ≥40px of strip
  inside the work area on every path that sets geometry (placement,
  drag, ConfigureRequest, liftTile).

### What warrants a second pair of eyes
- `configureFloat`'s coordinate translation (client-requested x/y →
  frame coords, `- BorderW` / `- TitleH`) — correct for the common
  case, but clients that position via `ConfigureRequest` after our
  reparent may expect root coordinates of the client itself.
- `handleUnmapNotify`'s float guard (`f.ws == w.desktop.Current`):
  relies on workspace switches never unmapping the *client* window
  (only the frame). True today; would silently unmanage floats if
  that changes.

### What should be done in the future
- Modal dialogs (`_NET_WM_STATE_MODAL`) don't block their leader.
- `focusNext` (Mod4-space) skips floats by design; revisit in daily use.

### Code review instructions
- Start at `pkg/wmx11/float.go` top comment; then `manage()` in
  manage.go (the three-exit front door); then `frameFocused` call
  sites. Validate: `go test ./pkg/wmx11/ ./pkg/draw/` and
  `scripts/float-smoke.sh` (any display number).

### Technical details
- Stacking: floats restack `StackModeAbove` on focus, then
  `raiseChrome()` re-raises bars/overlay/menu — one global order:
  chrome > focused float > other floats > tiles.
- Float placement: requested size clamped to hints and work area;
  centered on the leader's frame when the leader is managed, else on
  the work area.

## Step 3: T3 — float rules in JS, the push-down, i3.js floats

The scripting surface: `wm.rule` grows a three-valued `float` field
whose compiled list is pushed down to the WM (the keybinding-style
push-down from the design — module owns the store, WM owns the
map-time decision), `wm.float()` toggles, and i3.js gains the user's
entire `for_window … floating enable` list.

### Prompt Context

**User prompt (verbatim):** (see Step 1)

**Commit (code):** 5a5312d — "GGWM-007: float rules in JS — wm.rule({float}), WM push-down, wm.float, i3.js floats" (+ 295b1c8 gofmt straggler)

### What I did
- `wmmod.Backend` += `SetFloatRules`, `Float`; implemented by
  `IPCBackend` (socket) and `ScriptBackend` (posts); fakeBackend
  updated.
- `Rule.Float *bool`; `normalizeRule` accepts `float: true|false` and
  allows workspace-less rules when float is present; `armRules` skips
  float-only rules (no event watcher needed); `pushFloatRules`
  replaces the WM list after any float-rule definition.
- `wm.float()` export; JS tests: push-down list content, both
  validation errors, toggle alternation.
- i3.js: 21 class + 7 title float rules, `Mod4+Shift+space` →
  `wm.float()`, header mapping rows updated.

### Why
- Push-down (not event-driven): the float decision must run *before*
  placement, at map time — an event-driven rule would tile the window
  first and convert it after, visibly.

### What worked
- Live Xvfb check: WM booted with `--rc examples/scripts/i3.js`
  (32 rules), then `testwin --class Galculator` with *zero* float
  signals floats via the pushed rule — the full rc → push-down →
  map-time chain.
- rc-smoke and examples-smoke stayed green (no regression).

### What didn't work
- First run of the new JS tests:
  `runtime call panicked: runtime error: invalid memory address or nil
  pointer dereference` inside `normalizeRule`. In this goja version
  `obj.Get("absent-key")` returns a **nil** `goja.Value` (not
  undefined); the old `obj.Get("workspace").Export()` had simply never
  executed against a rule without a workspace. Guard added
  (`wv != nil && !goja.IsUndefined(wv) && !goja.IsNull(wv)`).

### What I learned
- goja API gotcha worth memorizing: nil check **before** IsUndefined
  on every optional-key `obj.Get`.

### What was tricky to build
- Deciding which rules arm the broker watcher: a rule may carry
  workspace, float, or both — only the workspace half needs events,
  and float-only rules must work with `fan == nil` (tests, no broker).

### What warrants a second pair of eyes
- `pushFloatRules` holds `ruleState.mu` only while snapshotting, then
  calls the backend outside the lock — rule-definition races (two
  rules defined concurrently from different runtimes) could push
  stale lists; last-push-wins is the intended semantic.

### What should be done in the future
- A2-daemon-registered float rules die with their process; stale-rule
  cleanup on broker disconnect is unhandled (same as verbs).

### Code review instructions
- `pkg/jsmod/wmmod/rules.go` (normalizeRule, pushFloatRules, armRules
  skip), then the i3.js float block. Validate:
  `go test ./pkg/jsmod/wmmod/ -run Float -v`.

### Technical details
- Wire shape: `{"q":"set-float-rules","float_rules":[{"class":"…",
  "float":true}]}`; invalid regexps reject the whole set atomically.

## Step 4: Docs, bookkeeping, wrap

Help topics, CLI columns, the design doc's as-built record, and ticket
hygiene — then push.

### Prompt Context

**User prompt (verbatim):** (see Step 1)

**Commit (code):** 1af06e6 — "GGWM-007: docs + bookkeeping — help topics, query windows columns, as-built notes"

### What I did
- Help topics: `wm-module` (floating section, rule float semantics),
  `js-api-reference` (wm.float, float rules, two new events, windows
  fields), `user-guide` (floats in concepts, gesture-table row,
  removed from not-yet-supported).
- `query windows` gains `class` and `floating` columns.
- Design doc Part VII (as-built notes); tasks.md all checked;
  changelog + relations; `docmgr doctor` clean; final full test sweep
  + float-smoke; pushed `d138333..1af06e6`.
- Memory updated (GGWM-007 → IMPLEMENTED, with the trap list).

### Why
- Docs-at-the-end keeps them consistent with what actually shipped.

### What worked
- Everything green on the first pass; doctor clean.

### What didn't work
- N/A

### What I learned
- N/A

### What was tricky to build
- N/A

### What warrants a second pair of eyes
- N/A

### What should be done in the future
- Next by dependency chain: GGWM-008-LAUNCHER (its L3 keyboard
  substrate gates GGWM-009's REPL tile).

### Code review instructions
- `git show 1af06e6 --stat`; render check via `go-go-wm help
  js-api-reference`.

### Technical details
- Events added to the vocabulary: `window.float-closed {client,
  title, class}`, `window.float-toggled {client, floating, leaf?}`;
  `window.managed` gains `floating`/`leader`/`client` for floats.
