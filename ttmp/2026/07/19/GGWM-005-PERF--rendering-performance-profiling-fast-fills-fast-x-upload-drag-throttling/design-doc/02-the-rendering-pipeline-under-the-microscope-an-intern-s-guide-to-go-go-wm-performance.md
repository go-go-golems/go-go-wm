---
Title: The rendering pipeline under the microscope — an intern's guide to go-go-wm performance
Ticket: GGWM-005-PERF
Status: active
Topics:
    - wm
    - performance
DocType: design-doc
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://pkg/wmx11/manage.go
      Note: paintFrame — the function this whole guide orbits
    - Path: repo://pkg/draw/ximage.go
      Note: the conversion layer studied in Part III
    - Path: repo://pkg/wmx11/input.go
      Note: the drag path studied in Part III
    - Path: repo://pkg/cmds/wm.go
      Note: the pprof entry point used in Part II
ExternalSources: []
Summary: A ground-up guide to how go-go-wm puts pixels on screen and where the time goes — the software-rendering pipeline, the X11 image and expose model, how to profile the WM under real input, a line-by-line study of the five optimizations from the GGWM-005 profile, and the working rules that keep the paint path fast.
LastUpdated: 2026-07-19T13:55:00-04:00
WhatFor: Onboarding for performance work — everything needed to understand, measure, and safely change the WM's rendering and input paths.
WhenToUse: Read with the GGWM-004 intern guide (system overview); this one goes deep on the paint pipeline only.
---

# The rendering pipeline under the microscope

This guide teaches the part of go-go-wm that turns state into pixels,
and the craft of making it fast. It exists because two innocuous
symptoms — a slow boot and a laggy divider drag — both traced to the
same twenty lines of paint code, and the path from "feels slow" to
"3.2× less CPU" is worth learning as a method, not just as a diff.
The companion GGWM-004 guide covers the whole system; here we assume
you know what the WM loop, frames, and ops are, and go deep on one
pipeline.

## Part I — How a frame becomes pixels

go-go-wm renders in software. There is no GPU, no compositor, no
toolkit: every title strip, bar, and builtin tile is an `image.RGBA`
filled by plain Go code and uploaded to the X server. The pipeline for
one frame paint (`pkg/wmx11/manage.go:paintFrame`) is five stages:

```
render          convert            upload           bind            show
image.RGBA  →   BGRA byte order →  PutImage over →  window bg    →  clear window
(pkg/draw)      (xgraphics.Image)  the socket       pixmap          (server blits)
```

- **Render.** `draw.Fill` paints the pane background, `TitleStrip.
  Render` draws the strip, `paintBuiltin` draws WM-rendered content
  (trace rows, launcher text), `draw.Border` strokes the edge. All of
  it is CPU writing bytes into one `Pix []uint8`.
- **Convert.** X11's 24/32-bit visuals want BGRA byte order;
  `image.RGBA` stores RGBA. Something must swap every pixel's R and B.
- **Upload.** `XDraw` sends the converted bytes to the server with
  PutImage requests — a memory copy onto a Unix socket.
- **Bind.** `XSurfaceSet` attaches the server-side pixmap as the frame
  window's *background pixmap*. This matters later: a window whose
  background is a pixmap can be repainted by the server alone.
- **Show.** `XPaint` clears the window, which makes the server blit
  the background pixmap into it. No client bytes move.

The sizes involved explain everything that follows. A full-screen
frame at 1272×744 is ~950,000 pixels ≈ 3.8 MB of RGBA. Render touches
all of it, convert touches all of it twice (read + write), upload
copies all of it to a socket. Do that a few times per second and it is
fine; do it per motion event, twice per paint, with two 3.8 MB
allocations each time, and you have the profile this ticket opened
with.

## Part II — Measuring: pprof against live input

Feelings about performance are hypotheses; profiles are evidence. The
WM serves Go's standard profiler when asked:

```bash
GO_GO_WM_PPROF=localhost:6060 go-go-wm wm --display :84 ... &
curl -o cpu.prof "http://localhost:6060/debug/pprof/profile?seconds=12" &
# ...drive real input while the profile records:
xdotool mousemove 640 400 mousedown 1
for x in $(seq 500 8 800); do xdotool mousemove $x 400; done
xdotool mouseup 1

go tool pprof -top  go-go-wm cpu.prof        # flat: where cycles burn
go tool pprof -top -cum go-go-wm cpu.prof    # cumulative: which paths
go tool pprof -http :8000 go-go-wm cpu.prof  # flamegraph in a browser
```

Three habits make the numbers trustworthy:

- **Profile under scripted input.** A WM idles unless driven; xdotool
  sweeps are reproducible, so before/after comparisons mean something.
  The full harness is `perf-test.sh` in this ticket's `scripts/`.
- **Read flat and cumulative separately.** Flat told us `convertRGBA`
  and `SetRGBA` burned the cycles; cumulative told us `dividerMotion`
  and the Expose handler were the paths delivering them. You need both
  to know *what* to fix and *where* it is called from.
- **Keep wall-clock counters too.** A debug-level duration log in
  `paintFrame` gives per-paint milliseconds; the boot harness measures
  time-to-nine-workspaces over the control socket. Profiles explain
  ratios; timers prove the user-visible symptom moved.

The GGWM-005 before-profile, in one table:

| share | node | meaning |
|---|---|---|
| 33% | `xgraphics.convertRGBA` | pixel-format conversion |
| 26% | `draw.Fill` | background fills |
| ~30% | GC nodes | per-paint allocations |
| 27% cum | Expose handler | duplicate repaints |
| 60% cum | `dividerMotion` | the drag path overall |

## Part III — The five fixes, as worked examples

