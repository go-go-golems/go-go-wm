# Changelog

## 2026-07-19

- Initial workspace created


## 2026-07-19

Design phase: wrote the intern guide (wolframjs-repl RichValue protocol mapped onto PBUI — views as uispec renderers, operations as verbs, Out[n] as live presentations; three new uispec segment kinds; replapi kernel unchanged; phases R1-R4, GGWM-008 keyboard substrate as dependency). Implementation deferred.


## 2026-07-19

Implemented R1-R3: pkg/repl (Value/Derive/NormalizeRich, bounded), uispec table/image/field segments, draw plotters, cell surface + repl --ui xapp host over an unmodified replapi kernel (prelude Out/$_/console shim, expression wrap + statement fallback, WithRuntime capture), repl verbs, launcher entry. 9-stage E2E incl. the accept-by-click thesis. builtin:repl tile and R4 enrichment deferred (recorded). Commits 1c58768, bfff739.

### Related Files

- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/cmds/replui.go — kernel capture + surface host
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/repl/derive.go — derivation + views
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/scripts/replui-smoke.sh — 9-stage E2E incl. thesis stage


## 2026-07-19

Dogfooding fix: notebook prelude pre-binds wm/pbui/ui (user hit 'wm is not defined'); terminal REPL keeps explicit require. replui-smoke stage added (10 green). Commit b2bc199.

