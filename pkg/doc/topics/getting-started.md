---
Title: Getting started with go-go-wm
Slug: getting-started
Topics:
- wm
- getting-started
IsTemplate: false
IsTopLevel: true
ShowPerDefault: true
SectionType: GeneralTopic
---

go-go-wm is a presentation-based tiling window manager for X11: every
interesting piece of data on screen — colors, files, commit hashes,
tiles, workspaces — is a typed object the whole desktop can click,
accept, and attach actions to. One binary contains the WM, the message
broker, the terminal tools, and a JavaScript runtime for scripting it.

## Build

    go build -o ~/.local/bin/go-go-wm ./cmd/go-go-wm

## First session (nested, safe)

Run it inside a nested X server so your real desktop is untouched:

    Xephyr :5 -screen 1600x900 &
    go-go-wm wm --display :5 --embedded-broker &
    DISPLAY=:5 kitty &          # feed it windows
    DISPLAY=:5 xterm &

Click inside the Xephyr window so it has your keyboard. If your real
WM eats Mod4, start Xephyr with `-no-host-grab` and press Ctrl+Shift
inside it to toggle the grab.

Headless (for tests and automation), substitute
`Xvfb :5 -screen 0 1600x900x24 &` and drive it with xdotool.

## Default keys

- Mod4-Return — terminal (`--spawn`, default xterm)
- Mod4-d / Mod4-s — split right / below
- Mod4-w — close tile · Mod4-space — focus next
- Mod4-1..9 — switch workspace · Mod4-n — new workspace
- Escape — cancel an accept or menu · Mod4-Shift-q — quit

Mouse: drag dividers to resize (sticky at ¼ ⅓ ½ ⅔ ¾), drag the ⠿ grip
to swap or dock tiles, click titles/chips/objects to interact,
right-click any object for its verb menu.

## Themes

    go-go-wm wm --display :5 --embedded-broker --theme dark

Three themes: `paper` (default), `light` (true white), `dark`. Switch
live from a script (`wm.theme("dark")`) or the control socket
(`{"q":"set-theme","theme":"light"}`).

## Try the presentation model (the point of it all)

In a terminal *outside* the session:

    go-go-wm accept --ptype color

The desktop enters accepting mode; click any color anywhere — a chip
in a demo app, a swatch a script printed — and the command prints the
chosen object. That round trip is the core interaction; everything
else builds on it.

## First script

    go-go-wm run --once /dev/stdin <<'EOF'
    const wm = require("wm"), pbui = require("pbui");
    wm.split(wm.focused(), "row", { app: "builtin:trace" });
    pbui.print("hello from a script:", pbui.object("color", "#b0563f"));
    EOF

The color it printed is live — click it.

## Your config is a JavaScript file

    go-go-wm wm --display :5 --embedded-broker \
      --theme dark --no-default-binds --rc examples/scripts/i3.js

`examples/scripts/i3.js` is a full i3-style configuration (numbered
workspaces, launcher keys, directional focus, window assignment
rules). Copy it and edit; `glaze help js-api-reference` documents
everything it uses.

## Where to next

- `glaze help user-guide` — daily use: workspaces, accepts, verbs,
  scripts as daemons.
- `glaze help js-api-reference` — the complete scripting API.
- `glaze help developer-guide` — architecture and contributing.
