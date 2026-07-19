---
Title: 'Preliminary research: X11, presentations, kitty, testing'
Ticket: GGWM-001-PBUI-WM
Status: active
Topics:
    - wm
    - pbui
    - x11
    - broker
    - kitty
DocType: reference
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://ttmp/2026/07/18/GGWM-001-PBUI-WM--pbui-window-manager-in-go-split-tree-wm-presentation-broker-kitty-integration/sources/pbui-shell.jsx
      Note: prototype the research analyzes
    - Path: repo://ttmp/2026/07/18/GGWM-001-PBUI-WM--pbui-window-manager-in-go-split-tree-wm-presentation-broker-kitty-integration/sources/screenshot-01-full-shell-object-menu.png
      Note: visual reference — full shell with object menu open
    - Path: repo://ttmp/2026/07/18/GGWM-001-PBUI-WM--pbui-window-manager-in-go-split-tree-wm-presentation-broker-kitty-integration/sources/screenshot-02-accept-mode-drag-swap.png
      Note: visual reference — drag preview, trace/event log
ExternalSources:
    - https://github.com/BurntSushi/wingo
    - https://github.com/jezek/xgb
    - https://github.com/BurntSushi/xgbutil
    - https://sw.kovidgoyal.net/kitty/kittens/hints/
    - https://sw.kovidgoyal.net/kitty/open_actions/
    - https://specifications.freedesktop.org/wm-spec/
Summary: Cleaned-up preliminary research for porting the PBUI shell prototype to a real X11 window manager in Go, covering the WM/presentation split, the broker protocol design, kitty as a presentation surface, module decomposition, and testing without touching the live session.
LastUpdated: 2026-07-18T20:30:00-04:00
WhatFor: Source material and rationale behind the design decisions in the design doc; read when you want the "why" in more depth than the design doc carries.
WhenToUse: Consult before re-litigating architecture choices (X11 vs Wayland, broker vs in-WM, kitty integration mechanics) or when extending the testing strategy.
---


# Preliminary research: X11, presentations, kitty, testing

This document is the cleaned-up preliminary research that preceded the design.
The prototype under discussion is `sources/pbui-shell.jsx` in this ticket — a
React implementation of a CLIM / Genera "Dynamic Windows" flavored shell:
a binary-split tiling window manager whose every visible object (colors,
numbers, tiles, workspaces, log events) is a *typed presentation* that commands
can `accept` from anywhere on screen.

The research splits the port into two very different problems, and it is worth
keeping them separate: the **window manager** part (tiles, sticky zones,
workspaces, the look) maps onto X11 almost embarrassingly well, while the
**presentation** part fights X11's model at a fundamental level.

## 1. The WM proper

The prototype's split tree is essentially **bspwm's data model** — binary space
partitioning with leaves as windows and internal nodes carrying a direction and
a ratio (`sources/pbui-shell.jsx:135-169`). A from-scratch implementation is a
*reparenting* WM:

- Select `SubstructureRedirectMask` on the root window; intercept `MapRequest`.
- Wrap each client in a frame window the WM owns; lay frames out from the tree.
- The frame is where the look lives: the 2px ink border, the colored title
  strip in IBM Plex Mono, the ⠿ grip, and the ⬌ ⬍ ✕ buttons are drawn by the
  WM itself, server-side.

That is the *authentic* way to get this aesthetic — flat fills, hard 1–2px
rules, no gradients, no rounded corners — because it needs nothing from a
compositor. Set the root window to the paper color and you are done.

### Sticky zones and drag previews

- Divider drag is a pointer grab (`GrabPointer`) with motion events, snapping
  the ratio to ¼ ⅓ ½ ⅔ ¾ exactly as the prototype does
  (`sources/pbui-shell.jsx:166-169`).
- For the drop preview (the dashed half-pane) there are two idiomatically
  different options: an override-redirect window with an alpha visual (requires
  a running compositor), or the old-school route — XOR rubber-banding or a
  stippled overlay drawn directly, which works everywhere and honestly *suits*
  the Genera look better than translucency does.
- The snapped-divider flash is just recoloring the divider window.
- Workspaces are N trees plus `_NET_CURRENT_DESKTOP` / `_NET_WM_DESKTOP` from
  EWMH so pagers and bars interoperate; the status bar and mouse-doc line are
  one dock-type strut window the WM owns.

## 2. The hard, interesting part: presentations

X11's ontology stops at windows. The WM can present *windows, tiles, and
workspaces* as first-class objects with menus — that part survives intact. But
"right-click a color swatch inside app A, then accept it into app B" requires
knowing about objects *inside* clients, which X11 deliberately does not
support. In the React prototype this was free because everything shares one
address space — which is precisely why it was free on the Lisp Machine too.
On X you need a protocol.

