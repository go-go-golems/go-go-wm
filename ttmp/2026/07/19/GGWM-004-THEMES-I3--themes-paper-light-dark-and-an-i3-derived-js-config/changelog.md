# Changelog

## 2026-07-19

- Initial workspace created


## 2026-07-19

Implemented theme engine (paper/light with true white/dark on #1f1f1f) across draw/wmx11/IPC/JS with live re-theme of script apps; added i3-parity API (wm.exec, wm.focus/move via wmcore.NeighborLeaf, class rules, --no-default-binds); ported ~/.config/i3/config to examples/scripts/i3.js; fixed SetInputFocus(None) keyboard kill and the torn-palette double-SetTheme race; examples-smoke 9/9; wrote design doc, intern guide, diary. Commits 0461cee, cd7f4b2.

### Related Files

- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/examples/scripts/i3.js — the i3 config port
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/draw/theme.go — theme engine
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/wmcore/neighbor.go — directional navigation

