---
Title: Investigation diary
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
    - Path: repo://ttmp/2026/07/18/GGWM-001-PBUI-WM--pbui-window-manager-in-go-split-tree-wm-presentation-broker-kitty-integration/design-doc/01-pbui-wm-design-and-implementation-guide.md
      Note: primary deliverable authored in entry 1
    - Path: repo://ttmp/2026/07/18/GGWM-001-PBUI-WM--pbui-window-manager-in-go-split-tree-wm-presentation-broker-kitty-integration/reference/01-preliminary-research-x11-presentations-kitty-testing.md
      Note: research doc authored in entry 1
    - Path: repo://ttmp/2026/07/18/GGWM-001-PBUI-WM--pbui-window-manager-in-go-split-tree-wm-presentation-broker-kitty-integration/sources/pbui-shell.jsx
      Note: prototype imported and analyzed in entry 1
ExternalSources: []
Summary: Chronological diary of the GGWM-001 ticket-creation and design session — prototype import and analysis, research cleanup, design-doc authoring, screenshot capture, and reMarkable delivery.
LastUpdated: 2026-07-18T21:45:00-04:00
WhatFor: Continuation context for whoever picks up the ticket next; records what was done, in what order, and why.
WhenToUse: Read before continuing work on this ticket or before restructuring its documents.
---


# Investigation diary

## Goal

Record the initial design session for GGWM-001: importing the PBUI shell
prototype, cleaning up the preliminary research, and producing the intern
design/implementation guide for the Go window manager.

## Entry 1 — 2026-07-18: ticket creation, prototype import, design authoring

### What I set out to do

Turn a working React prototype (`pbui-shell(3).jsx` from `~/Downloads`) plus
a body of preliminary research (X11 port strategy, broker protocol, kitty
integration, testing) into a docmgr ticket with (a) the prototype archived as
a source, (b) the research cleaned up as a reference doc, (c) a full
intern-level design/implementation guide, delivered to reMarkable.

### What was done, in order

1. Located the prototype: `~/Downloads/pbui-shell(3).jsx` (868 lines; note
   the literal `(3)` in the filename — quote it in shell). Read it fully and
   built the line-number map used throughout the design doc (P component
   52-73, World 95-123, tree ops 135-169, snapping 166-169, moveSplit
   594-607, zones 612-621, accept 676-689, actionsFor 712-760, APPS
   522-531).
2. Surveyed the repo: fresh go-go-golems scaffold
   (`github.com/go-go-golems/go-go-wm`, empty `cmd/go-go-wm/main.go`,
   logcopter/lefthook/Makefile wired), workspace `go.work` links `glazed`
   and `go-go-goja` checkouts — relevant because the CLI uses glazed and
   Phase 8 targets goja.
3. Created the ticket:

   ```bash
   docmgr ticket create-ticket --ticket GGWM-001-PBUI-WM \
     --title "PBUI window manager in Go: split-tree WM + presentation broker + kitty integration" \
     --topics wm,pbui,x11,broker,kitty
   ```

4. Imported the prototype to `sources/pbui-shell.jsx`.
5. Wrote `reference/01-preliminary-research-x11-presentations-kitty-testing.md`
   — the user's research, denoised and organized into: WM proper /
   presentations-as-IPC / Go stack / kitty / module split / testing, with an
   external-links section added (wingo, xgb, xgbutil, bspwm, EWMH/ICCCM,
   kitty kittens docs, OSC 8, CLIM spec).
6. Wrote `design-doc/01-pbui-wm-design-and-implementation-guide.md` — the
   primary deliverable. Structure: prototype walkthrough (Part I) →
   architecture and repo layout (Part II) → subsystem designs incl. the wire
   protocol (Part III) → six decision records (Part IV) → testing (Part V) →
   phases 0-8 (Part VI) → risks/open questions (Part VII).
7. Mid-session the user supplied two screenshots of the running prototype as
   the look reference. Matched them by pixel dimensions against
   `~/Pictures/Screenshots/` (1426×772 → `2026-07-18_19-47.png`, 947×730 →
   `2026-07-18_19-48.png`), verified visually, imported as
   `sources/screenshot-01-full-shell-object-menu.png` and
   `sources/screenshot-02-accept-mode-drag-swap.png`, and referenced them
   from both docs (they are the target for `pkg/draw` golden files).