The good news: X11 already contains a typed-object-transfer protocol to model
`accept` on — **selections**. `PRIMARY`/`CLIPBOARD` transfers are exactly
"requestor asks for a value, negotiates a type from a `TARGETS` list, owner
converts." XDND is the same machinery for drag-and-drop. So the honest design
is:

- A small client library (over a session Unix socket) that apps link against.
- Entering accept mode means the broker broadcasts "accepting `<color>`";
  participating clients highlight their matching presentations and, on click,
  publish the value with a MIME-ish type (`application/x-pbui-color`).
- The WM draws the red ACCEPTING banner; the mouse-doc line is populated by
  clients sending hover-doc messages to the bar.
- Non-participating apps (Firefox, a plain terminal) present as opaque
  `<window>` objects — degraded but coherent, which is exactly how CLIM treated
  foreign data too.

### Pragmatic paths, in increasing order of ambition

1. **Don't write the WM** — configure bspwm (identical tree model, `bspc`
   scripting) plus a custom bar, and spend all effort on the presentation
   protocol.
2. **StumpWM** — a Common Lisp tiling WM, hackable at runtime via a REPL, with
   the right cultural DNA; the accept broker could be implemented *in CL*, and
   McCLIM (the living CLIM implementation) could provide real presentations,
   `accept`, and presentation translators rather than a reimplementation.
3. **From scratch** for full control of frames and drawing, budgeting most of
   the pain for ICCCM/EWMH compliance edge cases (focus models,
   `WM_TAKE_FOCUS`, struts, xrandr — one tree per output).

This ticket chooses path 3, in Go (see below); paths 1 and 2 remain useful
references for semantics (bspwm's tree ops, CLIM's presentation translators).

### Wayland caveat

If this ever aims at daily use: Wayland (wlroots) makes the *visual* half
easier — the compositor composites everything, so previews, banners, and
per-tile effects are free — but makes the *protocol* half stricter, since
arbitrary client introspection is exactly what Wayland forbids; the pbui
protocol would become a proper Wayland protocol extension. On X11 the whole
thing can be prototyped loosely, which is right for this stage.

**One-sentence summary:** the tiling shell ports almost mechanically (it is
bspwm with better clothes), the aesthetic ports *better* than modern looks
would (it needs no compositor), and the presentation layer is the actual
research project — it stops being a UI pattern and becomes an IPC protocol,
with X selections/XDND as the precedent to steal from.

## 3. Go for the WM

Entirely viable, with an existence proof: **BurntSushi's wingo** is a full
reparenting, EWMH-compliant WM in pure Go, and it is the codebase to read
first. The stack:

