# Changelog

## 2026-07-18

- Initial workspace created


## 2026-07-18

Ticket created. Studied the go-go-goja and widget-dsl KB MOCs plus tribal notes (goja-execution-model owner-thread pattern, data-only vs host-access module split, DSL->normalized config->compiled plan). Wrote the DSL design: 6 script kinds, 3 attachment points (in-WM runtime+REPL, standalone broker-client scripts, one-shot macros), wm+pbui module surfaces with promise-based accept, normalize-then-compile for rules/layouts, 6 decision records, phases P1-P5, canonical examples.


## 2026-07-18

Added intern implementation guide (design-doc 02): go-go-goja runtime model (factory/owner/loop/runtimebridge), promise settlement pattern from fs_async, Backend interface for dual attachment, module implementation notes, concurrency contract with failure modes, testing strategy, phased file-level plan. Expanded tasks to file level.


## 2026-07-18

P1 complete: pkg/jsmod bridge+errors, pbuimod (promise accept, verbs, events w/ bounded queue, data helpers), go-go-wm run --once/--allow-exec, 9 broker-backed tests + queue tests + FuzzBridge (found & fixed 2 pre-existing pbui URI bugs + broker verb duplication), example scripts in examples/scripts/. Live-verified: hello.js print, git-verbs.js daemon verbs, palette.js cross-process accept flow.

### Related Files

- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/cmds/run.go — run command
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/jsmod/pbuimod/module.go — pbui native module