8. Bookkeeping: vocabulary slugs for the ticket topics, file relations,
   changelog, tasks; `docmgr doctor` clean; bundle upload to reMarkable
   under `/ai/2026/07/18/GGWM-001-PBUI-WM` (see changelog for the listing).

### Key decisions made while writing (full records in the design doc, Part IV)

- D1 from-scratch Go WM on xgb/xgbutil (not bspwm config, not StumpWM).
- D2 broker is display-agnostic; even embedded, the WM talks to it over the
  socket — no privileged side channel.
- D3 one binary, glazed commands; kitten + config snippets go:embed-ded.
- D4 NDJSON now, Codec seam for CBOR later.
- D5 ops-as-data + event bus + verb registry from day one — this is the
  goja-readiness pattern; JS later needs only `apply/on/registerVerb/accept`.
- D6 flat string ptypes, no type lattice.

### What worked

- Reading the prototype *before* writing anything: the design doc could then
  anchor every ported behavior to a line range, which keeps the port honest
  and reviewable.
- Treating the prototype as the future test oracle (random op scripts run
  through both the JS and Go trees) fell out of that reading for free.
- Matching the user's pasted screenshots to on-disk files by exact pixel
  dimensions — the two newest screenshots were red herrings (unrelated
  desktop captures); dimensions disambiguated instantly.

### What didn't work

- `ls ~/Download/pbui-shell*(N)` — the file lives in `~/Downloads` (plural),
  and the `(3)` suffix needs quoting.
- zsh ate `===` separators in compound Bash commands (`==: not found`);
  plain `---` separators are safe.
- `docmgr doc add` templates must be Read before Write in this harness;
  first Write attempt was rejected.

### What was tricky to build

- The wire protocol section: getting the accept flow right required
  reconciling the prototype's in-process semantics (one global `accepting`,
  overwrite on re-enter, Escape resolves null) with a multi-client world —
  hence session ids, stale-answer rejection, and requester-disconnect
  cancellation rules in §III.1.
- Keeping the glazed skeleton accurate to the current API (v1.0.5+ paths:
  `cmds/fields`, `cmds/values`, `cmds/schema`; `settings.NewGlazedSchema`) —
  cross-checked against the glazed-command-authoring skill notes.

### Code review instructions

There is no code yet; review the documents:

- `design-doc/01-pbui-wm-design-and-implementation-guide.md` — check the
  line references against `sources/pbui-shell.jsx`, the protocol tables for
  holes (esp. accept cancellation paths), and the phase ordering.
- `reference/01-preliminary-research-x11-presentations-kitty-testing.md` —
  confirm nothing from the original research was lost that you still want.
- Validate with `docmgr doctor --ticket GGWM-001-PBUI-WM`.

### Next steps

Phase 0 of the plan: wire `cmd/go-go-wm/main.go` glaze-style, add xgb /
xgbutil / glazed deps, then Phase 1 (`pkg/wmcore`) — pure tree + Ops +
property tests, no X required.

## Entry 2 — 2026-07-18 (evening): "build it" — Phases 0–6 plus the app layer, in one session

### What I set out to do

Implement the design: CLI scaffold, wmcore, pbui+broker+client, draw,
wmx11, WM⇄broker integration, terminal kit — then (added mid-session by
Manuel) the full demo app suite with trace/listener/inspector embedded in
the WM, file browser / todo / markdown viewer, and visible divider states
with snapping.

### What was done, in order

1. **Deps**: `jezek/xgb` + `jezek/xgbutil` (NOT BurntSushi/xgbutil — it
   imports BurntSushi/xgb and the types don't mix), glazed v1.3.8 (local
   workspace checkout), `golang.org/x/image`. Vendored IBM Plex Mono
   Regular/Bold TTFs (OFL) into `pkg/draw/fonts/` via curl from IBM/plex.