- `github.com/jezek/xgb` — the X protocol in pure Go, no cgo (the maintained
  fork of BurntSushi's xgb).
- `github.com/BurntSushi/xgbutil` — the tedious parts: `ewmh` and `icccm`
  helper packages, `keybind`/`mousebind`, and `xgraphics` for drawing into
  pixmaps.

The pleasant surprise: `xgraphics` plus `golang.org/x/image/font` with the
IBM Plex Mono TTF is *sufficient* for this aesthetic — flat fills, 1–2px hard
rules, monospace text — so Cairo/Pango can be skipped entirely and the result
is one static cgo-free binary.

Disciplines and caveats:

- Keep X event handling on a **single goroutine** (X connections do not love
  concurrent request interleaving for this kind of work); let goroutines shine
  where they actually help — the broker, timers, IPC.
- The Go X ecosystem is in maintenance mode, not growing: occasionally you
  will read protocol docs and write a request by hand where a C WM would call
  a library function. For a personal research WM that is a fine trade.
- Go is arguably the *best* choice for the broker daemon — the accept/menu
  protocol over a Unix socket is bread-and-butter Go. A deterministic-CBOR
  framing (as prototyped in SGEP) would work as the wire format; NDJSON is
  the simpler starting point.

## 4. Kitty as a presentation surface

Kitty accidentally contains most of a presentation system already. The problem
decomposes into *how apps mark objects* and *how the user picks them*, and
kitty has native machinery for both.

### Marking: OSC 8 hyperlinks as presentations

The precedent to steal is OSC 8 hyperlinks. Any CLI app can emit:

```
ESC]8;;pbui://color/%23b0563f ESC\ ▉ #b0563f ESC]8;;ESC\
```

— a hyperlink whose URI is a typed pbui object rather than an http URL. This
is the terminal-world equivalent of `<P ptype value>`: the visible text is the
presentation's face, the URI carries `{ptype, value}`. It degrades perfectly —
in a dumb terminal it is just text, in any OSC 8 terminal it is at least
clickable, and in *your* kitty it is a live presentation. A tiny helper
(`pbui-present color '#b0563f' '▉ #b0563f'`) makes opting in a one-liner for
your own tools, and filters can wrap `ls`/`git` output to inject links.

### Picking: the hints kitten is accept mode wearing a fake mustache

Kitty's **hints kitten** already scans the screen, overlays single-key labels
on matching items, and returns the selection to a program — and it natively
supports `--type hyperlink`, so it can select OSC 8 links specifically. The
accept flow becomes:

1. The WM enters accept mode for `<color>`.
2. The broker uses kitty remote control (`kitty @ kitten ...`) to launch a
   custom kitten in the focused kitty window that filters hyperlinks to
   `pbui://color/*`, highlights only those.
3. The kitten posts the chosen value back to the broker socket.

From the user's chair it is the same gesture as the React shell: banner
appears, matching objects light up, pick one. Custom kittens are Python with
real access to screen line data and hyperlink ids — this is maybe a hundred
lines, not a fork.

### Menus: open_actions

Kitty's `open_actions.conf` routes URL schemes to programs — a plain click on
any `pbui://` link invokes `pbui-menu <uri>`, which asks the broker to pop the
type-directed action menu as an override-redirect X window drawn by the WM in
the paper-and-ink style, positioned near the pointer. The menu is thus
*shared infrastructure*: the same verbs ("Inspect", "Mix with…", "Collect
into Notes") work whether the color came from a WM-native tile or from
`git log` output in kitty.

### Two more pieces

- **Non-cooperating programs**: do what the hints kitten does internally —
  regex/parser-based scraping (SHA-looking strings → `<git-commit>`, paths →
  `<file>`, PIDs, IPs, hex colors). That is the terminal analog of CLIM
  presentation translators from strings; tmux-fingers proves the UX works.
- **Faces**: kitty's graphics protocol means presentations can have real
  rendered faces — an actual swatch for a color, a thumbnail for an image
  path — which no other terminal integration could offer.

### Honest limitations

- The hover mouse-doc line will not work inside kitty (no hover event is
  exposed), so the documentation line only updates for WM-native objects or on
  selection.
- Right-click specifically is awkward — kitty's link handling is
  click-scheme-based rather than button-differentiated. Either accept that
  left click opens the menu (which is the prototype's convention anyway:
  `sources/pbui-shell.jsx:63-68` makes left-click fall through to the menu
  when there is no accept and no primary action) or bind a `mouse_map` to a
  "menu for object under cursor" kitten.

Neither gap undermines the architecture. Kitty ends up the best-behaved
citizen — fitting, since a terminal full of typed, clickable, accept-able
objects is about as close as modern software gets to a Genera Dynamic
Listener.

## 5. Module decomposition

The architecture almost dictates the split, because the seams wanted for
testing are the same seams wanted for composition: everything X-flavored on
one side, everything pure on the other, and the broker protocol as the
load-bearing contract in the middle. Seven modules with strict dependency
direction:

| Module | Contents | Depends on | X? |
|---|---|---|---|
| `pbui-core` | ptype names, typed values, verb descriptors, accept-session messages, wire framing | nothing | no |
| `pbui-broker` | accept state machine, action registry, Unix socket daemon | pbui-core | **no** |
| `wm-core` | split tree, sticky snapping, moveSplit, workspaces — pure functions | nothing | no |
| `wm-x11` | xgb event loop, reparenting, frames, EWMH, grabs | wm-core, pbui-core | yes |
| `pbui-draw` | paper-and-ink theme rendered into `image.RGBA` | nothing | no |
| `pbui-present` (+`pbui-scrape`) | OSC 8 emitters, broker client, regex translators | pbui-core | no |
| kitty pieces | accept kitten (Python), open_actions handler | broker socket | no |

The critical decision: **the broker does not link X**. The WM is just another
client of it — a privileged one that renders banners and menus, but a client.
That is what lets a kitty kitten, a CLI helper, and the WM all participate
symmetrically, and what makes the broker testable with zero graphical
environment. `wm-core` takes "leaf ids and rectangles in, rectangles out" with
no X types in its API. `wm-x11` stays thin and boring; every line there is a
line that can only be tested expensively.

## 6. Testing strategy

### Pure layers (free, and where the bugs are)

- **wm-core**: property-based tests over random operation sequences — leaf
  count conserved by swap, decremented-then-incremented by moveSplit, ratios
  in bounds, no dangling ids, close always yields a valid binary tree. A trick
  worth doing: the React prototype is a working reference implementation, so
  generate random op scripts, run them through both the JS and Go trees, and
  diff serialized results. Cross-implementation oracles find porting mistakes
  nothing else will.
- **broker**: table-driven tests with in-process fake participants (accept,
  cancel via Esc, participant disconnects mid-accept, two accepts racing),
  under `-race`, with `go test -fuzz` pointed at the wire decoder since it
  eats untrusted bytes from arbitrary clients.
- **pbui-draw**: golden PNGs — pin the bundled Plex Mono and render sizes so
  output is deterministic.

All of this runs anywhere, including CI, with no display.

### X layer without touching the live session

- **Xephyr** is the interactive dev loop: a nested X server appearing as a
  normal window inside the real session. `Xephyr :1 -screen 1280x800 &` then
  `DISPLAY=:1 ./go-go-wm wm & DISPLAY=:1 xterm` gives a complete sandboxed
  desktop. You will live in this window for months.
- **Xvfb** is the same headless, for automation: `xvfb-run` the WM, map
  synthetic clients, assert.
- Give the WM a **query IPC from day one** (`go-go-wm query tree`, in the
  spirit of `bspc`) — "ask the WM what it believes" is vastly more reliable
  than screenshot inspection, and doubles as the debugging tool forever.
- Complement with EWMH property checks (`xprop`/`wmctrl`, or xgbutil in the
  harness) to validate what *other* programs will see.
- Synthetic input (drags across dividers, ⠿ drops into edge zones) via
  `xdotool`/XTEST against the virtual display.
- When CI fails mysteriously: ffmpeg-record the Xvfb screen; keep `x11trace`
  in your pocket for protocol-level confusion. The historically awkward
  clients (Java apps, GIMP's dialog swarm) are cheap Xephyr smoke tests.

### Kitty pieces without your kitty

Kitty was built for this: a second instance with
`kitty --config test.conf --listen-on unix:/tmp/pbui-test-kitty -o allow_remote_control=yes`
plus `KITTY_CONFIG_DIRECTORY` pointed at a test directory shares nothing with
the daily instance. Run it inside Xephyr/Xvfb and it is fully sandboxed.
Drive it with remote control: `kitten @ send-text` to make a fake CLI print
pbui links, `kitten @ get-text --ansi` to assert the OSC 8 sequences landed,
`kitten @ send-key` to operate the picker.

Notes from the trenches:

- Kitty wants OpenGL; headless CI needs Mesa's software path
  (`LIBGL_ALWAYS_SOFTWARE=1` with llvmpipe — kitty's own CI runs this way).
- Structure the kitten so the interesting part — scan screen lines, extract
  `pbui://` candidates, rank them — is pure Python unit-tested against fake
  screen content, with only a sliver touching kitty's boss API.
- Below kitty entirely: test the OSC 8 emitter by running it under a pty you
  own (`creack/pty` in Go) and parsing the captured bytes.

### The pyramid, assembled

Unit tests on core/broker/draw/layout with no display; component tests of
broker-plus-fake-participants; golden renders; Xephyr for hands and Xvfb for
CI at the integration layer; and exactly **one** full end-to-end smoke test —
Xvfb, WM, broker on a tmpdir socket, isolated kitty, a demo script that prints
a `<color>`, an accept triggered through the WM, the picker driven by injected
keys, and an assertion that the broker delivered the right value. Keep that
E2E singular and paranoidly maintained; breadth belongs in the layers below.

Hermeticity is environment discipline: every test owns its `DISPLAY`,
`XDG_RUNTIME_DIR`, `PBUI_SOCKET`, `FONTCONFIG_FILE`, `KITTY_CONFIG_DIRECTORY`,
and ideally `HOME`, so nothing can reach the real session even by accident.

The payoff of this split: the two components iterated on most — layout
semantics and the accept protocol — never require a display to test, the
aesthetic is verified by diffing PNGs, and X11 is reduced to a thin adapter
whose tests are few, slow, and honest about it.

## 7. External references

- wingo (Go reparenting WM, read first): https://github.com/BurntSushi/wingo
- xgb (maintained fork): https://github.com/jezek/xgb
- xgbutil (ewmh/icccm/keybind/xgraphics): https://github.com/BurntSushi/xgbutil
- bspwm (identical tree model, `bspc` IPC precedent): https://github.com/baskerville/bspwm
- EWMH spec: https://specifications.freedesktop.org/wm-spec/
- ICCCM: https://www.x.org/releases/current/doc/xorg-docs/icccm/icccm.html
- kitty hints kitten: https://sw.kovidgoyal.net/kitty/kittens/hints/
- kitty open actions: https://sw.kovidgoyal.net/kitty/open_actions/
- kitty remote control: https://sw.kovidgoyal.net/kitty/remote-control/
- OSC 8 hyperlinks spec: https://gist.github.com/egmontkob/eb114294efbcd5adb1944c9f3cb5feda
- StumpWM: https://stumpwm.github.io/ · McCLIM: https://mcclim.common-lisp.dev/
- CLIM 2 spec (presentations, accept, translators): http://bauhh.dyndns.org:8000/clim-spec/index.html
