---
Title: 'go-go-wm Performance Engineering: An Intern''s Guide to the Resize and Render Path'
Ticket: GGWM-012-GUIDES
Status: active
Topics:
    - wm
    - performance
    - x11
DocType: design-doc
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://examples/scripts/i3.js
      Note: The i3 config port — a realistic workload for the resize benchmark harness
    - Path: repo://pkg/apps/xapp/xapp.go
      Note: Uncached, shm-less upload path per redraw; unbounded goroutine post fallback
    - Path: repo://pkg/draw/ximage.go
      Note: Row-major, parallelized RGBA to BGRA conversion (already optimized)
    - Path: repo://pkg/wmcore/layout.go
      Note: Layout recomputes the full tree and allocates a fresh map on every call
    - Path: repo://pkg/wmcore/tree.go
      Note: Find is O(n) DFS and is called inside reconciliation loops, making relayout O(n^2)
    - Path: repo://pkg/wmx11/divider.go
      Note: syncDividers repaints every divider unconditionally via the uncached blit path
    - Path: repo://pkg/wmx11/events.go
      Note: The Expose fast path — the model for cache-first repair
    - Path: repo://pkg/wmx11/focus_state.go
      Note: focusState/fullscreenState — the encapsulation pattern the resize controller should copy; documents why no mutex is needed
    - Path: repo://pkg/wmx11/input.go
      Note: dividerMotion 16ms gate, double Layout per tick, durable OpSetRatio; unthrottled gripMotion; exact release replay at :392
    - Path: repo://pkg/wmx11/manage.go
      Note: relayoutPaint (the sole reconciler) and paintFrame (full-pane paint + per-tick buffer churn); the ConfigureRequest full-relayout at :593
    - Path: repo://pkg/wmx11/wm.go
      Note: The single-owner event loop (:236-281), 256-slot blocking ops queue, ApplyBatch coalescing, afterOp paintAll relayout
    - Path: repo://pkg/xshm/xshm.go
      Note: xshm.New performs two checked X round trips (:92, :106) recreated on every resize tick — the central finding
ExternalSources: []
Summary: Evidence-backed architecture walkthrough and phased performance implementation plan, written for a new engineer.
LastUpdated: 2026-07-21T23:06:19.16841745-04:00
WhatFor: ""
WhenToUse: ""
---


# go-go-wm Performance Engineering
## An Intern's Guide to the Resize and Render Path

---

## How to read this document

You are about to work on a window manager written in Go that also wants to be a programmable, presentation-oriented desktop. That is two systems, and they meet at a narrow boundary. This guide teaches you the first system — the part that owns X11 geometry, focus, and pixels — because that is where the performance problems live, and because you cannot safely change the second system until you understand the first.

The document is organized so you can stop at natural points:

1. **Part I** is a primer on what an X11 window manager actually is. Read this even if you have used i3 for years. The specific facts that matter for performance — what causes a round trip, why reparenting splits geometry into two rectangles, how many bytes a 1080p frame is — are not obvious from using a WM.
2. **Part II** maps the code as it exists today, with `file.go:LINE` anchors for everything. Every claim here was read out of the source at the current commit.
3. **Part III** traces one divider drag end to end and builds a cost model from it. This is the core of the document.
4. **Part IV** is the gap analysis: what three external review guides recommended, versus what the code already does. A surprising amount is already done. Do not redo it.
5. **Part V** proposes the target architecture, with decision records.
6. **Part VI** is the phased implementation plan, at file granularity, with exit criteria you can test.
7. **Part VII** covers measurement and testing — which comes *first* in practice, even though it appears late here.
8. **Part VIII–IX** are risks, onboarding labs, and appendices.

**A convention used throughout:** claims are marked **OBSERVED** when they were read directly out of the source at the current commit, and **INFERRED** when they are a deduction that has not been measured. Nothing in this document is a benchmark result — there are no benchmarks in the repository yet. Building them is Phase 0.

### Where the material came from

This guide synthesizes three externally authored review documents, imported into this ticket under `sources/local/`:

| Source | Length | Emphasis |
|---|---|---|
| `go-go-wm-handbook.md` | ~3,220 lines | Latency budgets, cost model, resize scheduler, appendices with metric names |
| `go-go-wm_engineering_handbook.md` | ~3,000 lines | Preview-vs-commit resize, runtime supervision, phased roadmap, 7 ADRs |
| `go-go-wm-engineering-guide.md` | ~2,458 lines | Transaction stages, chrome/content separation, R0–R7 plan, benchmark spec |

All three were written against merge commit `5b73c9f37c97538f6767ecdc3ece4fb599932377`. They agree on the diagnosis and largely agree on the fix. Where this guide differs from them, it is because the live code was checked and found to already implement — or to differ from — what they describe. Those differences are called out explicitly in Part IV.

---

## Executive summary

**The diagnosis.** go-go-wm's interactive resize is not slow because Go is unsuitable, because software rendering is inherently too slow, or because the binary split tree is complex. It is slow because of **work amplification**: one accepted pointer sample during a divider drag currently triggers a durable model mutation, two complete workspace layout passes, a reconciliation loop that is quadratic in tree size, unconditional map/unmap requests for every frame in the process, an unconditional repaint of every divider through an uncached upload path, a full-pane RGBA repaint of each resized frame, and — because the pane's dimensions changed — a teardown and recreation of the MIT-SHM shared pixmap that contains **two synchronous X round trips**. The resized client then independently re-renders its entire interior.

**The single most important finding.** The three source guides all assert that the drag loop is free of synchronous round trips. It is not. `xshm.New` issues `shm.AttachChecked(...).Check()` (`pkg/xshm/xshm.go:92`) and `shm.CreatePixmapChecked(...).Check()` (`pkg/xshm/xshm.go:106-107`). A *checked* XCB request is a round trip. Because `paintFrame` destroys and recreates the surface whenever the pane's dimensions change (`pkg/wmx11/manage.go:420-432`), and because a divider drag changes dimensions on *every* tick, **each resized pane costs two blocking round trips plus a `shmget`/`shmat`/`IPC_RMID` syscall triple, per motion tick.** This is invisible to the obvious `grep '\.Reply()'` audit, which is why three independent reviews missed it. **INFERRED:** this is the dominant per-tick latency on a fast local machine.

**What is already correct — do not redo it.** A meaningful amount of the work the guides recommend has already landed under GGWM-005 and GGWM-006. There is a 16 ms motion gate with exact final-position replay on release (`pkg/wmx11/input.go:333`, `input.go:392-395`). There *is* an applied-geometry cache — `f.rect` is diffed before any `MoveResize` (`manage.go:332`). There is a resize-only repaint mode (`manage.go:311-314`). Expose is repaired server-side from background pixmaps without re-rendering (`events.go:17-34`). RGBA→BGRA conversion is row-major and parallelized (`pkg/draw/ximage.go:56-108`). `ApplyBatch` already coalesces a burst into one reconciliation (`wm.go:360-401`). And the concurrency architecture is genuinely sound: **there is not a single mutex in `pkg/wmx11`**, because one goroutine owns all X-facing state, and JavaScript provably never runs on it. Part IV tabulates all of this.

**The recommended sequence.** In priority order, by ratio of impact to risk:

1. **Measure first.** There are exactly two timing probes in the entire tree and zero benchmarks. Nothing else on this list should be merged without before/after evidence.
2. **Stop destroying and recreating the shm surface on every tick.** Capacity-based buffers, or suppressing content paint entirely during drag, removes two round trips and a syscall triple per pane per tick.
3. **Stop repainting every divider on every relayout.** `syncDividers` calls `paintDivider` unconditionally (`divider.go:60`), on the *uncached* blit path that allocates an image, creates a server pixmap, uploads, and frees — per divider, per tick.
4. **Compute the layout once per tick, not twice** (`input.go:342` and again via `manage.go:321`), and remove the `Find`-in-loop that makes reconciliation O(n²) (`manage.go:325`, `divider.go:41`).
5. **Separate chrome from content.** The title strip is drawn *into* the full-pane surface (`manage.go:381-413`), so a focus change repaints two entire panes to update 22 pixels of height. A title child window makes decoration cost scale with the title, not the pane.
6. **Make preview state transient.** One durable `OpSetRatio` per completed drag instead of one per tick.

**What this document deliberately does not solve.** It does not design the retained widget tree, the runtime supervisor, the capability system, the PBUI type registry, or the REPL control plane. Those are the subject of the source guides' later parts and are correctly sequenced *after* the work here. It also does not propose a compositor or GPU backend: the bottleneck is unnecessary work and resource churn, not the absence of GPU drawing.

---

## Problem statement and scope

### In scope

- The interactive divider-resize path, end to end, from `MotionNotify` to visible pixels.
- The reconciliation function that turns a layout into X requests.
- The rendering and upload path: `image.RGBA` → BGRA conversion → MIT-SHM shared pixmap or `PutImage` fallback.
- Buffer and X resource lifetime across geometry changes.
- The measurement and test infrastructure needed to make any of the above verifiable.

### Out of scope (deliberately deferred)

- The retained widget tree that would replace `pkg/apps/uispec`'s flat row/segment IR.
- Runtime supervision, capability manifests, and hot reload for JavaScript runtimes.
- The PBUI type registry, translators, and object handles.
- Multi-monitor / RandR work.
- Any display backend other than X11.

These are not unimportant — they are the substance of the source guides' Parts IV and V. They are out of scope here because the resize path is the current user-visible defect and because several of them (notably the retained widget tree) are much cheaper to build *after* the chrome/content split in Part V exists.

### The definition of success

A divider drag is fast when three things are true simultaneously:

1. The visible divider stays close to the pointer.
2. The event loop stays responsive to release, cancel, key, map, and unmap events *during* the drag.
3. The final committed geometry is exact.

Point 3 is already true (`input.go:392-395` replays the release coordinates). Points 1 and 2 are what this document is about.

---

# Part I. What an X11 window manager actually is

## 1.1 The window manager is a client with one exceptional privilege

The X server does not know what a "window manager" is. A WM is an ordinary X client that has done one special thing: it selected the `SubstructureRedirect` event mask on the root window. Only one client can hold that selection at a time; a second attempt gets an error, which is why "another window manager is already running" is a distinct failure mode.

In go-go-wm this happens in `becomeWM` (`pkg/wmx11/wm.go:285-306`), which selects `SubstructureRedirect | SubstructureNotify | ButtonPress | FocusChange`.

Once selected, client requests to map, move, resize, raise, or lower a top-level window are not executed by the server. They are **redirected** to the WM as events. The WM decides what actually happens. This is the whole mechanism: the WM's authority is the authority to intercept and reinterpret.

The architectural consequence is the rule that governs everything else in this document:

> **X11 is an external state machine. The window manager owns a model of intended desktop state, and continuously reconciles the X server's state to that model.**

Anything that treats an X call as "do this now, synchronously, and tell me it worked" is fighting the model.

## 1.2 Reparenting splits geometry into two rectangles

go-go-wm is a *reparenting* WM. When a client asks to be mapped, the WM creates its own **frame** window, adds the client to the X *save set*, reparents the client inside the frame, and maps both.

The save set matters: it is added *before* reparenting (`pkg/wmx11/manage.go`, the `manage` path) so that if the WM dies unexpectedly, the server reparents clients back to an ancestor instead of destroying them along with their frames.

After reparenting there are two rectangles, and confusing them is a classic source of bugs:

- The **frame rectangle**, in root coordinates.
- The **client rectangle**, in frame-local coordinates.

In go-go-wm the client sits below the title strip:

```text
+-- frame (root coords: x, y, w, h) ---------------+
|  title strip, height = draw.TitleH               |
+--------------------------------------------------+
|                                                  |
|  client window                                   |
|  frame-local origin (0, TitleH)  [tiled]         |
|  frame-local origin (BorderW, TitleH)  [float]   |
|                                                  |
+--------------------------------------------------+
```

**OBSERVED:** tiled clients are reparented at `(0, draw.TitleH)` (`manage.go:110`); floats at `(BorderW, TitleH)` (`float.go:233`).

Who owns geometry depends on what kind of window it is, and every `ConfigureRequest` handler needs to know which case it is in:

| Window kind | Geometry owner | Correct response to a `ConfigureRequest` |
|---|---|---|
| Tiled application | `wmcore` layout | Deny, but report actual geometry via a synthetic `ConfigureNotify` |
| Floating application | Float state, constrained by size hints | Honor reasonable requests |
| Fullscreen window | `fullscreenState` | Ignore conflicting geometry until fullscreen exits |
| Override-redirect popup | The creating client | Not redirected; the WM never sees it |
| Internal PBUI tile | `wmcore` layout | Host renders into the assigned rectangle |
| Bar / dock | Surface policy, work area | May reserve work area via EWMH struts |

**This table is the source of a concrete, cheap performance fix.** ICCCM requires that when a WM denies or modifies a client's requested geometry, it must tell the client its actual geometry with a *synthetic* `ConfigureNotify` carrying root-relative values. go-go-wm currently reasserts geometry by calling a full `w.relayout()` instead (`manage.go:593-595`). That is a whole-workspace layout and repaint in response to one client's request, with no rate limit. A resize-happy client can therefore drive WM-wide repaint storms at its own chosen frequency. The correct implementation touches no layout code at all:

```go
func (w *WM) rejectTiledConfigure(f *frame, ev ConfigureRequestEvent) {
    // Handle stack-only requests separately if policy permits them.
    sendSyntheticConfigureNotify(
        client:       f.client,
        rootX:        f.rect.X + BorderW,
        rootY:        f.rect.Y + draw.TitleH,
        width:        f.clientWidth(),
        height:       f.clientHeight(),
        borderWidth:  0,
        aboveSibling: 0,
    )
}
```

No layout traversal. No paint. No buffer allocation. This is both more correct (ICCCM-compliant) and dramatically cheaper.

## 1.3 Requests, replies, and the thing that actually costs you

This is the single most important mechanical fact in the document.

**An X API call is not completed server work.** Most X requests are serialized into a connection buffer and sent asynchronously. The client does not wait. Throughput is high because requests pipeline.

A **round trip** happens when the client must wait for the server to answer. That occurs in three situations:

1. A request that has a reply, and you call `.Reply()` on the cookie.
2. A **checked** request, where you call `.Check()` to collect errors immediately.
3. An explicit synchronization primitive (`Sync`, `GetInputFocus` used as a barrier).

A **flush** is *not* a round trip. Flushing sends buffered requests; it does not wait for anything.

Round trips are serial dependencies. They drain the pipelining that makes X fast. On a local Unix socket one is cheap in absolute terms, but a handful of them inside a per-frame loop is a latency cliff.

The practical policy:

- Query properties at lifecycle boundaries (map, property change), never during interactive rendering.
- Issue independent property requests together, collect replies afterward.
- **Never use a checked request in a motion hot path** unless the error must be handled immediately.
- Batch `ConfigureWindow`, map/unmap, stacking, and property requests; flush once at the end of reconciliation.
- Never call a synchronization primitive to "make sure the screen updated" inside a drag loop.

**Why this matters here.** A naive audit of go-go-wm's drag loop finds no round trips: `grep '\.Reply()'` over `pkg/` returns only map-time and startup call sites. That audit is wrong, because it does not look for `.Check()`. Two checked requests hide inside `xshm.New` (`pkg/xshm/xshm.go:92` and `xshm.go:106-107`), which the resize path calls on every tick. See §3.4.

### Throttling is not coalescing

These are different mechanisms and the distinction is the reason "we already throttle" is not a complete answer.

- **Throttling** limits how often you *admit* work. "Ignore this event because I processed one recently." It reduces the admission rate.
- **Coalescing** replaces stale pending state with fresh state. "Keep only the newest pointer position; discard the intermediate ones."

Throttling alone has a failure mode: it does not drain events already queued in the X connection. If input arrives at 500 Hz and you complete 40 updates per second, a time gate reduces how many you accept but does not guarantee the one you accept is the *newest*. Under load the WM can visibly trail the pointer while honestly reporting 60 updates per second, because those updates apply old coordinates. i3 handles this by draining all pending X events in its drag loop and invoking the drag callback once with only the latest `MotionNotify`.