Each fix below is a general pattern wearing a specific diff. Learn the
pattern; the diff is just this codebase's instance of it.

### 1. Memory-order matters: the column-major conversion

`xgraphics.NewConvert`'s loop iterates `for x { for y { ... } }` —
column-major over a row-major buffer. Every inner step jumps a full
row stride (~5 KB), so nearly every pixel visit misses cache. The
replacement (`pkg/draw/ximage.go`) walks both buffers row-major over
re-sliced windows:

```go
for y := range rows {
    src := img.Pix[so : so+w*4 : so+w*4]   // one row, bounds-checked once
    dst := ximg.Pix[do : do+w*4 : do+w*4]
    for i := 0; i < w*4; i += 4 {
        dst[i+0], dst[i+1], dst[i+2], dst[i+3] = src[i+2], src[i+1], src[i+0], src[i+3]
    }
}
```

Same bytes out, ~5× less time (2.57s → 0.54s under the stress).
**Rule: in any per-pixel loop, the inner index must move along memory,
and slice windows should be hoisted so the bounds check runs per row,
not per pixel.**

### 2. Don't call a function per pixel: the row-copy Fill

`draw.Fill` called `img.SetRGBA(x, y, c)` per pixel — a color-model
dance and offset computation, ~950k times per background. Now it
writes the 4-byte pattern across the first row once and `copy()`s that
row down the rectangle. `copy` is `memmove`; the golden-image tests
prove the output is byte-identical. **Rule: fills and blits are
`copy`/`memmove` jobs, never per-pixel call sites.**

### 3. Reuse buffers the GC was eating

Every paint allocated a fresh `image.RGBA` and a fresh
`xgraphics.Image` — 7.6 MB of garbage per paint, and the GC's mark
phase showed up as ~30% of the profile. Frames now own both buffers
(`frame.img`, `frame.ximg`), reallocating only when the size changes,
and dropping them when the frame leaves the screen (`dropBuffers` on
unmap/evict/destroy — the same teardown list that carries
`xevent.Detach`). **Rule: per-event allocations sized in megabytes are
a GC tax on every future frame; cache them keyed by size, and free
them when the surface goes invisible so memory doesn't scale with
hidden state.**

### 4. Let the server repaint: Expose as XPaint

Because `XSurfaceSet` makes the pixel content the window's background
pixmap, the server can restore a damaged window without asking us.
The old Expose handler re-ran the whole pipeline; during a drag every
`MoveResize` generated an Expose for a frame painted microseconds
earlier — a full duplicate paint, 27% of cumulative samples. The
handler now checks whether the cached ximg still matches the frame
size and, if so, issues a single `XPaint` (a server-side blit). The
full render path remains the fallback for frames with no current
buffer. **Rule: know which side of the wire owns the pixels; if the
server already has them, damage repair should not involve the
client.**

### 5. Coalesce input that outruns output

X delivers pointer motion at hundreds of events per second; a pane
repaint takes milliseconds. Painting per event means the queue only
drains as fast as painting allows — lag that *grows* while you drag.
The divider path now skips motion events closer than 16ms apart
(`dragState.lastPaint`), and `handleRelease` re-runs the final
position so the divider never lands short. Alongside, the drag calls
`relayoutResized`, which repaints only frames whose rect changed —
`relayout()` keeps its repaint-everything meaning because theme swaps
and workspace switches rely on it. **Rule: for continuous input, paint
the latest state at your own cadence; never paint every input event.
And keep "repaint what changed" and "repaint everything" as separate,
explicitly-named entry points.**

## Part IV — Why boot was slow, and what remains

Workspace pre-creation (i3.js creates workspaces 1–9 at boot) was slow
by multiplication, not by any single sin: `AddWorkspace` switches to
the new workspace (prototype semantics), each switch built and painted
a full-screen launcher tile at the old per-paint cost, and the rename
that follows repainted again. Nine workspaces ≈ thirty full-screen
paints ≈ four seconds. The per-paint fixes cut boot from 6.2s to 2.8s
without touching the semantics.

What remains, if boot ever matters again, in order of honesty:

- Most remaining paints are for workspaces nobody sees mid-boot; a
  batched pre-creation (defer relayout until the loop of ops ends)
  would remove them. It needs a WM-side notion of "op batch", which is
  a real design change — hence deferred.
- First paints pay a pixmap create + full upload; XSHM would cut the
  socket copy. Complexity says wait for a real display to show lag.
- The rc runtime boot (~1.3s of goja + broker setup) is untouched by
  any of this and bounds how fast boot can get.

## Part V — Working rules for the paint path

- Measure with `GO_GO_WM_PPROF` + scripted xdotool input before and
  after; archive the profiles in the ticket (`sources/cpu*.prof`).
- The inner loop of anything per-pixel moves along memory, hoists its
  slice windows, and never calls a method per pixel.
- No allocations sized by screen area inside paint paths; buffers are
  cached on the frame and dropped on invisibility.
- Expose handlers re-blit; they re-render only when the cache is
  stale.
- Input events are coalesced to paint cadence; releases apply final
  state.
- `relayout()` means "make the screen match the model, fully";
  `relayoutResized()` means "geometry changed, paint the deltas".
  Don't blur them — the theme engine and workspace switching depend
  on the strong one.
- Golden tests are the license to rewrite primitives: `draw.Fill`
  changed implementation entirely and no test needed touching. Keep
  primitives golden-tested so the next optimization is equally safe.

## Related

- design-doc/01 — the decision records and before/after numbers this
  guide teaches from.
- GGWM-004 design-doc/02 — the whole-system intern guide (Part IV
  covers the theme engine whose repaint contract constrains
  `relayout`).
- reference/01 — the diary, including the false leads.
