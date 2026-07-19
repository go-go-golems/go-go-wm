# Changelog

## 2026-07-18

- Initial workspace created


## 2026-07-18

Ticket created. Studied the go-go-goja and widget-dsl KB MOCs plus tribal notes (goja-execution-model owner-thread pattern, data-only vs host-access module split, DSL->normalized config->compiled plan). Wrote the DSL design: 6 script kinds, 3 attachment points (in-WM runtime+REPL, standalone broker-client scripts, one-shot macros), wm+pbui module surfaces with promise-based accept, normalize-then-compile for rules/layouts, 6 decision records, phases P1-P5, canonical examples.


## 2026-07-18

Added intern implementation guide (design-doc 02): go-go-goja runtime model (factory/owner/loop/runtimebridge), promise settlement pattern from fs_async, Backend interface for dual attachment, module implementation notes, concurrency contract with failure modes, testing strategy, phased file-level plan. Expanded tasks to file level.