**go-go-wm's current position is better than the guides assume but not complete.** It has a 16 ms gate (`input.go:333`), and — importantly — `handleRelease` clears the gate and replays the release coordinates (`input.go:392-395`), so the *final* position is never stale. What it does not have is X-level motion compression: it uses raw `MotionNotifyFun` callbacks rather than `mousebind.Drag`, which is the xgbutil facility that compresses motion events (`xgbutil/mousebind/drag.go:97,116`). Every motion event therefore still costs a full callback dispatch through `runCallbacks`, even when it is immediately discarded by the gate. **INFERRED:** this is cheap per event but not free at 500–1000 Hz input rates.

## 1.4 ICCCM and EWMH are behavior, not decoration

ICCCM and EWMH properties are not metadata you set for tidiness. Each belongs to a state transition:

- `_NET_ACTIVE_WINDOW` follows focus ownership. It is never set independently of the focus state machine.
- `_NET_WM_STATE_FULLSCREEN` follows the fullscreen transition — geometry, stacking, bar visibility, and restoration are one atomic transition.
- `_NET_WM_WINDOW_TYPE_DIALOG` and `WM_TRANSIENT_FOR` drive map-time classification and stacking.
- `_NET_WM_STRUT_PARTIAL` changes monitor work areas, and therefore changes layout input rectangles.
- `_NET_WM_SYNC_REQUEST` changes the *resize scheduler*, because it gives the WM a client-readiness signal.

The governing principle: **protocol properties are projections of authoritative state, or inputs to an explicit state machine. They are never a second competing model.**

`_NET_WM_SYNC_REQUEST` deserves special mention as a performance tool. A client advertises it in `WM_PROTOCOLS` and exposes `_NET_WM_SYNC_REQUEST_COUNTER`. Before resizing, the WM sends a sync request with a new counter value, then configures the window; the client bumps its counter after it has redrawn for that request. Used as backpressure, this lets the WM avoid sending an unbounded stream of live resizes to a client that has not finished the previous one — send the *newest* pending size when the counter advances, not every size that was skipped. It must never block the WM loop: readiness arrives as XSync alarm events posted into the resize controller.

## 1.5 Interactive performance is a latency budget

At 60 Hz a display interval is **16.67 ms**, and the WM does not own all of it. The X server processes requests, clients redraw, a compositor may compose.

Initial targets to design against — these are engineering targets, not measurements:

| Stage | Target during live resize |
|---|---:|
| Event queue lag at scheduler entry | < 8 ms median, < 24 ms p99 |
| WM preview layout and diff | < 1 ms for ordinary trees |
| X request construction and flush | < 1 ms |
| Decoration paint and upload | < 2 ms total for affected frames |
| **Total WM update** | **< 4–6 ms p95** |
| Client acknowledgement (when sync protocol used) | 1–2 display intervals before fallback |

The critical scheduling rule: **when an update exceeds budget, degrade the work — do not accumulate debt.** Legitimate degradations are switching to outline mode, lowering preview cadence, skipping animation, or preserving old content. Processing stale motion samples is not a degradation; it is a bug.

### The pixel arithmetic that explains everything

Small-looking code is expensive when it touches every pixel:

- A **1920 × 1080 RGBA** image is **2,073,600 pixels ≈ 7.9 MiB**. Filling it, converting all four channels, and having the server consume it 60 times per second means touching roughly **475 MiB/s per surface** — before allocations, title rendering, copies, or client work. Two resized windows plus a PBUI client push this into gigabytes per second.
- A **1920 × 22** title strip is **42,240 pixels ≈ 165 KiB**. At 60 Hz that is roughly **9.7 MiB/s**.
- Four 1920 × 1080 client frames hold about **31.6 MiB** of RGBA buffers. Four title strips hold about **660 KiB**.

That is a factor of roughly 48× in both bandwidth and resident memory, for exactly the same visible result — because the client window covers the pane interior, and the WM only actually owns the title strip and border.

> **The best optimization is not to make full-frame painting faster. It is to stop representing decoration as a full client-sized bitmap.**

---

# Part II. The system as it actually is

Everything in this part was read out of the source at the current commit on branch `task/go-go-wm-goja`. Line anchors are given so you can verify each claim yourself.

## 2.1 The shape of the repository

Roughly 21.6k lines of Go, excluding `ttmp/`:

| Package | LOC | Owns |
|---|---:|---|
| `pkg/wmx11` | 5,326 | X11 shell: frames, focus, floats, fullscreen, input, bars, menus, launcher, IPC, painting |
| `pkg/cmds` | 2,251 | Cobra/glazed commands, `wm` entrypoint, rc.js bootstrap, rich REPL UI |
| `pkg/jsmod/wmmod` | 2,097 | The `wm` JavaScript module and its backends |
| `pkg/wmcore` | 1,541 | The pure, display-free desktop model: tree, ops, layout, neighbors |
| `pkg/draw` | 1,175 | Software drawing primitives, theme, widgets, RGBA→BGRA conversion |
| `pkg/repl` | 1,069 | Rich REPL session, value derivation |
| `pkg/launcher` | 942 | Command registry, matching, frecency |
| `pkg/jsmod/pbuimod` | 880 | The `pbui` JavaScript module |
| `pkg/pbui/broker` | 877 | Accept sessions, verb registration, event routing |
| `pkg/apps/uispec` | 765 | The flat declarative row/segment UI IR |
| `pkg/xshm` | 151 | MIT-SHM shared pixmap upload |

The intended layering is: lower layers never import higher ones; `wmcore` is pure; `draw` owns software rendering; `xshm` owns shared upload surfaces; `wmx11` owns X11 state; `jsmod` exposes native JavaScript modules.

## 2.2 The event loop — one goroutine owns everything

This is the most important structural fact about the codebase, and it is *correct*. Understand it before you change anything.

**OBSERVED** (`pkg/wmx11/wm.go:236-281`):

```go
pingBefore, pingAfter, pingQuit := xevent.MainPing(w.X)
for {
    select {
    case <-pingBefore:
        <-pingAfter
    case fn := <-w.ops:
        fn()
    case <-ctx.Done():
        xevent.Quit(w.X)
        return nil
    case <-pingQuit:
        return nil
    }
}
```

`xevent.MainPing` spawns a goroutine running `mainEventLoop`. That goroutine sends on the **unbuffered** `pingBefore` channel *before* dequeuing each event, runs all registered callbacks, then sends `pingAfter`. So while X callbacks execute on the xevent goroutine, the `Run` goroutine is parked between `<-pingBefore` and `<-pingAfter`.

The net effect: **X event handlers and posted `w.ops` closures are mutually exclusive and strictly serialized.** The WM is effectively single-threaded for all its state.

```text
                 ┌──────────────────────────────┐
   X server ────▶│  xevent goroutine            │
                 │  runCallbacks(event)         │──┐
                 └──────────────────────────────┘  │ pingBefore / pingAfter
                                                    │ (unbuffered: mutual exclusion)
   IPC ─────┐                                       ▼
   Broker ──┤                            ┌──────────────────────────┐
   JS ──────┼──▶ w.Post(fn) ──▶ w.ops ──▶│  WM Run goroutine        │
   (256 cap, blocks when full)           │  owns ALL X-facing state │
                                          └──────────────────────────┘
```

Three consequences you must internalize:

1. **There are no mutexes in `pkg/wmx11`.** `grep 'sync\.\|Mutex\|RWMutex'` over the package's non-test files returns zero hits. All state — `frames`, `byClient`, `byFrame`, `floats`, `dividers`, `desktop`, `fstate`, `fs`, `drag`, `launcher`, `scriptTiles` (`wm.go:140-187`) — is owned by one goroutine and reached only through `Post`. The rationale is written down at `focus_state.go:224-228`. **There is no lock contention on the hot path, by construction.** This is a real architectural achievement; preserve it.
2. **There is exactly one serialization point, and no render thread.** A slow paint inside an X callback blocks the ops drain. A slow op blocks event dispatch. Any work you add to this loop is work the user feels as input latency.
3. **`Post` blocks when the queue is full.** `w.ops` is a 256-slot buffered channel (`wm.go:217`), and `Post` is `select { case w.ops <- fn: case <-w.ctx.Done(): }` (`wm.go:227-232`). This is the only backpressure in the WM. A wedged loop blocks every poster — IPC, broker callbacks, and JavaScript alike.

**Events handled.** Registered in `setupInput` (`input.go:73-93`) on the root: MapRequest, ConfigureRequest, DestroyNotify, UnmapNotify, ButtonPress, MotionNotify, ButtonRelease. Per-frame handlers add Expose, KeyPress, EnterNotify (`events.go:10-56`). Per-client handlers add DestroyNotify, UnmapNotify, ButtonPress (`manage.go:146-174`). Divider windows get Enter, Leave, Press, Motion, Release, Expose (`divider.go:93-122`).

**Boot sequence** (`wm.go:239-261`): `becomeWM` → `setupScreen` → `setupBars` → `setupEWMH` → `setupInput` → `setupLauncher` → `manageExisting` → `startIPC` → `connectBroker` → `syncBuiltins(); relayout(); refocusCurrent()`. The rc.js `OnReady` hook runs *before* the loop starts consuming (`wm.go:263-265`), which is safe only because `Post` buffers 256 deep.

## 2.3 `pkg/wmcore` — the pure model

The package documentation states its contract plainly: it "imports nothing X-flavored: leaf ids and rectangles in, rectangles out" (`tree.go:171-179`). This purity is why the model is testable without a display, and it should be preserved.

**Representation.** `Node` is a single tagged struct, not an interface: `Kind ∈ {leaf, split}`, `Dir ∈ {row, col}`, `Ratio float64`, `A`/`B *Node` (`tree.go:214-226`). `NodeID` is a `string` (`tree.go:187`) — so every map keyed by node ID hashes strings.

**Mutation is persistent.** `update` (`tree.go:398-414`) rebuilds only the spine down to the changed node and shares untouched subtrees. `SetRatio` (`tree.go:469-480`) therefore allocates one new node per ancestor on the path. That is already cheap and idiomatic; leave it alone.

**Layout is recursive and recomputes the entire tree on every call.** **OBSERVED** (`layout.go:104-152`):

```go
func Layout(root *Node, r Rect, gap int) map[NodeID]LayoutItem {
    out := map[NodeID]LayoutItem{}
    layoutInto(root, r, gap, out)
    return out
}
```

`layoutInto` recurses into both children and writes an entry for *every* node — splits as well as leaves — computing a `DividerRect` for each split (`layout.go:131`, `layout.go:148`). There is **no cache, no dirty marking, no incremental path, and no arena**. A fresh `map[NodeID]LayoutItem` is allocated on every call, sized for `2n-1` string-keyed entries.

**Where `Layout` is called on hot paths:** `relayoutPaint` (`manage.go:321`), `dividerMotion` (`input.go:342`), `gripMotion` (`input.go:357`), `handleRootPress` (`input.go:232`), `NeighborLeaf` (`neighbor.go:16`).

**A specific waste:** `dividerMotion` calls `Layout` to find the split rectangle (`input.go:342`), then immediately calls `relayoutResized`, which calls `Layout` again (`manage.go:321`). **Two full-tree layouts and two map allocations per accepted motion tick.**

**Other allocating helpers on hot-adjacent paths.** `Node.Leaves()` allocates a fresh slice via `append` on every call (`tree.go:316-324`), and is called from `syncBuiltins` per workspace (`builtin.go:65`), `placementLeaf` (`manage.go:181`), `focusNext` (`input.go:124`), `refocusCurrent` (`wm.go:446`), and `repaintScriptTile` (`scripttiles.go:68`).

**`Find` is an O(n) depth-first search** (`tree.go:290-304`) — and it is called *inside* loops. `relayoutPaint` iterates the layout map and does a `Find` per entry (`manage.go:325`); `syncDividers` does the same (`divider.go:41`). **Reconciliation is therefore O(n²) in tree size today.** There is no `map[NodeID]*Node` index anywhere in the package; adding one is the cheapest structural fix in this document.

**Neighbor selection is geometric, not tree-based** (`neighbor.go:15-70`): it runs `Layout`, then for each item does `root.Find(id)`, and picks the minimum edge-to-edge distance among candidates overlapping on the cross axis, with a deterministic tiebreak. Same O(n²) plus map allocation shape — but it runs once per `wm.focus("left")`, so it is an event-rate cost, not a frame-rate one. Leave it.

**Verdict.** The model layer is allocation-heavy per call, but the calls are mostly event-rate — *except during divider drags, where it runs twice per tick.*

## 2.4 `pkg/wmx11` — the X adapter

### File ownership at a glance

| File | Owns |
|---|---|
| `wm.go` | `WM` and `frame` structs, `New`/`Run`/`Post`, `Apply`/`ApplyBatch`/`afterOp`, event emission, shutdown |
| `manage.go` | Client adoption and teardown, **`relayout`/`relayoutPaint`**, **`paintFrame`**, `copyImage`, `focus` |
| `input.go` | Keybindings, click routing, `dragState`, **`handleMotion`/`dividerMotion`/`gripMotion`/`floatMotion`**, `handleRelease` |
| `events.go` | Per-window callback wiring; the Expose fast paths live here |
| `divider.go` | `dividerWin`, `syncDividers`, `createDivider`, `paintDivider`, drag feedback |
| `bars.go` | Top/bottom bars, `paintBars`, `blit`/`blitCached`, drop-preview overlay |
| `builtin.go` | Builtin tiles, listener commands, `repaintBuiltins`, broker event watcher |
| `float.go` | Float rules and detection, `syncFloats`, `configureFloat`, stacking |
| `fullscreen.go` | Thin delegates to `fullscreenState` |
| `focus_state.go` | `fullscreenState` and `focusState` — pure, display-free invariants |
| `theme.go` | `setTheme` buffer-rebuild discipline, `focusTarget`, `moveDir` |
| `launcher.go` | Launcher popup and launcher tile, command registry glue |
| `scripttiles.go` | `script:<name>` renderer registry, `RegisterTile`/`RepaintTile` |
| `scripting.go` | `ScriptBackend` — post-and-wait onto the WM loop |
| `pbui.go` | Broker connection, verbs, accept banner, object menus, `repaintAllFrames` |
| `ipc.go` | Unix-socket JSON control plane |
| `ewmh.go` | `_NET_*` property publication |

### The reconciliation function

There is exactly one, with two entry points. **OBSERVED** (`manage.go:305-369`, abridged):

```go
func (w *WM) relayout()        { w.relayoutPaint(true) }
func (w *WM) relayoutResized() { w.relayoutPaint(false) }

func (w *WM) relayoutPaint(paintAll bool) {
    ws := w.desktop.CurrentWorkspace()
    if ws == nil { return }
    items := wmcore.Layout(ws.Root, w.area, Gap)   // full tree, fresh map
    w.syncDividers(items, ws)                       // repaints EVERY divider
    visible := map[wmcore.NodeID]bool{}             // fresh map per call
    for id, item := range items {
        if n := ws.Root.Find(id); n == nil || n.Kind != wmcore.Leaf { continue }  // O(n) inside O(n) loop
        visible[id] = true
        f := w.frames[id]
        if f == nil || w.fs.Owns(f) { continue }
        r := item.Rect
        resized := f.rect != r                      // ← applied-geometry diff. GOOD.
        if resized {
            f.rect = r
            f.win.MoveResize(r.X, r.Y, r.W, r.H)
            if f.client != 0 {
                xproto.ConfigureWindow(w.X.Conn(), f.client, ...)
            }
        }
        if paintAll || resized { w.paintFrame(f) }
    }
    for leaf, f := range w.frames {                 // EVERY frame in the process
        if !visible[leaf] {
            f.win.Unmap(); f.rect = wmcore.Rect{}; f.dropBuffers()
        } else {
            f.win.Map()                             // unconditional, no `mapped` flag
        }
    }
    w.syncFloats(paintAll)
}
```

**What is already right here — do not re-recommend it:**

- **There is an applied-geometry cache.** `f.rect` (`wm.go:84`) is compared against the freshly computed rectangle (`manage.go:332`), and both `MoveResize` and the client `ConfigureWindow` are skipped when unchanged. This is a genuine diff, and it is exactly what the source guides ask for at the frame level.
- **`relayoutResized()` exists specifically** so divider drags repaint only changed panes (`manage.go:311-314`, with a comment citing GGWM-005).
- **Off-screen frames release their paint buffers** (`manage.go:363`), and `syncDividers` destroys dividers whose splits have vanished (`divider.go:62-67`).

