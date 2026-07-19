---
Title: go-go-wm user guide
Slug: user-guide
Topics:
- wm
- getting-started
IsTemplate: false
IsTopLevel: true
ShowPerDefault: true
SectionType: GeneralTopic
---

This guide covers daily use: the concepts on screen, the interactions,
and the command-line tools. Setup lives in `glaze help getting-started`;
scripting in `glaze help js-api-reference`.

## The six concepts

- **Tiles.** Every main window lives in a tile of a binary split tree —
  no overlap, no gaps in coverage. Splits are rows (side by side) or
  columns (stacked). Builtin tiles (trace, listener, inspector,
  launcher) are drawn by the WM itself; script tiles are drawn from a
  JavaScript app's state. Dialogs, utility palettes, splash screens,
  and fixed-size windows *float* above the tiled world instead: they
  keep their own size, center on their parent window, drag by their
  title strip, and never disturb the tree. `wm.float()` (Mod4-Shift-
  space in i3.js) toggles any window between the two worlds.
- **Workspaces.** Independent trees, switched via the top-bar chips or
  Mod4-1..9. New workspaces open on an empty launcher tile.
- **The launcher.** One command registry — your .desktop applications,
  the builtin tiles, and script-registered commands — behind two
  surfaces: the Mod4-d popup and every empty tile (just start typing;
  Enter launches into that tile). Results are fuzzy-matched and
  ordered by use (frecency). Launcher entries are `command`
  presentations: right-click one for its verbs, and a script can
  `accept("command")` to use any launcher surface as a picker.
- **Presentations.** Colors, files, numbers, commits — anything typed —
  render as chips. A chip is not a picture: it is the object.
- **Accept.** A program can ask the desktop for "a color, from
  anywhere". The banner shows the prompt, matching chips highlight
  everywhere, one click answers, Escape cancels. This is the
  desktop's inter-process glue.
- **Verbs.** Right-click any presentation for its action menu. Entries
  come from every running program that registered a verb for that
  type — a script you started an hour ago serves menu entries in
  every other window.

## Interactions reference

| gesture | effect |
|---|---|
| click title strip | focus tile |
| click inside a window | focus tile (click still reaches the app) |
| title-strip buttons | split right · split down · close |
| drag ⠿ grip onto a tile | center = swap, edge = dock beside |
| drag a divider | resize (sticky at ¼ ⅓ ½ ⅔ ¾) |
| drag a float's title strip | move the float (strip stays on screen) |
| type into an empty tile | filter the launcher; Enter launches here |
| Mod4-d | launcher popup (Esc closes, ↑↓ select) |
| left-click a chip | primary action / answer a pending accept |
| right-click a chip | verb menu |
| top-bar chip | switch workspace (answers `workspace` accepts too) |
| Escape | cancel accept / close menu |

The bottom bar always shows the current mode (READY / ACCEPT MODE /
RESIZING), a context hint for whatever the mouse is over, and counts.

## Themes

`--theme paper|light|dark` at start; live switching via scripts
(`wm.theme("dark")`) or a keybinding in your rc file (i3.js binds
Mod4-t to cycle). Client applications keep their own colors; the WM
themes its chrome, bars, and builtin tiles.

## Your configuration file

Start the WM with `--rc yourconfig.js` (and usually
`--no-default-binds` so your file owns the keyboard). The rc file is a
JavaScript program with the full API: keybindings (`wm.bind`),
launchers (`wm.exec`), window rules (`wm.rule({class: /Slack/,
workspace: "8"})`), layouts, theme choice. `examples/scripts/i3.js` is
a complete worked example ported from a real i3 config; its header
maps every i3 directive to its equivalent here.

## Scripts as tools and daemons

Any JavaScript file runs against the live desktop:

    go-go-wm run --once script.js     # run and exit when settled
    go-go-wm run script.js            # keep serving verbs/subscriptions
    go-go-wm repl                     # interactive, same API

One-shot scripts automate ("build my project workspace"); daemons
extend (register verbs, route windows on events, put whole JS-rendered
apps on screen with `ui.app`). Develop in the REPL, deploy the same
code in a script or your rc file.

## The CLI tools

    go-go-wm query tree                # what the WM believes (tables; --output json)
    go-go-wm query windows             # managed windows incl. class, focus
    go-go-wm query events              # follow the event bus (--count N)
    go-go-wm query verbs --ptype color # who serves what
    go-go-wm accept --ptype color      # ask the desktop from a shell
    go-go-wm answer --ptype color --value '#aabbcc'
    go-go-wm demo --app colors         # the demo applications
    go-go-wm scrape / present / kitty  # terminal-side presentation tools

Everything the WM knows is also one NDJSON line away on the control
socket (`$GO_GO_WM_SOCKET`): `echo '{"q":"windows"}' | nc -U $SOCKET`.

## Running it for real

A second full X session is the best daily-driver setup: log into a
text console (Ctrl+Alt+F3) and

    startx ~/ggwm-session.sh -- :2 vt3

where the session script execs `go-go-wm wm --embedded-broker --rc
~/.config/go-go-wm/rc.js ...`. Ctrl+Alt+F2/F3 switches between your
sessions. Not yet supported: multi-output workspace pinning,
scratchpad/sticky windows, fullscreen toggle.
