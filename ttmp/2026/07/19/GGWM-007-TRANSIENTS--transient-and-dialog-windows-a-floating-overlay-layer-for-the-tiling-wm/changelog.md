# Changelog

## 2026-07-19

- Initial workspace created


## 2026-07-19

Design phase: wrote the intern guide (float layer as shell state, X11 transient detection, focus routing, rule push-down, testwin harness, phases T1-T4). Implementation deferred to a future session.


## 2026-07-19

Implemented all four phases: float layer (detection precedence, lifecycle, two-register focus, stacking, workspace association, honored ConfigureRequests, strip-drag), scripting surface (wm.rule float push-down, wm.float, windows floating/leader fields), i3.js float rules (28 for_window entries + Mod4+Shift+space), testwin client, float-smoke E2E (6 stages green). Design doc gained Part VII as-built notes; help topics updated.

### Related Files

- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/cmds/testwin.go — float-signal test client
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/wmx11/float.go — the float layer
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/scripts/float-smoke.sh — E2E fixture


## 2026-07-19

Fullscreen toggle (Mod4-f / wm.fullscreen / IPC): shell state like floats, full-bleed clients, workspace-switch exit; float-smoke stage 7. Commit 0af7fc1.

