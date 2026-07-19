# Changelog

## 2026-07-19

- Initial workspace created


## 2026-07-19

Design phase: wrote the intern guide (command registry over .desktop/builtins/script commands, popup + tile surfaces, the shared keyboard-input substrate, PBUI command ptype, phases L1-L4). Implementation deferred.


## 2026-07-19

Implemented all four phases: pkg/launcher registry (.desktop parser, fuzzy scorer, frecency), Mod4+d popup with direct input focus, frame keyboard substrate + launcher tile v2 (per-leaf query state, launch-into-tile), command ptype (verbs, accept mode, script commands via wm.command, wm.launch/wm.launcher). 12-stage launcher-smoke E2E green; help topics updated. Commits 83ab0e8, c0d0e02, 1f2dadf, 1b44015.

### Related Files

- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/launcher/registry.go — the pure registry
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/wmx11/launcher.go — popup, tile, substrate, launch routing
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/scripts/launcher-smoke.sh — 12-stage E2E fixture


## 2026-07-19

A2 wm.command: daemon-served launcher entries via IPC registration + command.invoke event dispatch + client.disconnected pruning; launcher-smoke stages 13-14. Also scripts/playground.sh demo session. Commit 0af7fc1.


## 2026-07-19

Playground fix from dogfooding: dropped -no-host-grab (it disables Xephyr's Ctrl+Shift grab toggle entirely); cheat sheet + getting-started corrected. Commit b2bc199.

