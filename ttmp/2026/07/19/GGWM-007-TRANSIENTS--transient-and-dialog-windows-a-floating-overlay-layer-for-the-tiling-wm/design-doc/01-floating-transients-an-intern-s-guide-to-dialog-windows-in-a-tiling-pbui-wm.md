---
Title: Floating transients — an intern's guide to dialog windows in a tiling PBUI WM
Ticket: GGWM-007-TRANSIENTS
Status: active
Topics:
    - wm
    - ui
DocType: design-doc
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://pkg/wmx11/manage.go
      Note: manage() — the front door this design adds a second exit to
    - Path: repo://pkg/wmx11/pbui.go
      Note: the menu popup machinery whose stacking/lifecycle pattern floats reuse
    - Path: repo://pkg/wmx11/wm.go
      Note: WM state; floats become a sibling of frames
    - Path: repo://pkg/jsmod/wmmod/rules.go
      Note: the rule engine that gains float overrides
ExternalSources:
    - "ICCCM §4.1.2.6 WM_TRANSIENT_FOR; EWMH _NET_WM_WINDOW_TYPE (freedesktop wm-spec)"
Summary: Design for the floating overlay layer — how a strictly tiling WM learns to handle dialogs, utility windows, and splash screens without touching the wmcore tree; covers X11 transient/window-type detection, the float lifecycle as a sibling of frames, stacking and focus routing, size-hint honoring, the scripting surface (rules, events, wm.float toggle), and a phased implementation plan with a purpose-built test client.
LastUpdated: 2026-07-19T15:10:00-04:00
WhatFor: The governing design for GGWM-007; read before writing any float code.
WhenToUse: With the GGWM-004 intern guide (window lifecycle chapter) — this extends the manage() pipeline it describes.
---

# Floating transients

This guide designs the first deliberate exception to the WM's central
rule. Since GGWM-001, every managed window has been a leaf in a binary
split tree: `manage()` picks a leaf, reparents the client into a frame,
and geometry flows from `wmcore.Layout`. That rule is why the WM is
testable and replayable — and it is exactly wrong for a class of
windows that real applications produce constantly: dialogs, utility
palettes, splash screens, file pickers. Tiling a "Save changes?" prompt
into a half-screen pane is not a layout; it is a bug with good
intentions. The user's i3 configuration floats fourteen window classes;
none of them can be used comfortably today.

The design question is therefore not "how do we float windows" but
"how do we float windows **without breaking the property that makes the
tree valuable**." The answer this document develops: floats never enter
the tree at all. They are shell-side state, like the WM's own menus and
bars — windows the desktop model does not know exist.

## Part I — What the system does today (the parts you must know)

Three existing mechanisms bound the design space.

**The front door.** Every new window arrives at `handleMapRequest`
(`pkg/wmx11/manage.go:38`). Today it has two exits: override-redirect
windows (menus, tooltips — windows that asked not to be managed) are
mapped untouched, and everything else goes to `manage()`, which
allocates a leaf, creates a frame, reparents, connects the per-client
Destroy/Unmap handlers (the GGWM-004 zombie-frame lesson), installs the
click-to-focus grab, and emits `window.managed`. The float layer is a
third exit from this same door.

**The WM's own popups.** The verb menu (`pkg/wmx11/pbui.go:175`) and
the bars are override-redirect windows the WM creates, paints with
`pkg/draw`, and stacks above the tiled world. They already demonstrate
every mechanism a float needs — creation, painting via the cached
X-image path, click regions, stacking — except that their content is
WM-rendered rather than a reparented client.

**The tree's purity contract.** `wmcore` is a pure data model; every
mutation is an op; a recorded op stream replays to an identical tree
(`TestReplayReproducesTree`). Anything we add to the tree needs an op,
an event, serialization, and replay semantics. Floats would poison all
of that for no benefit: a dialog's position is not layout state anyone
wants to replay.

## Part II — How X11 marks a window as "not a main window"

There is no single "floating" flag in X11; there are three overlapping
signals, and a correct WM reads all of them.

1. **`WM_TRANSIENT_FOR`** (ICCCM). A window property naming another
   window — the "leader" — that this window is subordinate to. File
   dialogs, confirmation prompts, and preference sheets set it. Read
   with `icccm.WmTransientForGet(X, win)`. This is the strongest
   signal: a transient window is *for* another window and should
   appear above and centered on it.
2. **`_NET_WM_WINDOW_TYPE`** (EWMH). A list of type atoms; the ones
   that mean "float me" are `DIALOG`, `UTILITY`, `SPLASH`, and
   `TOOLBAR`. (`MENU`, `DROPDOWN_MENU`, `TOOLTIP`, `NOTIFICATION`
   are usually also override-redirect and never reach us; handle them
   as floats if they do.) Read with `ewmh.WmWindowTypeGet`.
   `NORMAL` or absent means tile.
