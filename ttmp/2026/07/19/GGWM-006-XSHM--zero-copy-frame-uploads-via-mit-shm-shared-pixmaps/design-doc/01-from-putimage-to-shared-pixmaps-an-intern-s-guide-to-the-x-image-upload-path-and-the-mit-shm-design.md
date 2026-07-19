---
Title: From PutImage to shared pixmaps — an intern's guide to the X image upload path and the MIT-SHM design
Ticket: GGWM-006-XSHM
Status: active
Topics:
    - wm
    - performance
DocType: design-doc
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://pkg/xshm/xshm.go
      Note: the shared-memory surface this design specifies
    - Path: repo://pkg/wmx11/manage.go
      Note: paintFrame, the integration point
    - Path: repo://pkg/draw/ximage.go
      Note: the conversion layer that gains a write-into-shm variant
ExternalSources:
    - "MIT-SHM extension specification (X.Org): https://www.x.org/releases/current/doc/xextproto/shm.html"
Summary: An intern-level guide to what "image upload" means in X11 — the PutImage copy chain from Go memory through the socket into a server pixmap — and the design for removing it with MIT-SHM shared pixmaps, where client and server address the same physical memory; covers the SysV shm plumbing, the xgb/shm API, capability detection and fallback, coherence caveats, and the phased implementation plan.
LastUpdated: 2026-07-19T14:20:00-04:00
WhatFor: Understanding and implementing the zero-copy frame upload path; the reference for pkg/xshm.
WhenToUse: Read after the GGWM-005 guide (the pipeline this optimizes); before touching pkg/xshm or the paintFrame upload branch.
---

# From PutImage to shared pixmaps

This guide answers one question precisely — *what actually happens when
the WM "uploads" a frame image, and why shared memory makes most of it
disappear* — and then designs the change. GGWM-005 made rendering and
conversion cheap; what remains in every profile is `memmove` and
syscalls under `XDraw`. That residue is the upload. To remove it you
must first understand it.

## Part I — What "image upload" means

X11 is a client/server protocol. The WM is a client; the thing that
owns your screen is the X server, a separate process. When the WM
renders a title strip into a Go `image.RGBA`, those bytes live in the
WM's heap. The server cannot see the WM's heap. Pixels must therefore
*travel*, and in the core protocol they travel exactly one way: the
`PutImage` request, pixel data serialized onto the connection like any
other protocol message.

Follow one full-screen frame (1272×744 ≈ 3.8 MB) through today's path
(`paintFrame` → `ximg.XDraw()`):

```
WM heap                       kernel                        X server
--------                      ------                        --------
frame.img  (RGBA)
   │ CopyToXImage (R/B swap)
frame.ximg (BGRA)  ──write()──►  socket buffer  ──read()──►  request buffer
                                                                │ copy
                                                             pixmap (server memory)
                                                                │ ClearArea/expose
                                                             window → screen
```

Count the traversals of those 3.8 MB: (1) the conversion write into
`ximg.Pix`, (2) the `write()` into the kernel socket buffer, (3) the
server's `read()` out of it, (4) the server's copy into the pixmap.
Four passes, two of them through the kernel with syscall overhead —
and `xgb` additionally chunks big images into multiple `PutImage`
requests because a single X request has a length limit (that is the
`Syscall6`/`memmove` cluster in the GGWM-005 "after" profile). The
upload is pure bookkeeping: no pixel is *computed*, they are only
moved.

The insight behind MIT-SHM: client and server are usually **the same
machine**. Two local processes do not need a byte stream to share
3.8 MB; they can map the same physical memory and stop moving bytes
altogether.

## Part II — The MIT-SHM extension, precisely

MIT-SHM ("the X11 shared memory extension") lets a client and the
server attach the same System V shared memory segment. It offers two
levels:

1. **`ShmPutImage`** — like `PutImage`, but the request carries only a
   segment id and offset; the server copies pixels *out of shared
   memory* into the pixmap. Traversals drop from four to two
   (conversion write + server copy). The request itself is ~40 bytes.
2. **Shared pixmaps** (`ShmCreatePixmap`) — the pixmap *is* the shared
   segment. The server does not copy at all; compositing reads
   straight from the memory the client wrote. Traversals drop to one:
   the conversion write. This is the zero-copy level, advertised by
   the server in `ShmQueryVersion`'s `SharedPixmaps` flag (and only
   valid for ZPixmap format).

The SysV plumbing, in order, with the exact APIs available to us
(`golang.org/x/sys/unix`, `github.com/jezek/xgb/shm`):

```
shmid  := unix.SysvShmGet(IPC_PRIVATE, w*h*4, IPC_CREAT|0600)   // kernel segment
data   := unix.SysvShmAttach(shmid, 0, 0)                        // map into our heap: []byte
seg    := <new X id>; shm.Attach(conn, seg, shmid, false)        // server maps it too
_      = unix.SysvShmCtl(shmid, IPC_RMID, nil)                   // mark for deletion NOW —
                                                                 // it lives until both detach,
                                                                 // and can't leak if we crash
pid    := <new X id>; shm.CreatePixmap(conn, pid, drawable, w, h, depth, seg, 0)
```

