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

