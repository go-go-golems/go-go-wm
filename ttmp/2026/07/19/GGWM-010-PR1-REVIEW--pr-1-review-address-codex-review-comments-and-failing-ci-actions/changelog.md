# Changelog

## 2026-07-19

- Initial workspace created


## 2026-07-19

Created ticket, captured 5 Codex review comments + reproduced 4 failing CI checks locally (golangci-lint, govulncheck, gosec, dependency-review). Wrote intern-ready analysis/design/implementation guide with system primer + topic-separated analysis (A: Codex bugs, B: lint, C: gosec, D: vulns) + phased plan. Added 5 implementation tasks.

### Related Files

- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/ttmp/2026/07/19/GGWM-010-PR1-REVIEW--pr-1-review-address-codex-review-comments-and-failing-ci-actions/design-doc/01-pr-1-review-analysis-and-intern-implementation-guide.md — Primary analysis/design/implementation guide


## 2026-07-19

Uploaded design doc + diary bundle to reMarkable at /ai/2026/07/19/GGWM-010-PR1-REVIEW (verified). Updated diary with Step 2.

### Related Files

- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/ttmp/2026/07/19/GGWM-010-PR1-REVIEW--pr-1-review-address-codex-review-comments-and-failing-ci-actions/reference/01-investigation-diary.md — Diary Step 2 — doc creation + reMarkable upload


## 2026-07-19

Implemented phased plan: toolchain bump (govulncheck 0), lint fixes (0 issues), gosec fixes (0 issues), Codex review bugs RC-1..RC-5 with regression tests. All checks pass locally; pushed to wesen/task/go-go-wm.

### Related Files

- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/cmds/replui.go — RC-1 parse-error status check + RC-2 serialized eval worker
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/jsmod/eventfan.go — RC-3 deep-copy goSubs under lock via snapshot()
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/repl/value.go — RC-4 NormalizeRich stores value not descriptor
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/wmx11/manage.go — RC-5 pin focus to fullscreen frame


## 2026-07-19

CI verified: lint/govulncheck/gosec all pass on pushed branch. Dependency Review fails due to repo's Dependency graph feature being disabled (settings issue, not code). Updated design doc + diary with confirmed root cause.


## 2026-07-19

Addressed 5 new Codex review comments (RC-6..RC-10, all P2) on commit ad7570b3: fullscreen+batch switch, floating fullscreen focus, desktop entry rescan, frecency flush, per-surface redraw hooks. All tests pass under -race; lint/gosec clean.

### Related Files

- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/launcher/frecency.go — RC-9 resettable timer flush + Flush()
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/wmx11/manage.go — RC-7 floating fullscreen focus via focusedFloat
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/wmx11/wm.go — RC-6 exitFullscreen in ApplyBatch + RC-9 Flush in Shutdown

