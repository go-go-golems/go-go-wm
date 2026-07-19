---
Title: Implementation diary
Ticket: GGWM-005-PERF
Status: active
Topics:
    - wm
    - performance
DocType: reference
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://pkg/draw/ximage.go
      Note: fast conversion built in entry 1
    - Path: repo://pkg/wmx11/manage.go
      Note: buffer-caching paintFrame built in entry 2
    - Path: repo://scripts/rc-smoke.sh
      Note: keypress retry added in entry 3
ExternalSources: []
Summary: Chronological diary of the GGWM-005 perf work — instrumentation, the two profiles, the five paint-path fixes with intermediate measurements, the click-to-focus addition, and the rc-smoke flake triage.
LastUpdated: 2026-07-19T14:00:00-04:00
WhatFor: The measurement trail behind commit bb5bfbf; read before re-profiling or extending the paint path.
WhenToUse: With design-doc/01 (decisions + numbers) and design-doc/02 (the teaching version).
---

# Implementation diary

## Goal

User-reported: workspace pre-creation at boot is slow, and drag-resizing
a tile lags. Instrument, profile with real input, fix what the profile
convicts, and prove the symptom moved.

## Entry 1 — instrumentation and the first profile

- Added duration logs (debug level) around `afterOp` and `paintFrame`,
  and `GO_GO_WM_PPROF=addr` serving `net/http/pprof` from the wm
  command. Harness `perf-test.sh` (ticket `scripts/`): Xvfb + i3.js
  boot timed over the control socket, then a 12s CPU capture while
  xdotool sweeps a divider (3 passes, mousedown → 8px steps → mouseup).
- First numbers: boot to nine workspaces 6.18s; full-screen paints
  100–140ms each; profile: convertRGBA 33%, draw.Fill 26%,
  dividerMotion 60% cum, paintFrame 69% cum.
- Root causes read straight out of the library and our code:
  `xgraphics.NewConvert` iterates column-major (a full stride jump per
  inner step); `draw.Fill` was per-pixel `SetRGBA`; every motion event
  repainted every visible pane; every paint allocated ~7.6 MB.

## Entry 2 — the fixes, measured between each round

1. **Row-copy Fill + row-major ToXImage** (pkg/draw). Golden tests
   pass unchanged — the rewrite license. Second profile: total samples
   7.74s → 7.66s (same stress, but) Fill gone, ToXImage 1.61s vs
   convertRGBA's 2.57s, boot 6.18s → 4.60s. Two NEW findings surfaced
   once the old noise cleared: GC ~29% (per-paint allocations) and the
   Expose handler 27% cum — every MoveResize during a drag exposed a
   frame we had just painted, doubling the work.
2. **Buffer caching + Expose-as-XPaint + motion coalescing +
   relayoutResized** (wmx11). frame.img/frame.ximg reused across
   paints, dropped on resize/unmap/evict/destroy (dropBuffers rides
   the same teardown list as GGWM-004's xevent.Detach); XSurfaceSet
   only on pixmap (re)creation; Expose with a size-matching ximg is
   one XPaint; divider motion coalesced to 16ms with a final apply in
   handleRelease; drags repaint only rect-changed frames
   (relayoutResized) while relayout() keeps repaint-everything
   semantics for themes/switches.
3. Third profile, same stress: **total samples 2.43s (was 7.74s)**,
   hottest node CopyToXImage 22% (0.54s), GC ~8%, Expose path absent.
   Boot 2.78s. Per-paint logs: first paints ~65–105ms (pixmap create +
   full upload), steady-state repaints far below.

## Entry 3 — click-to-focus and the flake triage

- **Click-to-focus** (user report: only the title strip focused a
  tile): synchronous passive `GrabButton` on each client,
  ButtonPress → focus + `AllowEvents(ReplayPointer)` so the app still
  receives the click. Verified in the harness: click inside left
  client → focus left; right → right.
- **rc-smoke went red** after the changes — Mod4-e "didn't split".
  Triage order mattered: before blaming the new grab code, re-ran
  interactively — the binding works; the harness presses a key exactly
  once, cold, seconds after Xvfb boot, and the first synthetic
  keypress can be swallowed while the keymap settles (the same
  first-press flake seen throughout GGWM-004 under load). Fix: the
  fixture retries up to five presses — a real binding regression still
  fails. Both smoke suites green afterwards (9/9 + rc-smoke).

### What was tricky

- Optimizing in the wrong order would have lied: Fill/convert had to
  go first, because only after their noise cleared did the GC share
  and the Expose double-paint become visible in the profile. Profile →
  fix → re-profile, one round at a time.
- `XSurfaceSet` semantics turned out to be the free lunch: content
  bound as the window background pixmap means the *server* can repair
  damage. The Expose handler shrank to a blit because of an X11
  feature that was already being paid for.
- The relayout split needed care: theme swaps (GGWM-004 T-D1) and
  workspace switches *require* repaint-everything; only the drag path
  may use the resized-only variant.

### Code review instructions

Read `pkg/draw/ximage.go` (both functions) and the new `draw.Fill`
against the profile tables in design-doc/01; then `paintFrame` +
`dropBuffers` (every teardown path must drop buffers — grep
`win.Destroy()` and check each is preceded by `dropBuffers`); then the
Expose handler in `events.go` (the size check is what keeps stale
blits impossible); then `dividerMotion`/`handleRelease` (the release
must re-run the final position). Run `go test ./...`, both smoke
suites, and `perf-test.sh` — compare `-top` to the archived
`sources/cpu-after.prof`.

## Related

- design-doc/01 — decision records P-D1..P-D5 and the numbers.
- design-doc/02 — the intern guide built from this work.
- GGWM-004 reference/01 — the session this continues (zombie frames,
  keybinding traps).
