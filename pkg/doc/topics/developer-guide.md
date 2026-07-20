---
Title: go-go-wm developer guide
Slug: developer-guide
Topics:
- wm
- scripting
IsTemplate: false
IsTopLevel: true
ShowPerDefault: true
SectionType: GeneralTopic
---

The architecture, the disciplines, and the workflow for changing
go-go-wm. The long-form versions live in the repository's tickets
(`ttmp/…/design-doc/` — GGWM-004 has a full system guide, GGWM-005 the
rendering/performance guide); this topic is the working summary.

## Package map (lower layers never import higher)

| package | role |
|---|---|
| `pkg/pbui` (+`broker`,`client`) | typed objects, verbs, wire protocol, the broker |
| `pkg/wmcore` | pure layout model: tree, ops, geometry — no X, no goroutines |
| `pkg/draw` | themes + software rendering primitives (image.RGBA) |
| `pkg/xshm` | zero-copy MIT-SHM upload surfaces |
| `pkg/apps`, `apps/uispec`, `apps/xapp` | app surfaces, region contract, spec IR, window shell |
| `pkg/wmx11` | the X11 shell: frames, input, IPC, painting |
| `pkg/jsmod` (+`pbuimod`,`wmmod`,`uimod`) | the JS native modules |
| `pkg/cmds` | glazed CLI; all cross-layer wiring |
| `pkg/xgojaprovider` | modules packaged for generated binaries |

The most common review comment is "that logic belongs one layer down."

## The four disciplines

1. **Ops-as-data.** Every layout mutation is a serializable
   `wmcore.Op` through `Apply` (or `ApplyBatch`); keyboard, mouse,
   IPC, and JS share one vocabulary; every op is emitted as an event;
   a recorded stream replays to an identical tree.
2. **One goroutine owns each world.** All WM state lives on the WM
   loop; all VM access on the JS loop; every crossing is a posted
   closure; queries use bounded (2s) waits; callbacks fired on a
   foreign loop do exactly one thing: post.
3. **Paint-time reads, cached buffers.** Renderers read theme colors
   at paint time (never captured at init); per-surface buffers are
   cached and dropped on invisibility; per-pixel loops move along
   memory and never call a method per pixel; input is coalesced to
   paint cadence.
4. **Complete teardown lists.** Destroying a window means: detach its
   xevent callbacks, drop paint buffers, release server-side
   references (background pixmaps), and clear every map entry — the
   recorded bug classes (zombie frames, shm leaks) were all missing
   items on this list.

## Build, test, verify

    go build ./... && go test ./...      # pure layers: fast, no X
    gofmt -l pkg/ && go vet ./...
    scripts/rc-smoke.sh                  # rc runtime + keybinding E2E (Xvfb)
    scripts/examples-smoke.sh            # every example is a fixture

Live work happens in Xvfb/Xephyr with xdotool, asserting over the
control socket and by pixel-sampling screenshots (never by eyeball —
themes and highlights have fooled eyeballs twice). Profiling:
`GO_GO_WM_PPROF=localhost:6060 go-go-wm wm …` then
`go tool pprof http://localhost:6060/debug/pprof/profile?seconds=12`
while scripted input runs. Debug switches: `GO_GO_WM_NO_SHM=1` forces
the PutImage upload path; `--log-level debug` adds paint timings.

## Adding a WM feature end to end (the checklist)

1. Pure logic in `wmcore` with table-driven tests (deterministic
   tie-breaks — map scans need a total order).
2. WM behavior in `wmx11`, on the WM loop, reusing existing
   mechanisms.
3. An IPC query/op case in `dispatchIPC`.
4. A method on `wmmod.Backend`, implemented by `IPCBackend` (socket)
   and `ScriptBackend` (posts); update the fake backend in tests.
5. The JS export in `wmmod/module.go` + the help topic.
6. A fixture in `examples-smoke.sh` or an example script, so the
   feature is exercised forever.

Adding a theme is a struct literal in `draw.Themes`; the swap and
contrast tests cover it automatically. Adding a builtin app is a
render function in `pkg/apps` returning `(image, []Region)`.

## Known traps (each cost a debugging session)

- Reparented clients deliver Destroy/Unmap against the *client*
  window — connect handlers there, not on root.
- `SetInputFocus` on window 0 (None) silently kills all keyboard
  processing, including root grabs.
- A window's background-pixmap attribute is a server-side reference;
  reset it before freeing the pixmap or the memory lives on.
- Two goroutines calling `draw.SetTheme` tear the palette; one writer
  per process.
- The first synthetic keypress after an X server boots may be
  swallowed (keymap settling): fixtures retry, never assert one cold
  press.
- In test scripts, `pkill -f` patterns must never appear in the
  invoking compound command (self-match kills the shell); kills live
  in script files invoked by bare path.

## Where documentation lives

- `ttmp/` — docmgr tickets: design docs with decision records,
  implementation diaries (including what went wrong), intern guides.
  Start with GGWM-004 design-doc/02 (whole system) and GGWM-005
  design-doc/02 (rendering pipeline).
- `pkg/doc/topics/` — these help topics (embedded; add a file and it
  ships).
- `examples/scripts/` — every example doubles as a smoke fixture.
