# Changelog

## 2026-07-20

- Initial workspace created


## 2026-07-20

Created ticket for the fullscreen/focus encapsulation refactor (Patterns A & B from GGWM-010 Step 5). Design-only: system primer, pattern diagnosis mapping 5 Codex comments to invariant violations, two design options (fullscreenState helper + unified focusState), phased behavior-preserving migration, testing strategy, risk register. No implementation.

### Related Files

- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/ttmp/2026/07/20/GGWM-011-FOCUS-FS--encapsulate-fullscreen-focus-state-patterns-a-b-from-pr-1-review/design-doc/01-fullscreen-focus-state-encapsulation-analysis-and-intern-implementation-guide.md — Primary analysis/design guide


## 2026-07-20

Replaced coarse Phase 4/5 with detailed Option B task breakdown (B1-B13): define focusState types, shadow old fields, route mutators (focus/focusFloat/unmanageFloat/frameFocused), migrate read sites per file (input/ipc/launcher/pbui/theme/float/scripting/wm), delete old fields, coordinate with fullscreenState, audit threading, verify. Updated design doc Phase 4 to reference the task sequence.

### Related Files

- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/ttmp/2026/07/20/GGWM-011-FOCUS-FS--encapsulate-fullscreen-focus-state-patterns-a-b-from-pr-1-review/tasks.md — Option B task breakdown B1-B13


## 2026-07-20

Implemented Phase 0 (regression tests for RC-5/6/7/12/13 via pure decision helpers) + Phase 1 (read-only fullscreenState: Active/Owns/OwnsGeometry/OwnsFocus/FocusTarget; all w.fullscreen reads routed through it). Build OK, tests pass -race, lint/gosec clean. Tasks x3yh + cet6 checked.

### Related Files

- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/wmx11/focus_state.go — Phase 0 decision helpers + Phase 1 fullscreenState read methods
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/wmx11/focus_state_test.go — RC-5/6/7/12/13 regression tests


## 2026-07-20

Implemented Phase 2 (fullscreen mutators moved into fullscreenState: Toggle/Enter/Exit/Clear; WM methods are thin delegates) + Phase 3 (focus() simplified to consult computeFocusDecision/FocusTarget as single source of truth; 4-case switch collapsed to 2 if-branches). Build OK, tests pass -race, lint/gosec clean, Phase 0 tests pass unchanged. Tasks cn83 + evg2 checked.

### Related Files

- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/wmx11/focus_state.go — Phase 2 fullscreenState mutators (Toggle/Enter/Exit/Clear)
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/wmx11/manage.go — Phase 3 simplified focus() via computeFocusDecision

