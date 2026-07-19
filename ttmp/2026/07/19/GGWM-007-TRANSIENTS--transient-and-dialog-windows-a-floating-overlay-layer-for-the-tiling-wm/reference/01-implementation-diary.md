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
ExternalSources: []
Summary: Chronological implementation diary for the floating-transients layer — decisions made against the design doc, what broke, and how each phase was verified.
LastUpdated: 2026-07-19T19:00:00-04:00
WhatFor: Continuation context for anyone extending the float layer; records deviations from design-doc/01.
WhenToUse: Read alongside design-doc/01; the diary records what the implementation actually did where it differs.
---


# Implementation diary

## Entry 1 — plan and one deliberate deviation from the design (2026-07-19)

Implementation session begins, following design-doc/01 phases T1–T4.

**Deviation decided up front.** The design sketches a dedicated `floatWin`
struct. The lookup maps (`byClient`, `byFrame`), the frame-event dispatch
(`connectFrameEvents` routes by `byFrame`), the Expose fast path, and
`dropBuffers` are all typed on `*frame`. A second struct would either
duplicate that machinery or force an interface through the paint hot
path. Instead `frame` gains four fields (`floating bool`, `leader`,
`ws`, plus size-hint bounds) and floats are frames with `leaf == ""`,
tracked in a sibling map `WM.floats[client]*frame`. The design's actual
constraint — floats never touch the wmcore tree — is untouched.

**Blast-radius survey.** Every `range w.frames` / `w.frames[...]` site
was enumerated before coding; the float-blind spots that need explicit
float handling: `repaintAllFrames` (accept highlighting), `setTheme`
back-pixel loop, `updateEWMH` (`_NET_WM_DESKTOP` derives from leaf),
`windowsSnapshot`, `relayoutPaint` (workspace hide/show), `closeFocused`
(Mod4-w), `refocusCurrent` (stale focusedFloat after a switch),
`handleUnmapNotify`/`handleConfigureRequest` client branches.

Order of work: T1+T2 together (lifecycle and interaction interlock),
then live E2E on Xvfb, then T3 rule push-down, then T4 toggle, docs.

## Entry 2 — T1/T2/T4 WM-side layer live and E2E-verified (2026-07-19)

**What was built.** `pkg/wmx11/float.go`: `floatDecision` (pure
precedence function), `fetchFloatProps` (WM_TRANSIENT_FOR, window type,
size hints, requested geometry), `SetFloatRules`/`floatRuleVerdict`
(WM-side rule overrides), `manageFloat`/`unmanageFloat` (lifecycle),
`focusFloat`/`frameFocused`/`raiseChrome` (focus + stacking),
`syncFloats` (workspace hide/show from `relayoutPaint`),
`configureFloat` (honored ConfigureRequests), `clampFloatRect` (strip
always reachable), and the T4 toggle (`toggleFloat`/`liftTile`/
`sinkFloat`). `manage()` reads title/class first and takes the float
exit before leaf allocation; the shared client wiring (lifecycle
connects + click-to-focus grab) was factored into `wireClient`.
`draw.TitleStrip` gained `Float` (close button only; golden added).
IPC gained `set-float-rules` and `float`; `WindowInfo` gained
`floating`/`leader`. The `testwin` client (`pkg/cmds/testwin.go`) maps
windows with exact float signals and speaks WM_DELETE_WINDOW.

**What worked.**
- The frame-reuse decision paid off immediately: Expose handling,
  buffer caching, shm upload, and the GGWM-004 client-window event
  discipline all applied to floats without new code.
- `scripts/float-smoke.sh` passes all six stages on the first run after
  one testwin fix (below): dialog floats with leaf count unchanged,
  fixed-size floats, teardown on kill, both rule directions, workspace
  round trip, toggle alternation.

**What didn't work.**
- First smoke run failed stage 3 "zombie float" — but the WM was
  innocent: `kill`ed testwin processes stayed alive. glazed cancels the
  command context on SIGTERM, and the shutdown goroutine called only
  `xevent.Quit(X)`, which just sets a flag the event loop checks *after
  the next event* — `xevent.Main` sat in a blocking read forever. Fix:
  also `X.Conn().Close()` on ctx.Done; the read error terminates the
  loop. (Manual repro: `kill -0` reported the process alive 1s after
  SIGTERM while `windows` still listed the float.)
