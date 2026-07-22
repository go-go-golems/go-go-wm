---
Title: 'Code Review Guide: The GGWM-012 Performance Work, Reviewed for a New Intern'
Ticket: GGWM-012-GUIDES
Status: active
Topics:
    - wm
    - goja
DocType: design-doc
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://pkg/draw/textcache.go
      Note: Reviewed as the model change — drift bound pinned by test
    - Path: repo://pkg/wmx11/manage.go
      Note: Findings R2 R3 R6 R7 R13 — paint path under review
    - Path: repo://pkg/wmx11/perf.go
      Note: Findings R1 R4 R5 R8 — env switches and counters
    - Path: repo://pkg/xshm/xshm.go
      Note: Findings R9 R10 — env parsing duplication, per-paint lock
ExternalSources: []
Summary: 'A worked code review of the GGWM-012 performance changes (commits 190e10c..faa610b): system primer, commit map, thirteen concrete findings with file/line references, a design-level critique of how the work was sequenced, and a reusable checklist for reviewing performance PRs.'
LastUpdated: 2026-07-22T16:20:29-04:00
WhatFor: Teach a new reviewer how to review performance work by actually reviewing this ticket's changes, defects included.
WhenToUse: Before reviewing any go-go-wm performance PR; when onboarding onto the paint path; when deciding whether the GGWM-012 follow-ups are done.
---


# Code Review Guide: The GGWM-012 Performance Work, Reviewed for a New Intern

This document is two things at once. It is a real code review of the performance work that landed on this branch between commits `190e10c` and `faa610b` — fourteen changes to the resize and paint path of an X11 window manager, described by their author in `design-doc/01-...` and in two vault articles. And it is a guide to *how* to review work like this, using the real findings as worked examples. Every claim below carries a file and line reference; every finding states what to check, what is wrong or fragile, and what the general lesson is.

The work under review succeeded by its own numbers: a paint went from 5.31 ms to 0.32 ms in the harness, and shared-pixmap creations per drag fell from 528 to 1. It also shipped two user-visible defects (black blocks, a frozen drag), spent six diary steps trusting a broken measurement harness, and — as this review found — left at least four latent problems in the tree, one of which is an instance of the very bug class the author fixed, documented, and turned into a written rule. Success and sloppiness coexist in the same diff. Learning to see both at once is the point of this guide.

How to use it:

- Read Part I even if you think you know the system. Every finding in Part IV depends on the two structural facts it establishes.
- Part III is the method. Parts IV–V are the method applied. If you only have an hour, read Parts III and V.
- Part VI is the design-level critique — the answer to "was this done systematically?" — and Part IX is the checklist you take to your next review.

---

## Part I: The system you are reviewing

You cannot review a change to a paint path without a mental model of the paint path. This part builds the minimum model. File references are to the current tree.

### 1.1 What the window manager is

`go-go-wm` is a reparenting X11 window manager. When a client application maps a window, the WM creates a *frame* window it owns, reparents the client into it, and draws decoration — a title strip and a border — on the frame (`pkg/wmx11/manage.go:58-137`, function `manage`). The layout is a binary split tree owned by the pure package `pkg/wmcore`; `pkg/wmx11` reconciles the X server against that tree.

```
+-- frame window (WM-owned) -------------------+
| title strip: 22 px, drawn by the WM          |
+----------------------------------------------+
|B|                                          |B|
|o|      client window (app-owned),          |o|   B = 2 px border,
|r|      reparented INTO the frame,          |r|   drawn by the WM
|d|      covers the entire interior          |d|
+----------------------------------------------+
| bottom border: 2 px, drawn by the WM         |
+----------------------------------------------+
```

Memorize this picture. The single largest optimization in the ticket (chrome-only upload) is nothing but this picture taken seriously, and the single most fragile assumption in the tree (the *covering invariant*, Part VI) is this picture assumed rather than enforced.

### 1.2 Two structural facts that govern every finding

**Fact 1: the WM is single-threaded by construction.** X event callbacks and posted closures run mutually exclusively, because `xgbutil`'s event loop sends on an unbuffered channel before dequeuing each event. There are no mutexes in `pkg/wmx11`. Consequences for review: any code in this package may freely read and write WM state without locks — *and therefore* any atomic operation, any mutex, or any comment about "foreign goroutines" in this package is a claim that some specific code violates the rule, and you must find that code or reject the claim (see finding R4).

**Fact 2: the upload installs a background pixmap; it does not blit.** `pkg/xshm` allocates a MIT-SHM shared-memory pixmap and installs it as the frame window's background (`manage.go:530-533`). The server composites the window *from* that memory. This makes Expose repair free — the server repaints exposed regions itself — and it means the WM's writes into that memory race with the server's reads unless something prevents it. Double buffering (finding R6) exists because of this fact; the rejected `shmSync` barrier exists because of it; the Expose fast path in `events.go:17-41` exists because of it.

### 1.3 The paint pipeline

One paint of one frame, `paintFrame` (`pkg/wmx11/manage.go:415-655`), has four phases. The perf counters are organized around exactly these phases, which is not an accident — the decomposition was the measurement that ended a sequence of four wrong diagnoses:

```
paintFrame(f):
  1. COMPOSE   fill background, render title strip, render border
               into f.img (an *image.RGBA scratch buffer)      -> compose_ms
  2. SURFACE   ensure the upload target exists at the right
               size (shm pixmap or fallback XImage); recreate
               costs two CHECKED (round-trip) X requests       -> surface_ms
  3. CONVERT   RGBA -> BGRA byte reorder into the target       -> convert_ms
  4. TRANSFER  shm: ClearArea (server blits from shared mem)
               fallback: PutImage chunks over the socket       -> (in upload_ms)
```

The pre-optimization cost split on the shm path was compose 16%, surface 56%, convert 27%, transfer 1%. Every landed change attacks one of these phases by *removing work from it*, not by speeding it up:

| Change | Phase attacked | Mechanism | File |
|---|---|---|---|
| Glyph mask cache | compose | render each `(string,bold,size)` once | `pkg/draw/textcache.go` |
| Chrome-only fill/convert/upload | compose, convert, transfer | skip pixels the client covers | `manage.go:913-929` |
| Capacity buckets, grow-only | surface | stop recreating on every size change | `manage.go:437-460, 840-900` |
| Double buffering | correctness of transfer | write the non-installed pixmap, swap | `manage.go:512-597` |
| Divider paint keying | (reconciliation) | repaint only on appearance change | `pkg/wmx11/divider.go:32-59` |
| Map-state mirror | (reconciliation) | issue Map/Unmap only on transitions | `manage.go:390-409` |
| Split-rect capture | (reconciliation) | one layout per drag tick, not two | `pkg/wmx11/input.go:41-49` |
| Synthetic ConfigureNotify | (event handling) | deny a request without a relayout | `manage.go:780-838` |

### 1.4 Key symbols, one line each

- `WM.relayoutPaint(paintAll bool)` — the sole reconciler: applies `wmcore.Layout` geometry to frames, diffs, paints (`manage.go:320-411`).
- `WM.paintFrame(f *frame)` — the four-phase paint above.
- `WM.chromeRects(f) []image.Rectangle` — the pixel-ownership function: which parts of a frame the WM actually renders; `nil` means "all of it" (builtin/script tiles with no client) (`manage.go:913-929`).
- `frame.img / frame.surf / frame.back / frame.ximg` — RGBA scratch, front shm surface (installed as background), back shm surface, PutImage fallback image (`pkg/wmx11/wm.go:77-114`).
- `xshm.Surface.WriteRGBA / WriteRGBARect / MarkDirtyAll` — BGRA conversion into shared memory; dirty flag forcing a full rewrite (`pkg/xshm/xshm.go`).
- `wmcore.Snap(f) (float64, bool)` — divider snapping with band half-width `Stick` (`pkg/wmcore/layout.go:23-58`).
- `wmcore.BuildIndexInto(root, dst)` — allocation-free node index for one reconciliation pass (`pkg/wmcore/layout.go:195-234`).
- `perfCounters` / IPC query `{"q":"perf"}` — the counter set behind every number in the ticket (`pkg/wmx11/perf.go`, `pkg/wmx11/ipc.go:118-126`).

---

## Part II: The work under review

### 2.1 The commit map

Read the commits in this order. The order is the argument: instrumentation first, then elimination, then interaction repair. The middle column tells you what a reviewer's job is for that commit.

| Commit | What to review | Risk profile |
|---|---|---|
| `190e10c` | Counters, benchmarks, glyph cache, motion throttling, divider keying, map mirror, synthetic ConfigureNotify | Wide but shallow; check each mechanism independently |
| `8c2cc7e`, `aae4f2e` | Paint timing decomposition | Trivial code; review the *inference* it corrects |
| `1afd898` | Capacity-sized buffers | The bucket-tiling assumption (X clips surplus) |
| `4f04364` | Bucketing made conditional on upload path | Later reverted — see R3 |
| `a82eabd`, `012a252` | Chrome-only upload and fill | **Highest risk in the ticket.** Failure modes are silent and visual |
| `b4eef24`, `1eac1a8` | Bucket sweep to 128; resident-bytes meter | Check the meter (R2) |
| `8ad5572` | The env-switch bug fix + withdrawal of a finding | Check the bug *class* was killed, not the instance (R1) |
| `309fa41` | Grow-only stores | Interaction with everything keyed on buffer size |
| `5df4628` | Snap-on-release | A behavior default changed inside a perf ticket |
| `a127a71` | Black-blocks fix (full init of fresh buffers) | A shipped defect being repaired; ask why it shipped |
| `faa610b` | Double buffering + coalesced repair | The dirty-tracking state machine (R6) |

### 2.2 Where the claims live

The author left an unusually complete paper trail, and you should use it the way a reviewer uses a test suite — as claims to spot-check, not as truth:

- `design-doc/01-go-go-wm-performance-engineering-...md` — the intern guide, including correction sections §4.2a–§4.2d where earlier claims are withdrawn.
- `reference/01-investigation-diary.md` — 21 steps, chronological, failures included.
- Two vault articles (`go-go-parc/Projects/2026/07/22/`): "Measuring Before Optimizing" (epistemics: five diagnoses, four wrong) and "Optimizing an X11 Window Manager Paint Path" (the engineering catalogue).
- `scripts/ggwm-xephyr-validate.sh`, `scripts/ggwm-xephyr-scenarios.sh` — the harnesses; `scripts/shmprobe/` — the capability probe; `images/` — 94+ screenshots.

---

## Part III: How to review performance work

Performance changes have a different defect profile from feature changes, and reviewing them with a feature checklist misses the defects that matter. Six questions structure the review. Each is illustrated below with a place this ticket passed it and a place it failed it.

**Q1. Is every performance claim attached to a measurement, and is the measurement attached to the right machine?**
A number without a stated environment is not a measurement. This ticket is mostly exemplary — the bucket sweep table, the sub-image A/B, the index benchmark all state their conditions — and it also contains the sharpest counterexample you will ever see: a harness bug (`GO_GO_WM_NO_SHM=0` *disabling* shared memory) produced `shared_pixmaps: false`, which was recorded as a property of the hardware for six diary steps and propagated into three documents. The two harnesses disagreed the whole time. **Rule: when two instruments disagree, the disagreement is the finding.** And the target-vs-harness gap is not a detail: a shared-pixmap creation costs 2.6 ms on Xephyr and 15.4 ms on the glamor-accelerated target — a 6× difference that inverted a conclusion.

