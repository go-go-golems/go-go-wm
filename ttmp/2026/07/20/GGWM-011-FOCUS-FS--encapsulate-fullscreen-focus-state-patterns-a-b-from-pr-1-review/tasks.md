# Tasks

## TODO

- [x] Phase 0: add regression tests for current focus/fullscreen behavior (RC-5/6/7/12/13) <!-- t:x3yh -->
- [x] Phase 1: extract read-only fullscreenState helpers (Option A, read side) <!-- t:cet6 -->
- [x] Phase 2: move fullscreen mutators into fullscreenState (Option A, write side) <!-- t:cn83 -->
- [x] Phase 3: simplify focus() via fullscreenState.FocusTarget <!-- t:evg2 -->
- [x] B1: define focusTarget/focusKind + focusState type with Current()/Focused(f) read methods (no fields wired yet; pure types in a new focus_state.go) <!-- t:hjh4 -->
- [x] B2: add focusState to WM struct alongside focused/focusedFloat (shadow, not replace); seed it from the old fields so Current() agrees <!-- t:qtkf -->
- [x] B3: implement focusState mutators FocusTile/FocusFloat/FocusFullscreen/Restore, each updating target + preservedTile atomically <!-- t:5wug -->
- [x] B4: route focus() through focusState (manage.go:506) — the RC-5/7/13 special cases collapse into FocusFullscreen/FocusTile <!-- t:vvpn -->
- [x] B5: route focusFloat() through focusState.FocusFloat (float.go:285); make preservedTile explicit <!-- t:cqbh -->
- [x] B6: route unmanageFloat() restoration through focusState.Restore (float.go:255) — replaces the implicit w.focused convention <!-- t:fnkd -->
- [x] B7: replace frameFocused() (float.go:303) with focusState.Focused(f) — delete the old predicate <!-- t:rg94 -->
- [x] B8: migrate w.focused read sites in input.go (7) + ipc.go (2) + launcher.go (2) + pbui.go (2) + theme.go (6) to focusState.Current() <!-- t:n1fa -->
- [x] B9: migrate w.focusedFloat read sites in float.go (8) + input.go (1) + launcher.go (1) + scripting.go (1) + wm.go (2) to focusState.Current() <!-- t:40zr -->
- [x] B10: delete the focused + focusedFloat fields from WM; focusState is the single source of truth <!-- t:bp5v -->
- [x] B11: coordinate focusState with fullscreenState (Phase 1-3) so the two can't disagree on who owns focus <!-- t:jz9o -->
- [x] B12: audit WM threading model (ops channel) — confirm focusState needs no mutex, or add one if X-event handling isn't serialized <!-- t:8r4i -->
- [x] B13: verify — go test ./... -race; manual X11 (fullscreen tile/float + Mod4-space + close + workspace switch); confirm RC-5/6/7/12/13 tests pass <!-- t:z904 -->
