# Tasks

## TODO

- [x] Create ticket and import the three engineering guides into sources/ <!-- t:zacw -->
- [x] Verify i3 port (examples/scripts/i3.js) is present in the workspace <!-- t:dd28 -->
- [x] Read and extract performance findings from all three guides <!-- t:5d9r -->
- [x] Map live codebase hot paths with file:line evidence (resize, render, JS crossings) <!-- t:ukt0 -->
- [x] Reconcile guide claims against the current code; mark already-done vs outstanding <!-- t:6vbj -->
- [x] Write the intern-facing performance design and implementation guide <!-- t:6nch -->
- [x] Maintain investigation diary <!-- t:paz0 -->
- [x] Relate key source files and update changelog <!-- t:zayb -->
- [x] Run docmgr doctor and resolve warnings <!-- t:pgxl -->
- [x] Upload the bundle to reMarkable <!-- t:j43y -->
- [x] P0: fix A/B harness logging (script output was swallowed) <!-- t:zps3 -->
- [x] P0: record shared_pixmaps=false finding; correct Part IV of the design doc <!-- t:1aaw -->
- [x] P0: add xshm/ximg surface-recreate counters + resize spans <!-- t:px75 -->
- [x] P0: add GO_GO_WM_NO_RESIZE_PAINT flag to measure the paint-removal upper bound <!-- t:mhl9 -->
- [x] P0: add benchmarks for Layout, ConvertRows, TitleStrip.Render, uispec.Render <!-- t:8l60 -->
- [x] P1: TreeIndex - remove O(n^2) Find-in-loop from relayout and syncDividers <!-- t:e8nl -->
- [x] P1: one wmcore.Layout per motion tick instead of two <!-- t:upde -->
- [x] P1: divider appearance generation - stop repainting dividers every relayout <!-- t:iry1 -->
- [x] P1: frame.mapped - stop unconditional Map/Unmap for every frame <!-- t:jl7z -->
- [x] P1: synthetic ConfigureNotify instead of full relayout for tiled clients <!-- t:9icy -->
- [x] P1: throttle gripMotion and cache the drop-preview surface <!-- t:lxoy -->
- [x] P1: verify with go test + benchmarks; no visible behaviour change <!-- t:e4yb -->
- [x] P2: instrument ConvertRows and upload separately (98% of a paint, undifferentiated) <!-- t:z0iv -->
- [ ] Phase 4 chrome/content split — RE-COST before scheduling: its main saving was obtained without it (Step 12); remaining value is only pane-width TitleStrip.Render and the pane-sized RGBA scratch <!-- t:a5q3 -->
- [x] Retire the VT harness in favour of the Xephyr one <!-- t:cov3 -->
- [x] P2: capacity-sized backing stores (bucket 128) — DONE, 8.3x fewer surface creations <!-- t:529x -->
- [x] P2: chrome-only compose/convert/upload — DONE, delivers Phase 4's main saving without new windows <!-- t:qnk5 -->
- [x] P0: bucket granularity sweep + buffer_bytes counter — DONE <!-- t:mny4 -->
- [x] Scenario sweep harness (fullscreen/float/workspace/theme) with screenshots — DONE <!-- t:m0xt -->
