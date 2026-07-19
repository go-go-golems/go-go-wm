---
Title: Implementation diary
Ticket: GGWM-006-XSHM
Status: active
Topics:
    - wm
    - performance
DocType: reference
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://pkg/xshm/xshm.go
      Note: the Surface built in entry 1
    - Path: repo://pkg/wmx11/manage.go
      Note: the paintFrame integration
ExternalSources: []
Summary: Diary of the MIT-SHM shared-pixmap implementation — the Surface lifecycle, the paintFrame integration with fallback, and the verification results (pixel-identical output, zero leaked segments after kill -9, 55ms CPU per workspace, boot 0.38s).
LastUpdated: 2026-07-19T14:35:00-04:00
WhatFor: The record of building and verifying the zero-copy upload path.
WhenToUse: With design-doc/01 (the guide + design this implements).
---

# Implementation diary

## Goal

Implement design-doc/01: replace the PutImage copy chain with MIT-SHM
shared pixmaps, with a clean fallback, and prove correctness and the
win.

## Entry 1 — 2026-07-19: X1–X3 in one pass

### What was done

- `pkg/xshm`: `Available` (shm.Init + QueryVersion SharedPixmaps,
  cached per connection, `GO_GO_WM_NO_SHM` kill-switch), `Surface`
  (SysvShmGet → SysvShmAttach → shm.Attach → **IPC_RMID immediately**
  → shm.CreatePixmap), `WriteRGBA` (row-major R/B swap into the shared
  mapping, same loop shape as draw.CopyToXImage), `Destroy`
  (FreePixmap, shm.Detach, shmdt).
- `wmx11`: `frame.surf`; paintFrame prefers the surface (create on
  size change, set as the window background pixmap, WriteRGBA,
  ClearAll), falls back to the cached ximg path unchanged; the Expose
  handler does *nothing* for a current surface (the server repaints
  exposed regions from the background pixmap before the event is even
  sent); `dropBuffers` resets the background to a plain pixel before
  destroying the surface — the background-pixmap attribute holds a
  server-side reference that would otherwise keep the shm pages alive.
- Boot log line: `frame upload path shared_pixmaps=true|false`.

### Verification (shm-test.sh, Xvfb :86)

- `shared_pixmaps=true` on Xvfb; with `GO_GO_WM_NO_SHM=1` the same
  scene renders through the fallback and the two root screenshots are
  **pixel-identical** (ImageChops diff bbox = None).
- Segment hygiene: large segments visible in `ipcs -m` while running;
  after `kill -9` the only survivor predated the test by a day
  (another app's). The RMID-immediately idiom holds: zero WM segments
  leak on any exit path.
- Both smoke suites pass (9/9 examples + rc-smoke).

### Numbers (ws-cpu.sh / perf-test.sh; load ~8.6 after the ollama
renice, so wall numbers are finally clean)

- Workspace creation: **55ms CPU each** (76ms before shm; 344ms before
  the copyImage fix; ~600ms at GGWM-005's start). Burst profile is now
  WriteRGBA 36% + memmove + font rendering — the upload syscall
  cluster is gone.
- Boot to nine workspaces: **0.38s** (6.18s at the start of GGWM-005).
- 12s drag stress: 2.06s samples (2.43s before); WriteRGBA is now 53%
  of what remains — the single irreducible pixel pass. Next win, if
  ever needed, is rendering directly in BGRA to delete that pass too.

### What was tricky

- **The background-pixmap reference.** FreePixmap does not free a
  pixmap that is still a window's background; without resetting the
  attribute first, dropBuffers would leak the shm pages server-side
  for every unmapped workspace — invisible to Go tooling, visible only
  in the X server's memory. This is the shm sibling of the GGWM-004
  lesson: every resource teardown list must be complete, and
  "reference" can mean an X-side attribute, not just a Go pointer.
- **kill -9 testing needed identity checks.** The first leak check
  counted all large segments system-wide and "found" a leak that was a
  day-old browser segment; `ipcs -m -i` (cpid, att_time) attributed
  it. Assert ownership, not counts.

### Code review instructions

Read `pkg/xshm/xshm.go` top to bottom against Part II of
design-doc/01 (the lifecycle order and the RMID comment are the
contract), then the paintFrame upload branch (`manage.go`) checking
the three-way fallback, then `dropBuffers` (background reset before
Destroy), then the Expose switch in `events.go`. Verify with
`shm-test.sh` (pixel diff + kill -9) and `ipcs -m` afterwards.

## Related

- design-doc/01 — the guide and design this implements.
- GGWM-005 — the profiling rounds that motivated it.