3. **Size hints** (`WM_NORMAL_HINTS`). A window whose minimum and
   maximum sizes are equal has declared itself fixed-size; tiling it
   is meaningless (the client area cannot fill the pane). Treat
   `min == max` as a float signal. Read with `icccm.WmNormalHintsGet`.

The user's config adds a fourth, human signal: **rules**. i3's
`for_window [class="Galculator"] floating enable` becomes a `float`
field on the existing rule engine (`wm.rule({class: /Galculator/,
float: true})`). Rules override detection in both directions
(`float: false` forces tiling), because heuristics are wrong somewhere
and the user is the tiebreaker.

Decision function, in order of precedence:

```
shouldFloat(win):
    if a matching rule sets float        → that value
    if WM_TRANSIENT_FOR is set           → true
    if window type ∈ {DIALOG, UTILITY,
                      SPLASH, TOOLBAR}   → true
    if sizeHints.min == sizeHints.max
       and both are set                  → true
    else                                 → false
```

Detection runs once, in `manage()`, before leaf allocation. A
`PropertyNotify` for `WM_TRANSIENT_FOR` after mapping (rare, but GTK
does it) re-runs the decision and migrates tile→float; the reverse
migration only happens via the explicit toggle.

## Part III — The float layer design

### F-D1 — Floats are shell state, not tree state

- **Context.** A float needs position, size, stacking, and a workspace
  association. The tree offers none of these shapes.
- **Options.** (a) A `float` node kind in wmcore with ops; (b) a
  parallel map in the WM shell, invisible to wmcore.
- **Decision.** (b). `wmcore` stays a pure tiling model; the replay
  property, the op vocabulary, and every existing test are untouched.
  Floats live in `WM.floats map[xproto.Window]*floatWin`, a sibling of
  `frames`, with the same lifecycle discipline (per-client event
  handlers, teardown lists, buffer drops).
- **Consequences.** Floats do not appear in `wm.tree()`; they appear
  in `wm.windows()` (with `"floating": true`) and in events. Scripts
  that walk the tree see only tiles — which is correct, because tree
  walks feed layout logic.
- **Status.** proposed.

### The float record

```go
type floatWin struct {
    client   xproto.Window
    win      *xwindow.Window  // the frame (strip + border, reparented)
    title    string
    class    string           // WM_CLASS, for rules and wm.windows()
    instance string
    leader   xproto.Window    // WM_TRANSIENT_FOR target, 0 if none
    ws       string           // workspace it belongs to
    rect     wmcore.Rect      // current geometry (screen coords)
    hints    icccm.NormalHints
    img      *image.RGBA      // cached paint buffers — same discipline
    ximg     *xgraphics.Image //   as frame (GGWM-005), shm-eligible
    surf     *xshm.Surface    //   (GGWM-006)
}
```

Floats are framed like tiles — the same title strip, the same border,
painted by the same buffer-cached pipeline — because visual consistency
is a feature and the machinery is already paid for. The strip differs
in its buttons: a float strip has drag-grip and close only (no
split-right/split-down; splitting a dialog is meaningless). This is a
`draw.TitleStrip` variant, not a new renderer.

### Placement, size, and the hints contract

- Initial size: the client's requested geometry, clamped to the
  workspace area and to `hints.Min/Max` when set.
- Initial position: centered on the leader's frame if
  `WM_TRANSIENT_FOR` resolves to a managed window; else centered on
  the work area. No smart cascade; no position persistence (out of
  scope until proven wanted).
- `ConfigureRequest` from a float client is **honored** (move/resize
  as asked, re-clamped) — the exact opposite of tiles, where the tree
  owns geometry and requests are answered with a synthetic
  ConfigureNotify. This split goes in `handleConfigureRequest`, which
  currently assumes every managed window is a tile.

### Stacking

One global order, maintained with `ConfigureWindow(StackMode)`:

```
bars and menus         (top; existing override-redirect windows)
focused float
other floats           (most recently focused first)
tiled frames           (bottom; internal order irrelevant — no overlap)
```

Floats restack to the top of the float band on focus. Tiles never
restack (they cannot overlap each other). The WM's menu must stay
above floats — it is transient to the whole desktop.

### Focus routing

`w.focused` (a leaf id) has been the single focus register since
GGWM-001, and directional navigation, `wm.focused()`, and paint
highlighting all read it. Floats need focus too (you must be able to
type into a dialog) without corrupting leaf-based navigation.

