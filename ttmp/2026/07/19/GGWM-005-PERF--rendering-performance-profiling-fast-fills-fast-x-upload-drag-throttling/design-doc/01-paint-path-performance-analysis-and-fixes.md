---
Title: Paint-path performance analysis and fixes
Ticket: GGWM-005-PERF
Status: active
Topics:
    - wm
    - performance
DocType: design-doc
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://pkg/draw/ximage.go
      Note: the fast RGBA→BGRA conversion (P-D2)
    - Path: repo://pkg/draw/theme.go
      Note: the row-copy Fill (P-D1)
    - Path: repo://pkg/wmx11/manage.go
      Note: buffer-caching paintFrame and relayoutResized (P-D3/P-D4)
    - Path: repo://pkg/wmx11/input.go
      Note: divider-drag motion coalescing (P-D4)
    - Path: repo://ttmp/2026/07/19/GGWM-005-PERF--rendering-performance-profiling-fast-fills-fast-x-upload-drag-throttling/sources/cpu.prof
      Note: the before profile (12s drag stress)
    - Path: repo://ttmp/2026/07/19/GGWM-005-PERF--rendering-performance-profiling-fast-fills-fast-x-upload-drag-throttling/sources/cpu-after.prof
      Note: the after profile, same stress
ExternalSources: []
Summary: CPU-profile-driven analysis of the WM's paint path (user-reported slow workspace creation and laggy divider drags) and the five fixes — row-copy Fill, row-major BGRA conversion, per-frame buffer/pixmap caching with Expose-as-XPaint, motion coalescing with resized-only repaints, and a pprof entry point — cutting drag CPU 3.2× and boot time 2.2×.
LastUpdated: 2026-07-19T13:40:00-04:00
WhatFor: The record of why the paint path is shaped the way it now is, with the profiles that justify each decision.
WhenToUse: Read before touching paintFrame, draw.Fill, ToXImage, or drag handling; use the perf-test script to re-measure after changes.
---

# Paint-path performance analysis and fixes

## Executive summary

Two user-visible symptoms — multi-second workspace pre-creation at boot
and visibly laggy divider drags — turned out to be one problem: the
frame paint path did roughly ten times more work per paint than
necessary, and was invoked several times more often than necessary. A
12-second CPU profile under a scripted drag stress attributed 69% of
all samples to `paintFrame`. Five fixes later, the same stress costs
3.2× less CPU (7.74s → 2.43s of samples), boot with nine pre-created
workspaces dropped from 6.2s to 2.8s, and drags coalesce to the frame
rate instead of the X motion-event rate.

## How the numbers were obtained

The `wm` command now serves `net/http/pprof` when
`GO_GO_WM_PPROF=localhost:6060` is set (`pkg/cmds/wm.go`). The stress
harness (`scripts/` in this ticket: `perf-test.sh`) boots Xvfb + the
i3.js config, measures time-to-nine-workspaces over the control socket,
then captures `?seconds=12` of CPU profile while xdotool sweeps a
divider back and forth three times. Per-paint wall times come from a
debug-level duration log in `paintFrame`. Profiles and callgraph SVGs
for before and after live in this ticket's `sources/`.

## What the profile said (before)

| cost | where | why |
|---|---|---|
| 33% | `xgraphics.convertRGBA` | library conversion loop iterates **column-major**: every inner step jumps a full row stride; ~950k cache-missing pixel visits per full-frame paint |
| 26% | `draw.Fill` | per-pixel `img.SetRGBA` calls — the background fill alone touches every pixel through a function call |
| 27% (cum) | Expose handler | every `MoveResize` during a drag generates an Expose for a frame that was *just* painted → full second re-render |
| ~30% | GC (`gcDrain`, `scanObject`, `memclr`) | every paint allocated a fresh `image.RGBA` **and** a fresh `xgraphics.Image` — two ~3.8 MB garbage objects per paint |
| 60% (cum) | `dividerMotion` | every X motion event (hundreds/s) ran ratio-apply + relayout + full repaints of **all** visible panes |

Boot paid the same unit costs: workspace pre-creation switches to each
new workspace (an `AddWorkspace` semantic), painting a full-screen
launcher tile at 100–140ms per paint, ~30 times.

## Decision records

### P-D1 — Fill by row pattern + copy