The `IPC_RMID`-immediately idiom deserves a sentence: SysV segments
survive process death (they are kernel objects, not file descriptors),
and a crashing WM would otherwise strand 3.8 MB segments until reboot.
Marking for deletion right after both sides attach means the kernel
reclaims the segment as soon as the attachments drop, whatever the
exit path. `ipcs -m` shows the segments while running; after any
crash it must show none.

## Part III — Design

### D1 — A `pkg/xshm.Surface` owning the whole lifecycle

One type encapsulates segment + mapping + server attach + pixmap:

```go
type Surface struct {
    X      *xgbutil.XUtil
    Seg    shm.Seg          // server-side segment id
    Pixmap xproto.Pixmap    // the shared pixmap (zero-copy target)
    Data   []byte           // our mapping, w*h*4 BGRA
    W, H   int
}

func Available(X) bool                  // extension present + SharedPixmaps
func New(X, drawable, w, h) (*Surface, error)
func (s *Surface) WriteRGBA(img *image.RGBA)  // R/B-swapped copy into Data
func (s *Surface) Destroy()             // FreePixmap, shm.Detach, shmdt
```

`Available` is computed once per connection (`shm.Init` +
`QueryVersion`): false on remote displays, forwarded connections, or
servers without shared pixmaps — anywhere the WM must fall back.

### D2 — Integration: the pixmap is the frame's background, again

GGWM-005 already made frame content live in the window's *background
pixmap* (`XSurfaceSet`), with Expose handled by a server-side blit.
The shared pixmap slots into exactly that seam:

```
paintFrame:
    render into frame.img (unchanged)
    if frame.surf exists and matches size:
        surf.WriteRGBA(img)                  // the only pixel pass
        ClearArea(frame window)              // server re-blits background
    else if xshm available:
        create surf; ChangeWindowAttributes(bg-pixmap = surf.Pixmap); as above
    else:
        existing ximg path (XDraw/XPaint)    // the fallback, byte-identical
```

The Expose handler needs no change at all: a window whose background
pixmap is current is repaired by the server, and the existing
size-check branch keeps working for both surface kinds.

### D3 — Coherence: accept the benign race, on the WM's terms

With a shared pixmap there is no synchronization between our writes
and the server's reads; a compositor-less X server reads the pixmap
when it repaints the window. Writing while it reads could show a torn
frame for one refresh. The WM's discipline makes this acceptable:
paints happen on the WM loop, complete in single-digit milliseconds,
and are immediately followed by `ClearArea` — the same benign-race
posture as the theme palette (GGWM-004 T-D1). If tearing is ever
observed, the escalation is `ShmPutImage` with completion events
(level 1), not locks.

### D4 — Fallback is the same code, not a degraded mode

The ximg path stays exactly as it is and remains what tests exercise
by default (Xvfb supports SHM, so live smoke covers the new path; unit
tests never talk to X at all). `GO_GO_WM_NO_SHM=1` forces the fallback
for debugging — when a rendering bug appears, flipping the switch
answers "is it the shm path?" in one restart.

### What this buys, in numbers

Per full-frame paint today (post-GGWM-005): one conversion pass
(~4 ms), one socket write of 3.8 MB, one server read, one server copy,
plus chunking syscalls. With shared pixmaps: the conversion pass
writes directly into server-visible memory and everything else
disappears — first paints stop paying the multi-copy toll entirely,
which is most of what remains of workspace-creation cost (76 ms CPU
per workspace, dominated by exactly these copies).

## Part IV — Implementation plan

- **X1** — `pkg/xshm`: `Available`, `Surface`, lifecycle exactly as
  Part II's sequence (IPC_RMID immediately after attach); `WriteRGBA`
  reuses the row-major R/B-swap loop shape from `draw.CopyToXImage`.
- **X2** — `wmx11`: `frame.surf *xshm.Surface`; the paintFrame branch
  from D2; `dropBuffers` destroys the surface (it already runs on
  every teardown path); one boot log line saying which upload path is
  active.
- **X3** — Verification: pixel-assert screenshots on Xvfb (identical
  content through both paths, `GO_GO_WM_NO_SHM` as the control);
  `ipcs -m` clean after WM exit *and* after `kill -9`; both smoke
  suites; `ws-cpu.sh` and `perf-test.sh` before/after CPU numbers.

## Risks and open questions

- Depth mismatches: the pixmap depth must equal the window depth (24
  on our 32-bpp visuals). `New` validates against the screen's root
  depth and refuses otherwise (falls back).
- Some servers advertise SHM but not `SharedPixmaps` (common with
  glamor-accelerated Xorg). Level 1 (`ShmPutImage`) would still help
  there; deferred until such a server actually appears in use — the
  fallback keeps correctness everywhere.
- `xgb`'s `AttachFd` (POSIX fd-passing, no SysV ids) is the modern
  variant; SysV chosen because it needs no fd-passing support in the
  xgb connection and the RMID idiom handles cleanup equally well.
- Bars and xapp windows still use the ximg path — smaller surfaces,
  lower payoff; promote them only if a profile ever says so.