- **Decision.** Add `w.focusedFloat xproto.Window` (0 = none). Exactly
  one of `focused`/`focusedFloat` is "hot": focusing a float clears
  the visual highlight of the focused tile but *preserves*
  `w.focused`, so when the float closes or is clicked away from, focus
  returns to the tile the user was on. Directional focus
  (`wm.focus("left")`) operates on tiles only and clears
  `focusedFloat` — pressing a navigation key means "back to the tiled
  world."
- Click-to-focus applies to floats with the identical
  sync-grab-and-replay mechanism from GGWM-005.
- When a float closes, focus returns to `w.focused`'s client (or the
  leader's frame if the leader is a tile — usually the same thing).

### Workspace association and lifecycle

A float belongs to the workspace that was current when it mapped
(`floatWin.ws`); `relayout` maps/unmaps floats by workspace exactly as
it does frames, and `dropBuffers` applies on hide (no megabytes for
hidden dialogs). Destroy/Unmap handlers connect to the float's client
window (the GGWM-004 dispatch lesson applies verbatim), and teardown
detaches callbacks, drops buffers, and — new — restacks focus.

The `window.managed` event gains `"floating": true` and the leader id;
a new `window.float-closed` mirrors `close_tile`. `wm.windows()` rows
gain `floating` and `leader`.

### The toggle (phase 2)

`wm.float(target?)` toggles the focused (or named) window between
worlds: tile→float detaches the leaf (close-leaf; the frame converts
to a floatWin, client untouched), float→tile runs the normal
`placementLeaf` insertion. i3 binds this to `$mod+Shift+space`; i3.js
will too. The conversion functions are where most edge cases live
(lone leaf → launcher rules, focus handoff); they are phase 2
deliberately so phase 1 ships a useful dialog layer without them.

## Part IV — Scripting surface

```js
wm.rule({ class: /Galculator/, float: true });   // force float
wm.rule({ class: /mpv/, float: false });         // force tile
wm.windows();       // [{leaf?, floating, leader, class, ...}]
wm.float();         // toggle focused window (phase 2)
wm.on("window.managed", ev => ev.data.floating && ...);
```

Rules: `normalizeRule` gains an optional `float *bool` (three-valued:
unset/force-true/force-false); the Go-side rule watcher already runs on
`window.managed` — but float rules must run **before** manage decides,
so the rule store moves its match helper into a form the WM can query
synchronously at map time. This is the one place the design touches an
existing seam: the rule engine currently lives module-side (wmmod), and
the float decision is WM-side. Decision: the WM gains a small
`FloatRule` list set over the Backend (`SetFloatRules`), pushed by
wmmod whenever rules change — the same push-down pattern as keybindings
(rc.js-only), and it degrades cleanly when no runtime is attached.

## Part V — Test strategy

The blocker for honest tests: standard tools cannot easily create a
window with `WM_TRANSIENT_FOR` set. The ticket therefore includes a
purpose-built test client (`cmd/go-go-wm testwin`, ~60 lines of
xgbutil): flags for `--transient-for <id>`, `--type dialog|utility`,
`--fixed WxH`, `--title`, `--class`. With it:

- Unit: `shouldFloat` decision table (pure function over fetched
  properties — table-driven).
- E2E (Xvfb, extending `examples-smoke.sh`): map a `--type dialog`
  testwin → assert `wm.windows()` shows `floating:true`, tree leaf
  count unchanged, geometry centered and clamped; kill the client →
  float record gone, no zombie (the GGWM-004 regression class);
  workspace switch hides/shows the float; a `float:false` rule forces
  the same window to tile.
- Focus: click float → type (xdotool) → text lands in float; press
  `Mod4-Right` → focus returns to tiles.

## Part VI — Phases

- **T1** — detection (`shouldFloat` + properties), float record,
  map/paint/place/stack, lifecycle handlers, `window.managed`
  extension, testwin client, E2E fixtures.
- **T2** — interaction: drag by strip, close button, focus routing,
  ConfigureRequest honoring, workspace association.
- **T3** — scripting: rule float overrides with WM-side push-down,
  `wm.windows()` fields, i3.js gains its fourteen `for_window` float
  rules.
- **T4** — the toggle (`wm.float`), i3.js `$mod+Shift+space`.

## Risks and open questions

- Modal dialogs (`_NET_WM_STATE_MODAL`) arguably should block input to
  their leader; deferred — X11 modality is advisory and most toolkits
  self-enforce.
- Focus-follows into floats from `focusNext` (Mod4-space): excluded in
  T1 (tiles only); revisit if it feels wrong in use.
- xterm-based utilities (copyq runs as `instance=copyq` with no
  transient hint) rely entirely on rules — which is why rule overrides
  are T3, not optional polish.
- Multi-output interacts with centering ("center of which output?");
  answered by the future multi-output ticket, not here.
