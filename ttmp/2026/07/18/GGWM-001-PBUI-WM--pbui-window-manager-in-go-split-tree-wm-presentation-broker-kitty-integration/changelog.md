# Changelog

## 2026-07-18

- Initial workspace created


## 2026-07-18

Ticket created. Imported PBUI shell prototype (pbui-shell.jsx, 868 lines) and two look-reference screenshots into sources/. Wrote cleaned-up preliminary research (X11 port, presentations-as-IPC, kitty, module split, testing), the intern-level design/implementation guide (architecture, wire protocol, glazed command surface, 6 decision records, Phases 0-8 incl. goja-readiness), and the investigation diary. Uploaded design bundle to reMarkable.

### Related Files

- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/ttmp/2026/07/18/GGWM-001-PBUI-WM--pbui-window-manager-in-go-split-tree-wm-presentation-broker-kitty-integration/design-doc/01-pbui-wm-design-and-implementation-guide.md — primary design and implementation guide
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/ttmp/2026/07/18/GGWM-001-PBUI-WM--pbui-window-manager-in-go-split-tree-wm-presentation-broker-kitty-integration/reference/01-preliminary-research-x11-presentations-kitty-testing.md — cleaned-up preliminary research
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/ttmp/2026/07/18/GGWM-001-PBUI-WM--pbui-window-manager-in-go-split-tree-wm-presentation-broker-kitty-integration/reference/02-investigation-diary.md — session diary
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/ttmp/2026/07/18/GGWM-001-PBUI-WM--pbui-window-manager-in-go-split-tree-wm-presentation-broker-kitty-integration/sources/pbui-shell.jsx — imported prototype


## 2026-07-18

Implemented Phases 0-6 plus the application layer in one session: wmcore (pure tree+ops, property-tested), pbui wire protocol + broker (race-tested, fuzzed) + client lib, glazed CLI (broker/wm/accept/answer/present/menu/scrape/demo/kitty/query), draw package with vendored IBM Plex Mono and golden PNGs, reparenting X11 WM (frames, EWMH, drags with sticky snapping + divider state windows, workspaces, bspc-style IPC), WM-broker integration (banner, menus, verbs incl accept-composing swap), WM-embedded launcher/about/trace/listener/inspector over the event bus, six demo client apps (colors, numbers, notes, files, todo, markdown) with cross-process accept verified live in Xvfb (markdown Open... answered by file-browser click; color picked into listener; todo typed via keyboard). go vet + golangci-lint + all tests green. 12 verification screenshots saved for the blog post.

### Related Files

- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/apps/apps.go — presentation-surface framework
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/apps/demoapps/markdown.go — flagship cross-app accept demo
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/cmds/kitty/pbui_accept.py — accept kitten
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/pbui/broker/broker.go — accept state machine + event bus
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/wmcore/tree.go — pure layout engine
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/wmx11/wm.go — X11 shell entry

