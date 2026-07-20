---
Title: Implementation diary
Ticket: GGWM-004-THEMES-I3
Status: active
Topics:
    - wm
    - scripting
    - ui
    - goja
DocType: reference
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://pkg/draw/theme.go
      Note: theme engine built in entry 1
    - Path: repo://pkg/wmcore/neighbor.go
      Note: directional-navigation geometry built in entry 2
    - Path: repo://examples/scripts/i3.js
      Note: the i3 config port built and live-debugged in entry 3
    - Path: repo://scripts/examples-smoke.sh
      Note: stage 3 fixture added in entry 3
ExternalSources: []
Summary: Chronological diary of the GGWM-004 build — the theme engine (paper/light/dark), the i3-parity API extensions (exec, directional focus/move, class rules, --no-default-binds), the i3.js port, and the three live-found bugs (torn palette race, SetInputFocus(None) keyboard kill, double-grabbed combos).
LastUpdated: 2026-07-19T13:45:00-04:00
WhatFor: Continuation context and the record of what broke and why during the themes/i3 work.
WhenToUse: Read with design-doc/01 before touching themes, focus navigation, or i3.js.
---

# Implementation diary

## Goal

Execute GGWM-004: swappable themes (light mode with true white, dark
mode anchored on the user's i3 `#1f1f1f`), and a faithful port of
`~/.config/i3/config` to an rc.js file, extending the JS API where the
config could not be expressed.

## Entry 1 — 2026-07-19 morning: H1/H2, the theme engine

### What was done

- `pkg/draw/theme.go`: `Theme` struct (14 slots), `Themes` registry
  (paper = the prototype palette, light = true white `#ffffff`, dark =
  `#1f1f1f` surfaces with darkened accents), `SetTheme` swapping the
  package-level vars + rebuilding `AppColors`, `CurrentTheme`,
  `ThemeNames`. The goroutine contract (swap only on the render-owning
  loop, then repaint) is documented on the var block.
- Converted the three init-time color copies to paint-time lookups:
  `AppColors` (rebuilt in SetTheme), `uispec` tones map → `tone(name)`
  function, `apps` trace-chip map → `traceTone(event)` function.
- `pkg/wmx11/theme.go`: `setTheme` on the WM loop — `draw.SetTheme`,
  re-set root/frame/divider/bar back pixels (set at window-create time;
  stale ones flash the old theme in expose gaps), `relayout` +
  `paintBars`, emit `theme.changed`.
- IPC `theme` / `set-theme`; `Config.Theme` + `--theme`; Backend
  `Theme`/`SetTheme`; `wm.theme()` / `wm.themes()`; script processes
  align at boot (query the control socket, `$GO_GO_WM_THEME` fallback)
  and follow `theme.changed` via an EventFan Go-subscriber, repainting
  live `ui.app` surfaces (`uimod.Module.Retheme` over a tracked app
  list).
- Tests: swap completeness (every var equals the theme value;
  `AppColors[0]` rebuilt), dark-differs-from-paper on every slot,
  light-is-true-white, and an ink contrast floor (≥60 luminance gap on
  every surface/accent, every theme).

### What worked

- The contrast test passed on the first run — picking dark accents by
  computing luminance targets beforehand beat eyeballing.
- The palette-var seam held: zero render-path signatures changed.

## Entry 2 — midday: H3, the i3-parity API

- `pkg/wmcore/neighbor.go`: `NeighborLeaf` — strict half-plane filter,
  cross-axis overlap requirement, min edge distance, cross-center
  tie-break. **Flake found by the first test run:** an exact two-way
  tie (editor → right against a 50/50 stack) was resolved by Go's
  randomized map order. Added a final deterministic tie-break (topmost,
  then leftmost). Lesson: any best-of-map scan needs a total order.
- `wmx11`: `focusTarget` (leaf id | left/right/up/down | next/prev),
  `moveDir` (swap with the geometric neighbor — an ordinary
  `swap-leaves` op, so it lands in the trace); IPC `focus`/`move`;
  WM_CLASS read at manage time (`icccm.WmClassGet`) onto the frame,
  the `window.managed` payload, and `WindowInfo`.
- `wmmod`: Backend `Focus`/`Move`; `wm.focus`/`wm.move`; `wm.exec`
  (`sh -c`, fire-and-forget, reaped; `WithExec(display)` — rc.js
  unconditional, run/repl behind `--allow-exec`); rules accept
  `class` and/or `title` (string or RegExp via the shared
  `patternSource` helper), all present patterns must match.
- Tests: neighbor table + reshape tie-break; theme round-trip through
  the module; exec gating (disabled throws, enabled touches a marker
  file); class-rule normalization (class-only ok, pattern-less
  rejected, bad regexp rejected). Commit `0461cee`.

## Entry 3 — afternoon: H4, i3.js and the three live bugs

### The port

`examples/scripts/i3.js` with a line-by-line mapping table in the
header (including NOT-PORTED rows: floating, scratchpad, modes,
tabbed/stacked, multi-output). Structure: pre-create workspaces 1–9
(then switch home to 1), back-and-forth via `wm.on("switch-workspace")`
state, launcher/kill/split/focus/move/workspace bindings, a Mod4-t
theme cycler, four class-rule assigns, opt-in autostart list.

New WM flag `--no-default-binds`: an rc config that owns the keyboard
cannot coexist with the built-in grabs — the same combo bound twice
fires twice (Mod4-Shift-q would close the window *and* shut the WM
down). Escape stays (modal accept/menu cancel).

### What didn't work, in order

1. **Test harness self-kill (exit 144), again.** A compound command
   containing `DISPLAY=:79` died to the test script's own
   `pkill -f "DISPLAY=:79"`. The GGWM-002 rule holds and was extended:
   not only the kill, but **any follow-up step naming the pattern text
   must live in a script file** (`i3-test.sh`, `i3-verify.sh`,
   `i3-builtins.sh` in this ticket's `scripts/`).
2. **All keybindings died after the first workspace switch.** IPC and
   mouse stayed healthy; `kill -QUIT` showed both event loops parked
   idle — so not a deadlock. Root cause: the new refocus-on-switch
   landed on a builtin tile, whose frame has `client == 0`, and
   `w.focus` called `SetInputFocus` on window 0 = None — after which
   the X server discards keyboard processing, killing even root
   grabs. Fix: focus the frame window for client-less tiles; also
   stopped ConfigureWindow-ing window 0 (the long-standing BadWindow
   log spam). The scripted test still shows a first-press flake right
   after Xvfb boot (keymap warm-up); interactively every binding chain
   passed: 2/5/t/1 switches, arrows walking three tiles, Shift-arrow
   swap verified by rect movement.
3. **Torn palette.** The dark-theme screenshot showed paper surfaces
   with dark accent chips. Two goroutines were both calling
   `draw.SetTheme` in the WM process: the WM loop (IPC set-theme) and
   the rc runtime's fan drainer reacting to the WM's own
   `theme.changed` event — interleaved writes over 14 slots. Fix:
   `followThemeChanges(..., swapPalette)` — false in-process (repaint
   only), true for run/repl. After the fix, cycling
   dark→light→paper→dark pixel-asserts clean on every stop
   (bar `#1f1f1f`/`#ffffff`/`#e9e2d0`, pane `#262626`/`#ffffff`/
   `#f5f0e3`).

### Verification

- `go test ./...` green (draw/wmcore/wmmod suites grew);
  `scripts/examples-smoke.sh` extended with stage 3 (i3.js boots dark
  with --no-default-binds; workspaces 1..9 with home 1; set-theme
  round-trips) — **9/9 fixtures PASS**.
- Live on Xvfb :79: class rule moved a fake Slack xterm to workspace 8
  (visible as `window.managed class=Slack` → `move-leaf` in the trace
  tile); Mod4-Return spawned kitty; back-and-forth returned across
  workspaces; theme screenshots with builtin tiles captured for all
  three themes (`various/build-screenshots/01-05`).
- Commits: `0461cee` (H1–H3), `cd7f4b2` (H4).

### Code review instructions

Read `pkg/draw/theme.go` first (registry + SetTheme contract), then
`pkg/wmx11/theme.go` against dispatchIPC (every entry point posts to
the WM loop), then `pkg/wmcore/neighbor.go` with its test table, then
`wmmod/module.go` new exports + `rules.go` class matching, then
`pkg/cmds/run.go` `followThemeChanges` (the swapPalette comment is the
race postmortem). Run `go test ./pkg/draw/ ./pkg/wmcore/
./pkg/jsmod/wmmod/` and `GO_GO_WM_BIN=<bin> scripts/examples-smoke.sh`.

## Entry 4 — 2026-07-19 afternoon: the unclosable-tile bug (user report)

### Symptom

"Closing tiles / clicking ✕ doesn't seem to work" — while Mod4-Shift-q
(wm.close via close-leaf) worked. Second user datapoint: after Ctrl-D
in an xterm the tile stayed, and only *then* could it be killed; the
session log showed repeating BadWindow spam on MajorOpcode 42
(SetInputFocus) and 12 (ConfigureWindow) against stale client ids.

### Investigation

- Instrumented `handleFramePress`/`closeClient`: the ✕ click IS
  delivered, hits the close branch, and WM_PROTOCOLS reads
  ["WM_DELETE_WINDOW"] — so the click path was innocent.
- A standalone probe (`/tmp/claude-1000/wmdel`) sending the identical
  ClientMessage closed the xterm — the wire format was innocent too.
- The probe run exposed the real bug: the xterm *process exited* but
  the WM's windows query still listed it. **DestroyNotify was never
  handled**: `xevent.DestroyNotifyFun(...).Connect(w.X, root)` — but a
  reparented client's StructureNotify events carry the *client* as the
  event window, and xgbutil dispatches callbacks by that window. The
  root-connected handlers only ever matched events on root children
  (our frames), so client death — via ✕/WM_DELETE or Ctrl-D — left a
  zombie frame the WM kept focusing and configuring (the BadWindow
  spam). The ✕ *did* close the client every time; the frame just
  never went away, which reads as "close doesn't work".
- Separately: ✕ on a *builtin* tile was a true no-op — `closeClient`
  asked window 0 for WM_PROTOCOLS and then Kill(0).

### Fixes (all in wmx11)

1. `manage()` connects DestroyNotify/UnmapNotify handlers on the
   client window itself.
2. `unmanage()`/`placementLeaf()`/`syncBuiltins()` call
   `xevent.Detach` for windows they destroy (X recycles ids; stale
   callbacks would fire for strangers).
3. `closeClient` on a client-less frame closes the leaf via ops:
   close-leaf normally, set-leaf-app "" for a lone leaf (mirrors
   unmanage) — afterOp/syncBuiltins reaps the frame.

### Verification (close-test3.sh, Xvfb :82)

Five scenarios, all asserted over the control socket: external
WM_DELETE probe → client gone AND unmanaged; ✕ click on a real client
→ gone; client self-exit (Ctrl-D analogue, pkill) → no zombie, ws
back to a lone empty leaf; ✕ on a builtin in a split → leaf closed;
✕ on a lone builtin → becomes launcher. Full `go test ./...` green.

### What was tricky

- The first repro was polluted: the bash harness had SIGHUPed the
  victim xterm between steps, so the probe hit a genuinely-dead window
  (BadWindow) and looked like a serialization bug. Repro processes now
  start with `setsid`.
- examples-smoke stage 3 started flaking (5/9 workspaces): the fixture
  raced rc.js's boot-time workspace creation with a fixed sleep.
  Fixture now polls to a deadline. Rule: never assert a fixed delay
  after an asynchronous boot — poll the condition.
- pkill self-match, twice more: the *creation* of a script whose text
  contains the kill pattern must not share a compound command with its
  invocation; and `export DISPLAY` belongs inside the script so the
  outer command never names it.

## Related

- design-doc/01 — decisions T-D1..T-D4 and the phase plan.
- design-doc/02 — the intern guide (Parts IV, V, VII retell the three
  bugs as teaching material).
- GGWM-002/GGWM-003 diaries — the scripting-layer history this builds on.
