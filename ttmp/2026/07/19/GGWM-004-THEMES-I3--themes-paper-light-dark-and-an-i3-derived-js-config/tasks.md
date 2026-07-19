# Tasks

## H1 — theme engine
- [ ] Theme struct + paper/light/dark registry + SetTheme in pkg/draw
- [ ] Convert init-time color copies (AppColors, uispec.tones, traceTone)
- [ ] Swap-completeness + contrast tests

## H2 — WM/scripting wiring
- [ ] Config.Theme + --theme flag; boot applies theme
- [ ] IPC theme / set-theme; repaint + back pixels; theme.changed event
- [ ] wm.theme() via Backend (IPC + ScriptBackend); script initial theme; ui.app live re-theme

## H3 — i3-parity API
- [ ] wm.exec (rc always; run/repl behind --allow-exec)
- [ ] wm.focus(target) / wm.move(dir) — geometry + IPC + backends
- [ ] WM_CLASS in window.managed + WindowInfo; class rules

## H4 — i3.js
- [ ] Port ~/.config/i3/config to examples/scripts/i3.js (mapping table in header)
- [ ] Live Xvfb verification + smoke fixture + theme screenshots

## H5 — docs
- [ ] Intern guide (design-doc/02)
- [ ] Diary, changelog, doctor, reMarkable upload
