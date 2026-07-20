---
Title: Example scripts cookbook
Ticket: GGWM-002-GOJA-DSL
Status: active
Topics:
    - wm
    - pbui
    - goja
    - scripting
DocType: reference
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://examples/scripts/hello.js
      Note: canonical copies live in examples/scripts; this ticket keeps show-and-tell copies in sources/scripts
    - Path: repo://scripts/examples-smoke.sh
      Note: runs every script here as a CI fixture
ExternalSources: []
Summary: The example scripts that demonstrate the go-go-wm scripting layer, from a three-line hello to a declarative project switcher — what each one shows, how to run it, and what it proves about the architecture.
LastUpdated: 2026-07-18T23:05:00-04:00
WhatFor: Show-and-tell tour of the scripting DSL; each script is also a live test fixture.
WhenToUse: Read alongside design-doc/01 §examples; run them with `go-go-wm run` against a live desktop (or let scripts/examples-smoke.sh run them for you).
---

# Example scripts cookbook

Nine scripts, ordered simple → interesting. Canonical copies:
`examples/scripts/` (repo). Copies for reading live in this ticket's
`sources/scripts/`. Every one of them runs in CI via
`scripts/examples-smoke.sh` (broker-only ones against a bare broker, the
layout ones inside Xvfb), and every one asserts its own behavior — the
demo *is* the test.

Boot a playground first:

```bash
Xephyr :1 -screen 1280x800 &
go-go-wm wm --display :1 --embedded-broker --rc examples/scripts/rc.js &
```

## 1. hello.js — print is not console.log

```bash
go-go-wm run --once examples/scripts/hello.js
```

Three lines. `pbui.print("…", pbui.object("color", "#b0563f"), "…")`
appears in every listener tile — and the color segment is a *live
presentation*: click it, right-click it, mix it. The lesson: script
output enters the same typed-object world as everything else.

## 2. palette.js — a script that asks the desktop

```bash
go-go-wm run --once examples/scripts/palette.js   # then click any color
```

`await pbui.accept("color", …)` flips the whole desktop into ACCEPTING
mode; clicking any color presentation in any process resolves the
promise. The script then prints four derived shades — as clickable color
objects. Cancellation (Escape) resolves `null`, and the script says so.

## 3. git-verbs.js — teach every window a new trick

```bash
go-go-wm run examples/scripts/git-verbs.js        # daemon
```

Registers `git.copy-short` and `git.compare-with` on the `git-commit`
ptype. From that moment every commit hash on the desktop — scraped from
a kitty terminal, printed by another script — has these in its
right-click menu, routed by the broker to this process.
`git.compare-with` is accept-composing: click it on one commit, then
click the *other* commit.

## 4. golden.js — the layout test that scripts itself

```bash
go-go-wm run --once examples/scripts/golden.js
```

Builds editor | (trace / listener) with `wm.split(…, {ratio: 0.62})`,
reads `wm.tree()` back, and *asserts its own postconditions* — non-zero
exit on failure. This is the A2 integration test in the design doc: the
script is the harness.

## 5. router.js — an event-driven window butler

```bash
go-go-wm run examples/scripts/router.js           # daemon
xterm -T "Mozilla Firefox" &                      # → workspace "web"
```

Subscribes to `window.managed` (every op is an event, because every op
is data) and calls `wm.workspace("web").adopt(leaf)`. The window's frame
survives the move because `move-leaf` keeps the leaf id.

## 6. rc.js — the in-process startup file

```bash
go-go-wm wm --display :1 --embedded-broker --rc examples/scripts/rc.js
```

Runs *inside* the WM process. `wm.bind("Mod4-e", …)` — real X
keybindings, only possible here — plus a verb and a declarative rule
(`wm.rule({title: /zoom/, workspace: "calls"})`). Everything except
`bind` is copy-paste identical to standalone scripts: develop in the
REPL, deploy in rc.js.

## 7. project-switcher.js — one command, a whole context

```bash
go-go-wm run --once examples/scripts/project-switcher.js
```

`wm.layout("dev", {split: "row", ratio: 0.62, a: {app: "editor"}, b: …})`
is normalized at definition time and inspectable via `wm.layouts()`
before anything executes. `wm.workspace("go-go-wm").switch().apply("dev")`
builds it — but only on a fresh workspace, so running it twice is a
no-op: idempotent context switching.

## The REPL

```bash
go-go-wm repl
wm> const wm = require("wm")
wm> wm.leaves()
wm> wm.split(wm.focused(), "col", {app: "builtin:trace"})
```

Same modules, live desktop, persistent bindings across lines (the REPL
kernel IIFE-rewrites cells). The API you explore here is exactly the API
of `run` scripts and rc.js.

## 8. js-colors.js — a whole app in JavaScript (GGWM-003)

```bash
go-go-wm run examples/scripts/js-colors.js            # daemon
```

`ui.app({render, actions, verbs})` — the render() returns rows of
segments; Go draws them and wires the click contract. The color chips
answer desktop accepts (highlighting included), the buttons mutate JS
state, and `color.darken` appears in every color's right-click menu,
served by this script.

## 9. rc-tile.js — a WM-painted scripted tile (GGWM-003)

```bash
go-go-wm wm --display :1 --embedded-broker --rc examples/scripts/rc-tile.js
```

`app.tile()` registers the surface with the WM itself; the WM paints it
like trace/listener and routes clicks back into the rc runtime. The
returned `"script:js-counter"` string is placed with an ordinary
`wm.split`.
