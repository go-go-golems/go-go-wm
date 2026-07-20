---
Title: Themes and the i3 config port — design
Ticket: GGWM-004-THEMES-I3
Status: active
Topics:
    - wm
    - scripting
    - ui
    - goja
DocType: design-doc
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://pkg/draw/theme.go
      Note: the palette this design turns into swappable themes
    - Path: repo://pkg/wmx11/wm.go
      Note: WM Config and event emission the theme state hangs off
    - Path: repo://pkg/jsmod/wmmod/module.go
      Note: the wm module gaining theme/exec/focus/move exports
    - Path: repo://pkg/jsmod/wmmod/rules.go
      Note: the rule engine gaining class matching
ExternalSources:
    - ~/.config/i3/config (the user's live i3 configuration, ported in sources/i3-config)
Summary: Design for GGWM-004 — a named-theme engine (paper / light with true white / dark anchored on i3's #1f1f1f) swapped into the existing pkg/draw palette, propagated over IPC and the broker; plus the three JS API extensions (wm.exec, directional focus/move, class-based rules) needed to port the user's i3 config to examples/scripts/i3.js.
LastUpdated: 2026-07-19T11:40:00-04:00
WhatFor: Governing design for the theme engine and the i3 parity work; read before touching draw palette code or the wm module surface.
WhenToUse: Read with the GGWM-002 design doc (attachment points, backend seam) before extending themes or the i3.js config.
---

# Themes and the i3 config port — design

## Executive summary

Two deliverables share this ticket because they meet in the same place —
the `wm` module surface:

1. **Themes.** The paper-and-ink palette in `pkg/draw/theme.go` becomes
   one of three named themes: `paper` (the current beige), `light` (true
   white `#ffffff`, not beige), and `dark` (anchored on `#1f1f1f`, the
   background color of the user's i3 setup). The theme is WM state,
   switchable at boot (`--theme`), over the control socket
   (`{"q":"set-theme"}`), and from JavaScript (`wm.theme("dark")`);
   changes propagate to standalone script apps via a `theme.changed`
   broker event.
2. **The i3 config port.** `~/.config/i3/config` becomes
   `examples/scripts/i3.js`, an rc file reproducing the workspace
   discipline, keybindings, exec launchers, and window assignment rules.
   Three JS API gaps block a faithful port and are closed here:
   `wm.exec()`, directional focus/move (`wm.focus("left")`,
   `wm.move("right")`), and class-based rules
   (`wm.rule({class: /Slack/, workspace: "8"})`).

## Problem statement

The WM renders exactly one look. Every color is a package-level `var` in
`pkg/draw` (`theme.go:23-38`), referenced directly by every paint path in
`wmx11`, `apps`, and `uispec`. There is no way to get a white or dark
desktop without recompiling.

Separately, the scripting layer (GGWM-002/003) claims "your window
manager config is a JavaScript program", but the API cannot yet express
an ordinary i3 config: i3 configs are dominated by `bindsym … exec …`
(no process spawning in the API), directional `focus left` / `move
right` (only `focusNext` exists, WM-internal), and `assign
[class="Slack"]` (rules match titles only).

## Current state (evidence)

- Palette: `pkg/draw/theme.go:23-38` — 14 named `color.RGBA` vars.
  Reads are paint-time everywhere except three init-time copies:
  `draw.AppColors` (`theme.go:44`), `uispec.tones`
  (`pkg/apps/uispec/uispec.go:55`), `builtin.traceTone`
  (`pkg/apps/builtin.go:186`).
- X-side color that is not repainted per frame: the root window back
  pixel (`wmx11/wm.go:226`), frame/bar/tile window back pixels set at
  create time (`manage.go:76`, `bars.go:25,122`, `builtin.go:81`,
  `pbui.go:176`, `divider.go:82`).
- Focus: `w.focused` lives on the WM, changed by clicks and
  `focusNext` (`input.go:111`); it is not reachable as a mutation from
  IPC or the Backend seam (`wmmod/backend.go:20-29`).
- Rules: `wmmod/rules.go` matches `window.managed` events on `title`
  only; the event payload (`manage.go:108-112`) carries no WM_CLASS.
- Spawning: only the WM's own `Mod4-Return` terminal binding
  (`input.go:80`); scripts have no process API (GGWM-002 deferred the
  `exec` capability behind `--allow-exec`, which gates go-go-goja's
  exec module, not a wm-flavored helper).

## Decision records

### T-D1 — Themes are palette swaps into the existing draw vars

- **Context.** Every renderer reads `draw.Paper`, `draw.Ink`, … at
  paint time. The alternative is threading a `*Theme` through every
  draw call and struct.
- **Options.** (a) Explicit theme parameter everywhere; (b) mutable
  package palette + `draw.SetTheme(name)` + full repaint; (c) per-window
  themes.
- **Decision.** (b). The palette vars are already the single source of
  truth, and every process owns exactly one render loop, so the rule
  "call `SetTheme` only from the loop that renders, then repaint" is
  enforceable and race-free in practice. The three init-time copies are
  converted to paint-time lookups so a swap is complete.
- **Consequences.** Golden/pixel tests must pin a theme explicitly
  (`draw.SetTheme("paper")`) at test start. Per-window theming is out
  of scope (would force option (a)).
- **Status.** accepted.

### T-D2 — Three themes; light is true white; dark anchors on #1f1f1f

- **Context.** The user explicitly wants a light mode with real white
  (`#ffffff`), not the beige `#e9e2d0`, and their i3 theme is a
  near-black `#1f1f1f` scheme.
- **Decision.** `paper` keeps today's values and stays the default
  (existing screenshots, goldens, and docs stay truthful). `light` sets
  Paper/Pane/Field to pure white with a `#f2f2f2` PaneAlt and near-black
  ink. `dark` inverts: Paper `#1f1f1f`, Pane `#262626`, light ink
  `#e6e2da`, and accent tones darkened so the light ink keeps contrast
  on chips and buttons.
- **Consequences.** `i3.js` selects `dark` to match the config it
  ports. Accent-on-ink contrast is validated by a luminance-delta test,
  not by eyeball (lesson from GGWM-003's accept-highlight episode).
- **Status.** accepted.

### T-D3 — Theme is WM state, propagated as an event

- **Context.** Scripts run in other processes with their own copy of
  the `draw` package; a WM-side swap does not reach them.
- **Decision.** The WM owns the current theme name (`Config.Theme`,
  default `paper`). IPC gains `{"q":"theme"}` (returns the name and the
  available names) and `{"q":"set-theme","theme":"dark"}` (validates,
  swaps, re-sets root/frame back pixels, repaints everything, emits
  `theme.changed {theme}` on the broker). Script runtimes set their
  initial theme from `{"q":"theme"}` when a wm socket is reachable
  (else `$GO_GO_WM_THEME`, else `paper`), and follow `theme.changed`
  through the EventFan's Go-subscriber path, repainting live `ui.app`
  surfaces.
- **Consequences.** `wm.theme()` works identically at all three
  attachment points (A1 rc.js, A2 run, A3 REPL) because it rides the
  Backend seam like every other call.
- **Status.** accepted.

### T-D4 — i3 parity through three small API extensions, not a compatibility layer

- **Context.** An i3-config parser would inherit i3's full command
  grammar for one user config.
- **Decision.** Port by hand to idiomatic rc.js, extending the API only
  where the config cannot be expressed at all:
  1. `wm.exec(cmdline)` — `sh -c` in the *script's* process,
     fire-and-forget, child reaped, DISPLAY forced to the WM's display.
     Enabled unconditionally in rc.js (the rc file is exactly as
     trusted as an i3 config, which is nothing but exec lines) and
     behind the existing `--allow-exec` for `run`/`repl`.
  2. `wm.focus(target)` / `wm.move(dir)` — target is a leaf id or
     `left|right|up|down|next|prev`. Directional resolution is
     geometric (nearest leaf center in the half-plane), computed on the
     WM loop from `wmcore.Layout`; `move` swaps the focused leaf with
     that neighbor. New IPC queries `focus` / `move`; two new Backend
     methods; ScriptBackend posts.
  3. Class rules — `manage()` reads WM_CLASS (`icccm.WmClassGet`) into
     the `window.managed` payload and `WindowInfo`; `wm.rule` accepts
     `title` and/or `class` (string or RegExp), requiring at least one;
     all present fields must match.
- **What is deliberately not ported.** Floating, scratchpad, gaps
  modes, tabbed/stacked layouts, multi-output workspace pinning, and
  i3 "modes" (global grabs of bare letters would swallow application
  keys). The port document in `i3.js` carries a line-by-line mapping
  table including the unsupported rows.
- **Status.** accepted.

## Phased plan

- **H1** — theme engine in `pkg/draw` (Theme struct, registry,
  `SetTheme`, `ThemeNames`), init-time copies converted, contrast +
  swap tests.
- **H2** — WM wiring: `Config.Theme`, `--theme`, IPC `theme` /
  `set-theme`, back-pixel refresh + full repaint, `theme.changed`
  emission; `wm.theme()` through both backends; script-side initial
  theme + live re-theme of `ui.app` surfaces.
- **H3** — `wm.exec`, `wm.focus`/`wm.move` (geometry, IPC, backends),
  WM_CLASS in events/WindowInfo, class rules.
- **H4** — `examples/scripts/i3.js` port + live Xvfb verification +
  `examples-smoke.sh` fixture; screenshots of all three themes.
- **H5** — intern guide (design-doc/02), diary, doctor, reMarkable.

## Testing

- `pkg/draw`: theme swap completeness (every slot differs from paper in
  dark), contrast floor (|luminance(Ink) − luminance(tone)| above a
  threshold for all button tones per theme).
- `wmmod`: fakeBackend grows Focus/Move/theme records; rule tests for
  class/title conjunction; exec test behind a temp script.
- Live: Xvfb boot with `--rc i3.js --theme dark`, xdotool Mod4-2 /
  Mod4-Shift-3 / Mod4-Return chains asserted over the query socket;
  set-theme flip asserted by pixel sampling the root/frame colors.

## Risks and open questions

- Benign data race if a worker goroutine reads palette vars mid-swap;
  accepted under the "swap only on the render-owning loop" rule
  (documented on `SetTheme`).
- `xgraphics.NewConvert` copies at paint time, so stale back pixels
  only show during expose gaps; we re-set root + frame + bar pixels on
  swap to avoid flashes.
- Directional focus across workspaces (i3 wraps outputs) is not
  defined; we stop at the workspace edge.