- The smoke's toggle stage exposed a focus hole: `afterOp` refocused
  only on `switch-workspace`, but `add-workspace` switches Current too
  (documented prototype semantic) — after `add-workspace`, a hidden
  float kept `focusedFloat` and the keyboard. `afterOp` now refocuses
  on both, matching what `ApplyBatch` already did via `anySwitch`.

**What was tricky.**
- Focus is now a two-register system (`focused` leaf + `focusedFloat`
  client) with the invariant "exactly one is hot". Every mutation site
  has to keep it: `focus()` clears the float register (navigation means
  back-to-tiles), `focusFloat` repaints the still-`focused` tile to
  drop its highlight, `unmanageFloat` hands focus back, and
  `refocusCurrent` clears a float register pointing at a hidden float.
  The single predicate `frameFocused(f)` is what paints read.

Commit: T1/T2/T4 WM layer + testwin + float-smoke fixture.

## Entry 3 — T3 scripting surface: float rules, push-down, i3.js floats (2026-07-19)

**What was built.** `wmmod.Backend` gained `SetFloatRules` and `Float`
(both `IPCBackend` and `ScriptBackend` implement them). `Rule` gained a
three-valued `Float *bool`; `normalizeRule` accepts `float: true|false`
and now allows workspace-less rules when a float field is present. The
`wm.rule` export arms the event watcher only for workspace rules and
pushes the compiled float-override list down after any float rule —
the keybinding-style push-down from the design: the module owns the
store, the WM owns the map-time decision. `wm.float()` toggles.
i3.js ports the config's entire `for_window … floating enable` list
(21 class rules + 7 title rules) and binds Mod4+Shift+space.

**What didn't work.**
- First run of the new JS tests: nil-pointer panic inside
  `normalizeRule`. In this goja version `obj.Get("absent-key")` returns
  a nil `goja.Value` (not undefined), and the old
  `obj.Get("workspace").Export()` had never executed against a rule
  without a workspace — every pre-GGWM-007 rule had one. Guard added.
  Lesson: any `obj.Get(k)` on an optional key needs the nil check
  *before* IsUndefined.

**Verification.** wmmod unit tests (push-down list content, both rule
validation errors, toggle alternation); live Xvfb check: WM booted with
`--rc examples/scripts/i3.js` (32 rules load), a `testwin --class
Galculator` with zero float signals floats via the pushed rule.
rc-smoke and examples-smoke both PASS (no regression).

Commit: T3 scripting surface.

## Entry 4 — docs, bookkeeping, wrap (2026-07-19)

Help topics updated for the float layer: `wm-module` (floating section,
rule float semantics, windows fields), `js-api-reference` (wm.float,
float rules, the two new events, windows fields), `user-guide` (floats
in the tiles concept, drag row in the gesture table, removed floats
from the not-yet-supported list). `query windows` CLI table gained
`class` and `floating` columns. Design doc gained Part VII as-built
notes (frame-reuse amendment to F-D1, add-workspace refocus, testwin
SIGTERM trap). tasks.md all checked; changelog updated; doctor clean.

**Code review instructions.** Start at `pkg/wmx11/float.go` top comment,
then read `manage()` in manage.go (the three-exit front door) and
`frameFocused` call sites (the two-register focus invariant). The E2E
truth is `scripts/float-smoke.sh`; run it on any display number. The
one deliberately-unported i3 behavior: border styles / sticky /
`resize set` on for_window rules (recorded in i3.js comments).

**Open follow-ups** (not blocking): modal dialogs
(`_NET_WM_STATE_MODAL`) don't block their leader; `focusNext`
(Mod4-space) skips floats by design — revisit if it feels wrong in
daily use; float positions are not persisted across workspace hides
beyond the rect field; multi-output centering deferred to the future
multi-output ticket.

