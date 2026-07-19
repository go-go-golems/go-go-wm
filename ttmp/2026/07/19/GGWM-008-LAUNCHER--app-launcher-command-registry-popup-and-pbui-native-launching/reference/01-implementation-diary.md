---
Title: Implementation diary
Ticket: GGWM-008-LAUNCHER
Status: active
Topics:
    - wm
    - ui
    - pbui
DocType: reference
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://pkg/apps/builtin.go
      Note: renderLauncher — the four-button placeholder being replaced
    - Path: repo://pkg/wmx11/pbui.go
      Note: showMenu — the popup-window species the launcher overlay extends
ExternalSources:
    - "freedesktop.org Desktop Entry Specification"
Summary: Step-formatted implementation diary for the launcher subsystem (registry, popup, keyboard substrate, PBUI integration) — decisions, verbatim failures, verification per phase.
WhatFor: Continuation context for anyone extending the launcher or the frame-keyboard substrate GGWM-009 inherits.
WhenToUse: Read alongside design-doc/01; deviations from the design are recorded here first.
---

# Diary

## Goal

Capture the implementation journey of GGWM-008 (launcher): the command
registry, the Mod4+d popup, the keyboard-input-for-WM-surfaces
substrate (which GGWM-009's REPL tile inherits), and the `command`
ptype integration — with every failure recorded verbatim.

## Step 1: Kickoff — design re-read and machinery survey

GGWM-007 wrapped in the previous turn; the user's "do it" starts
GGWM-008, the next ticket in the recorded dependency chain (008 L3's
keyboard substrate gates GGWM-009 R3). Before writing code: re-read
design-doc/01 and survey the five pieces of existing machinery the
design leans on, so the new code lands on the real seams.

The survey confirmed the design's map with one nuance worth writing
down: the empty-tile surface (`renderLauncher`) is not an app that
owns state — it re-renders from scratch on every paint via
`RenderBuiltin(name, w, h, world, accepting)`. A launcher tile with a
live query string therefore needs *WM-side state* (the query lives in
the WM process, like `World` does), not a new renderer contract.

### Prompt Context

**User prompt (verbatim):** "do it"

**Assistant interpretation:** Implement GGWM-008-LAUNCHER (the next
ticket), same working discipline as GGWM-007: diary, phased commits,
live verification.

**Inferred user intent:** Keep the ticket queue moving autonomously;
the launcher was explicitly designed for implementation in this
follow-up session.

**User prompt (verbatim, mid-step):** "jeep a detailed diary and backfill (use skill to read about the format)"

**Assistant interpretation:** Keep ("jeep") a detailed diary in the
`diary` skill's strict step format, and backfill what has already
happened — including converting the GGWM-007 diary written earlier
this session to that format.

**Inferred user intent:** Diaries across tickets should be uniform and
review-grade; the skill format (Prompt Context, verbatim failures,
review instructions) is the standard.

### What I did
- Re-read GGWM-008 design-doc/01 in full (registry shape, two
  surfaces, keyboard substrate, decision records L-D1..L-D4, phases
  L1-L4).
- Surveyed the build-on machinery: `pkg/apps/builtin.go`
  (`renderLauncher`, `RenderBuiltin` dispatch, `World`, Region/Btn/
  Hint helpers), `pkg/wmx11/builtin.go` (openBuiltin frame creation,
  `paintBuiltin` → regions in frame coords, `builtinClick` →
  `apps.Resolve` contract, `builtinAction` namespace), `pkg/wmx11/
  pbui.go` (`showMenu`: override-redirect window + `blit` + click
  routing — the popup species), GGWM-007's `raiseChrome` (the popup
  must be raised with menus, above floats).
- Backfilled both diaries into the strict skill format (GGWM-007's
  four steps rewritten; this file started).

### Why
- The design names exact seams (`renderLauncher`,
  `pkg/wmx11/pbui.go`, `wm.exec`, uispec); verifying they still look
  as designed is cheap insurance — GGWM-007 changed stacking and
  focus, both of which the popup and tile touch.

### What worked
- All the design's assumed seams exist and are current; GGWM-007's
  `raiseChrome` already has the menu-above-floats slot the popup needs
  to join.

### What didn't work
- N/A (survey step).

### What I learned
- Builtin tiles are stateless renderers over WM-process state
  (`World`); the launcher tile's query state must live WM-side, not in
  `pkg/apps`. This shapes L3: the WM owns a `launcherState` per… or
  rather one global (focused-tile) query — decided in the L3 step.
