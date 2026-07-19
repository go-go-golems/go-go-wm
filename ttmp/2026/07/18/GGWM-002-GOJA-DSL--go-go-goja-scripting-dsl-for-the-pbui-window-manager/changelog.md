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


## 2026-07-18

P2 complete: wmmod (Backend seam + IPCBackend, sugar compiling to Ops, fluent workspace, wm.on via shared EventFan, wm.bind stub), new wmcore move-leaf op (DetachLeaf/GraftLeaf; frames survive cross-workspace moves), 6-test fake-backend suite incl. replay property, live Xvfb verification of golden.js (self-asserting) and router.js (adopted fake-firefox into 'web' workspace). Fixed pre-existing launcher-app bug (blank tile in new workspaces).

### Related Files

- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/jsmod/wmmod/module.go — wm native module
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/wmcore/ops.go — move-leaf op


## 2026-07-18

P3 complete: wmx11.ScriptBackend (in-process Backend posting onto the WM loop), Config.OnReady hook, wm --rc flag booting an in-process goja runtime with its own broker connection, wm.bind live keybindings (X event -> JS post -> Op), go-go-wm repl over replapi (persistent bindings via IIFE rewriting), checked-in scripts/rc-smoke.sh E2E (PASSES). Live-verified: Mod4-e/Mod4-Shift-e scripted splits, REPL-driven splits.

### Related Files

- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/cmds/rc.go — rc.js bootstrap
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/wmx11/scripting.go — in-process backend
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/scripts/rc-smoke.sh — E2E smoke


## 2026-07-18

P4 complete: rules.go normalize->compile (wm.layout/wm.layouts/wm.rule/wm.rules, workspace.apply idempotency contract), Go-side rule engine via EventFan.SubscribeGo (window.managed -> move-leaf, no JS in the path), glaze help topics wm-module/pbui-module, scripts/examples-smoke.sh running all 5 runnable examples as fixtures (PASS), example scripts cookbook reference doc. Live-verified: zoom-rule adoption into 'calls', project-switcher dev layout at 0.62.

### Related Files

- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/jsmod/wmmod/rules.go — normalize->compile pipeline
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/scripts/examples-smoke.sh — examples as CI fixtures