2. **pkg/wmcore** (~700 lines): tagged-struct Node tree, all prototype ops
   as pure functions, serializable `Op` + `Apply` (goja-readiness D5),
   explicit `Layout` (flexbox replacement; leaf rects + divider rects tile
   the screen exactly), `Snap` (¼ ⅓ ½ ⅔ ¾, 0.022 band), `ZoneAt`.
   Property tests: 20 seeds × 500 random ops with full-tree validation and
   leaf-count invariants per op class. All green.
3. **pkg/pbui + broker + client**: one NDJSON `Msg` frame type behind a
   `Codec` seam; broker as single-goroutine state loop with per-conn
   reader/writer goroutines; accept sessions with supersede/cancel/
   disconnect semantics; verb registry; event bus; `menu.request` falls
   back to a verb list when no WM is connected. 8 session tests under
   `-race`, `FuzzDecodeFrame` 720k execs clean.
4. **CLI**: glaze-style main.go; bare commands broker/wm/accept/answer/
   present/menu/scrape/demo/kitty-install; glazed query commands
   events/verbs/tree/windows. Verified the full accept→answer round trip
   through the CLI alone (no X).
5. **pkg/draw**: palette, embedded Plex faces (deterministic hinting),
   widgets (TitleStrip/Banner/StatusLine/TopBar/Menu/DropPreview), golden
   PNGs. The menu golden is a near-pixel match of the prototype
   screenshot.
6. **pkg/wmx11** (~1500 lines): reparenting WM on the MainPing loop
   pattern, frames with WM-drawn strips, `Layout`-driven ConfigureWindow,
   EWMH (wmctrl interoperates), keybindings, divider + ⠿ grip drags with
   stippled drop previews, bspc-style NDJSON IPC (`{"q":"tree"|"windows"|
   "op"}`), broker integration (banner, Escape-cancel, menu.show rendering,
   tile/workspace verbs incl. the accept-composing "Swap app with…").
7. **Verified in Xvfb :77** with xdotool: two xterms auto-tiled; set-ratio
   via IPC; accept `<tile>` answered by clicking a title (banner + ACCEPT
   MODE + highlight all correct); tile menu rendered and a row click split
   the tile via verb.invoke→verb.run; workspace add updated
   `_NET_CURRENT_DESKTOP`; divider drag snapped 0.507→½; grip drag showed
   the red stipple + "= swap apps" chip and swapped apps on drop.
8. **App layer** (added scope): `pkg/apps` Region/Resolve framework with
   golden-tested pure renderers; WM-embedded launcher/about/trace/
   listener/inspector over a World fed by the event bus; `xapp` client
   shell (X window + broker bridge + Keyer keyboard support);
   six demo apps. Live-verified the flagship flows: markdown **Open…
   accepted a `<file>` answered by clicking a file-browser row** (README
   rendered); Pick color… answered by a color-lab swatch (live chip in the
   listener); todo task typed on a real keyboard. Three processes, one
   accept protocol.
9. **Dividers** (added scope): real windows with idle/hot/drag/snap states,
   resize cursors, mustard snap flash — verified ⅓ snap live.
10. **Hygiene**: `go vet` clean, `make lint` (golangci-lint) clean after
    fixing 9 findings, `GOWORK=off go mod tidy` so CI builds without the
    workspace, 6 test packages green, goldens committed, 12 screenshots
    saved to `various/build-screenshots/` for the blog post.

### Commands that matter (dev loop)

```bash
Xvfb :77 -screen 0 1280x800x24 &                    # or Xephyr for hands-on
go-go-wm wm --display :77 --embedded-broker \
  --socket /tmp/pbui-x.sock --ipc-socket /tmp/ggwm-x.sock &
DISPLAY=:77 xterm &
go-go-wm demo --app files --display :77 --socket /tmp/pbui-x.sock &
go-go-wm query tree --wm-socket /tmp/ggwm-x.sock --output json
echo '{"q":"op","op":{"op":"set-ratio","node":"n3","ratio":0.333}}' \
  | socat - UNIX-CONNECT:/tmp/ggwm-x.sock
go test ./... && make lint
go test ./pkg/draw -update    # regenerate goldens after look changes
```

### What worked