**What is not diffed — the open work:**

- **Map/Unmap are unconditional.** `f.win.Map()` and `f.win.Unmap()` are issued for *every frame in the process* — across all workspaces — on every relayout (`manage.go:359-366`). There is no `mapped bool` on `frame`. **INFERRED:** with nine workspaces and several tiles each, one relayout emits dozens of redundant Map requests. They are asynchronous, so the cost is protocol bytes and server work rather than latency — but it is pure waste. `syncFloats` does the same for every float (`float.go:335-347`).
- **`syncDividers` repaints every divider unconditionally.** It is called on *every* relayout including `relayoutResized`, and `paintDivider` is invoked outside any `if changed` guard (`divider.go:60`). `paintDivider` allocates a fresh `image.NewRGBA` per call (`divider.go:132`) and goes through the **uncached** `blit` path (`divider.go:148` → `bars.go:110-115`), which does `xgraphics.New` (client allocation plus BGRA conversion), `XSurfaceSet` (server `CreatePixmap` plus `ChangeWindowAttributes`), `XDraw` (PutImage), `XPaint` (ClearArea), then `Destroy` (FreePixmap). **Per divider, per motion tick, during a drag.** This is the clearest un-optimized hot path in the reconciler.
- **The `ws.Root.Find(id)` inside the item loop** (`manage.go:325`) makes reconciliation quadratic.

### What forces a global relayout and full repaint

**OBSERVED**, and worth memorizing because these are the accidental amplifiers:

- **`afterOp` runs after *every* op**: `syncBuiltins(); relayout(); updateEWMH(); paintBars()` (`wm.go:412-415`). That `relayout()` is `paintAll=true` — **every visible frame repaints on every layout operation**, even a workspace rename.
- `ApplyBatch` deliberately does this **once** for a burst (`wm.go:360-401`). This is an existing, correct coalescing mechanism; the doc comment cites the workspace-boot case.
- **`handleConfigureRequest` for a tiled client calls `w.relayout()`** to reassert geometry (`manage.go:593-595`), with no rate limit. See §1.2 for the correct fix.
- `setTheme` drops every frame, float, and bar buffer, then relayouts and repaints (`theme.go:217-263`) — correct and necessarily expensive.
- **Accept-mode enter/exit repaints all frames and floats** (`pbui.go:33-48` → `repaintAllFrames`, `pbui.go:155-166`). A semantic overlay change should not rasterize every frame.
- `setMouseDoc` calls `paintBars()` (`input.go:434-437`), and is invoked on every `EnterNotify` over any frame (`events.go:47-55`). Bars use the *cached* blit path (`bars.go:118-133`), so this is a `screen.W × 24` repaint, not a full-screen one.
- `updateEWMH` (`ewmh.go:29-60`) runs per op and issues `WmDesktopSet` per client inside a nested workspace loop plus `ClientListSet` — O(clients × workspaces) property writes each time.

### Window structure: one frame window, title painted into it

**OBSERVED:** each managed client gets exactly **one** WM-owned window. `manage` creates the frame with `CreateChecked` (`manage.go:87-102`) and reparents the client inside it (`manage.go:110`). There are **no title or border child windows**: `grep xwindow.Generate` in `pkg/wmx11` yields the frame, builtin frame, float frame, bars, divider, overlay, menu, launcher popup, and the EWMH check window — none of which are children of a frame.

The consequence is visible in `paintFrame` (`manage.go:373-455`). Title strip, border, and — for builtin tiles — the entire app content are composited into **one full-pane `image.RGBA`**:

```go
if f.img == nil || f.img.Bounds().Dx() != f.rect.W || f.img.Bounds().Dy() != f.rect.H {
    f.img = image.NewRGBA(image.Rect(0, 0, f.rect.W, f.rect.H))   // realloc on ANY size change
}
img := f.img
draw.Fill(img, img.Bounds(), draw.Current().Pane)                  // fill W×H
...
stripImg := strip.Render()                                         // fresh image per paint
copyImage(img, stripImg, 0, 0)
if f.client == 0 { f.regions = w.paintBuiltin(f, img) }
draw.Border(img, img.Bounds(), draw.BorderW, draw.Current().Ink)
```

So **every geometry change forces a full-pane repaint**: a `Fill` over W×H, a freshly allocated strip image blitted in, a border, and then the whole surface converted RGBA→BGRA. And **a focus change repaints two entire panes** (`manage.go:545-550`) to update a 22-pixel-high strip.

**The Expose fast path is already correct** and is a good model for the rest (`events.go:17-34`): if a live shm surface exists, *no client work at all* is needed because the server repaints from the background pixmap; if a live `ximg` exists, one `XPaint` suffices; only a stale buffer triggers a full `paintFrame`. The comment records that a naive full re-render on Expose was about 27% of the profile.

### Focus, fullscreen, floats

There is no `focus.go`; `focus()` lives in `manage.go:506-551` and routes every state change through `focusState` (`focus_state.go:217-342`). `focusState` holds a single `focusTarget` enum plus `preservedTile` (`focus_state.go:229-237`), and `Current()` *derives* fullscreen focus from `fullscreenState` rather than storing it twice (`focus_state.go:266-276`). This encapsulation, delivered under GGWM-010/011, is the pattern to copy for the interaction/drag state proposed in Part V.

`fullscreenState` owns geometry while active: `relayoutPaint` skips the fullscreen frame (`manage.go:330`), `Enter` resizes the frame to the full screen (`focus_state.go:72-94`), and `Exit` for a tile zeroes `f.rect` and calls `w.relayout()` — a full-workspace repaint (`focus_state.go:115-116`).

## 2.5 `pkg/draw` and `pkg/xshm` — rendering and upload

### Drawing primitives

Everything renders into plain `image.RGBA`; the package "knows nothing about X" (`theme.go:1-9`).

Already optimized, per GGWM-005:
- `Fill` writes one row's byte pattern and then `copy`s it down the surface (`theme.go:216-234`). The comment records that per-pixel `SetRGBA` was a quarter of CPU.
- Font faces are cached, keyed by `(bold, size)` behind a mutex (`theme.go:167-209`), with `fontOnce` parsing the embedded IBM Plex TTFs once. Hinting is pinned for golden-test determinism (`theme.go:201-203`).
- The palette is an `atomic.Pointer[Palette]` snapshot; `Current()` is a lock-free load (`theme.go:108-128`).

Still allocating per call:
- `draw.Text` allocates an `image.NewUniform` **per call** (`theme.go:250`).
- `Stipple` still uses per-pixel `SetRGBA` (`theme.go:265-274`) — but only drop previews use it.
- **Every widget is an immediate-mode value type that allocates a new image per `Render()`**: `TitleStrip` (`widgets.go:42-85`), `Banner`, `StatusLine`, `TopBar`, `Menu`, `DropPreview`, `LauncherPanel`. **There is no retained widget surface anywhere.** The model is uniformly "rebuild the pixels from the model, then upload."

### Upload

