# Changelog

## 2026-07-21

- Initial workspace created


## 2026-07-21

Imported three externally authored go-go-wm engineering guides (437 KB, ~8,700 lines) into sources/local/ and confirmed the i3 config port is present in the workspace.

### Related Files

- /home/manuel/workspaces/2026-07-21/go-go-wm-goja/go-go-wm/examples/scripts/i3.js — 187-line port of the user's i3 config to the go-go-wm JS API (GGWM-004)


## 2026-07-21

Wrote the intern-facing performance design and implementation guide (1,807 lines): X11 primer, evidence-anchored codebase map, end-to-end divider-drag trace, cost model, gap analysis against the source guides, five decision records, a seven-phase plan with exit criteria, test strategy, onboarding labs, and appendices.

### Related Files

- /home/manuel/workspaces/2026-07-21/go-go-wm-goja/go-go-wm/pkg/wmx11/manage.go — relayoutPaint and paintFrame are the documented hot path


## 2026-07-21

Central finding: xshm.New issues two CHECKED X requests (synchronous round trips) and paintFrame destroys/recreates the shm surface on every dimension change, so a divider drag pays ~4 round trips plus syscalls per tick. All three source guides assert the drag loop is round-trip-free; they audited for .Reply() and missed .Check().

### Related Files

- /home/manuel/workspaces/2026-07-21/go-go-wm-goja/go-go-wm/pkg/wmx11/manage.go — Surface destroy/recreate on size change at :420-432
- /home/manuel/workspaces/2026-07-21/go-go-wm-goja/go-go-wm/pkg/xshm/xshm.go — AttachChecked().Check() at :92 and CreatePixmapChecked().Check() at :106-107


## 2026-07-22

Published the five-document bundle to reMarkable at /ai/2026/07/21/GGWM-012-GUIDES after installing pandoc-cli, texlive-xetex, texlive-latexrecommended, texlive-latexextra, texlive-mathscience and texlive-fontsrecommended; rendered with Noto Sans / JetBrains Mono since DejaVu is not installed.


## 2026-07-22

Re-rendered and replaced the reMarkable bundle using remarquee's standard layout instead of the editor preset, per user preference; fonts unchanged (Noto Sans / JetBrains Mono).


## 2026-07-22

Implemented Phase 0 (perf counters, perf IPC query, GO_GO_WM_NO_RESIZE_PAINT, first benchmarks in the repo) and Phase 1 (glyph-run cache, divider paint guard, map-state mirrors, single layout per drag tick, synthetic ConfigureNotify, gripMotion throttle, scratch tree index). draw.Text 7.2x faster; TitleStrip.Render 1.9x. All 15 packages pass. (commit 190e10c)

### Related Files

- /home/manuel/workspaces/2026-07-21/go-go-wm-goja/go-go-wm/pkg/draw/textcache.go — Glyph-run alpha-mask cache — the largest single measured win
- /home/manuel/workspaces/2026-07-21/go-go-wm-goja/go-go-wm/pkg/wmx11/perf.go — Counters that make the resize path measurable for the first time


## 2026-07-22

Corrected the design doc from implementation evidence: shared_pixmaps is false on this host so the shm path never runs (GGWM-006 inert here); the O(n^2) removal is scaling insurance rather than a speedup below ~24 leaves; and >95% of a frame paint is BGRA conversion plus upload, not fill or text.


## 2026-07-22

Measured three conditions on a real WM under Xephyr: the Tier-1 round-trip hypothesis is REFUTED (disabling shm removes 1024 round trips and is 15% slower), reconciliation is 1.8% of a relayout, and ~98% of a paint is BGRA conversion plus upload. Plan reordered: Phase 4 (chrome/content split) now precedes Phase 2.


## 2026-07-22

Upload only the rectangles a reparented client does not cover: ~20k visible pixels instead of ~422k per frame. Combined with capacity buffers this is 2.92x faster per paint on both upload paths and ~3x less WM-loop work per drag. Delivers Phase 4's main saving without creating any new X windows.


## 2026-07-22

Swept backing-store granularity and raised the default to 128px. Cumulative result against baseline: MIT-SHM 5.31 -> 1.11 ms/paint (4.8x), PutImage fallback 6.63 -> 1.89 ms/paint (3.5x), WM-loop work per drag down 4.8x and 3.7x respectively.


## 2026-07-22

WITHDRAWN: the claim that this machine lacks shared-pixmap support. A probe reports SharedPixmaps=true; the false reading came from GO_GO_WM_NO_SHM=0 being non-empty and thus disabling shm in the harness's 'enabled' arm. Fixed in xshm.Available with envDisabled + test. The machine takes the MIT-SHM path and gets the 4.8x, not 3.5x.


## 2026-07-22

Consolidated all optimization findings: 5.31 -> 0.32 ms/paint in the harness (16.6x), with a catalogue of what was built and rejected (sub-image transfer, sync barrier, allocating index, bucket 64, Stick shrink, hysteresis) and the measurements that changed direction.


## 2026-07-22

Step 22: fresh-eyes code review of the performance work — design-doc 02 with 13 ranked findings (R1 bare env test survives in perf.go, R2 bufferBytes omits back buffer, R3 stale bucketSizeFor comment, R13 paintFrame state machine), design-level critique, reviewer checklist

### Related Files

- /home/manuel/workspaces/2026-07-21/go-go-wm-goja/go-go-wm/ttmp/2026/07/21/GGWM-012-GUIDES--import-go-go-wm-engineering-guides-and-handbook/design-doc/02-code-review-guide-the-ggwm-012-performance-work-reviewed-for-a-new-intern.md — The review guide


## 2026-07-22

Step 23: confirmed the chrome-bounce artifact — MoveResize precedes the repair by the full paint cost, so the server repaints the resized frame from the stale background pixmap; instrumented (stale_chrome_gap_* counters, staleChromeGap log), measured (663/663 resizes, p50 0.3ms harness / ~9ms real hw), photographed (gap-delay-*.png)

### Related Files

- /home/manuel/workspaces/2026-07-21/go-go-wm-goja/go-go-wm/pkg/wmx11/manage.go — resizedAt stamp + noteRepair + repair delay switch


## 2026-07-22

Step 24: fixed the chrome bounce — paintFrame split into content (upload + background swap) and repair; relayoutPaint orders upload → MoveResize → repair so the server's resize repaint draws new-size chrome; gap avg 0.36→0.024ms, delayed-repair photos clean, scenario sweep clean

### Related Files

- /home/manuel/workspaces/2026-07-21/go-go-wm-goja/go-go-wm/pkg/wmx11/manage.go — paintFrameContent/repairFrame split and relayoutPaint reorder