- `builtinClick` resolves clicks through `apps.Resolve(accepting,
  region, button)`, which already implements the accept-answer
  contract — launcher rows that carry `command` objects get accept
  behavior for free (L4's accept mode partially falls out).

### What was tricky to build
- N/A yet.

### What warrants a second pair of eyes
- N/A yet.

### What should be done in the future
- N/A

### Code review instructions
- Nothing to review yet; start with design-doc/01 Parts II-III.

### Technical details
- Planned order (same as design): L1 pure registry (pkg/launcher) →
  L2 popup + Mod4+d → L3 frame KeyPress substrate + tile v2 → L4
  command ptype/verbs/accept + wm.command/wm.launch + i3.js. Commit
  per phase; float-smoke/rc-smoke/examples-smoke as regression gates.

## Step 2: L1 — the pure registry (pkg/launcher)

The registry landed as four small files — `registry.go` (types,
sources, Match/All/Bump), `desktop.go` (XDG scan + parser),
`match.go` (subsequence scorer), `frecency.go` (bucketed store) — and
a nine-test suite that passed on the first run. No X, no broker, no
dependencies beyond the standard library.

One design choice made concrete here: the registry knows nothing about
`pkg/apps` or JS. Builtins and script commands arrive via
`SetStatic(kind, cmds)` — the WM and wmmod own those vocabularies, and
`SetStatic(KindScript, nil)` is exactly the stale-daemon cleanup the
design's risk list asked for.

### Prompt Context

**User prompt (verbatim):** (see Step 1, "do it")

**Commit (code):** 83ab0e8 — "GGWM-008: pkg/launcher — registry, .desktop parser, fuzzy scorer, frecency"

### What I did
- `Command{ID, Label, Exec, Kind, Terminal, Keywords, Doc}`, kinds
  app/builtin/script; `Registry` with `Refresh` (mtime-gated rescan),
  `All` (frecency-ordered), `Match` (scored), `Get`, `Bump`,
  `SetStatic`; options `WithDataDirs`/`WithStatePath`/`WithNow` for
  tests.
- .desktop parser: `[Desktop Entry]` group only, `Type=Application`
  gate, NoDisplay/Hidden skip, field-code stripping (%f/%u/…, `%%`
  unescape), Keywords+Categories → match terms, XDG first-dir-wins
  precedence by desktop-file id.
- Scorer: greedy subsequence, +3 boundary / +2 consecutive, leading
  penalty, short-field preference; field weights label 1.0 / keyword
  0.8 / id 0.6; final × (1 + log1p(frecency)).
- Frecency: JSON at `$XDG_STATE_HOME/go-go-wm/launcher.json`, bucket
  weights 4/2/1/0.5 (hour/day/week/older), writes debounced to 5s,
  everything best-effort.
- Tests: parsing fixtures (incl. action groups, Link type, %% escape),
  precedence, mtime refresh, scorer ordering table, keyword matching,
  frecency ordering + decay + persistence, static-source replacement.

### Why
- Purity is the testability story (L-D1); the injected clock makes
  frecency deterministic; `SetStatic` keeps layer direction clean
  (launcher imports nothing above the standard library).

### What worked
- Entire suite green on first `go test` run.

### What didn't work
- N/A this step (first clean run).

### What I learned
- `os.Chtimes` on the directory is needed in the mtime-refresh test —
  same-second writes are invisible to coarse filesystem timestamps.

### What was tricky to build
- The scorer's greedy (not optimal-alignment) subsequence is a
  deliberate simplification; the test table pins the orderings that
  matter (boundary > middle, consecutive > scattered, early > late)
  so a future rewrite has a contract.

### What warrants a second pair of eyes
- `stripFieldCodes` drops every two-char `%x` token in a known list;
  an Exec using literal `%f` as an argument to its own flag would lose
  it — acceptable per the recorded non-goal, but worth knowing.
- `Refresh` treats "any dir mtime changed" as "rescan all dirs" —
  simple and correct, but O(all entries) per change.

### What should be done in the future
- N/A (later phases wire it up).

### Code review instructions
- Read `pkg/launcher/registry.go` top comment, then `match.go`.
  Validate: `go test ./pkg/launcher/ -v`.

### Technical details
- Frecency score = count × recency-bucket weight; Match multiplies
  `(1 + log1p(frecencyScore))` so heavy use can reorder near-ties but
  cannot bury a much better textual match.

## Step 3: L2 — the Mod4+d popup

The popup is the menu's species with a keyboard: an override-redirect
window that takes input focus directly while open, rendered by a new
pure `draw.LauncherPanel` (query field + accent-chip rows + selection,
golden-tested), filtering the registry on every keystroke. Launch
routing went in with it: apps through one shared `execCommand` path,
builtins through `placementLeaf` + `set-leaf-app`, scripts stubbed
until L4.

The E2E fixture (`scripts/launcher-smoke.sh`, six stages against a
fixture XDG dir with a `touch`-marker .desktop entry) caught one real
bug that would have bitten daily use: on a fresh boot nothing holds
`w.focused`, so the first builtin launch did nothing.

### Prompt Context

**User prompt (verbatim):** (see Step 1, "do it")

**Commit (code):** c0d0e02 — "GGWM-008: launcher popup — Mod4+d overlay over the registry, launch routing"

### What I did
- `pkg/draw/launcher.go`: `LauncherPanel`/`LauncherRow` with
  `Render`, `RowRects`, `RowAt`, `MaxRows`; Field-surface query box
  with block caret and prompt placeholder; `Compact` mode reserved for
  the L3 tile. Goldens: `launcher-panel`, `launcher-panel-empty`.
- `pkg/wmx11/launcher.go`: registry setup (`setupLauncher` registers
  the four builtins, first .desktop scan off-loop), popup lifecycle
  (`toggleLauncher`/`openLauncher`/`closeLauncher` + restoreInputFocus),
  key handling via `keybind.LookupString` (Escape/Return/Up/Down/
  BackSpace/space/printables; Mod4/Alt/Ctrl chords ignored), launch
  routing (`launchCommand`/`launchBuiltin`/`execCommand`), and the
  `{"q":"launcher"}` debug view.
- Wiring: Mod4+d → toggle (split-right moved to Mod4+Shift+d — the
  design assigns Mod4+d to the launcher, matching the user's i3
  muscle memory); Escape handled in `cancelAccept` (it is a global
  grab, the popup never sees it); outside clicks (root + top bar)
  close the popup; theme swap repaints it; `spawnTerminal` now rides
  `execCommand`.
- Boot focus: `Run` calls `refocusCurrent` after the first relayout.

### Why
- Popup-first (before the tile) because its keyboard is the easy case:
  input focus goes to the popup window itself; the frame-tile KeyPress
  substrate is L3's job.
- One `execCommand` path (design: the launcher composes command
  lines, it does not spawn in new ways).

### What worked
- Stages 1-5 of the smoke passed on the first run: open-with-rows,
  typed filtering (input focus works), marker launch on Enter, Mod4+d
  toggle (with the standing first-keypress retry), Escape close.

### What didn't work
- Stage 6: `FAIL: builtin:trace never appeared in the tree`. The
  popup closed and `launcherActivate` ran, but `launchBuiltin`
  returned early — on a fresh boot `w.focused == ""` (nothing ever
  called focus()). Two fixes: `launchBuiltin` now routes through
  `placementLeaf()` (reuse the empty leaf, else split — the same rule
  clients use, and better than the design's focused-leaf special
  case), and `Run` lands boot focus via `refocusCurrent` so keyboard
  nav works before the first click. Re-run: all six green.

### What I learned
- The boot state had no focused leaf for the entire project's life —
  invisible until a keyboard-only flow (launch-into-tile) needed it.

### What was tricky to build
- Escape ordering: Escape is grabbed on the root (accept/menu cancel),
  so the focused popup never receives it — the close must live in
  `cancelAccept`, ordered before the menu branch. Symptom if missed:
  Escape closes an accept session underneath while the popup stays.
- Keyboard decode: `keybind.LookupString` returns keysym names
  ("space", "BackSpace", dead keys as multi-char names); the printable
  filter is `len==1 && 0x20..0x7e` plus an explicit "space" case —
  non-ASCII input is out of scope and recorded here.

### What warrants a second pair of eyes
- `restoreInputFocus` duplicates focus() logic without the repaint
  side effects — if the focus model changes again, this is the site
  that drifts.
- The popup ignores ButtonRelease/motion; a press-drag-release across
  a row activates on press only (fine, but different from menus).

### What should be done in the future
- L3: the tile variant over the same panel (Compact mode) + the frame
  KeyPress substrate; L4: script commands, command ptype, wm.command/
  wm.launch/wm.launcher exports, i3.js `d` binding.

### Code review instructions
- `pkg/wmx11/launcher.go` top to bottom (300 lines), then
  `draw.LauncherPanel`. Validate: `scripts/launcher-smoke.sh`, and
  `go test ./pkg/draw/` for the goldens.

### Technical details
- Debug query: `{"q":"launcher"}` → `{open, query, selected,
  rows: [command ids]}` — the E2E assertion surface.
- Fixture trick: `Exec=touch $MARKER` in a temp XDG dir +
  `HOME`/`XDG_DATA_DIRS`/`XDG_STATE_HOME` overrides isolate the
  registry (no real ~/.local/share pollution, no frecency bleed).

## Step 4: L3 — the frame keyboard substrate and launcher tile v2

The substrate GGWM-009 inherits: builtin frames select KeyPress, and
`handleFrameKey` routes typed input to the focused WM-rendered
surface. With it, the empty-tile placeholder became a real launcher —
the compact panel over the same registry, with per-leaf query state
living WM-side (the Step 1 survey called this: `pkg/apps` renderers
are stateless).

A satisfying find: `scriptTile.key` and uimod's `dispatchKey` had
existed since GGWM-003 with *nothing ever delivering keys* — the JS
half of the seam was waiting for exactly this substrate. Script tiles
with `onKey` now work with zero uimod changes.

### Prompt Context

**User prompt (verbatim):** (see Step 1, "do it")

**Commit (code):** 1f2dadf — "GGWM-008: frame keyboard substrate + launcher tile v2"

### What I did
- `openBuiltin` frame windows add `EventMaskKeyPress`;
  `connectFrameEvents` gains a KeyPress dispatch to `handleFrameKey`.
- `handleFrameKey`: client frames and WM-modifier chords return
  immediately; keysym via `keybind.LookupString`; script tiles →
  `tile.key(s)` (a post to the JS loop), launcher tiles →
  `launcherTileKey`.
- Launcher tile: `launcherTiles map[leaf]*launcherTile{query, sel}`
  (pruned in syncBuiltins' destroy branch; leaf ids never recycle),
  `renderLauncherTile` (Compact panel + hint line + `launchcmd:<id>`
  click regions), `launchIntoTile` (builtin → set-leaf-app this leaf;
  app → exec, the client lands here via placementLeaf; script → L4
  stub), Escape-clears-query in `cancelAccept`'s idle branch.
- `paintBuiltin` branches launcher tiles to the WM-side renderer;
  `builtinAction` handles `launchcmd:`; `{"q":"launcher-tile"}` debug
  view (focused tile only).
- launcher-smoke stages 7–9: fresh workspace focuses its tile and
  typing filters it; Mod4+d still opens the popup with a tile focused
  and leaks no characters; Enter launches builtin:trace into that
  exact leaf and drops the tile state.

### Why
- The design rule "typed input goes to the focused surface; chords
  with the WM modifier never do" is enforced in one place
  (handleFrameKey's mods check) — X grab semantics already keep bound
  chords away; the check covers unbound ones.

### What worked
- All three new E2E stages green on the first run — the substrate
  worked immediately because focus() already lands on frame windows
  for client-less tiles (the GGWM-004 SetInputFocus(None) fix built
  the foundation).

### What didn't work
- Nothing failed in this step. (The Step 3 boot-focus fix is what
  made "fresh workspace focuses its tile" hold; without it stage 7
  would have failed the same way stage 6 did.)

### What I learned
- The `scriptTile.key` closure was dead code since GGWM-003 — a
  designed seam that nothing exercised. The substrate completed it
  without touching uimod: evidence the original layering was right.

### What was tricky to build
- Escape has three meanings now (close popup > close menu > cancel
  accept > clear tile query) and is a global grab — the priority chain
  lives entirely in `cancelAccept`, and the tile-clear branch must be
  the *idle* fallback or it would eat accept cancellation.
- Tile state lifetime: keyed by leaf id, cleared on launch, pruned
  with the frame in syncBuiltins — three sites, any missed one is a
  slow leak or a ghost query on a reused empty tile.

### What warrants a second pair of eyes
- `launcherTileRows` clamps `sel` as a side effect of rendering
  (Down past the end relies on the next render clamping); ugly but
  contained.
- The printable filter is ASCII-only (0x20–0x7e), same as the popup —
  recorded limitation, not an accident.

### What should be done in the future
- L4: script command dispatch (the `dispatchScriptCommand` stub),
  command ptype + verbs + accept mode, wm.command/wm.launch/
  wm.launcher exports, i3.js `d` binding, help topics.

### Code review instructions
- `handleFrameKey` + `launcherTileKey` + `launchIntoTile` in
  `pkg/wmx11/launcher.go`; the `paintBuiltin` branch in builtin.go.
  Validate: `scripts/launcher-smoke.sh` (9 stages).

### Technical details
- Debug: `{"q":"launcher-tile"}` → `{leaf, query, selected, rows}`
  for the focused launcher tile ({} when none focused) — stage 7-9's
  assertion surface.

## Step 5: L4 — the command ptype, script commands, and the JS surface

Launching became a typed activity: every launcher row is now a
`command` presentation (right-click → verb menu; a pending
`accept("command")` turns Enter/click into an answer), scripts can
serve registry entries whose callbacks run on their own runtime, and
the wm module gained `launch`/`launcher`/`command`.

### Prompt Context

**User prompt (verbatim):** (see Step 1, "do it")

**Commit (code):** 1b44015 — "GGWM-008: command ptype, script commands, wm.command/launch/launcher, accept mode"

### What I did
- `commandObject` + `maybeAnswerCommand` (the accept check every
  surface calls before launching); popup right-click → RequestMenu;
  tile regions carry the command Object so `apps.Resolve` gives
  answer/menu behavior for free (the Step 1 prediction held).
- WM verbs `command.launch` and `command.edit` (opens `Command.Src` —
  a new registry field — in `${EDITOR:-vi}` inside the terminal);
  `runVerb` routes `command.*` to `runCommandVerb`.
- Script commands: `ScriptBackend.RegisterCommand(id, label, doc,
  fire)` (WM-side map + `SetStatic(KindScript, …)` update;
  re-registration replaces); `dispatchScriptCommand` fires the stored
  post — the L3 stub closed.
- `launchTarget`: registry id → kind router, else raw exec fallback;
  IPC `launch` + `commands` queries.
- wmmod: Backend += `Launch`/`OpenLauncher`/`RegisterCommand`
  (+`ErrNoScriptCommands` for IPC backends); exports `wm.launch`,
  `wm.launcher`, `wm.command` (jsBind-pattern fire wrapping); i3.js
  binds Mod4+d → `wm.launcher()` (its i3 line was a rofi-style
  launcher script).
- Tests: launch kind routing + open counting; command registration
  payload and *firing the stored callback back into the runtime*
  (asserted via a JS-side counter); validation errors.
- E2E stages 10–12: launch by id re-runs the marker app and raw
  command lines exec; `query verbs --ptype command` lists
  command.launch; `accept --ptype command` + popup Enter answers with
  `app:marker` and — asserted — does *not* launch it.

### Why
- `maybeAnswerCommand` centralizes accept-mode so the popup and the
  tile cannot drift; the no-launch assertion in stage 12 pins the
  semantic ("answer instead of run") the design called the point of
  the ptype.

### What worked
- The tile's accept behavior needed zero new code — regions with
  Objects already answer through `apps.Resolve`.
- The fire-into-runtime test worked immediately with
  `PostWithLifetimeContext` (same seam as wm.bind).

### What didn't work
- Stage 12 first run: `unknown flag: --output` — `go-go-wm accept`
  has no `--output json`; the smoke asserted on its default output
  instead. (Test-script bug, not code.)

### What I learned
- `accept` CLI prints the chosen object's wire form on stdout by
  default — good enough for E2E greps; no JSON flag exists.

### What was tricky to build
- Deviations from the design, recorded: `command.launch-here` was
  dropped — with launchBuiltin/launchCommand routing through
  `placementLeaf`, plain `command.launch` already lands builtins in
  the empty tile, so the second verb would be indistinguishable.
  A2 (broker-daemon) script commands are not implemented: wm.command
  is in-process-only like wm.bind (the design's verb-style broker
  dispatch is real work and no current user needs it).
- Escape-ordering again: `launcher.open({accept: true})` from the
  design became implicit — the popup answers whenever an accept for
  "command" is pending, no separate mode flag.

### What warrants a second pair of eyes
- `command.edit` builds a quoted shell string by concatenation; a
  .desktop path containing a single quote would break the quoting
  (paths under XDG dirs realistically never do, but it is string-
  built shell).
- `RegisterCommand` keeps a def slice + map in the WM; unregistration
  only happens via replacement — an rc.js reload that drops a command
  leaves a stale entry until restart (recorded, matches verb
  semantics).

### What should be done in the future
- Docs (help topics: wm-module launcher section, js-api-reference,
  getting-started key change Mod4+d, user-guide) + ticket bookkeeping
  + push — the wrap step.
- A2 broker-routed wm.command, if a standalone daemon ever needs it.

### Code review instructions
- `maybeAnswerCommand` call sites (launcherActivate, launcherTileKey),
  `RegisterCommand`/`dispatchScriptCommand`, `jsCommand` in
  wmmod/module.go. Validate: `scripts/launcher-smoke.sh` (12 stages),
  `go test ./pkg/jsmod/wmmod/ -run 'Launch|Command'`.

### Technical details
- Wire additions: `{"q":"launch","target":…}` → kind string;
  `{"q":"commands"}` → registry listing. Events: `command.launched
  {id, label, kind, leaf?}`.