`ToXImage` / `CopyToXImage` / `ConvertRows` (`ximage.go:46-108`) perform the RGBA→BGRA swap. This is already the optimized version: row-major (the comment records that `xgraphics.NewConvert`'s column-major loop was a third of WM CPU), and **parallelized across up to 4 goroutines for surfaces ≥ 128 KiB**, serial below (`ximage.go:85-93`). It is also the only remaining full-surface pixel pass on the shm path.

`pkg/xshm` (151 LOC) is a proper zero-copy path. `Available` caches per connection, requires `SharedPixmaps` and 24-bit root depth, and honors `GO_GO_WM_NO_SHM` (`xshm.go:41-59`). And here is the finding that matters most:

```text
xshm.New(w, h):
    shmget                                        syscall
    shmat                                         syscall
    shm.AttachChecked(...).Check()          ←──── ROUND TRIP        (xshm.go:92)
    IPC_RMID                                      syscall
    shm.CreatePixmapChecked(...).Check()    ←──── ROUND TRIP        (xshm.go:106-107)

xshm.Destroy():
    FreePixmap + Detach + shmdt                                     (xshm.go:136-143)
```

`WriteRGBA` converts directly into the shared mapping (`xshm.go:121-132`), which is the whole point of the design and is excellent — *once the surface exists*.

### Buffer lifetime — the crux

Per `frame`: `img *image.RGBA` (client scratch), `ximg *xgraphics.Image` (PutImage fallback), `surf *xshm.Surface` (`wm.go:100-102`). `dropBuffers` resets the window's back pixel first — because `back_pixel` and `back_pixmap` are mutually exclusive — then destroys (`wm.go:110-122`).

Buffers are dropped on workspace hide (`manage.go:363`), float hide (`float.go:344`), unmanage (`manage.go:225`), theme swap (`theme.go:224-229`), and launcher-frame eviction (`manage.go:188`).

**And they are recreated whenever the pane's dimensions change** — which, during a divider drag, is every single tick:

- `f.img` is reallocated on any size change (`manage.go:381-383`).
- The shm surface is destroyed and recreated (`manage.go:420-432`), paying both round trips and the syscall triple above.
- `f.ximg` likewise on the PutImage fallback (`manage.go:442-451`).

The GGWM-005 "reuse buffers between paints" optimization (`wm.go:95-99`) works correctly for repaints at *constant* size. It does not help resize — which is precisely the resize-performance case.

**Damage tracking: none.** `paintFrame` always fills and repaints the whole pane (`manage.go:385`), and `XPaint` is `ClearArea(0,0,0,0)` — the entire window. Expose events are used only as a *trigger*, with `ev.Count == 0` used to coalesce multi-rectangle exposes (`events.go:18`, `divider.go:119`, `bars.go:88,93`); the exposed rectangle itself is discarded. The shm path makes this mostly moot for Expose, but not for content updates.

## 2.6 `pkg/apps/uispec` — the immediate-mode UI IR

`Seg` is a tagged struct with kinds `text | object | button | hint | table | image | field` (`uispec.go:26-71`); `Row []Seg`; `Spec []Row` (`uispec.go:74-77`). The `image` kind is Go-side only and `Normalize` rejects it from JavaScript (`uispec.go:193-195`) — enforcing the "JS supplies data, Go renders pixels" rule.

`Render` is **fully immediate-mode** (`uispec.go:256-386`): allocate a fresh `apps.NewSurface(w, h)`, then a single top-to-bottom pass placing segments left to right with wrapping, appending `apps.Region` entries for objects, buttons, table color cells, and fields. No layout cache, no per-row memoization, no dirty tracking, no stable identity.

`renderTable` (`uispec.go:401-486`) measures **every cell more than once** — `draw.TextWidth` per header (`:411`), per cell for width (`:416`), and again per numeric right-align (`:454`) — plus a `strconv.ParseFloat` per cell for numeric detection (`:423`) and a regexp match per cell for colour chips (`:417`, `:456`).

**Is the whole tile re-serialized and repainted per update?** The re-serialization happens on the JS side, once per state change — not per paint. The split is deliberate and correct (`uimod/app.go:20-42`): `jsAppState` keeps VM-owned handlers and a `rows uispec.Spec` snapshot guarded by a mutex. `rerenderOnLoop` runs `render()` and `uispec.Normalize`, then swaps the snapshot under the lock (`app.go:181-194`). The tile render closure registered with the WM is **VM-free** (`app.go:148-155`):

```go
err := m.opts.TileHost.RegisterTile(a.name,
    func(w, h int, accepting []string) (*image.RGBA, []apps.Region) {
        a.mu.Lock()
        rows := a.rows       // snapshot pointer copy only
        a.mu.Unlock()
        img, regions := uispec.Render(w, h, rows, accepting)
        return img, regions
    },
```

So the IR snapshot *is* reused. **The pixels are not.** Every WM-side paint of a script tile re-runs the full `uispec.Render` at the tile's current pixel size (`builtin.go:140-141` → `scripttiles.go:81-90`) — on Expose with a stale buffer, on resize, on focus change, and on accept-mode enter/exit.

## 2.7 JavaScript crossings — the ground truth

The source guides flag "JS on the render loop" as an architectural risk. **It is not a risk in this codebase. JavaScript provably never runs on the WM loop**, confirmed from both directions.

**WM → JS.** Every callback handed to the scripting layer is a *single post* into the goja owner loop (`pkg/jsmod/wmmod/module.go:378-404`):

```go
err := m.backend.Bind(combo, func() {
    // Fired on the WM loop (or X callback): a single post, never
    // JS execution here (concurrency rule 2).
    _ = services.PostWithLifetimeContext("wm.bind:"+combo,
        func(_ context.Context, vm *goja.Runtime) {
            if _, err := fn(goja.Undefined()); err != nil { ... }
        })
})
```

`wm.command` is identical (`module.go:442-449`). Script-tile actions and keys have the same shape (`scripttiles.go:94-101` → `app.go:222-246`).

**JS → WM.** `ScriptBackend` is *post-and-wait*: it posts a closure and blocks on a `done` channel, honoring both the caller's context and WM shutdown (`scripting.go:25-39`). Every backend method funnels through it (`scripting.go:41-188`).

**Backpressure — the honest picture:**

| Path | Policy | Anchor |
|---|---|---|
| JS → WM ops | Bounded (256) and **blocking** | `wm.go:217`, `wm.go:227-232` |
| Broker events → JS | Bounded (256), **drops newest**, counts drops, reports as one `script.error` | `queue.go:30-46`, `eventfan.go:111-114` |
| `xapp` shell posts | 64-slot channel, **then spawns a blocking goroutine** — unbounded | `xapp.go:228-234` |

The residual risks are therefore: (a) `w.ops` blocking its posters when the WM loop is slow; (b) the synchronous post-and-wait shape making every `wm.*` call cost a full WM-loop round trip; and (c) the unbounded goroutine fallback in `xapp.shell.post`. Note also that `boundedQueue` drops the **newest** item on overflow (`queue.go:30-46`) — correct for ordered broker events, but exactly the wrong policy for pointer-like state, where the newest is the only one that matters. That distinction becomes a design requirement in Part V.

## 2.8 What is measurable today — almost nothing

This section is short because the answer is short.

**Timing probes: two.** `grep time.Since` over non-test `pkg/` yields exactly:
- `afterOp` — `log.Debug().Dur("ms", ...).Str("op", op.Op)` (`wm.go:405-407`).
- `paintFrame` — `log.Debug().Dur("ms", ...).Str("leaf", ...).Int("w",...).Int("h",...)` (`manage.go:377-380`).

Both are `Debug` level. **Not measured anywhere:** `relayoutPaint` as a whole, `wmcore.Layout`, `syncDividers`/`paintDivider`, `xshm.New`/`Destroy` (the round trips!), `ConvertRows`, `uispec.Render`, `updateEWMH`, `paintBars`, motion arrival rate versus accepted tick rate, ops-queue depth, or dropped-event counts outside the EventFan's own `script.error`.

**Counters: one.** `boundedQueue.dropped`, surfaced once per drain (`queue.go:57-64`).

**Profiling:** `GO_GO_WM_PPROF=host:port` starts a dedicated-mux `net/http/pprof` server including `/trace`, with `WriteTimeout` deliberately set to 0 so long captures work (`pkg/cmds/wm.go:82-102`). There are no profiling *flags* — it is env-gated only. No `runtime/metrics`, no expvar, no Prometheus.

**Benchmarks: zero.** `grep "func Benchmark"` across the repository returns nothing.

**Tests: 23 files, 3,976 LOC** — covering `wmcore` tree/ops/neighbors, `focusState`/`fullscreenState` regressions against a bare `&WM{}` with no display, float decisions, `draw` golden PNGs, `uispec` normalization, the JS modules, the broker, the launcher, and the REPL.

**Nothing exercises `relayoutPaint`, `paintFrame`, `dividerMotion`, `syncDividers`, or `xshm`.** The entire hot path is untested and unbenchmarked. This is why Phase 0 is measurement.

---

# Part III. One divider drag, traced end to end

This is the core of the document. Read the code alongside it.

## 3.1 The path as written

**OBSERVED** (`pkg/wmx11/input.go:330-353`):

```go
func (w *WM) dividerMotion(d *dragState, x, y int) {
    // Coalesce: skip repaints closer than a frame apart; handleRelease
    // runs a final dividerMotion with the release coordinates.
    if time.Since(d.lastPaint) < 16*time.Millisecond { return }

    d.lastPaint = time.Now()
    ws := w.desktop.CurrentWorkspace()
    n := ws.Root.Find(d.split)
    if n == nil { return }

    items := wmcore.Layout(ws.Root, w.area, Gap)      // FULL LAYOUT #1
    item, ok := items[d.split]
    if !ok { return }

    f := wmcore.RatioForPointer(item.Rect, n.Dir, x, y)
    f, snapped := wmcore.Snap(f)
    d.snapped = snapped
    _, _ = wmcore.Apply(w.desktop, wmcore.Op{Op: wmcore.OpSetRatio, Node: d.split, Ratio: f})
    w.relayoutResized()                               // FULL LAYOUT #2 + reconcile + paint
    w.dividerDragFeedback(d.split, snapped)
}
```

Expanded, one accepted tick does this:

```text
MotionNotify (root / frame / divider window)
  │
  ├─ handleMotion → dispatch on d.kind                        input.go:300-313
  │
  ├─ 16 ms wall-clock gate                                    input.go:333
  │     ⚠ limits admission; does NOT drain queued events
  │
  ├─ ws.Root.Find(d.split)                          O(n) DFS  input.go:338
  ├─ wmcore.Layout(...)              FULL TREE + fresh map    input.go:342
  ├─ RatioForPointer + Snap
  ├─ wmcore.Apply(OpSetRatio)     durable tree mutation + op  input.go:350
  │
  └─ relayoutResized() → relayoutPaint(false)                 manage.go:316
        ├─ wmcore.Layout(...)       FULL TREE + fresh map #2  manage.go:321
        ├─ syncDividers(items, ws)                            manage.go:322
        │     ├─ ws.Root.Find(id) per divider      O(n) each  divider.go:41
        │     └─ paintDivider(d) UNCONDITIONALLY              divider.go:60
        │           ├─ image.NewRGBA                          divider.go:132
        │           └─ blit()  ── UNCACHED PATH ──            bars.go:110-115
        │                 xgraphics.New   (alloc + BGRA)
        │                 XSurfaceSet     (CreatePixmap + ChangeWindowAttributes)
        │                 XDraw           (PutImage)
        │                 XPaint          (ClearArea)
        │                 Destroy         (FreePixmap)
        │
        ├─ for each layout item:
        │     ├─ ws.Root.Find(id)         O(n) INSIDE O(n)    manage.go:325
        │     ├─ if f.rect != r:   ← applied-geometry diff ✓  manage.go:332
        │     │     ├─ f.win.MoveResize(...)
        │     │     └─ xproto.ConfigureWindow(client, ...)    manage.go:347
        │     └─ if resized: paintFrame(f)                    manage.go:354
        │           ├─ f.img realloc (size changed)           manage.go:381
        │           ├─ draw.Fill over W×H                     manage.go:385
        │           ├─ TitleStrip.Render() → fresh image      widgets.go:44
        │           ├─ copyImage strip into pane              manage.go:409
        │           ├─ draw.Border                            manage.go:413
        │           └─ if size changed:
        │                 surf.Destroy()                      manage.go:420
        │                 xshm.New(w,h)
        │                     shmget / shmat        syscalls
        │                     AttachChecked().Check()   ⚠ ROUND TRIP   xshm.go:92
        │                     IPC_RMID              syscall
        │                     CreatePixmapChecked().Check() ⚠ ROUND TRIP xshm.go:106
        │                 WriteRGBA → ConvertRows (full-surface BGRA)
        │
        ├─ Map()/Unmap() for EVERY frame in the process       manage.go:359-366
        └─ syncFloats(false)                                  float.go:335

  … and then each resized client independently re-renders its own interior.
```

## 3.2 Where the cost actually is

Ordered by **INFERRED** magnitude on a fast local machine. None of this is measured yet — measuring it is Phase 0, and this ordering is the hypothesis Phase 0 must confirm or refute.

### Tier 1 — the round trips (latency, not throughput)

Two blocking round trips per resized pane per tick, from `xshm.New` (`xshm.go:92`, `xshm.go:106-107`), plus `shmget`/`shmat`/`IPC_RMID`/`shmdt` syscalls and a `FreePixmap`. Two panes change on a divider drag, so **four round trips per tick**.

This is the finding all three source guides missed, because they audited for `.Reply()` and this is `.Check()`. A checked request is a round trip.

It also explains an otherwise puzzling property: making the *pixel* work faster (which GGWM-005 and GGWM-006 both did successfully, with measured CPU reductions) would not proportionally improve perceived latency, because the latency is a serial dependency on the server, not CPU time.

### Tier 2 — full-surface pixel work

Per resized pane per tick: `draw.Fill` over W×H, plus a full-surface RGBA→BGRA `ConvertRows`. For a 1920 × 1080 pane that is ~7.9 MiB written and ~7.9 MiB converted, twice per tick (two panes), i.e. roughly **32 MiB of memory traffic per tick** before the client does anything. At an aspirational 60 Hz that is ~1.9 GiB/s.

Almost all of those pixels are invisible: the client window covers the pane interior. The WM owns a 22-pixel title strip and a 2-pixel border.

### Tier 3 — the divider blit

Per divider per tick: one `image.NewRGBA`, one `xgraphics.New` (allocation plus BGRA conversion), one server `CreatePixmap`, one `ChangeWindowAttributes`, one `PutImage`, one `ClearArea`, one `FreePixmap`. A divider is small, so the pixel cost is trivial — but the **X resource churn is not**, and it happens unconditionally even when the divider's appearance has not changed at all. A divider's visual state space is tiny: orientation, idle/hot/dragging/snapped, theme revision. Moving a divider window should require no raster work whatsoever.

### Tier 4 — algorithmic waste

- Two full-tree layouts per tick, each allocating a `map[NodeID]LayoutItem` (`input.go:342`, `manage.go:321`).
- `Find`-in-loop making reconciliation O(n²) (`manage.go:325`, `divider.go:41`).
- One `visible` map allocated per relayout (`manage.go:323`).
- Unconditional Map/Unmap for every frame in the process (`manage.go:359-366`).

For an ordinary desktop tree these are small in absolute terms. They matter for two reasons: they scale badly, and they are trivially removable. Do not, however, mistake them for the main event — as one source guide puts it, replacing the layout map with a clever arena while still recreating 8 MiB shm buffers will not make resizing feel good.

### Tier 5 — the client

Even a zero-cost WM configure can trigger expensive application work: terminal emulators reflow grids, browsers relayout documents, IDEs recalculate panes, GPU clients may recreate swapchains. **A WM must not assume that "60 geometry changes per second" means "60 responsive frames per second."** This is what outline mode and `_NET_WM_SYNC_REQUEST` backpressure exist to manage.

## 3.3 The other unthrottled paths

Two sibling paths share the drag machinery and deserve the same scrutiny.

**`gripMotion` has no throttle at all** (`input.go:355-372`). Per *raw* motion event it runs `wmcore.Layout`, DFS-`Find`s each item, and on a hit calls `showDropPreview`, which does `MoveResize` + `Map` + `Stack` plus a **full uncached blit** of a freshly rendered `DropPreview` image (`bars.go:137-160`). At 500 Hz input this is the worst path in the codebase. It is also less noticeable than divider drag because tile-drag gestures are shorter.

**`floatMotion` also has no throttle** (`input.go:317-328`) — and that is fine. It only clamps and issues `f.win.Move`, with an early-out when the rectangle is unchanged, and it explicitly avoids repainting because the content rides in the background pixmap (`input.go:315-316`). **This is the model for what a cheap motion handler looks like.**

## 3.4 The cost model to instrument

Do not measure "CPU percentage." Measure the following, per accepted tick, correlated by a drag ID and a sequence number.

**Counters** (`wm_render_*`, `wm_x_*` families):

| Counter | Why |
|---|---|
| Nodes visited, layout items produced | Confirms/refutes Tier 4 |
| Frames whose *position* changed | Position-only moves need no paint |
| Frames whose *size* changed | Size changes trigger buffer churn |
| Dividers moved vs. dividers repainted | Should diverge to ~0 repaints |
| RGBA bytes filled; pixels converted | Tier 2 |
| Images allocated; bytes allocated | GC pressure |
| **SHM segments attached/detached; pixmaps created/freed** | **Tier 1 — should be 0 during a steady drag** |
| ConfigureWindow / Map / Unmap / ClearArea / property requests | Tier 4 |
| **Flushes, reply waits, and checked-request waits** | **Tier 1 — instrument checked waits separately** |
| Client ConfigureNotify events produced | Tier 5 |

**Latency timestamps**, carried per pointer sample:

```text
queue_lag        = scheduler_start  - event_receipt
preview_latency  = x_flush          - event_receipt
wm_work          = x_flush          - scheduler_start
client_wait      = sync_ack         - x_flush
release_latency  = final_commit     - release_receipt
stale_distance   = abs(pointer_latest - pointer_rendered)
```

`stale_distance` is the metric that catches the throttling-versus-coalescing failure: a system can report 60 updates per second while visibly trailing the pointer, if those updates apply old coordinates.

**Stage spans**, with stable names:

```text
wm.resize.admit          queue lag, samples replaced
wm.resize.ratio          snap target, mode
wm.resize.preview_model  durable ops expected: 0
wm.resize.layout         nodes visited; full or subtree
wm.resize.diff           positions/sizes changed
wm.resize.xbuild         request counts by opcode
wm.resize.decor_paint    bytes, affected surfaces
wm.resize.upload         reuse / create / destroy counters
wm.resize.flush          must not imply a reply wait
wm.resize.client_sync    counter value, timeout, fallback
wm.resize.commit         exactly one per successful drag
```

**Diagnostic signatures.** Once you have the above, the numbers tell you where to go next:

| Observed signature | Likely cause | Next experiment |
|---|---|---|
| High WM CPU and paint pixels; outline mode fast | Raster/upload coupling | Suppress paint; split chrome; inspect damage |
| Low WM CPU; many client configures; outline fast | Client relayout pressure | Lower live configure rate; try another client |
| Low paint; high X request count | Redundant reconciliation, map/stack churn | Enable desired/applied request diffing |
| **High `checked_wait` count** | **shm recreate per tick** | **Capacity buffers, or suppress content paint** |
| Input-to-begin latency grows over the drag | WM-loop backlog or a blocking host call | Inspect ops-queue depth and synchronous calls |
| Geometry flush fast but visible motion lags | Server/compositor presentation delay | Compare Xephyr vs. Xorg, compositor off |
| Frequent allocations despite caches | Exact-size buffer recreation | Capacity reuse, pools, allocation labels |

An **outline mode** is worth building early purely as a diagnostic: if outline motion is smooth while live mode is slow, the remaining bottleneck is definitively geometry and client rendering, not input handling.

---

# Part IV. Gap analysis — what the guides asked for versus what exists

This is the part that saves you weeks. The three source guides were written against a specific commit and recommend a great deal of work. A substantial fraction of it has already landed.

## 4.1 Already implemented — do not redo

| Recommendation in the guides | Status | Evidence |
|---|---|---|
| Throttle divider motion | **Done** — 16 ms gate | `input.go:333` |
| Ensure the *final* position is exact | **Done** — release replays release coords | `input.go:392-395` |
| Cache applied geometry; skip unchanged configures | **Done** at frame level via `f.rect` | `wm.go:84`, `manage.go:332` |
| Resize-only repaint mode | **Done** — `relayoutResized()` | `manage.go:311-314` |
| Repair Expose from cached content, don't re-render | **Done** — server-side via background pixmap | `events.go:17-34` |
| Zero-copy uploads via MIT-SHM | **Done** | `pkg/xshm`, `manage.go:419-437` |
| Row-major RGBA→BGRA conversion | **Done**, plus parallel bands ≥128 KiB | `ximage.go:56-108` |
| Row-pattern fills instead of per-pixel | **Done** | `theme.go:216-234` |
| Cache bar surfaces | **Done** — `blitCached` | `bars.go:118-133` |
| Cache font faces | **Done**, keyed by (bold, size) | `theme.go:189-209` |
| Lock-free theme/palette reads | **Done** — `atomic.Pointer[Palette]` | `theme.go:108-144` |
| One reconciliation per operation burst | **Done** — `ApplyBatch` | `wm.go:360-401` |
| Release large buffers for hidden surfaces | **Done** | `manage.go:363`, `float.go:344` |
| Bounded event queue with drop accounting | **Done** | `pkg/jsmod/queue.go`, `eventfan.go:111-114` |
| Keep JavaScript off the X/render loop | **Done, rigorously** | `module.go:389-398`, `scripting.go:25-39`, `scripttiles.go:3-7` |
| Encapsulate focus/fullscreen as state machines | **Done** — GGWM-010/011 | `focus_state.go:26-342` |
| Avoid mutexes in the WM by single-goroutine ownership | **Done** — zero mutexes in the package | `focus_state.go:224-228` |

## 4.2 Where this guide corrects the source material

| Guide claim | Reality | Anchor |
|---|---|---|
| "No reply wait or checked request in the admitted motion hot path" | **False.** Two `.Check()` round trips per resized pane per tick, inside `xshm.New` | `xshm.go:92`, `xshm.go:106-107` |
| "Replace the time gate" (implying none exists / final position is stale) | A gate exists *and* the final position is already exact | `input.go:333`, `input.go:392-395` |
| "Add an applied-geometry cache" | One exists for frame rectangles | `manage.go:332` |
| "Every preview applies a durable ratio operation" | True | `input.go:350` |
| "`emitEvent` launches a goroutine per event" | True, and the pattern is widespread | `wm.go:459` and the goroutine inventory in §2.7 |
| Handbook Appendix C "contains three ADRs" | It contains **seven** (ADR-1 … ADR-7) | `sources/local/go-go-wm_engineering_handbook.md` |
| Proposed backlog assigns **GGWM-012** to "Divider gesture preview and latest-motion scheduler" | **Numbering collision:** GGWM-012 is this ticket. Renumber before adopting GGWM-013…029 | ibid. §44 |

## 4.2a WITHDRAWN: "this machine does not support shared pixmaps"

Sections 4.2b, 4.2c and 4.2d below repeatedly state that the development
machine's Xorg reports `shared_pixmaps: false`, that it therefore runs the
PutImage fallback, and that GGWM-006's MIT-SHM work is inert on it.

**All of that is false and is withdrawn.** A probe against the live session:

```
display        :2
root depth     24
MIT-SHM        1.2
SharedPixmaps  true
=> go-go-wm will use the MIT-SHM shared-pixmap path.
```

The `false` reading came from the measurement harness, not the hardware.
`xshm.Available` gated on `os.Getenv("GO_GO_WM_NO_SHM") != ""` — a bare
non-empty test — and the VT harness expressed its enabled condition as
`GO_GO_WM_NO_SHM=0`, which is non-empty. The "shm on" arm ran with shared
memory switched off.

Corrected in `pkg/xshm` (`envDisabled` honours `0`/`false`/`no`/`off`, pinned
by `env_test.go`) and in the harness. Read the affected sections below with
this in mind:

- **This machine takes the MIT-SHM path**, so the relevant result is
  **5.31 → 1.11 ms per paint, 4.8x**, not the fallback's 3.5x.
- **There is no Xorg configuration to change.** Shared pixmaps are already
  available with `glamor` acceleration enabled.
- The shm-versus-fallback *comparison* remains valid as a comparison of two
  code paths. Only the claim about which path this machine takes was wrong.

Use `scripts/shmprobe` to check any display rather than inferring from the
driver.

## 4.2b Corrections from implementation (added after Phase 0/1 landed)

Three claims in this document were tested by implementing them. Two survived; one did not.

### The MIT-SHM path does not execute on every host

**OBSERVED**, from a live run on this machine's Xorg (`modesetting`, `Virtual 1280 800`):

```json
{"shared_pixmaps": false, "message": "frame upload path"}
```

`xshm.Available` requires `rep.SharedPixmaps && RootDepth == 24` (`xshm.go:53`). The server here reports no shared-pixmap support, so go-go-wm **always takes the `ximg` PutImage fallback**, and `GO_GO_WM_NO_SHM=1` changes nothing.

Consequences:

1. The Tier-1 hypothesis in §3.2 — two checked round trips per resized pane per tick from `xshm.New` — **cannot apply on this host**, because that code never runs. It remains the correct analysis wherever shared pixmaps *are* available.
2. **GGWM-006's shared-pixmap optimization is inert in this configuration.** Nothing logged it above `Info` and nobody was looking.
3. The *shape* of the problem survives on the fallback: `f.ximg` is destroyed and recreated whenever bounds change (`manage.go:441-450`) — every tick during a drag — each recreation doing `xgraphics.New` plus `XSurfaceSet` (CreatePixmap + ChangeWindowAttributes), and `XDraw` then pushes the whole surface through the socket via PutImage on **every** paint.

So the priority stands, but state it in terms of **per-tick surface recreation**, not specifically shm. The `ximg_creates` counter added in Phase 0 measures it directly.

### The O(n²) removal is not a win at realistic tree sizes

§4.3 item 6 and Phase 1 present removing the `Find`-in-loop as a straightforward improvement. Benchmarked (`pkg/wmcore/layout_bench_test.go`, ns/op):

| leaves | `Find` (old) | `BuildIndex` (allocating) | `BuildIndexInto` (shipped) |
|---:|---:|---:|---:|
| 2 | 92 | 531 | 179 |
| 4 | 240 | 690 | 341 |
| 8 | 710 | 1179 | 800 |
| 16 | 2651 | 3509 | **1579** |
| 32 | 11213 | 8014 | **3309** |

A fresh `map[NodeID]*Node` costs more than the depth-first scans it replaces until roughly 24 leaves; a real workspace holds two to eight tiles. Reusing one scratch map moves the crossover to ~10 leaves and leaves a ~70 ns penalty below it — noise against a `paintFrame` measured at ~3.1 ms.

**Keep it as scaling insurance, not as a speedup.** Do not cite it as a performance win.

### The largest single win was not in this plan

Benchmarking the paint path found that `draw.Text` was **72%** of a title-strip render, and a title does not change while its pane resizes. Caching glyph runs as alpha masks:

| Benchmark | Before | After |
|---|---:|---:|
| `draw.Text` (24 chars) | 53.9 µs | **7.46 µs** |
| `TitleStrip.Render` w=1272 | 73.7 µs | **38.8 µs** |

This cost a bounded rendering change — compositing a run into one mask differs from per-glyph blending by one LSB where antialiased glyphs overlap, measured at 14 of 179,200 pixels — pinned by `TestTextCacheMatchesDirect` at a tolerance of 1/255.

**The general lesson matters more than the specific fix:** the plan in Part VI was derived from reading code, and the first hour of *measuring* it surfaced a bigger win than anything on the list. Phase 0 is not bureaucracy.

### Revised statement of where a frame paint goes

Measured, 1272×664 pane:

```
draw.Fill            ~105 us    32 GB/s, memory-bandwidth bound — finished work
TitleStrip.Render     ~39 us    after the glyph cache (was 74 us)
                     --------
subtotal             ~144 us
observed paintFrame ~3100 us    live WM debug log
                     --------
unaccounted         ~2956 us    BGRA conversion + upload + X
```

**Over 95% of a frame paint is conversion, upload, and X** — not fill, not text. Instrument `ConvertRows` and the upload separately before optimizing either.

## 4.2c MEASURED: the Tier-1 hypothesis is refuted, and the plan reorders

Everything above §4.2c was derived from reading code. This section reports what
happened when it was **measured**, on a real window manager under a scripted
drag (`scripts/ggwm-xephyr-validate.sh`, nested Xephyr, 1280x800, 644 motion
events per run). Where this section disagrees with earlier sections, believe
this one.

### The mechanism is confirmed; the conclusion is not

`shm_creates = shm_destroys = frames_resized = 512` for one drag. The surface
really is torn down and rebuilt once per resized pane per tick, and `xshm.New`
really does issue two checked requests — **1,024 synchronous round trips for a
single drag.** §3.2 Tier 1 describes this correctly.

But removing them does not help:

| Condition | `shm_creates` | `ximg_creates` | ms/paint | p50 | p95 |
|---|---:|---:|---:|---:|---:|
| default (MIT-SHM) | 512 | 0 | **4.98** | 4.82 | 8.29 |
| `GO_GO_WM_NO_SHM=1` | 0 | 510 | **5.85** | 5.47 | 9.44 |

Disabling shm eliminates every round trip and is **15% slower**. MIT-SHM pays
for its round trips several times over. **The round trips are real and are not
the bottleneck.**

### Reconciliation is 1.8% of a relayout; paint is the rest

With `GO_GO_WM_NO_RESIZE_PAINT=1`, which commits geometry but skips decoration
paint during a drag:

```
relayout_ms_total   2595 ms  ->  13.8 ms      (188x)
ms per relayout     7.416    ->  0.040
```

Layout, the tree index, map diffing, geometry requests and divider
synchronisation together are **0.13 ms of a 7.4 ms relayout**. Every algorithmic
item in §4.3 and Phase 1 targets that 1.8%. They remove real waste and should
be kept, but they cannot be felt.

### Where a paint actually goes

`paintFrame` averages **4.98 ms** for ~470 kilopixels. `draw.Fill` runs at
32 GB/s, i.e. ~0.06 ms for that many pixels. With the glyph cache from §4.2b:

```
draw.Fill            ~0.06 ms    ~1%
TitleStrip.Render    ~0.04 ms    ~1%
                     ---------
everything else      ~4.9  ms   ~98%    BGRA conversion + upload + X server
```

### Consequence: Phase 4 moves ahead of Phase 2

Part VI orders Phase 2 (capacity buffers, killing per-tick surface recreation)
before Phase 4 (chrome/content split), because Phase 2 attacks round trips.
**That ordering is wrong.** The dominant cost is pixel volume through conversion
and upload, and the chrome/content split is the change that reduces it — a
22-pixel title strip instead of a 664-pixel pane is roughly a 30x reduction in
pixels touched per decoration repaint.

**Do Phase 4 first.** Phase 2 remains worth doing, for allocation pressure and
for the pathological cases, but it is no longer the headline.

### One design lesson from the suppression experiment

Under `NO_RESIZE_PAINT`, `resize_paint_suppressed` reached 506 while
`frames_painted` stayed at 506 and `paint_ms_total` did not move. The paints
relocated to the `Expose` handler: skipping the paint leaves the background
pixmap stale at the new size, the server exposes the window, and the frame
repaints anyway. **"Do not paint during the drag" is not implementable on its
own** — it requires retained content or a preview representation that remains
valid while geometry changes. The flag is a valid measurement tool and a false
design direction.

### Environment caveats, which matter

- Xephyr reports `shared_pixmaps: True`; **the target machine's Xorg reports
  `False`** (`modesetting`). The live deployment therefore runs the *slower*
  PutImage path, and has no shm path to fall back from. The `noshm` row above is
  the one that represents production.
- Xephyr is nested, so absolute timings are inflated. Corroboration: the live
  Xorg log recorded `paintFrame` at 3.1-3.5 ms versus Xephyr's 4.8 ms p50 — same
  order, so ratios are trustworthy and the absolute budget is not.

### What to instrument next

`ConvertRows` and the upload are ~98% of a paint and are still one
undifferentiated block. Splitting them is the highest-value instrumentation
remaining, and it decides how Phase 4 should be built.

## 4.2d OUTCOME: what was implemented and what it measured

Phases 0 through 2 are implemented and measured. This section supersedes the
priority arguments in §4.2b and §4.2c, both of which were made before the work
existed.

### Result

One scripted drag, three sweeps, 644 motion events, nested Xephyr at 1280x800:

| | baseline | after | |
|---|---:|---:|---:|
| MIT-SHM ms/paint | 5.31 | **1.11** | **4.8x** |
| MIT-SHM relayout total | 2852 ms | **595 ms** | **4.8x** |
| PutImage fallback ms/paint | 6.63 | **1.89** | **3.5x** |
| PutImage fallback relayout total | 3518 ms | **963 ms** | **3.7x** |
| shm surface creations per drag | 528 | **64** | 8.3x |
| `draw.Text` (24-char title) | 53.9 us | **7.46 us** | 7.2x |

### What produced it

1. **Glyph-run cache** (§4.2b). Text rasterization was 72% of a title render and the string never changes during a resize.
2. **Capacity-sized backing stores.** Rounding to a 128-pixel bucket makes buffers survive until a drag crosses a boundary, cutting shm surface creations 528 to 64.
3. **Chrome-only composition, conversion and upload.** A reparented client covers the frame interior, so the window manager's visible pixels are the title strip and border: ~20k of ~422k for a 636x664 pane. Builtin and script tiles are excluded and still get a full-surface treatment.
4. Phase 1's reconciliation work — node index, single layout per tick, divider paint guard, map-state mirrors, synthetic `ConfigureNotify`.

### The Phase 2 versus Phase 4 question, settled

§4.2c argued Phase 4 (chrome/content split via title child windows) should precede Phase 2 (capacity buffers), then §4.2d's measurements reversed that again. Both arguments are now moot:

**The chrome/content split's principal saving was obtained without it.** Its purpose is to stop touching pixels the client covers. Those pixels are already covered — only the upload had to stop treating them as visible. No new X windows were created, and the highest-risk item in the plan was not attempted.

What a structural split would still buy is narrower: `TitleStrip.Render` still renders at pane width, and the RGBA scratch is still pane-sized. That is a fraction of a now ~1-2 ms paint. **Phase 4 should be re-costed before it is scheduled; its case is much weaker than when it was written.**

### Where the remaining time goes

Per paint at bucket 128, MIT-SHM path, roughly 1.1 ms total: composition, conversion, surface management and transfer are now within a small factor of each other, with no single dominant component. Further gains require either fewer paints (preview/commit separation, Phase 3) or a different rendering model, not another constant-factor fix to this path.

### Method notes worth keeping

- **`GO_GO_WM_SIZE_BUCKET`** makes the granularity sweepable. The sweep table lives at the `sizeBucket` declaration.
- **`{"q":"perf"}` / `{"q":"perf-reset"}`** return bounded aggregates including `buffer_bytes`, so both sides of the capacity trade are measured.
- **Two harnesses**, both in `scripts/`: `ggwm-xephyr-validate.sh` drives a scripted drag and reports counters; `ggwm-xephyr-scenarios.sh` drives fullscreen, float, workspace switch, focus and theme swap and screenshots each. Screenshots are committed under `images/` because every failure mode of the chrome-only upload is visual and silent.

## 4.3 Open, evidence-backed work

Ordered by impact-to-risk ratio. This ordering *is* the roadmap in Part VI.

| # | Item | Anchor | Tier |
|---:|---|---|---|
| 1 | No benchmarks; two `Debug`-level timing probes total | `wm.go:405-407`, `manage.go:377-380` | — |
| 2 | shm surface destroyed/recreated per tick — 2 checked round trips + syscalls | `manage.go:420-432`, `xshm.go:92,106` | 1 |
| 3 | `f.img` reallocated on every size change | `manage.go:381-383` | 2 |
| 4 | Every divider repainted on every relayout, uncached blit path | `divider.go:60,132,148`, `bars.go:110-115` | 3 |
| 5 | Two full `wmcore.Layout` calls per motion tick | `input.go:342`, `manage.go:321` | 4 |
| 6 | `Find`-in-loop → O(n²) reconciliation | `manage.go:325`, `divider.go:41` | 4 |
| 7 | Unconditional Map/Unmap for every frame in the process | `manage.go:359-366`, `float.go:335-347` | 4 |
| 8 | Title drawn into the pane surface; focus change repaints two whole panes | `manage.go:381-413`, `manage.go:545-550` | 2 |
| 9 | `gripMotion` unthrottled, with per-event layout and uncached blit | `input.go:355-372`, `bars.go:137-160` | 3 |
| 10 | `paintAll=true` relayout after **every** op | `wm.go:412-415` | 2 |
| 11 | `handleConfigureRequest` → full relayout, unrated | `manage.go:593-595` | 4 |
| 12 | Durable `OpSetRatio` per tick instead of per drag | `input.go:350` | 4 |
| 13 | Accept-mode transition repaints all frames | `pbui.go:33-48`, `pbui.go:155-166` | 2 |
| 14 | `xapp` upload path entirely uncached and shm-less | `xapp.go:274-286` | 2 |
| 15 | REPL materializes all cells, then windows to the visible budget | `session.go:133-170` | — |
| 16 | Synchronous `registry.Refresh()` on the WM loop at popup open | `launcher.go:90` | — |
| 17 | Unbounded goroutine fallback in `xapp.shell.post` | `xapp.go:228-234` | — |

---

# Part V. The target architecture

## 5.1 The geometry transaction

Replace "every accepted sample mutates the model and repaints" with an explicit six-stage transaction:

```text
1. Capture intent      store the latest pointer position; derive a preview ratio
2. Apply preview       a transient ratio override — NOT a durable tree mutation
3. Derive dirty geom   recompute rectangles for the affected subtree only
4. Diff platform state compare desired vs. applied frame/client/divider rects
5. Commit X effects    send only changed requests; flush ONCE
6. Schedule paint      mark chrome/content damaged per policy — often nothing
```

Button release commits the ratio as one canonical `wmcore.Op`, emits one durable operation event, repaints at final dimensions, and clears preview state. **One drag becomes one logical operation with a start and end ratio, not sixty durable ratio operations.** That improves undo, replay, and event-log clarity as much as it improves performance.

Target path, contrasted with §3.1:

```text
MotionNotify  ──▶  mailbox.latest = (x, y, seq)     ← O(1), no layout, no paint
                     └─ if !scheduled: schedule one step

step():                                              ← at most ONE in flight
   read newest (x, y)
   previewRatio = RatioForPointer(cachedSplitRect, ...)
   overrides[split] = previewRatio                   ← committed tree UNCHANGED
   LayoutSubtree(split, cachedSplitRect, overrides)  ← affected subtree only
   diff vs. applied state → ReconcilePlan
   issue batched ConfigureWindow requests; flush once
   move divider window   (NO repaint — appearance unchanged)
   paint: title strips only, or nothing at all in outline mode
   if released:  commit one OpSetRatio; else if newer sample: schedule again
```

## 5.2 The latest-wins mailbox

The controller is owned by the WM loop, so it needs no synchronization — a plain struct field and one scheduling flag suffice.

```go
type resizeController struct {
    active       bool
    split        wmcore.NodeID
    mode         ResizeMode          // outline | live | adaptive
    latestX      int
    latestY      int
    latestSeq    uint64
    renderedSeq  uint64
    scheduled    bool
    inFlight     bool
    released     bool
    lastStart    time.Time
    lastDuration time.Duration
}

func (r *resizeController) Motion(x, y int) {
    r.latestX, r.latestY, r.latestSeq = x, y, r.latestSeq+1
    if !r.scheduled && !r.inFlight {
        r.scheduled = true
        r.wm.Post(r.step)          // ONE wake token, not one closure per event
    }
}

func (r *resizeController) step() {
    r.scheduled = false
    if !r.active { return }

    seq, x, y := r.latestSeq, r.latestX, r.latestY
    r.inFlight = true
    started := time.Now()
    r.applyPreview(x, y)
    r.lastDuration = time.Since(started)
    r.renderedSeq = seq
    r.inFlight = false

    if r.released { r.commitLatest(); return }
    if r.latestSeq != r.renderedSeq { r.scheduleNext(r.nextDelay()) }
}
```

The semantic requirement is **one wake token, not one queued function per event**. The controller must never enqueue multiple pending steps.

**Release and cancel.** `ButtonRelease` stores the release coordinates and sets `released`; it does *not* wait for a throttle interval. If no step is in flight it commits immediately; otherwise the in-flight step drains the latest coordinate before committing. Escape restores the original ratio and exits without any durable operation — which is trivially correct, because the committed tree was never mutated.

**Backpressure policy differs by event class**, and this is a design requirement, not a detail:

| Event class | Policy |
|---|---|
| Pointer motion, hover, resize preview | **Replace** the pending item with the newest |
| Durable ops, lifecycle events, accept results | **Preserve order**; never silently replace |
| High-volume telemetry | Sample or aggregate; report the lost count |
| Script state snapshots per surface | Keep the newest complete snapshot |
| Text and key input | Preserve order; explicit overflow failure |

Note that the existing `boundedQueue` drops the **newest** on overflow (`queue.go:30-46`) — correct for the broker's ordered events, exactly wrong for pointer state. Do not reuse it for the mailbox.

## 5.3 Preview state versus committed state

Two implementations, in increasing order of cleanliness.

**Minimal: a ratio override map.** The authoritative desktop keeps its original ratio; layout takes an override.

```go
type LayoutOverrides struct {
    Ratios map[wmcore.NodeID]float64
}

func LayoutWithOverrides(root *Node, area Rect, gap int, o LayoutOverrides) LayoutResult
```

On release, exactly one call emits the durable operation and the broker event:

```go
_, err := w.Apply(wmcore.Op{Op: wmcore.OpSetRatio, Node: tx.split, Ratio: tx.previewRatio})
```

**Better: a layout view interface.** Useful later for drag-docking, workspace previews, and animated transitions, and it avoids cloning trees:

```go
type LayoutView interface {
    Root() *wmcore.Node
    Ratio(node wmcore.NodeID) float64
    LeafApp(node wmcore.NodeID) string
}
```

The durable desktop implements it directly; a preview wrapper delegates everything except a small override map; `Layout` consumes the interface.

**Observers.** Preview cadence should still be visible to scripts, but as explicitly lossy telemetry, clearly distinct from the committed `wm.op` event that follows once on release:

```json
{
  "event": "window.resize-preview",
  "data": { "split": "n17", "ratio": 0.618, "sequence": 42, "mode": "live" }
}
```

## 5.4 Chrome and content are different things

This is the largest structural change and the largest payoff.

**Target window hierarchy:**

```text
frame (root child; owns placement and clipping; no large painted background)
├── title window     y=0, height=draw.TitleH, width=frame width
├── client window    y=TitleH, fills the interior
├── left border      optional thin child (InputOutput or InputOnly)
├── right border     optional thin child
└── bottom border    optional thin child
```

Three viable implementations:

1. **Title child window plus frame background pixel and X border width.** The frame uses `CwBackPixel`; only the title child owns an RGBA and shm surface. **Recommended first step** — it is sufficient for the current paper-and-ink visual style, and the drawing stack already produces title images.
2. **Four thin decoration children.** Supports per-edge visuals and hit areas without a full bitmap.
3. **Core X or XRender drawing.** Rectangles and text drawn directly; text may still use a cached glyph backend.

**Geometry reconciliation becomes trivial:**

```go
frame.MoveResize(x, y, w, h)
title.MoveResize(0, 0, w, draw.TitleH)
client.Configure(BorderW, draw.TitleH, w-2*BorderW, h-draw.TitleH-BorderW)
```

Only the title's *width* changes. A pane **height** change requires no title pixels at all. A **position-only** change requires no paint whatsoever.

**A title render key** makes even width changes cheap to skip:

```go
type TitleRenderKey struct {
    Width         int
    Title         string
    IconRevision  uint64
    Focused       bool
    Urgent        bool
    ThemeRevision uint64
    ButtonState   uint32
}
```

**Internal PBUI surfaces are the exception.** Builtin and script tiles have no client child covering the interior, so they genuinely need a content surface. Split the record conceptually:

```go
type frameChrome struct {
    frameWin *xwindow.Window
    titleWin *xwindow.Window
    titleBuf *Surface
    borders  [...]
}

type clientContent struct { client xproto.Window }

type pbuiContent struct {
    contentWin *xwindow.Window
    surface    *PixelBuffer
}

type ContentHost interface {
    Configure(Rect)
    SetVisible(bool)
    Invalidate(Invalidation)
    DropLargeResources()
    Destroy()
}
```

A frame composes one chrome with exactly one content host.

**Consequences:** focus and accept-mode transitions become cheap — repaint the old and new *title* layers, not two whole panes, and do not touch ordinary client chrome at all for an accept-mode change. Four 1920 × 1080 frames drop from ~31.6 MiB of RGBA buffers to ~660 KiB of title buffers.

## 5.5 Buffer lifetime: capacity, not exact size

Once chrome is separated, most WM-side resize cost disappears without a complex allocator. For the content surfaces that remain (PBUI tiles), use capacity buffers:

```go
type PixelBuffer struct {
    CapacityW, CapacityH int
    ViewW, ViewH         int
    Pix                  []byte
    Surface              *xshm.Surface
}
```

Growth uses buckets rather than exact dimensions:

```text
requested  641 × 421  →  capacity  768 × 512
requested  770 × 512  →  capacity 1024 × 512
requested 1000 × 700  →  capacity 1024 × 768
```

Shrinking changes only the viewport. Growth recreates resources occasionally, not per pixel — and therefore pays the two round trips occasionally, not per tick.

**A caveat that must be prototyped, not assumed:** shared pixmaps have fixed dimensions, so a larger pixmap cannot simply serve as the background pixmap of a smaller window without a clipping policy. The practical approaches are a content child window sized to capacity and clipped by its frame, or `ShmPutImage` from a capacity buffer into the logical drawable. **Benchmark both before committing.**

## 5.6 Reconciliation as an explicit plan

Replace the implicit "relayout means: geometry, dividers, map/unmap, resize, paint, floats, focus" with a computed plan:

```go
type ReconcilePlan struct {
    Geometry   []GeometryChange
    Visibility []VisibilityChange
    Stacking   []StackChange
    Decoration []DecorationDamage
    Content    []ContentDamage
    Divider    []DividerChange
    Focus      *FocusTransition
}

type FrameSnapshot struct {
    Rect      wmcore.Rect
    Visible   bool
    StackBand StackBand
    Focused   bool
    TitleHash uint64
    ThemeGen  uint64
    AcceptGen uint64
}
```

```text
model + previous snapshot + preview overrides
        │
        ▼
compute desired snapshot
        │
        ▼
diff snapshots  →  ReconcilePlan
        │
        ▼
execute batched X requests, then render damage
```

**Execution order matters** and should be deterministic:

1. Unmap surfaces that must disappear before overlap changes.
2. Configure parent frames and decoration children.
3. Configure client children.
4. Configure dividers and overlays.
5. Map newly visible surfaces.
6. Apply stacking changes.
7. Install or copy damaged pixmaps.
8. Update EWMH properties whose values changed.
9. **Flush once.**

The divider plan should distinguish `move`, `resize`, `map`, `unmap`, and `appearance` — because today only `appearance` requires a repaint and it is the rarest.

## 5.7 Subtree-limited layout

A divider belongs to a split node. Changing its ratio changes only rectangles within that split's descendant subtree; ancestors and unrelated branches keep theirs.

```go
type TreeIndex struct {                    // ephemeral, derived, not serialized
    Parent map[NodeID]NodeID
    Node   map[NodeID]*Node
    Depth  map[NodeID]int
}

type LayoutSlice struct { Items []LayoutItem }   // stable depth-first order

func LayoutSubtree(split *Node, splitRect Rect, gap int,
                   overrides LayoutOverrides, dst []LayoutItem) LayoutSlice
```

The `TreeIndex` alone removes the O(n²) `Find`-in-loop (open item #6) and is worth doing on its own merits, before any incremental layout exists. A stable slice order (instead of map iteration) additionally makes diffing and X request construction deterministic, and lets each `LayoutItem` carry node kind and split direction so consumers stop re-finding nodes.

**Keep the full `Layout` path** as the correctness reference for workspace switches, monitor changes, startup, deserialization, and tests — and assert their equivalence with a property test:

```text
For every generated tree, area, gap, and valid ratio override:
    merge(previousFullLayout, LayoutSubtree(activeSplit)) == FullLayoutWithOverride
```

## 5.8 Resize modes

**Outline** moves a thin helper window while the pointer is down and does not resize application frames; on release the final ratio commits and normal reconciliation runs once. This is what i3 does for graphical tiled resize. It keeps pointer tracking responsive with slow Electron, Java, remote-X, or graphics-heavy clients, and it produces one entry in the operation log. Cost: content does not resize continuously, and release produces one visible jump.

**Live** is reserved for clients and surfaces that keep up — and even then it must use latest-wins coalescing, transient ratios, subtree geometry, and thin decoration.

**Adaptive** picks between them from measured budget:

```text
start drag:
    mode = live; budget = 6 ms WM work per update

each preview:
    if queue_lag > 24 ms:                       use outline from now on
    else if last_wm_work > 10 ms twice:         use outline
    else if a sync-capable client has not acked: do not issue another live resize
    else:                                        perform a live update

on release:
    apply final geometry regardless of preview mode
```

A less abrupt variant renders the outline at pointer rate while performing live application resizes at 20–30 Hz when budget allows. Policy can be advisory per client:

```js
wm.resizePolicy({ default: "adaptive", targetHz: 60 });
wm.rule({ class: /mpv|gamescope/i, resize: "outline" });
wm.rule({ class: /kitty|Alacritty/i, resize: "live" });
```

## 5.9 Decision records

### Decision: Preview-only resize is the default; live resize is a policy

- **Context.** Every accepted pointer sample currently mutates the durable tree and triggers a full reconciliation, coupling client redraw cost and X resource churn to pointer frequency.
- **Options considered.** (a) Keep live-by-default and optimize the paint path further. (b) Preview-only by default, commit on release, live as opt-in policy. (c) Always outline.
- **Decision.** (b).
- **Rationale.** GGWM-005 and GGWM-006 already took the paint path most of the way; the remaining costs are structural (round trips, resource churn, client redraw), not micro-optimizable. i3 uses precisely this design for graphical tiled resize. Preview-only also makes cancellation trivially correct, because the committed tree is never mutated.
- **Consequences.** Content does not resize continuously by default, so the preview affordance must be clear and snap-aware. The operation log gains one entry per drag instead of sixty, improving replay and undo. Live mode must be re-earned with measurements.
- **Status.** proposed.

### Decision: A latest-wins mailbox replaces the time gate as the admission mechanism

- **Context.** The existing 16 ms gate limits admission but does not drain queued X events; there is no motion compression. The final position is already correct (`input.go:392-395`), so the defect is mid-drag lag, not final accuracy.
- **Options considered.** (a) Keep the gate and raise the interval. (b) Adopt `mousebind.Drag`, which compresses motion in xgbutil. (c) A one-slot mailbox plus a single scheduled step, owned by the WM loop.
- **Decision.** (c), with (b) evaluated as a complementary cheap win.
- **Rationale.** A one-slot mailbox gives i3's "latest state wins" semantics without an event-loop rewrite, and — unlike raising the throttle interval — it improves responsiveness rather than masking overload. Because the WM loop already owns all state, no atomics or locks are required; a plain field and a `scheduled` flag suffice.
- **Consequences.** Intermediate positions are intentionally discarded, so any observer of preview events must treat them as lossy telemetry. The controller must guarantee at most one pending step, or it degenerates back into a queue.
- **Status.** proposed.

### Decision: Separate chrome windows from content surfaces

- **Context.** The title strip is drawn into a full-pane `image.RGBA` (`manage.go:381-413`), so any geometry or focus change repaints the whole pane, and any size change reallocates client and server resources.
- **Options considered.** (a) Damage tracking on the existing full-pane buffer. (b) A title child window plus frame background pixel and X border. (c) Four decoration children. (d) Shape/XFixes input regions.
- **Decision.** (b) first; (c) later if per-edge visuals are needed.
- **Rationale.** Option (a) adds complexity while preserving the wrong representation — it makes a 7.9 MiB buffer smarter instead of making it 165 KiB. Option (b) is the smallest change that makes decoration cost scale with the title rather than the pane, and the existing drawing stack already produces title images.
- **Consequences.** Frame creation and teardown must own title and border resources with an idempotent close path, and title hit regions, drag gestures, menus, and focus visuals all migrate. This is the highest-risk item in the plan because it touches frame lifecycle; it is sequenced after the cheap wins for that reason.
- **Status.** proposed.

### Decision: Keep the single-owner loop; do not add locks or a render thread

- **Context.** All X-facing state lives on one goroutine, and `pkg/wmx11` contains zero mutexes. A tempting response to "the loop is busy" is to parallelize it.
- **Options considered.** (a) Introduce a render thread with locked state. (b) Parallelize pixel conversion further. (c) Keep the single owner and remove work instead.
- **Decision.** (c). Retain the existing bounded parallelism inside `ConvertRows` (`ximage.go:85-93`), which operates on stable independent buffers.
- **Rationale.** Parallelizing mutable X and WM state reintroduces exactly the ordering problems that `focusState`/`fullscreenState` were built to eliminate, and X commits must stay centralized regardless. The measured problem is unnecessary work, not insufficient concurrency.
- **Consequences.** Every optimization must reduce work rather than move it. Pure, cancellable work (scene compilation, image decoding) may still use workers, with generation stamps.
- **Status.** accepted (this is existing practice; recorded to prevent re-litigation).

### Decision: Do not begin with a compositor, GPU backend, or damage tracking

- **Context.** "Rendering is slow" invites reaching for GPU acceleration or dirty rectangles.
- **Options considered.** (a) Compositor/OpenGL renderer. (b) Damage tracking on current buffers. (c) Remove the work and the resource churn first.
- **Decision.** (c).
- **Rationale.** A GPU backend does not remove stale-event processing, durable preview operations, `ConfigureRequest` storms, checked round trips, or duplicated client redraws. Damage tracking on a full-size decoration buffer adds complexity while preserving the wrong representation.
- **Consequences.** Damage tracking is deferred to Phase 6, after the retained-surface work makes honest dirty rectangles available. Revisit a GPU backend only after profiling a retained CPU path.
- **Status.** accepted.

---

# Part VI. Implementation plan

Seven phases. Each has a goal, file-level work, and **exit criteria you can actually test**. Phases 0–2 are the ones that matter most; 3 onward are structural.

A note on sequencing philosophy: fix input responsiveness before adding widgets; make ownership explicit before adding lifecycle features; and keep every phase testable without JavaScript where possible.

## Phase 0 — Measure

**Goal.** Make the current path observable. Nothing else on this list should merge without before/after evidence, and the Tier-1 hypothesis in §3.2 is unproven until this phase confirms it.

**Work.**

- New `pkg/wmx11/perftrace.go` (or a small `pkg/perftrace`), with cheap no-op behavior when disabled. Assign every drag a `DragID` and every accepted tick a sequence number.
- Add the stage spans from §3.4 around: `dividerMotion`, `relayoutPaint`, `wmcore.Layout`, `syncDividers`/`paintDivider`, `paintFrame`, `xshm.New`/`Destroy`, `ConvertRows`, the flush.
- **Instrument checked-request waits as their own counter.** This is the whole point: `.Check()` is invisible to a `.Reply()` audit.
- Record a bounded ring of `ResizeSample` records:

```go
type ResizeSample struct {
    DragID          uint64
    Seq             uint64
    QueueLag        time.Duration
    InputToFlush    time.Duration
    LayoutTime      time.Duration
    ReconcileTime   time.Duration
    PaintTime       time.Duration
    UploadTime      time.Duration
    NodesVisited    int
    XConfigureCount int
    MapCount        int
    CheckedWaits    int
    PaintPixels     int64
    UploadBytes     int64
    ShmCreates      int
    Coalesced       int
}
```

- Add a `--pprof` flag alongside the existing `GO_GO_WM_PPROF` env gate (`pkg/cmds/wm.go:82-102`) — the env-only interface is a discoverability problem.
- Expose the ring through the existing IPC control plane (`pkg/wmx11/ipc.go`) as a `perf` query returning **bounded aggregates**, not an unbounded trace.
- **Write the first benchmarks.** These need no display and can land immediately: `wmcore.Layout`, `wmcore.NeighborLeaf`, `draw.Fill`, `draw.Text`, `draw.ConvertRows`, `draw.TitleStrip.Render`, `uispec.Render`, `launcher.Registry.Match`, `repl.Session.Spec`.
- Add a scripted drag harness under Xvfb/Xephyr driving `xdotool` at 60, 120, 240, and 1000 samples/second.

**Exit criteria.**

- A scripted drag reports p50/p95/p99 for input-to-flush, plus X request counts, paint pixels, shm creates, and coalesced samples.
- `go test -bench` produces a committed baseline for the pure functions above.
- The Tier-1 hypothesis is **confirmed or refuted with numbers.** If shm recreation is not in fact dominant, Phase 2 gets re-ordered.

**Risk.** Low. No behavior changes.

## Phase 1 — Cheap structural wins

**Goal.** Remove obviously redundant work. Everything here is small, local, and independently testable.

**Work.**

- **`pkg/wmcore`:** add an ephemeral `TreeIndex` (`map[NodeID]*Node` plus parents), rebuilt at transaction boundaries. Replace `Find`-in-loop at `manage.go:325` and `divider.go:41`. *(open item #6)*
- **`pkg/wmx11/input.go`:** pass the already-computed `items` from `dividerMotion` into the reconciler instead of recomputing, or hoist the split rectangle so only one `Layout` runs per tick. *(item #5)*
- **`pkg/wmx11/divider.go`:** add an appearance generation to `dividerWin`; call `paintDivider` only when orientation, state, theme generation, or size actually changed. Move the window otherwise. Consider a background pixel plus `ClearArea`, or one cached pixmap per (orientation, state). *(item #4)*
- **`pkg/wmx11/manage.go`:** add `mapped bool` to `frame`; issue Map/Unmap only on a visibility *transition*. Same in `syncFloats`. *(item #7)*
- **`pkg/wmx11/manage.go`:** replace the `relayout()` in `handleConfigureRequest` (`:593-595`) with a synthetic `ConfigureNotify` (§1.2). *(item #11)*
- **`pkg/wmx11/input.go`:** give `gripMotion` the same admission control as `dividerMotion`, and cache the drop-preview surface instead of blitting it uncached per event. *(item #9)*

**Exit criteria.**

- Reconciliation performs zero recursive `Find` calls; a benchmark shows layout+reconcile time scaling linearly, not quadratically, in node count.
- One `wmcore.Layout` call per accepted tick (assert with a counter).
- A drag over an unchanged-appearance divider produces **zero** `paintDivider` calls.
- A relayout with unchanged visibility produces **zero** Map/Unmap requests.
- A test client spamming `ConfigureRequest` produces zero relayouts and zero paints, and receives correct synthetic notifications.
- No visible behavior change; golden tests unchanged.

**Risk.** Low–medium. The Map/Unmap change is the one to test carefully — workspace switching intentionally unmaps frames, and those events must not be mistaken for client withdrawal.

## Phase 2 — Kill the resize-time resource churn

**Goal.** Remove the round trips and the reallocation from the hot path. This is the phase that should produce the biggest measured win.

**Work.**

- **`pkg/wmx11/manage.go`:** add a resize path that configures frame and client geometry **without** calling `paintFrame`, preserving existing pixels until release or Expose. Use this to quantify the upper bound of paint removal *before* restructuring chrome. *(items #2, #3)*
- **`pkg/xshm`:** separate backing memory from the logical viewport. Add capacity-based sizing (§5.5) with bucketed growth. Add buffer state (`free`, `drawing`, `server-reading`) and memory counters. Keep the non-SHM fallback correct.
- Validate the shared-pixmap clipping caveat in §5.5 experimentally before committing to a strategy.
- **`pkg/wmx11/wm.go`:** stop `afterOp` from calling `paintAll=true` relayout for operations that changed no geometry. Derive a coarse dirty classification from the op kind. *(item #10)*
- **`pkg/wmx11/pbui.go`:** make accept-mode transitions damage only the semantic overlay, not every frame. *(item #13)*

**Exit criteria.**

- **Zero `shmget`/`Attach`/`CreatePixmap` calls during a steady-state divider drag** after warm-up, verified by the Phase-0 counters.
- **Zero checked-request waits in the admitted motion path**, verified by counter.
- Manager paint pixels and upload bytes approach zero during a terminal-only drag.
- A workspace rename produces no frame repaints.
- An accept-mode toggle repaints no client chrome.
- Final geometry after release is pixel-identical to the pre-change implementation.

**Risk.** Medium. Buffer lifetime bugs manifest as flicker or stale pixels; the golden tests plus an explicit "no stale blank region after release" assertion are the guard.

## Phase 3 — Preview/commit separation and resize modes

**Goal.** Remove durable model and event work from pointer cadence, and provide a cheap fallback for slow clients.

**Work.**

- New `pkg/wmx11/resize.go` owning: the resize transaction and mode; the latest-coordinate mailbox and scheduler; preview ratio calculation and snapping; budget state and adaptive transitions; optional client sync tracking; final commit and cancel; resize metrics. X execution stays delegated to reconciliation; tree mutation stays delegated to `WM.Apply` on commit.
- **`pkg/wmcore/layout.go`:** add `LayoutWithOverrides` (§5.3).
- **`pkg/wmx11/input.go`:** route press/motion/release/cancel through `resizeController.Begin`/`Motion`/`Release`/`Cancel`; remove `lastPaint` as the admission policy.
- Add an outline helper surface — possibly reusing the active divider window — and the `outline | live | adaptive` policy, with the budget transitions from §5.8.
- Emit `window.resize-preview` as explicitly lossy telemetry; emit exactly one `wm.op` on release.

**Exit criteria.**

- **Durable operations per completed drag: exactly one. Per cancelled drag: zero.**
- An outline drag produces **zero** client `ConfigureNotify` events until release.
- Replaying the committed operation log reproduces the final tree exactly.
- Under an artificially injected 20 ms paint delay, release latency stays bounded and the final position is exact.
- A burst of 1000 synthetic motion events produces far fewer preview updates and **no post-release stale replay**.

**Risk.** Medium. Cancellation and teardown during a drag (client destroyed, workspace switched, fullscreen entered) are the sharp edges; they need explicit tests.

## Phase 4 — Chrome/content split

**Goal.** Make decoration cost scale with the title, not the pane. Highest payoff, highest risk.

**Work.**

- New `pkg/wmx11/frame_chrome.go`: title windows, border policy, title hit regions, title buffers, focus and accept appearance, and an **idempotent** chrome lifecycle with tests for partial construction failure and repeated close.
- Migrate title hit testing, drag gestures, menus, and focus visuals off the pane surface.
- Introduce `ContentHost` (§5.4); keep full content surfaces only for builtin and script tiles.
- Add the `TitleRenderKey` (§5.4) so unchanged titles skip rendering.

**Exit criteria.**

- Normal client resize creates and destroys **no** full-size RGBA or shm surface.
- Decoration bytes scale with title and border area, not client area.
- A **pane height change paints zero title pixels.**
- A focus change paints only the old and new title layers.
- Memory for four large client frames falls by roughly an order of magnitude (§1.5 arithmetic: ~31.6 MiB → ~660 KiB).
- Golden title screenshots unchanged; no flicker or stale background under tested compositors.

**Risk.** High — frame lifecycle changes. Mitigate with the idempotent-close discipline and an Xvfb lifecycle test suite.

## Phase 5 — Subtree layout and the reconcile plan

**Goal.** Make per-tick work scale with the affected subtree, and make reconciliation an explicit, testable diff.

**Work.**

- `pkg/wmcore/layout.go`: `LayoutSubtree` plus a stable-order `LayoutSlice` (§5.7), with `LayoutItem` carrying node kind and split direction so consumers stop re-finding nodes.
- `pkg/wmx11`: `FrameSnapshot` and `ReconcilePlan` (§5.6); a pure `DiffXState(desired, applied) []XRequest` that can be table-tested without a display.
- Batch all geometry requests in the deterministic order of §5.6 and flush once.

**Exit criteria.**

- The subtree-equivalence property test passes (§5.7).
- Assertions confirm no object outside the dirty subtree receives a geometry request.
- Request counts become testable without a live X server.
- Reconciliation is idempotent: applying the same desired snapshot twice produces no additional requests or paint.

**Risk.** Medium.

## Phase 6 — Retained surfaces, damage, and the other consumers

**Goal.** Extend the gains beyond the WM's own frames.

**Work.**

- `pkg/apps/xapp`: add a redraw scheduler with a latest pending size and a single dirty flag; cache the image and upload objects; adopt the shm path. **This is currently the largest gap between the WM's paint path and everything else** — `xapp` creates a fresh client image, a fresh server pixmap, uploads, and frees, *per redraw* (`xapp.go:274-286`). *(item #14)*
- Replace the unbounded goroutine fallback in `xapp.shell.post` (`xapp.go:228-234`) with a proper policy. *(item #17)*
- `pkg/repl/session.go`: window the cell range **before** materializing rows, not after (`session.go:133-170`). *(item #15)*
- `pkg/wmx11/launcher.go`: move `registry.Refresh()` off the WM loop at popup open (`launcher.go:90`). *(item #16)*
- `pkg/apps/uispec`: add optional stable keys to rows and segments; split `Render` into measure / layout / paint; add a retained layer cache and region-based damage.
- `pkg/draw`: add dirty-rectangle operations; hoist the per-call `image.NewUniform` out of `draw.Text` (`theme.go:250`); memoize `TextWidth` for table rendering.

**Exit criteria.**

- The `xapp` shell performs at most one pending redraw and installs the exact final size.
- Hovering one menu row repaints only the old and new rows.
- An accept-mode transition runs no application render and leaves base-layer pixel hashes unchanged.
- A REPL with 200 cells materializes only the visible window.
- Existing `ui.row/text/object/button` scripts remain source-compatible through adapters.

**Risk.** Medium. Staged; each bullet is independently shippable.

## Deliberately not in this plan

- Retained keyed widget trees as a full replacement for `uispec` (Phase 6 lays groundwork only).
- Runtime supervision, capability manifests, hot reload.
- PBUI type registry, translators, object handles.
- Multi-monitor / RandR.
- Any compositor or GPU backend.

---

# Part VII. Testing and validation strategy

## 7.1 What to test at which layer

**Pure unit and property tests** (no display; these should dominate):

- Tree operations: ID uniqueness, ratio bounds, no missing children, deterministic serialization.
- Layout: rectangles partition the parent subject to gaps; no negative sizes; deterministic output.
- **Subtree layout equals full layout with the same override** (§5.7).
- Replay: the committed operation stream reproduces the final desktop.
- **A preview override does not mutate the serialized desktop.**
- Neighbor selection determinism.
- `DiffXState` request-plan tables.

**Display-free state-machine tests** — the pattern already established by `focus_state_test.go`, which exercises RC-5/6/7/12/13 against a bare `&WM{}`:

- Resize transaction begin / motion / release / cancel.
- Latest-wins coalescing under arbitrary event sequences.
- Adaptive mode transitions driven by injected timing.
- Termination of the transaction when a window disappears mid-drag.

**Xvfb integration tests:**

- Manage / reparent / save-set lifecycle.
- **Synthetic `ConfigureNotify` contents** for denied tiled requests.
- Client destroy or unmap during a resize.
- Float / fullscreen / workspace transitions during a drag.
- MIT-SHM fallback behavior with `GO_GO_WM_NO_SHM=1`.

**Golden rendering tests.** Extend the existing `pkg/draw/golden_test.go` to title strips at multiple widths and focus states. Add **incremental-render tests**: render a full surface, apply a small change through the damage path, and assert the result is pixel-identical to a fresh full render.

**Benchmarks.** Start with the pure functions listed in Phase 0 — they need no display and can land before any behavior change.

## 7.2 The environment matrix

| Environment | Purpose |
|---|---|
| Xvfb | Deterministic CI; validates request counts and catches gross regressions |
| Xephyr | Nested interactive testing and visual capture; disposable |
| Bare local Xorg | The actual deployment path and real input latency |
| With and without a compositor | Presentation delay differs materially |
| `GO_GO_WM_NO_SHM=1` | Validates the fallback and prevents shared-memory assumptions |
| Remote or delayed X proxy | Exposes round-trip assumptions — **this is where Tier 1 becomes obvious** |

## 7.3 The client corpus

Grow `testwin` into a family of purpose-built fixtures:

- **Fast client** — repaints a solid fill immediately.
- **Slow client** — sleeps a configurable interval (say 40 ms) on configure before repainting.
- **Sync client** — implements `_NET_WM_SYNC_REQUEST` and acknowledges after repaint.
- **Hint client** — exercises min/max/base/increment/aspect hints.
- **Storm client** — floods `ConfigureRequest`.
- **Hostile client** — destroys itself mid-drag; ignores sync; advertises a dead transient leader.

Plus real applications: a terminal, a browser or Electron app, GTK and Qt dialogs, and `go-go-wm demo` PBUI clients.

## 7.4 The scripted resize scenario

Synthesize press, N motion positions, and release over a fixed duration, at 60 / 120 / 240 / 1000 samples per second. **The WM should perform bounded commits independent of input rate, and always apply the final position.** Emit a machine-readable report:

```json
{
  "scenario": "two-1920x1080-clients",
  "mode": "adaptive",
  "input_samples": 240,
  "preview_updates": 58,
  "coalesced_samples": 182,
  "durable_ops": 1,
  "p50_wm_ms": 2.1,
  "p95_wm_ms": 4.8,
  "p99_queue_lag_ms": 12.0,
  "shm_creates_during_drag": 0,
  "checked_waits_during_drag": 0,
  "rgba_megabytes_written": 19.4,
  "release_to_final_ms": 17.3
}
```

## 7.5 Correctness assertions that must hold during every benchmark

- No stale blank region remains after release.
- Client and frame rectangles maintain the title and border offsets.
- Snapped ratios produce deterministic geometry.
- Fullscreen consistently cancels or blocks divider interaction.
- Workspace switching during a drag cancels or transfers the interaction by one documented rule.
- Destroying either affected client during a drag cleans up safely.
- A slow client cannot stall the WM loop.
- A slow script renderer cannot delay geometry commits.
- Disabling SHM produces identical final pixels.

## 7.6 Definition of done for resize performance

1. Before/after traces exist for outline, live-terminal, live-PBUI, and slow-client scenarios.
2. Motion coalescing and release latency are measured, not asserted.
3. **No checked request or reply wait appears in the admitted motion path** without an explicit, documented design reason.
4. Durable operations per drag: one on success, zero on cancel.
5. Resource creation during a stable-capacity drag: zero.
6. Correctness tests cover destroy, unmap, fullscreen, and workspace switch during a drag.

---

# Part VIII. Risks, alternatives, and open questions

## 8.1 Risks

| Risk | Consequence | Containment |
|---|---|---|
| The Tier-1 hypothesis is wrong | Phase 2 is mis-prioritized | Phase 0 is explicitly gated on confirming it; re-order if refuted |
| Chrome/content split destabilizes frame lifecycle | Leaked windows, stale pixels, focus bugs | Idempotent `FrameResources.Close()`; Xvfb lifecycle suite; phase it last among the cheap wins |
| Oversized-pixmap reuse does not work as assumed | Phase 2's capacity design fails late | Prototype the clipping question *first* (§5.5); benchmark both `ShmPutImage` and content-child approaches |
| Map/Unmap diffing breaks workspace switching | Windows vanish or fail to appear | Track desired vs. observed visibility explicitly; use suppression tokens around WM-initiated unmaps |
| Preview/commit separation breaks replay | Operation logs no longer reproduce state | Property test: replaying committed ops reproduces the final tree |
| Performance work stalls on missing measurement | Cargo-cult optimization | Phase 0 first; no merges without before/after traces |
| `w.ops` backpressure masks a regression | A slow loop silently throttles JavaScript | Expose ops-queue depth and oldest-task age in the `perf` query |

## 8.2 Alternatives considered and rejected

- **More goroutines in the WM loop.** Rejected. Parallelizing mutable X state reintroduces the ordering problems `focusState` eliminated. Keep X commits centralized; reduce work instead. (The existing bounded parallelism in `ConvertRows` is fine — it operates on stable, independent buffers.)
- **Raise the drag throttle interval.** Rejected. A lower update rate masks overload, worsens responsiveness, and does nothing about stale queued events.
- **Use MIT-SHM more aggressively.** Rejected as a *first* step, and this codebase is the proof: SHM improves transport, but exact-size shared-resource creation during resize is *worse* than the plain path. Minimize damage and stabilize lifetime first.
- **Implement dirty rectangles before changing frame buffers.** Rejected. Damage tracking on a full-size decoration buffer adds complexity while preserving the wrong representation.
- **Let JavaScript draw directly.** Rejected. It couples VM latency to Expose and resize, and destroys caching, validation, resource budgets, and future backends. The current `uimod` boundary is correct.
- **Compositor / GPU backend.** Deferred. It removes none of the identified costs.

## 8.3 Open questions

1. **Does the Tier-1 hypothesis hold?** Unresolved until Phase 0. Everything downstream is contingent.
2. **Can a capacity-sized shared pixmap serve a smaller window?** §5.5 flags this as needing an experiment, not an assumption.
3. **Is `mousebind.Drag` a drop-in win?** xgbutil already compresses motion there (`drag.go:97,116`). Adopting it might deliver most of the mailbox benefit for a fraction of the work — or it might conflict with the existing grab handling. Worth a spike in Phase 0.
4. **What is the right default resize mode?** The guides disagree: one recommends preview-only as the default, another treats live as the target with outline as a diagnostic. The adaptive policy in §5.8 defers the choice to measurement, which is probably right, but the *default* still needs a decision.
5. **Which of the three source guides is canonical?** Two overlap heavily. The ticket should declare one, or merge them.
6. **The GGWM-012 numbering collision.** The proposed backlog assigns GGWM-012 to the divider-preview work; this ticket already holds it. Renumber before filing GGWM-013…029.
7. **Should `boundedQueue`'s drop policy be configurable per class?** It currently drops the newest — right for ordered broker events, wrong for pointer-like state. The mailbox avoids it, but other coalescible streams will want the other policy.

---

# Part IX. Intern onboarding

## 9.1 First week reading path

Do these in order. Do **not** start by changing `handleMotion`.

1. `pkg/wmcore/tree.go`, `layout.go`, `ops.go` — then run `go test ./pkg/wmcore/...`. This is the whole model, and it has no X in it.
2. `pkg/wmx11/wm.go:236-281` — the event loop. Convince yourself of the mutual-exclusion argument in §2.2 by reading `xgbutil`'s `eventloop.go` alongside it.
3. `pkg/wmx11/focus_state.go` — the best-factored code in the repository, and the model for what the resize state should become. Read `focus_state_test.go` to see how display-free decisions get tested.
4. `pkg/wmx11/manage.go:305-455` — `relayoutPaint` and `paintFrame`. This is the hot path.
5. `pkg/wmx11/input.go:300-400` — the drag handlers.
6. `pkg/xshm/xshm.go` — all 151 lines. Find the two `.Check()` calls yourself.
7. `pkg/draw/ximage.go` and `theme.go` — the optimized rendering primitives, and the comments recording what they replaced.
8. `pkg/jsmod/wmmod/module.go:378-404` and `pkg/wmx11/scripting.go:25-39` — the two directions of the JS boundary.
9. Run the WM under Xephyr with `GO_GO_WM_PPROF=localhost:6060` and capture a profile during a drag.

## 9.2 Labs

Each lab has a deliverable that is *evidence*, not just working code.

**Lab 1 — Trace one client lifecycle.** Run a test window under Xephyr. Record `MapRequest`, property reads, frame creation, reparent, map, focus, `UnmapNotify`, teardown. Draw the sequence diagram from the logs. Explain why the save set is installed *before* reparenting. **Evidence:** annotated trace plus a lifecycle diagram; before/after X resource counts proving idempotent teardown.

**Lab 2 — Prove layout invariants.** Generate random valid op sequences; assert unique node IDs, valid ratios, correct leaf counts, and that layout rectangles partition the parent subject to gaps. Minimize a failing sequence. **Evidence:** property-test output and the exact invariant that failed.

**Lab 3 — Find the round trips.** Instrument `xshm.New` and `Destroy`. Run a scripted drag and count `Attach`/`CreatePixmap` calls per second. Then set `GO_GO_WM_NO_SHM=1` and compare. **Evidence:** a table of checked waits per tick, and a written answer to "why did the fallback path behave differently than you expected?" *(This lab reproduces the central finding of this document.)*

**Lab 4 — Make `ConfigureRequest` correct and cheap.** Write a client that repeatedly requests random sizes while tiled. Assert the WM sends a synthetic `ConfigureNotify` with correct root-relative geometry. Remove the full relayout. Measure paint and layout counts before and after. **Evidence:** a request trace showing zero relayouts and correct notifications.

**Lab 5 — Implement latest-wins motion.** Build a display-free mailbox test with 1000 motion samples and one release. Integrate into the divider drag *without changing rendering*. Add queue-lag and replaced-sample counters. Demonstrate bounded release latency under an artificial 20 ms paint delay. **Evidence:** before/after timeline.

**Lab 6 — Add outline resize.** Create an outline helper window. Keep the durable ratio unchanged during the drag; commit one operation on release and restore on cancel. **Evidence:** a one-drag operation log containing exactly one `set-ratio`.

**Lab 7 — Split frame chrome.** Add a title child window for normal clients. Move title hit regions and painting to it. Remove the full-frame decoration buffer. **Evidence:** a memory profile and a golden title screenshot proving visual equivalence.

**Lab 8 — Stop repainting dividers.** Add an appearance generation to `dividerWin`. Prove that a drag produces zero `paintDivider` calls while the divider still tracks the pointer. **Evidence:** paint counters plus a screen capture.

## 9.3 Code review checklist

**Before editing an X event handler:**

- Which state machine owns this event?
- Is the event ordered, durable, replaceable, or sampleable?
- Can the handler perform a reply wait **or a checked request**?
- Can it call JavaScript, disk, process, network, or broker synchronously?
- Which fields may change, and does one method own them?
- What happens if the target window disappears mid-operation?
- What events will the WM's own requests generate in response?

**Before adding a render call:**

- Did geometry, content, style, semantic state, or *only position* change?
- Can this be expressed as damage on an existing layer?
- Is the buffer sized to what is actually visible, or to an unnecessarily large parent?
- Does this allocate Go memory, SHM, pixmaps, GCs, fonts, or X images?
- Will Expose repair from a cache afterward?
- Can multiple invalidations before the next turn be coalesced?
- Is the final exact render guaranteed after lossy previews?

**Before adding a JS callback:**

- Which owner loop runs it?
- What queue and backpressure policy applies — and is "drop newest" or "drop oldest" correct for this class?
- What happens if the callback throws, hangs, or returns malformed data?
- Does the previous valid snapshot remain visible?

---

# Appendix A. X11 event cheat sheet

| Event | Meaning in go-go-wm | Correct response | Common mistake |
|---|---|---|---|
| `MapRequest` | A non-override top-level wants to be visible | Classify, manage or float, reparent, map, update state | Mapping before reading transient/type/class policy |
| `ConfigureRequest` | A client requests geometry or stacking | Delegate by geometry owner; deny-and-report for tiles | **Calling a full relayout for a denied tiled request** |
| `DestroyNotify` | The X resource is gone | Idempotent teardown; clear focus/fullscreen refs | Assuming `UnmapNotify` always arrives first |
| `UnmapNotify` | Window unmapped — by the client *or* by us | Distinguish WM-initiated from withdrawal | Destroying a frame on our own workspace hide |
| `PropertyNotify` | Title, hints, state, type, protocols changed | Read only the relevant property | Refetching everything and repainting everything |
| `ClientMessage` | EWMH/ICCCM protocol message | Parse the atom; route to an explicit transition | Toggling raw fields directly |
| `FocusIn`/`FocusOut` | Server focus changed | Reconcile with focus state; check detail/mode | Treating every notification as user intent |
| `Expose` | Server needs content repaired | Reuse background pixmap or cached layer | Calling the application renderer unconditionally |
| `MotionNotify` | A pointer sample | **Latest-wins coalescing** | Preserving every sample; applying durable ops |
| `ButtonRelease` | Complete the gesture | Drain the latest coordinate; exact commit | Applying a stale last-admitted coordinate |
| `KeyPress` | Ordered keyboard input | Global binding, then scope/focus | Coalescing or dropping keys like pointer motion |
| `MappingNotify` | Keyboard mapping changed | Rebuild grabs and key translation | Keeping stale keycodes |

**Checked versus unchecked.** Use checked requests at lifecycle boundaries where immediate error attribution matters (creating a critical window). In a hot path, prefer unchecked plus connection-level error monitoring. **Record any checked request used during a drag in the performance review** — this is exactly how the current shm cost went unnoticed.

**Window IDs are reusable.** XIDs can be recycled after resources are destroyed. Never make long-lived script identity equal to an XID alone; pair logical identity with a generation.

# Appendix B. Metric names

Keep label cardinality bounded — no window titles, script paths, or node keys as metric labels; those belong in traces.

```text
wm_resize_input_total
wm_resize_preview_total
wm_resize_motion_replaced_total
wm_resize_commit_total
wm_resize_cancel_total
wm_resize_mode_transition_total{from,to,reason}
wm_resize_queue_lag_seconds
wm_resize_work_seconds{stage}
wm_reconcile_change_total{kind}
wm_x_request_total{opcode}
wm_x_wait_seconds{kind}          # kind ∈ {reply, checked}  ← instrument both
wm_render_pixels_total{kind}
wm_render_damage_pixels_total{surface}
wm_surface_resource_bytes{kind}
wm_shm_create_total
wm_shm_destroy_total
wm_ops_queue_depth
wm_script_event_total{queue,result}
```

# Appendix C. File reference index

Every file this document cites, and why it matters.

| Path | Relevance |
|---|---|
| `cmd/go-go-wm/main.go` | Command registration; deliberate absence of env auto-binding |
| `pkg/cmds/wm.go:50-102` | `wm` flags; `GO_GO_WM_PPROF` pprof server |
| `pkg/wmx11/wm.go:140-187` | All WM state — single-goroutine owned |
| `pkg/wmx11/wm.go:217-232` | `w.ops` (cap 256) and blocking `Post` |
| `pkg/wmx11/wm.go:236-281` | **The event loop** |
| `pkg/wmx11/wm.go:360-401` | `ApplyBatch` — existing burst coalescing |
| `pkg/wmx11/wm.go:405-415` | `afterOp` — one of two timing probes; `paintAll` relayout per op |
| `pkg/wmx11/manage.go:305-369` | **`relayoutPaint`** — the reconciler |
| `pkg/wmx11/manage.go:373-455` | **`paintFrame`** — full-pane paint and buffer churn |
| `pkg/wmx11/manage.go:420-432` | shm surface destroy/recreate on size change |
| `pkg/wmx11/manage.go:545-550` | Focus change repaints two whole panes |
| `pkg/wmx11/manage.go:593-595` | `ConfigureRequest` → full relayout |
| `pkg/wmx11/input.go:330-353` | **`dividerMotion`** — the 16 ms gate, double layout, durable op |
| `pkg/wmx11/input.go:355-372` | `gripMotion` — unthrottled |
| `pkg/wmx11/input.go:392-398` | Release replays the exact final coordinates |
| `pkg/wmx11/divider.go:41,60,132,148` | Unconditional divider repaint on the uncached path |
| `pkg/wmx11/events.go:17-34` | The Expose fast path — a model to copy |
| `pkg/wmx11/bars.go:110-133` | `blit` (uncached) versus `blitCached` |
| `pkg/wmx11/focus_state.go:26-342` | `fullscreenState` / `focusState` — the pattern to copy |
| `pkg/wmx11/scripting.go:25-39` | `ScriptBackend` post-and-wait |
| `pkg/wmcore/layout.go:104-152` | `Layout` — full tree, fresh map, every call |
| `pkg/wmcore/tree.go:290-324` | `Find` (O(n)) and `Leaves()` (allocates) |
| `pkg/draw/theme.go:108-144` | Lock-free atomic palette |
| `pkg/draw/theme.go:216-234` | Row-pattern `Fill` |
| `pkg/draw/ximage.go:56-108` | Row-major, parallel RGBA→BGRA |
| `pkg/xshm/xshm.go:74-116` | **`New` — the two `.Check()` round trips** |
| `pkg/apps/uispec/uispec.go:256-486` | Immediate-mode `Render`; repeated cell measurement |
| `pkg/jsmod/uimod/app.go:148-194` | The VM-free snapshot boundary |
| `pkg/jsmod/queue.go:30-46` | `boundedQueue` — drops the **newest** |
| `pkg/apps/xapp/xapp.go:228-234, 274-286` | Unbounded post fallback; uncached upload path |
| `examples/scripts/i3.js` | The i3 config port — useful as a realistic test workload |

# Appendix D. Glossary

**Adaptive resize.** A policy performing live resize while budget and client readiness permit, degrading to outline or lower cadence under load.

**Applied X state.** The WM's cache of geometry, mapping, stacking, and focus it believes it has already sent to the server. In go-go-wm today this exists only as `frame.rect`.

**Checked request.** An X request whose errors are collected synchronously via `.Check()`. **A checked request is a round trip.**

**Coalescing.** Replacing stale pending state with fresher state. Distinct from throttling.

**Damage.** The region of a surface whose pixels need repainting.

**Desired X state.** The X-facing state computed from the authoritative model. Not yet represented explicitly in the codebase.

**Durable operation.** A serializable model mutation whose replay reproduces committed desktop state — a `wmcore.Op`.

**Frame.** The WM-created parent window around a client or internal content host.

**Latest-wins queue.** A one-slot pending state where a new sample replaces an older unprocessed one.

**Owner loop.** The single serialized execution context allowed to touch a given mutable world — the WM loop for X state, a goja runtime owner for each VM.

**Preview state.** Lossy, transient interaction state used to display an in-progress gesture. Not part of the durable operation log.

**Reconciliation.** Computing and applying the minimal X and render changes needed to make observed state match desired state.

**Round trip.** A client-server exchange in which the client waits for a reply. Caused by `.Reply()`, `.Check()`, or explicit synchronization. **A flush is not a round trip.**

**Save set.** The X mechanism that reparents clients back to an ancestor if the WM dies. Installed before reparenting.

**`SubstructureRedirect`.** The root-window event mask that makes a client the window manager. Exclusive.

**Throttling.** Limiting how often work is admitted. Distinct from coalescing.

# Appendix E. Sources

**Imported into this ticket** (`sources/local/`):

- `go-go-wm-handbook.md` — "Building go-go-wm: X11 performance, interactive resizing, and a fully scriptable presentation-based UI"
- `go-go-wm_engineering_handbook.md` — "Building go-go-wm as a Programmable Presentation-Based Desktop"
- `go-go-wm-engineering-guide.md` — "go-go-wm Engineering Guide: A presentation-oriented, scriptable desktop runtime on X11"

All three were prepared 21 July 2026 against merge commit `5b73c9f37c97538f6767ecdc3ece4fb599932377`.

**Prior tickets** whose conclusions this document builds on: GGWM-005 (paint-path profiling, fast fills, drag throttling), GGWM-006 (MIT-SHM shared pixmaps), GGWM-010 (PR review, system primer), GGWM-011 (focus/fullscreen state encapsulation).

**External specifications and comparable implementations** cited by the source guides:

- ICCCM — https://www.x.org/releases/current/doc/xorg-docs/icccm/icccm.html
- EWMH — https://specifications.freedesktop.org/wm-spec/latest/
- MIT-SHM extension protocol — https://www.x.org/releases/current/doc/xextproto/shm.html
- X Synchronization extension — https://www.x.org/releases/current/doc/xextproto/sync.html
- XCB tutorial — https://xcb.freedesktop.org/tutorial/
- i3 `src/drag.c` (event draining, latest-motion handling) and `src/resize.c` (helper-bar graphical resize) — https://github.com/i3/i3
- i3 `src/x.c` — `con_state` and `x_push_changes`, the applied-state cache model
- sway `sway/desktop/transaction.c` — dirty nodes and one commit boundary
- AwesomeWM `lib/wibox/widget/base.lua` — the `layout_changed` versus `redraw_needed` distinction
- CLIM / McCLIM — presentation types and translators, the conceptual ancestor of PBUI