**Q2. Did an A/B get treated as a decomposition?**
Toggling between two implementations tells you which *bundle* is faster, never which component inside either is expensive. Diary Step 9 drew "round trips are not the bottleneck" from exactly this error; the four-phase timers (`perf.go:99-110`) are what corrected it. When you review a claim of the form "we disabled X and it got slower, so X is not the problem," reject it and ask for the phase timers.

**Q3. What work did the change *stop doing*, and who proves nobody needed it?**
Every elimination has a correctness obligation exactly as large as the work eliminated. Chrome-only upload stops writing 95% of the pixels — so something must prove those pixels are never seen. Here that proof is: the client covers them (unenforced invariant), plus a screenshot sweep across seven scenarios (`ggwm-xephyr-scenarios.sh`), plus a `nil` escape hatch for frames with no client. Your job as reviewer is to hunt for the population the proof does not cover. Finding R3 (builtin tiles on the fallback path) and finding R6 (stale chrome after a same-bucket resize) are both members of exactly that hunt.

**Q4. What does the change do to the *instrumentation*?**
Counters and meters are code. A change that adds a resource must extend the meter that counts resources, or the meter silently becomes a lie — which is worse than no meter, because people trust it. Finding R2: double buffering (`faa610b`) added a second shared pixmap per frame; `bufferBytes()` (added one commit earlier, `1eac1a8`, specifically so the memory side of the capacity trade would be "measured rather than estimated") does not count it.

**Q5. When a bug is fixed, was the bug *class* fixed?**
`8ad5572` fixed the env-switch bug, added `env_test.go`, and the author wrote — in a published article — "Never write a boolean environment switch as a non-empty test." Finding R1: `pkg/wmx11/perf.go:15` still contains `os.Getenv("GO_GO_WM_NO_RESIZE_PAINT") != ""`. One `grep -rn 'os.Getenv' pkg/ | grep '!= ""'` at fix time would have found it. **Rule: a bug class fix ends with the grep, not with the instance.**

**Q6. Did any behavior change ride along?**
Perf tickets attract behavior changes because the author is living in the code. Here, snap-on-release (`5df4628`) changed the default drag feel (correctly, and env-gated, and with a test pinning the dead-zone cost) — but a reviewer must notice that a perf ticket changed an interaction default, say so out loud, and confirm the escape hatch and the changelog entry exist.

---

## Part IV: File-by-file review, with findings

Findings are labeled R1–R13 and ranked in Part V. Severity is from the reviewer's chair: **defect** (wrong today), **hazard** (correct today, wrong after a plausible future change), **debt** (correct but misleading or unmaintainable), **note** (worth knowing, no action demanded).

### 4.1 `pkg/wmx11/perf.go` — the instrumentation

This file is the foundation of the whole ticket's credibility, so it deserves the pickiest read, and it does not fully survive one.

**R1 (defect, the important one). The bare non-empty env test survives — in the measurement switch itself.**

```go
// perf.go:15
var suppressResizePaint = os.Getenv("GO_GO_WM_NO_RESIZE_PAINT") != ""
```

`GO_GO_WM_NO_RESIZE_PAINT=0` *enables* suppression. This is byte-for-byte the bug class that inverted six diary steps: the fix in `pkg/xshm/xshm.go:194-201` (`envDisabled`, honoring `0/false/no/off`) landed with a regression test and a published working rule — and this instance sits in the same ticket's own diff, in the file that defines the *other* environment switches, two of which (`envOn`, line 29) already do it correctly. The consequence is live: the next harness author who writes `GO_GO_WM_NO_RESIZE_PAINT=0` to express "paint normally" will silently benchmark a WM that never paints during drags, and the resulting numbers will be spectacular and false — the exact failure mode this ticket documented.

Fix: `var suppressResizePaint = envOn("GO_GO_WM_NO_RESIZE_PAINT")`. Then grep the repository for the class: `grep -rn 'os.Getenv' --include='*.go' | grep '!= ""'` and audit every hit that is semantically boolean.

*Lesson: when the author of a fix writes a rule, hold the author's own diff to the rule first. The rule was published; the grep was never run.*

**R4 (debt, with a possible race behind it). Half-atomic counters with a comment that doesn't match the code.**

The struct comment (`perf.go:66-69`) says "the two fields that a foreign goroutine can touch are atomic," and `snapshot()` performs `atomic.LoadUint64(&p.shmCreates)` (lines 161-162). But every writer is a plain `w.perf.shmCreates++` on the WM loop (`manage.go:523, 529, 541`); there is no `atomic.Add` anywhere in the package. Two possibilities, both requiring action:

- If no foreign goroutine touches these fields (the IPC handler posts onto the WM loop, so `snapshot` runs there too — check `ipc.go`'s dispatch), the atomics are dead weight and the comment is false. Delete both.
- If some path does read from another goroutine, then the *writes* race and the atomics on the read side are theater — `atomic.LoadUint64` cannot repair a non-atomic `++`.

Either way the current state is the worst option: it documents a concurrency contract nobody keeps. Under Fact 1 (single-threaded by construction), any atomic in this package is an extraordinary claim; make the code either plainly loop-owned or properly atomic on both sides.

*Lesson: concurrency annotations are claims about the existence of specific code. Verify the code exists. `go build -race` plus a test that queries perf during a drag would settle this in one run.*

**R5 (debt). A doc comment attached to the wrong declaration.**

Lines 17-27: the comment explaining `snapOnRelease` sits directly above `var doubleBuffer`, with `doubleBuffer`'s own comment appended below it. Godoc and every human reader binds a comment to the declaration that follows it. Move the `snapOnRelease` paragraph down to line 53 where the variable is declared.

**R8 (debt). The snapshot is a two-writer struct.**

`snapshot()` fills most of `perfSnapshot`; the IPC handler patches in `BufferBytes`/`BufferFrames` afterwards (`ipc.go:123-125`). The next person to add a field must know there are two filling sites. Pass the buffer totals into `snapshot` (or give `snapshot` the `*WM`) so the struct has one producer.

### 4.2 `pkg/wmx11/manage.go` — the paint path

**R2 (defect). The memory meter does not count the back buffer.**

`bufferBytes()` (`manage.go:935-963`) sums `f.img`, `f.surf`, and `f.ximg` — and not `f.back`, which `faa610b` added one commit after the meter. On the double-buffered shm path (the default), every on-screen frame holds two shared pixmaps of equal size, so the reported resident bytes are low by roughly the size of all front surfaces — close to a factor of two on the dominant term. This meter exists for one purpose, stated in its own comment: to make the capacity-sizing memory trade "measured rather than estimated." It currently measures the trade wrong, and any future bucket sweep that consults `buffer_bytes` (as the 64-vs-128 decision did) inherits the error.

Fix is four lines in `count()`:

```go
if f.back != nil {
    b += int64(f.back.W) * int64(f.back.H) * 4
}
```

Then re-run the Step 14 sweep once to confirm the 128-bucket conclusion survives the corrected meter (it should — doubling both sides of a comparison preserves the comparison — but "should" is what the sweep exists to replace).

*Lesson: Q4 from Part III. A feature that allocates a resource must update the meter that counts that resource, in the same commit. Reviewers: when a diff adds a field to `frame`, grep for every function that enumerates `frame`'s fields.*

**R3 (hazard + debt). `bucketSizeFor` contradicts its own comment, and the fallback pays for it on builtin tiles.**

The function (`manage.go:887-889`) unconditionally buckets. Its doc comment (lines 878-886) still explains why the PutImage fallback should be *exempt*: "The PutImage fallback is billed per pixel transferred instead, so a larger backing store makes it slower ... On that path, exact sizing wins." The exemption was real code once — added in `4f04364`, removed in `a82eabd` — and the removal was justified, but only for frames with clients: chrome-only transfer made the fallback insensitive to backing-store size *because it stopped transferring the interior*. That justification quantifies over the wrong population. `chromeRects` returns `nil` for builtin and script tiles (`manage.go:914-915`), which therefore take the full-buffer path: on a fallback host, a builtin tile's every paint calls `f.ximg.XDraw()` on a capacity-sized image (`manage.go:653`), transferring up to 127 surplus pixels per axis that the comment correctly says are pure loss on this path.

Two actions. First, fix the comment — it describes deleted code, and a stale justification is worse than none because it teaches the next reader a false invariant. Second, either restore exact sizing for `client == 0` frames on the fallback path, or measure the cost on a fallback host and document that it was accepted. The measurement harness for exactly this exists (`ggwm-shm-ab.sh` with `GO_GO_WM_NO_SHM=1`).

*Lesson: when a special case is deleted, its comment must die with it; and when an argument of the form "X is now free because of Y" licenses a simplification, check which cases Y actually covers. Y here was chrome-only transfer, which by design excludes exactly the tiles that pay.*

**R6 (hazard). The double-buffer dirty tracking is correct for a subtle, unwritten reason.**

After the swap (`manage.go:577-584`), the new back buffer holds the previous frame. `MarkDirtyAll` is called only when the two buffers' *sizes* differ (line 581). But the case that actually threatens correctness is a *viewport* change within the same bucket: buffer sizes equal, no dirty flag, next paint writes only the new chrome rects — and the buffer still contains the previous viewport's chrome (the old bottom-border row at `oldH-b`, the old right-border column at `oldW-b`) at positions the new paint does not touch.

Work through why this is nonetheless safe today; the exercise is the review. If the pane grew, the old border rows/columns now lie inside the new interior — covered by the client (covering invariant). If the pane shrank, they lie outside the new viewport — clipped, because X displays only the window-sized top-left of an oversized background pixmap. So every stale pixel is either covered or clipped. The argument rests on three facts: chrome hugs the viewport edges, the client covers the interior, and the surplus is clipped. None of this is written anywhere near the `MarkDirtyAll` condition, and one of its legs is the unenforced covering invariant (Part VI). There is also a known soft spot: during a drag, the frame is resized *before* the client is reconfigured (`relayoutPaint` orders `MoveResize` at line 359 ahead of the client `ConfigureWindow` at line 370) — the same exposure window that produced the black-blocks defect can, for a frame or two, show previous-frame chrome inside the pane. Previous-frame pixels are far less jarring than uninitialized black, which is presumably why nobody has reported it.

Fix options, in order of preference: (a) write the covered-or-clipped argument as a comment on the `MarkDirtyAll` condition, so the next person changing chrome geometry knows what they are invalidating; (b) conservatively `MarkDirtyAll` on any viewport change — the cost is one full `WriteRGBA` per buffer-size-stable resize tick, and the `convert_ms` counter will tell you immediately whether that is affordable (pre-chrome-only it was 1.44 ms; for a full write it still is).

*Lesson: "correct for reasons not stated in the code" is a finding even when no behavior is wrong. The next edit is made against the comments, not against the author's memory.*

**R7 (note). Creation counters count attempts on one path and successes on the other.**

Front surface: `w.perf.shmCreates++` *before* `xshm.New` (line 529), so a failed creation still counts. Back surface: incremented *after* success (line 541). If shm creation ever starts failing intermittently (the exact scenario where you'd stare at these counters), `shm_creates` will disagree with reality by the failure count on one path only. Count successes on both.

**R13 (hazard, design-level — detailed in Part VI). `paintFrame` has become a 240-line state machine with three interacting booleans.**

`freshBuffer`, `doubleBuffer`, and `Surface.dirtyAll` jointly decide whether the write is chrome-only or full, into which buffer, followed by which repair. Both shipped defects of this ticket (black blocks, `a127a71`; the tearing that motivated `faa610b`) and finding R6 all live in the interactions of these flags. The structure is reviewable today only by simulation — you hold the flags in your head and walk the branches. Part VI proposes the refactor (extract a `paintPlan`); as a reviewer, the actionable point is: **when a diff adds a boolean that modifies the meaning of two existing booleans, ask for the state table.** Eight states; the author should be able to write down all eight and say which are reachable.

### 4.3 `pkg/xshm/xshm.go` — the shared-memory surface

Mostly clean, with careful lifecycle discipline worth studying (the `IPC_RMID` immediately after attach, so segments die with the process even on `kill -9` — lines 106-117). Three observations:

- **R9 (debt, cross-cutting).** `envDisabled` (lines 194-201) is the fourth environment-parsing implementation in the tree: `envOn` (`wmx11/perf.go:29`), `envInt` (`wmx11/manage.go:869`), `envFloat` (`wmcore/layout.go:37`), and this — plus the R1 bare test. `envDisabled` and `envOn` have identical bodies and opposite-sounding names; only `xshm` has tests (`env_test.go`). After an env-parsing bug cost this ticket six steps of false findings, the parsing should live in one tested package (`pkg/env` or similar, ~30 lines) and the five call sites should use it. This is the *structural* fix for R1's bug class; the grep is the tactical one.
- **R10 (note).** `Available` is called on every `paintFrame` (`manage.go:510`) and takes a global mutex around a map lookup (`xshm.go:46-49`). The cost is nanoseconds and irrelevant; the smell is architectural: it is the only lock on the hot path of a system whose stated design is "no locks on the WM loop," guarding a value that never changes after the first call for a given connection. Latching the result on the `WM` struct at startup would remove both the lock and the per-paint lookup and make Fact 1 literally true along the whole paint path. Also `availCache` never evicts closed connections — harmless at one connection per process, but a reviewer should note when a cache has no eviction story at all.
- **Good pattern to copy:** `WriteRGBA`'s size-mismatch fallback (lines 140-147) writes the overlap instead of silently doing nothing. The comment records the defect that taught this (`GGWM-012 Step 17` — a grow-only surface receiving a smaller image blanked the pane). Defensive code that cites the incident it prevents is exactly how comments should earn their keep.

### 4.4 `pkg/draw/textcache.go` — the glyph mask cache

The best-reviewed change in the ticket, and a model for how to land a behavior-adjacent optimization:

- The load-bearing decision (cache coverage, not color) is stated with its reason: an RGBA cache would miss on every focus recolor.
- The known deviation (associativity of alpha blending: `over(over(dst,g1),g2)` vs `over(dst,over(g1,g2))`) is quantified — 14 pixels of 179,200, max channel delta 1/255 — and *pinned by a test* that keeps the original implementation as a reference and fails if drift exceeds 1 (`textcache_test.go`). Accepting a rendering change is a judgment call; bounding it with a permanent test converts the judgment into a contract.
- Eviction is a wholesale clear at 512 entries (lines 57-60) — crude, correct, and honestly argued in the comment (working set is ~one entry per visible title; an LRU buys nothing). Appropriate simplicity.

Two review notes, neither blocking: the mask width pad `w = adv + int(size) + 2` (line 78) is a heuristic against glyph overhang; a face with extreme right-side bearing could clip, and the golden tests would catch it only for strings they cover. And the cache is guarded by a mutex (fine — `draw` is used off the WM loop by design), which is worth contrasting with R4: *this* package declares itself concurrent and locks consistently; `wmx11` declares itself single-threaded and should contain nothing that hedges.

### 4.5 `pkg/wmcore/layout.go` and `snap_test.go` — the pure core

- `BuildIndexInto` (lines 218-225) is the honest version of a "textbook optimization": the comment records that the allocating variant was *slower* than the O(n²) it replaced below ~24 leaves, and that the scratch-reuse form exists as scaling insurance, not as a speedup. Keeping the benchmark (`layout_bench_test.go`) means the claim stays checkable.
- `TestSnapDeadZone` pins an *interaction* cost in pixels of pointer travel — it counts only genuinely snapped runs, excluding the clamp at the extremes (a bug in the test's first version; the diary records the fix). Tests that measure feel-adjacent properties are rare and valuable: this one turns "the drag feels broken" into a number with a ceiling.
- **R11 (note).** `Stick` and `sizeBucket` are package-level `var`s initialized from the environment. Fine for sweepability, but they are mutable globals; a test that sets them and forgets to restore poisons its neighbors. `t.Cleanup` discipline or a setter that returns a restore func would harden this.
- Note the remaining allocation asymmetry: the ticket taught that a fresh map per pass can cost more than it saves, and `BuildIndexInto` fixed the index — but `wmcore.Layout` still allocates its result map on every call (line 128), once per drag tick, and `relayoutPaint` allocates `visible` (line 337) per pass. Probably negligible; the point for a reviewer is consistency of argument — the reasoning that justified `BuildIndexInto` applies verbatim here and was not applied. Measure before acting (the ticket's own rule), but the question should be on file.

### 4.6 `pkg/wmx11/input.go` — the drag state machine

Snap-on-release is cleanly built: `dividerMotion` computes both `raw` and `snappedRatio` on every tick, applies `raw` during the gesture, and `handleRelease` replays the final coordinates with `d.finalizing = true` so the commit snaps (`input.go:394-401, 466-474`). The `snapped` boolean still drives the divider's color, so the affordance survives without the dead zone. Points a reviewer should articulate:

- **R12 (note). Coalescing drops; it does not latch.** A motion event arriving <16 ms after the last admitted one is discarded (`input.go:358-360`), not stored for a trailing-edge tick. During a continuous drag X delivers motion faster than the throttle, so the divider is never more than ~16 ms stale, and the release replay (`d.lastPaint = time.Time{}` then a forced `dividerMotion`) guarantees the endpoint is exact. This is drop-with-replay, a deliberate and simpler alternative to latch-and-timer; the review question for any throttle is "what happens to the *last* event?", and here the answer is correct. The same pattern was retrofitted to grip drags (`handleRelease:451-458`) — check that any *future* drag kind gets it too, since nothing structural enforces it.
- The gesture-scoped `splitRect` capture (struct comment, lines 41-49) rests on the invariant that changing a split's ratio never moves the split's own rectangle. That is a theorem about `wmcore.Layout` (a split hands its children new rects but keeps its own), currently proven by reading `layoutInto`. Cheap to enforce: a one-line assertion in the harness, or a `wmcore` unit test — set ratio, relayout, assert the split's rect is unchanged.

### 4.7 `pkg/wmx11/divider.go` and `events.go` — keyed paints and the Expose fast path

- The divider paint key (`dividerPaintKey{mode, dir, w, h}`, `divider.go:47-51`) is a textbook derived-state cache with explicit invalidation (`invalidate()` on Expose and theme swap). Review question for any such key: is every input that affects the pixels in the key? Here the theme is *not* in the key and is handled by out-of-band invalidation — that is the fragile edge. A new appearance input (say, per-workspace accent colors) must either enter the key or add an invalidate call, and only a comment guards it.
- The Expose fast path (`events.go:17-41`) is where this ticket's best war story lives: currency used to be checked against `f.rect`, so the moment buffers became capacity-sized, *every* buffer looked stale and every Expose triggered a full repaint — invisible on screen, invisible in any single counter, and obvious only as the ratio `frames_painted:frames_resized` (1340:496). The fix checks against `bucketSizeFor`. Two lessons: derived-state comparisons must compare against the representation actually stored, not the one the author first imagined; and **counter ratios catch what counters cannot** — put `frames_painted / frames_resized ≈ 1` on your post-merge checklist for any paint-path change.

---

## Part V: Findings, ranked

| # | Severity | Where | One-line statement |
|---|---|---|---|
| R1 | Defect | `wmx11/perf.go:15` | Bare `!= ""` env test; `=0` enables suppression — the ticket's own documented bug class, alive in its own diff |
| R2 | Defect | `wmx11/manage.go:935` | `bufferBytes` omits `f.back`; resident memory under-reported ~2× on the default path |
| R3 | Hazard | `wmx11/manage.go:878` | `bucketSizeFor` comment describes deleted conditional; builtin tiles on the fallback transfer bucket surplus every paint |
| R6 | Hazard | `wmx11/manage.go:581` | Back-buffer dirty marking is safe only via an unwritten covered-or-clipped argument resting on an unenforced invariant |
| R13 | Hazard | `wmx11/manage.go:415-655` | `paintFrame` is a 3-boolean state machine that has already produced two shipped defects; needs structure, not vigilance |
| R4 | Debt | `wmx11/perf.go:66,161` | Atomic loads paired with non-atomic increments and a comment describing a contract nobody keeps |
| R9 | Debt | four packages | Env parsing implemented four ways plus one bare test; only one has tests |
| R5 | Debt | `wmx11/perf.go:17` | `snapOnRelease` doc comment attached to `doubleBuffer` |
| R8 | Debt | `wmx11/ipc.go:123` | `perfSnapshot` has two filling sites |
| R7 | Note | `wmx11/manage.go:529,541` | `shm_creates` counts attempts on one path, successes on the other |
| R10 | Note | `xshm/xshm.go:46` | Per-paint mutexed lookup for a value fixed at startup; cache has no eviction |
| R11 | Note | `wmcore/layout.go:35`, `manage.go:867` | Env-tunable package globals; tests can poison each other |
| R12 | Note | `wmx11/input.go:358` | Throttle is drop-with-replay, not latch — correct, but the property is per-drag-kind and unenforced |

Suggested disposition: R1 and R2 are small, mechanical, and should land immediately with a regression test each (R1's test already exists in shape — clone `xshm/env_test.go`; R2's is an assertion in the harness that `buffer_frames × 2 surfaces` roughly matches `buffer_bytes` under double buffering). R3's comment fix is immediate; its behavioral half needs one fallback-host measurement. R6 costs one paragraph of comment now, and a measured decision later. R13 and R9 are refactors to schedule, not hotfixes.

---

## Part VI: Design review — was this systematic?

The honest answer: the *measurement discipline* became excellent, and got there by trial and error rather than by plan; the *architectural* reasoning that should have come first arrived in the middle; and the code that resulted is locally justified everywhere but structurally weaker than it should be in one central place. This section is the longest because it is the one you should imitate and improve on.

### 6.1 What was genuinely well done

Name these practices; they are the reusable assets of the ticket, and most tickets have none of them:

- **Decomposed cost accounting before optimization.** The four-phase timers ended a run of four wrong diagnoses and every subsequent decision cites them.
- **Sweeps instead of arguments.** The bucket granularity was decided by a five-point sweep with both time and memory columns. The conservative default (64) turned out to cost 1.5× for zero memory savings — a fact nobody would have conceded in an argument.
- **Rejected work recorded with numbers.** Seven built-and-rejected changes (sub-image transfer, the sync barrier, the allocating index, bucket 64, shrinking `Stick`, hysteresis, completion events) are documented with the measurements that killed them. This is the difference between a team that re-litigates and one that accumulates.
- **Corrections published in place.** The false `shared_pixmaps` finding was withdrawn in numbered sections (§4.2a–d) rather than silently edited away. You can audit the epistemic history.
- **Tests that pin unusual properties**: a rendering-drift bound (≤1 channel delta vs a retained reference implementation), a dead-zone ceiling in pixels of pointer travel, a golden-delta diagnostic. Screenshots committed as evidence where assertions cannot reach.

### 6.2 Where it was unsystematic, precisely

**The structural insight came last when it should have come first.** Chrome-only upload — the largest win, 20k visible pixels out of 422k — requires no profiler, no harness, no counter. It follows from the reparenting diagram in Part 1.1 by asking one question: *for each pixel we produce, who ever sees it?* That question is answerable by reading `manage()` on day one. Instead the sequence was: read reviews, form hypotheses, build harness, refute hypotheses, decompose, optimize surface management — and only *then* notice the pixels. Three prior review documents (~8,700 lines) also missed it, which tells you something important: everyone was reading the code for *mechanisms* (round trips, allocations, algorithms) and nobody drew the dataflow and asked what the work was *for*.

The systematic method this implies, and the one to use next time, is a **pixel-ownership audit** before any measurement:

```
for each buffer/surface in the pipeline:
    who writes it?  how often?  what fraction is ever observable?
    what invalidates it?  is the invalidation condition == the
    observable-change condition, or coarser?
```

Run that table against the pre-ticket code and it yields, in order: chrome-only (observable fraction ≈ 5%), capacity buffers (invalidation = "any size change" vs observable change = "chrome layout change"), divider paint keying (invalidation = every relayout vs observable = mode/dir/size), and the map-state mirror (requests issued with no state change at all). That is 90% of the ticket, derived from one table. Measurement's proper role — and this ticket proves it in both directions — is then to *size* candidates and *veto* the plausible ones that lose (the index, the sub-image transfer). Architecture proposes; measurement disposes. This ticket ran the two in the wrong order and paid for it in four wrong diagnoses and one harness artifact.

**A predicted defect shipped anyway.** Diary Step 13 predicted, in writing, that chrome-only fill would leave uninitialized interiors ("black") exposed by the frame-resize/client-configure gap. The change shipped; the user hit exactly that (Step 19); the fix (`a127a71`) came after. There is no review-process substitute for the rule this implies: **the author's own recorded prediction of a user-visible failure is a blocking finding.** When you review a PR whose description or diary says "this could show X in case Y," your job is to require either the fix or the demonstration that Y is unreachable — before merge, not after the bug report.

**The instrument was trusted against a visible contradiction.** The two harnesses disagreed about `shared_pixmaps` for six steps, and the disagreement was rationalized ("one server is software-rendered") instead of investigated. Add to this the fact that the false reading was *load-bearing* — it demoted the entire shm path to "inert on this machine" — and the process failure is clear: contradictions between instruments were not treated as first-class findings. The published working rule now exists; hold future work to it.

**The state that accumulated in `paintFrame` was never consolidated (R13).** Each change was individually justified and individually reviewed-in-diary — and their composition is now three booleans (`freshBuffer`, `doubleBuffer`, `dirtyAll`) times two upload paths times chrome-vs-full, threaded through 240 lines with timing instrumentation interleaved. Both shipped defects were composition bugs, not component bugs: black blocks was `chrome-only fill × fresh buffer`; the dirty-flag machinery exists because `double buffer × grow-only × chrome-only` interact. When defects live in the composition, the fix is structural. Proposed shape:

```go
type paintPlan struct {
    target    *xshm.Surface     // or ximg; where pixels go
    rects     []image.Rectangle // nil => full write
    fullWrite bool              // fresh or dirty target
    repair    image.Rectangle   // the single ClearArea
    swap      bool              // install target as background after write
}

// planPaint is PURE: state in, plan out. Unit-testable with a
// table of the eight boolean states, no X server required.
func planPaint(f *frame, cfg paintConfig) paintPlan

// executePaint performs the plan. All X calls live here.
func (w *WM) executePaint(f *frame, p paintPlan)
```

The value is not elegance; it is that the eight-row truth table of the flags becomes a unit test, and R6's covered-or-clipped argument becomes a comment on one field of one function instead of folklore.

**The covering invariant remains folklore.** Everything — chrome-only fill, chrome-only convert, chrome-only transfer, the R6 staleness argument, the Expose fast path — rests on "a reparented client covers exactly the frame interior," established at reparent time (`manage.go:113`, offset `(BorderW, TitleH)`) and maintained by reconciliation (`manage.go:361-374`). Nothing states it in one place; nothing checks it; its failure mode is silent stale pixels. Minimum viable enforcement: a named predicate with the doc anchor (`func (f *frame) clientCoversInterior() bool` comparing the client's configured geometry against the interior rect), asserted in the scenario harness after every scenario, and cheap enough to assert in a debug build after every `relayoutPaint`. This converts the ticket's biggest structural bet from "verified by screenshot on 2026-07-21" to "checked on every run."

### 6.3 Decision record for this review

- **Context:** the ticket achieved its numbers; four defects/hazards and a central structural weakness were found in review; two unexplained measurements remain open.
- **Decision (proposed):** land R1+R2+R5+comment-half-of-R3 immediately; schedule R9 (env package), R13 (`paintPlan`), and covering-invariant enforcement as a follow-up ticket; require the two open measurements (Part VII) to be resolved or explicitly ticketed before GGWM-012 closes.
- **Rationale:** the immediate items are minutes of work with regression tests available; the structural items are exactly the places the next defect will occur, as evidenced by the last two defects; the open measurements gate the claim that the work is done on the *target*, which is the only machine that matters.
- **Status:** proposed.

---

## Part VII: What is still open, and the experiments that close each item

A review is not complete until it lists what the author's own evidence says is unfinished.

1. **Real hardware is 28× slower than the harness** (9.16 ms/paint vs 0.32). Leading hypothesis, from the live session's own state: one visible leaf is the builtin launcher tile (`client: 0`), which takes the full-surface path *and* re-runs `registry.Match` in its renderer per paint; its `compose_ms` was 3.94 ms/paint vs 0.39 in the all-terminal harness. **Experiment:** on the real session, drag with two xterms only; if `compose_ms` collapses, cache the builtin tile's match result (invalidate on registry change) and re-measure. Until run, the 16.6× headline describes the harness, not the machine the user sits at.
2. **Grow-only reduced shm creations to 1 in the harness but only 36 on the real machine.** Something on the target invalidates surfaces that the harness does not — different pane geometry crossing buckets, float/fullscreen traffic, or an unnoticed `dropBuffers` path. **Experiment:** log the stack (or a reason code) on each `shmCreates` increment during one real drag; 36 events is a small, readable list.
3. **The residual tearing window is unmeasured.** Double buffering makes the server read the buffer being written only if it falls more than a frame behind; nobody knows whether it ever does. **Experiment:** run with `GO_GO_WM_SHM_SYNC=1` for a session and read `sync_waits` and `sync_ms` — the counters were built precisely so this costs one env var to answer.
4. **R3's behavioral half:** one fallback-host run of the drag harness with a builtin tile visible, comparing exact-size vs bucketed `ximg`.

---

## Part VIII: Verifying this review yourself

Do not take the findings on authority; they are cheap to check.

```bash
# R1 — the surviving bare test, and the rest of the class:
grep -rn 'os.Getenv' --include='*.go' pkg/ cmd/ | grep '!= ""'

# R2 — the meter misses f.back:
grep -n 'f\.back' pkg/wmx11/manage.go       # present in paint/drop paths
sed -n '935,963p' pkg/wmx11/manage.go        # absent in bufferBytes

# R3 — the deleted conditional the comment still describes:
git log --oneline -L :bucketSizeFor:pkg/wmx11/manage.go

# R4 — atomics with no atomic writers:
grep -rn 'atomic\.' pkg/wmx11/ | grep -v _test
grep -rn 'shmCreates' pkg/wmx11/

# The tests and benchmarks the ticket added:
go test ./pkg/... -count=1
go test ./pkg/draw -bench Text -run xx -count=1
go test ./pkg/wmcore -bench . -run xx -count=1

# The harnesses (need an X session at :0; disposable nested server):
T=ttmp/2026/07/21/GGWM-012-GUIDES--import-go-go-wm-engineering-guides-and-handbook
PARENT=:0 GO_GO_WM=$PWD/go-go-wm bash $T/scripts/ggwm-xephyr-validate.sh review-check
PARENT=:0 GO_GO_WM=$PWD/go-go-wm bash $T/scripts/ggwm-xephyr-scenarios.sh review-scen
```

Counters to read after a harness run (`{"q":"perf"}` over the IPC socket), with the healthy shapes:

- `shm_creates` ≈ number of panes (grow-only working); hundreds means regression.
- `frames_painted / frames_resized` ≈ 1; a large ratio is the Expose-staleness regression pattern.
- `divider_paint_skipped >> divider_painted` during drags.
- `motion_coalesced > 0` and `map_requests_skipped > 0`.
- `buffer_bytes` — currently under-reported (R2); after the fix, expect ≈ `2 × ΣbucketW×bucketH×4` for on-screen client frames.

---

## Part IX: The reviewer's checklist, distilled

For any future performance PR on this codebase (and most others):

1. **Demand the decomposition.** No claim of the form "component X was the cost" without per-phase numbers. A/B comparisons compare bundles.
2. **Ask what stopped happening, and who proves nobody needed it.** Every elimination carries a proof obligation; hunt the population the proof skips (builtin tiles, fallback paths, degenerate sizes).
3. **Check the meters in the same diff as the resources.** New allocation ⇒ updated accounting, same commit (R2).
4. **When a bug is fixed, require the class-wide grep in the PR.** An instance fix without the grep is half a fix (R1).
5. **Read every comment as a claim and test it against the code.** Stale justifications (R3), misattached docs (R5), and unkept concurrency contracts (R4) are all comment-vs-code divergences, and all were found by the same act: believing the comment long enough to check it.
6. **Ask for the state table when booleans compose.** Two interacting flags = four states the author should enumerate; three = eight, and past experience here says two of them ship broken (R13).
7. **Treat the author's own recorded risk predictions as blocking findings.** "This might show black in case Y" in the diary means the review requires the fix or the unreachability argument.
8. **Insist that harness numbers are labeled as harness numbers.** The target machine differed by 6× on the critical constant and 28× on the headline. "Fast in Xephyr" is a different claim from "fast."
9. **Look for invariants that multiple changes lean on, and require them named and checked.** The covering invariant carries five separate optimizations and exists only as prose.
10. **Watch counter ratios, not counters.** The one regression that neither tests nor screenshots caught was visible only as `frames_painted : frames_resized`.
11. **Check that behavior changes riding a perf ticket are flagged, gated, and changelogged** (snap-on-release: yes on all three — verify, don't assume).
12. **End by listing what remains open.** A review that doesn't state the unfinished measurements silently converts "not yet known" into "fine."

---

## References

**Code under review (current tree):**
- `pkg/wmx11/manage.go` — reconciler and paint path; findings R2, R3, R6, R7, R13
- `pkg/wmx11/perf.go` — counters and env switches; findings R1, R4, R5, R8
- `pkg/wmx11/input.go`, `pkg/wmx11/divider.go`, `pkg/wmx11/events.go` — drag state machine, keyed divider paints, Expose fast path
- `pkg/xshm/xshm.go` — shared-memory surfaces; findings R9, R10
- `pkg/draw/textcache.go`, `pkg/draw/textcache_test.go` — glyph mask cache and drift bound
- `pkg/wmcore/layout.go`, `pkg/wmcore/snap_test.go` — layout, snapping, node index

**Commits:** `190e10c` (instrumentation) … `faa610b` (double buffering); full map in Part II.

**Documents:**
- `design-doc/01-go-go-wm-performance-engineering-an-intern-s-guide-to-the-resize-and-render-path.md` — the original guide, including correction sections §4.2a–§4.2d
- `reference/01-investigation-diary.md` — 21 chronological steps, failures verbatim
- Vault: `Projects/2026/07/22/ARTICLE - Measuring Before Optimizing - An X11 Window Manager Resize Path.md` and `ARTICLE - Optimizing an X11 Window Manager Paint Path.md`
- Harnesses and probe: `scripts/ggwm-xephyr-validate.sh`, `scripts/ggwm-xephyr-scenarios.sh`, `scripts/shmprobe/`
