# Tasks

## H1 — theme engine
- [x] Theme struct + paper/light/dark registry + SetTheme in pkg/draw
- [x] Convert init-time color copies (AppColors, uispec.tones, traceTone)
- [x] Swap-completeness + contrast tests

## H2 — WM/scripting wiring
- [x] Config.Theme + --theme flag; boot applies theme
- [x] IPC theme / set-theme; repaint + back pixels; theme.changed event
- [x] wm.theme() via Backend (IPC + ScriptBackend); script initial theme; ui.app live re-theme

## H3 — i3-parity API
- [x] wm.exec (rc always; run/repl behind --allow-exec)
- [x] wm.focus(target) / wm.move(dir) — geometry + IPC + backends
- [x] WM_CLASS in window.managed + WindowInfo; class rules

## H4 — i3.js
- [x] Port ~/.config/i3/config to examples/scripts/i3.js (mapping table in header)
- [x] Live Xvfb verification + smoke fixture + theme screenshots

## H5 — docs
- [x] Intern guide (design-doc/02)
- [x] Diary, changelog, doctor, reMarkable upload
