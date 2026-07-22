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

