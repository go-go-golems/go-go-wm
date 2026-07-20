# Changelog

## 2026-07-19

- Initial workspace created


## 2026-07-19

Profiled the paint path under scripted drag stress (pprof via GO_GO_WM_PPROF): convertRGBA 33%, Fill 26%, Expose double-paints 27%, GC 30%. Fixed with row-copy Fill, row-major BGRA conversion, per-frame buffer/pixmap caching (Expose becomes one XPaint), 16ms drag coalescing with resized-only repaints. Same stress: 7.74s→2.43s CPU samples; boot 6.2s→2.8s. Added click-to-focus (sync GrabButton+ReplayPointer). Commit bb5bfbf.

### Related Files

- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/draw/ximage.go — fast conversion
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/wmx11/manage.go — buffer caching paintFrame


## 2026-07-19

Entry 4: workspace-creation CPU measured directly (344ms/workspace, load-independent) — copyImage's per-pixel Set/At blit (allocates a color interface per pixel) was 73% of the burst; rewritten as clipped row copies → 76ms/workspace (4.5x). Under load average 51, ~85% of remaining wall time is scheduler queueing, not WM CPU.

### Related Files

- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/wmx11/manage.go — row-copy copyImage

