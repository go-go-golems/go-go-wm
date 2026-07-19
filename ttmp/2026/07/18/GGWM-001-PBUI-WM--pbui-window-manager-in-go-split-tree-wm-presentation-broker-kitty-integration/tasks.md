# Tasks

## TODO

- [x] Phase 0: wire cmd/go-go-wm/main.go glaze-style (help system, logging), add xgb/xgbutil/glazed deps <!-- t:s4ac -->
- [x] Phase 1: pkg/wmcore — tree, Ops, Apply, Layout, Snap, ZoneAt + property tests + JS-oracle runner <!-- t:gonv -->
- [x] Phase 2: pkg/pbui + broker + client lib; accept/answer/query events CLI commands <!-- t:528h -->
- [x] Phase 3: pkg/draw — palette, embedded Plex Mono, widgets, golden PNG tests <!-- t:4xbp -->
- [x] Phase 4: pkg/wmx11 minimal WM in Xephyr (reparent, frames, drags, workspaces, EWMH, query IPC) <!-- t:bkju -->
- [x] Phase 5: WM-broker integration — banner, menus, tile/workspace ptypes and verbs, mouse-doc line <!-- t:coii -->
- [x] Phase 6: terminal participation — present, scrape, kitty install, accept kitten, open_actions <!-- t:67n0 -->
- [ ] Phase 7: built-in apps (listener/notes/inspector/trace), E2E smoke test, awkward-client tests, xrandr <!-- t:aljx -->
- [ ] Phase 8 (later ticket): go-go-goja bindings — apply/on/registerVerb/accept, repl command <!-- t:ufeq -->
- [x] Ticket setup: import prototype+screenshots, research doc, design doc, diary, reMarkable upload <!-- t:mgj2 -->
- [x] App layer: pkg/apps framework, WM-embedded launcher/about/trace/listener/inspector, demo apps colors/numbers/notes/files/todo/markdown (done 2026-07-18) <!-- t:ki8w -->
- [x] Divider windows with hot/drag/snap visual states (done 2026-07-18) <!-- t:vw78 -->
- [ ] Listener eval line: keyboard routing into builtin tiles <!-- t:dimz -->
- [ ] Trace/listener scrolling + golden tests for files/todo/markdown renderers <!-- t:tcde -->
- [ ] Kitty live E2E: isolated instance + accept kitten against real kitty <!-- t:a7us -->
- [ ] ICCCM edge matrix (WM_TAKE_FOCUS, transients), awkward clients (Java/GIMP), xrandr <!-- t:dfkm -->
- [ ] Cross-implementation oracle: random op scripts through JSX tree vs wmcore <!-- t:4u04 -->
- [ ] Checked-in E2E smoke script (Xvfb + WM + broker + demo apps) <!-- t:mp2p -->