`draw.Fill` writes the first row's 4-byte pattern once, then
`copy()`s it down the rectangle. Byte-identical output (golden tests
unchanged), removes Fill from the profile entirely. Same idea is
available for future hot primitives (`Stipple` was left alone — it is
not on any hot path).

### P-D2 — Own the RGBA→BGRA conversion, row-major

`draw.ToXImage` / `draw.CopyToXImage` (`pkg/draw/ximage.go`) walk both
`Pix` slices row-major with re-sliced bounds-checked windows, swapping
R/B. Same result as `xgraphics.NewConvert`, ~5× cheaper (2.57s → 0.54s
under the stress). All three upload sites (frames, bars, xapp windows)
switched. The remaining 22% conversion cost is the irreducible price of
X11's BGRA wire format at this abstraction level; going lower would
mean SHM or rendering directly in BGRA, both out of scope (see Open
questions).

### P-D3 — Frames cache their paint buffers and X pixmap

`frame` now holds `img *image.RGBA` and `ximg *xgraphics.Image`,
reused across paints and dropped on resize, unmap, and destroy
(`dropBuffers`, called on every teardown path — the same list that
learned `xevent.Detach` in GGWM-004). Consequences:

- GC largely disappears from the profile (no per-paint multi-MB
  garbage).
- `XSurfaceSet` (pixmap creation) happens only when the size changes.
- **Expose becomes one `XPaint`**: the frame's content already lives
  in its window's background pixmap, so the Expose handler re-blits
  instead of re-rendering — the drag path's duplicate full paint is
  gone. A frame without a current ximg still takes the full
  `paintFrame` path.
- Off-screen frames hold no buffers (`dropBuffers` in the relayout
  unmap branch), so nine workspaces do not pin nine full-screen
  buffer pairs.

### P-D4 — Drags: coalesce motion, repaint only what resized

X delivers pointer motion much faster than panes can paint. Divider
drags now (a) skip motion events closer than 16ms apart, with
`handleRelease` re-running the final position so nothing is lost, and
(b) call `relayoutResized`, which repaints only frames whose rect
actually changed. `relayout()` keeps its repaint-everything semantics —
theme swaps and workspace switches depend on it (T-D1's "swap then
repaint" contract).

### P-D5 — pprof is a standing capability, not a one-off patch

`GO_GO_WM_PPROF=addr` enables the profiler on any run. Perf work on a
WM is only credible against live input; keeping the hook means the
next regression gets a flamegraph in minutes.

## Results

| metric (same harness) | before | after |
|---|---|---|
| CPU samples, 12s drag stress | 7.74s | 2.43s |
| boot → 9 workspaces (i3.js) | 6.18s | 2.78s |
| hottest node | convertRGBA 33% | CopyToXImage 22% |
| GC share | ~30% | ~8% |
| Expose re-renders during drag | full re-render | one XPaint |

Also folded into this ticket (adjacent input-path work, same commit):
click-to-focus via a synchronous passive button grab on clients with
`ReplayPointer` (before, only the title strip focused a tile) —
verified by clicking into client areas and asserting focus over the
control socket.

## Testing and validation

- `go test ./...` — the Fill rewrite is covered by the existing draw
  golden tests (byte-identical), the theme tests, and uispec renders.
- `scripts/examples-smoke.sh` (9 fixtures) and `scripts/rc-smoke.sh`
  pass. rc-smoke now retries its keypress: the first synthetic press
  after Xvfb boot can be swallowed while the keymap settles, which is
  a harness artifact, not a WM bug.
- Before/after profiles archived; re-run `perf-test.sh` after any
  paint-path change and compare `-top` output.

## Risks and open questions

- `CopyToXImage` assumes the xgraphics BGRA layout; a compositor or
  depth change would need a second path (NewConvert remains the
  fallback semantics).
- Remaining boot cost is dominated by first-paint pixmap uploads for
  frames nobody sees mid-boot; batching workspace pre-creation behind
  a single deferred relayout is the next win if it matters.
- XSHM (shared-memory PutImage) would remove the socket copy for
  full-frame uploads; not worth the complexity until a real display
  shows lag.
- The 16ms drag throttle is wall-clock; a compositor-driven frame
  callback would be more principled but X core has none.