- The layering paid off exactly as designed: broker, tree, and renderers
  were all fully tested before X entered the picture, and the X shell
  worked essentially on first contact (wmx11 compiled first try and
  managed xterms on the first Xvfb run).
- MainPing + posted-closure loops (wingo's pattern) for both the WM and
  xapp shells — no X races at all under real interaction.
- The prototype's line-anchored port map from entry 1: every behavior had
  a reference implementation to diff against.
- `identify`-by-dimensions and IPC-based assertions ("ask the WM what it
  believes") — never had to debug from screenshots alone.

### What didn't work

- `pkill -f "Xvfb :77"` killed the *shell itself* (the pattern matched the
  shell's own eval string) — exit 144, twice. Fix: bracket trick
  (`pkill -f "[X]vfb :77"`).
- Unix socket paths in the session scratchpad exceeded the 107-byte
  `sun_path` limit → `bind: invalid argument`. Fix: short `/tmp` paths.
- Background processes died when the Bash tool call exited; `setsid nohup
  … </dev/null &` keeps the sandbox desktop alive across commands.
- `golangci-lint` under `GOWORK=off` needed `go mod tidy` + a prewarmed
  build cache (export-data errors otherwise); also zsh word-splitting bit
  once (`xdotool mousemove $p` with `p="300 200"`).
- First lint run: 9 findings (gofmt, errcheck, exhaustive, predeclared
  `max`, nonamedreturns) — all mechanical.

### What was tricky to build

- **Accept semantics across processes**: supersede-on-new-accept had to
  resolve the first requester with nil (prototype overwrites `accepting`);
  requester-disconnect must clear the session but answerer-disconnect must
  not; stale answers rejected by session id. The broker test matrix
  encodes all of this.
- **Builtin tiles vs client placement**: launcher leaves (`App == ""`) are
  reusable by new clients (evict the launcher frame), builtin app tiles
  are not; a closed client's lone leaf reverts to a launcher via
  `syncBuiltins` reconciliation rather than imperative bookkeeping.
- **The todo "mystery split"** during live testing was not a bug: the
  first click hit the tile *title* (→ object menu, correct), and the next
  click landed on the menu's "Split - new tile below" row. The contract
  did exactly what it says.

### Code review instructions

- Start at `pkg/wmcore` (pure, property-tested) and `pkg/pbui/broker`
  (state machine + tests) — they carry the invariants.
- `pkg/wmx11` review focus: `manage.go` reparenting lifecycle,
  `builtin.go` reconciliation, drag paths in `input.go`/`divider.go`.
  Everything mutating goes through `wmcore.Apply` — grep for direct tree
  edits (there should be none outside wmcore).
- Run: `go test ./... && make lint`; visually:
  `go test ./pkg/draw ./pkg/apps` golden dirs, and the screenshot series
  `various/build-screenshots/01…12`.
- Live: the "Commands that matter" block above; the kitty path is
  `go-go-wm kitty install --config-dir /tmp/x` + the kitten's stdin
  self-test (`python3 pkg/cmds/kitty/pbui_accept.py color < fake-links`).

### Known gaps / next steps

1. Listener eval line (keyboard into builtin tiles), trace/listener
   scrolling.
2. Kitty E2E in an isolated instance (kitten is installed + self-tested;
   the in-kitty picker flow hasn't run against a live kitty yet).
3. `WM_TAKE_FOCUS`/`WM_DELETE_WINDOW` full ICCCM matrix; awkward clients
   (Java, GIMP); xrandr multi-output.
4. Cross-implementation oracle vs the JSX tree (needs node in the loop).
5. The E2E smoke test as a checked-in script; golden tests for
   files/todo/markdown renderers.
6. Phase 8 (goja) unblocked: `apply(op)` / `on(event)` / `registerVerb` /
   `accept` all exist as clean seams now.

## Related

- `design-doc/01-pbui-wm-design-and-implementation-guide.md`
- `reference/01-preliminary-research-x11-presentations-kitty-testing.md`
- `sources/pbui-shell.jsx`, `sources/screenshot-01-full-shell-object-menu.png`,
  `sources/screenshot-02-accept-mode-drag-swap.png`
