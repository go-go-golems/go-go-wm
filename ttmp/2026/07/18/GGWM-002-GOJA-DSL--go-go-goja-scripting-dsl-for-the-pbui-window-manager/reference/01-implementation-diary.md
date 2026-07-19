---
Title: Implementation diary
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
    - Path: repo://pkg/jsmod/bridge.go
      Note: wire⇄JS conversion, built and fuzzed in entry 1
    - Path: repo://pkg/jsmod/pbuimod/module.go
      Note: pbui native module, built in entry 1
    - Path: repo://pkg/cmds/run.go
      Note: go-go-wm run command, built in entry 1
    - Path: repo://examples/scripts/hello.js
      Note: P1 example scripts live in this directory
ExternalSources: []
Summary: Chronological diary of the GGWM-002 implementation — building the pbui/wm goja modules, the run/repl commands, and the rules/layouts pipeline, phase by phase, with live-verification transcripts and the bugs found along the way.
LastUpdated: 2026-07-18T22:20:00-04:00
WhatFor: Continuation context for whoever picks up the scripting layer next; records what was built, what broke, and what the tricky parts were.
WhenToUse: Read before continuing GGWM-002 work; pair with design-doc/02 (the implementation guide this diary executes).
---

# Implementation diary

## Goal

Execute the GGWM-002 plan (design-doc/02, Parts III–VIII): P1 pbuimod +
run, P2 wmmod over IPC, P3 in-process runtime + REPL, P4 rules/layouts.
Each entry records one phase: what was built, what was verified live, what
went wrong, and what a reviewer should look at.

## Entry 1 — 2026-07-18: P1 — bridge, pbui module, `run` command

### What I set out to do

The P1 slice: `pkg/jsmod` (bridge + errors), `pkg/jsmod/pbuimod` (accept
promise pattern, verbs, print, events, data helpers), `go-go-wm run
[--once]` with capability flags, the broker-only test suite, the bridge
fuzzer, and the first example scripts — everything that makes
`go-go-wm run --once palette.js` a real sentence.

### What was done, in order

1. Verified the current go-go-goja APIs against the workspace checkout
   before writing code: `engine.NewRuntimeFactoryBuilder` →
   `factory.NewRuntime()` → `Runtime{VM, Loop, Owner, Require}`;
   `runtimebridge.Lookup(vm)` returns the `RuntimeServices` the factory
   stored (that is how a native function reaches the owner loop from a
   worker); `modules/fs/fs_async.go` is the promise-pattern template.
   Module selection: explicit `WithModules` suppresses the implicit
   default-registry modules, data-only defaults ride along regardless,
   and `UseModuleMiddleware(MiddlewareOnly("exec"))` is the capability
   gate for `--allow-exec`.
2. `pkg/jsmod/bridge.go`: `ObjectToJS`/`JSToObject` (plain `{ptype,
   value, label?, doc?}` maps, JSON value decoded), `OpFromJS`
   (marshal→unmarshal into `wmcore.Op`, op-name checked early),
   `SegsToWire` (the `listener.print` convention), `EventToJS`.
   `errors.go`: `EmitScriptError` — script failures become `script.error`
   bus events, never crashes.
3. `pkg/jsmod/pbuimod/`: `module.go` (loader, data helpers, print/emit/
   hover with 2 s deadlines), `accept.go` (accept/answer/cancel/menu —
   accept and menu are promises settled only via
   `PostWithCustomContext`; cancellation resolves **null**, not a
   rejection), `verbs.go` (definition-time validation, broker
   re-registration with the accumulated set, `OnVerbRun` → single-post
   dispatch, `onAcceptMode`/`onAcceptClear`), `events.go` (`on(event,
   fn)` with `"*"` wildcard), `queue.go` (generic bounded queue: keep
   oldest, drop+count newest, coalesced wake channel; drops surface as
   one `script.error` with a count).
4. `pkg/cmds/run.go`: glazed BareCommand; positional `script` arg;
   `--once` (poll the script's completion Promise on the owner loop,
   rejection = non-zero exit), daemon mode (serve until signal or broker
   death), `--allow-exec`, `--no-broker` (data-only profile). Client
   name = script basename = verb ownership.
5. go.mod: `go-go-goja` pinned at pseudo-version b5f41a1 (workspace
   overrides locally; `GOWORK=off` builds fetch from GitHub).
6. Tests: 9 broker-backed cases in `pbuimod_test.go` (accept resolve /
   cancel-null / verb round-trip / re-registration replaces / print segs
   / event order+wildcard / data round-trip / clientless throws), queue
   unit tests, bridge unit tests + `FuzzBridge`.
7. **The fuzzer found two real GGWM-001 bugs in `pkg/pbui/object.go`**
   (details below); fixed both, plus a broker verb-duplication bug found
   during live smoke.
8. Live smoke against a real broker (no X): `run --once hello.js`
   produced a `listener.print` with an embedded color object;
   `git-verbs.js` as a daemon registered two verbs owned by
   `git-verbs.js`; the full palette flow ran cross-process:
   `run --once palette.js` (accept) ← `go-go-wm answer --ptype color
   --value '#b0563f'` → script printed 4 derived color presentations and
   exited 0.
9. Example scripts written to `examples/scripts/` (hello, palette,
   git-verbs runnable today; golden/router P2, rc P3, project-switcher
   P4 as previews) and copied into this ticket's `sources/scripts/`.

### What worked

- The fs_async promise pattern transplanted without surprises; the
  accept promise resolved from a CLI answer on the first live run.
- The factory's module-selection semantics did exactly what design doc
  02 predicted: explicit modules + no middleware = no exec, data-only
  helpers everywhere.
- Fuzzing the bridge for 20 s (392 k execs) immediately paid rent — two
  latent URI bugs from GGWM-001 that no unit test had touched.

### What didn't work

- **Top-level `let` is invisible to `GlobalObject().Get`** — lexical
  bindings are not global-object properties, so every test that read
  script state back polled `nil` forever (all timeouts, 5 s each). Fix:
  test scripts declare probe state with `var`.
- **A literal NUL byte snuck into a fuzz seed string** in the test file
  (`illegal character NUL`); replaced with the two-character escape.
- **`pkill -f` self-match, twice more**: even with the `[g]` bracket
  trick, a compound command that *also spawns* `go-go-wm run …` contains
  that literal text in its own cmdline and pkill kills the shell (exit
  144). Rule: kill and spawn in **separate** Bash invocations, or run
  the spawn from a script file.
- `query events --count N` races: the watcher eats connect/disconnect
  events, so the interesting event lands after the cutoff. Count
  generously.

### Bugs found and fixed (all pre-existing)

1. `pbui.ObjectToURI` put the ptype in the URI **host** position without
   restricting its alphabet; a ptype containing `\` (fuzz seed) or `%`
   produced URIs Go refuses to parse back. Fix: `pbui.ValidPtype`
   (`[A-Za-z0-9._-]+`) enforced in `NewObject` and `ObjectFromURI` —
   normalize-then-fail-early at the entry points.
2. `pbui.ObjectFromURI` **double-decoded** the value: `url.Parse` already
   decodes `u.Path`, then the code ran `PathUnescape` on it again — a
   value containing `%` either corrupted ("100%20off" → "100 off") or
   errored (`#fff%0X`). Fix: unescape `u.EscapedPath()` instead.
3. The broker's `TRegister` handler blindly appended, so re-registration
   duplicated menu entries (live smoke showed `git.copy-short` twice).
   Fix: upsert by `(owner, id)`.

### What was tricky to build

- The event delivery chain has three hops (client read goroutine →
  bounded queue → one posted batch per wake) because no hop may block
  the previous one and the loop must not be flooded one-post-per-event.
  The queue keeps the *oldest* items on overflow: consumers care about
  contiguity of what they saw more than freshness, and the drop count
  arrives as a single `script.error`.
- `--once` semantics: goja has no "loop drained" signal usable from
  outside, so run polls the completion Promise's state via `Owner.Call`
  every 25 ms. Scripts that want daemon-until-done simply end with a
  top-level promise expression.

### Code review instructions

Start with `pkg/jsmod/pbuimod/accept.go` (the promise pattern — check
rules II.2/VI of design-doc/02: NewPromise on VM thread, settle only via
posts). Then `events.go` + `queue.go` (the no-blocking chain), then the
broker upsert in `pkg/pbui/broker/broker.go` and the URI fixes in
`pkg/pbui/object.go` (run `go test -fuzz=FuzzBridge -fuzztime=30s
./pkg/jsmod/`). `run.go` is glazed boilerplate plus `waitForCompletion`.

## Entry 2 — 2026-07-18: P2 — the `wm` module over the control socket

### What I set out to do

`pkg/jsmod/wmmod` (Backend seam + IPC backend + sugar + events +
fluent workspace), the fake-backend test suite, and the two example
scripts that prove it live in Xvfb: `golden.js` (self-asserting layout
build) and `router.js` (event-driven window routing).

### What was done, in order

1. **New wmcore op: `move-leaf`** — the op vocabulary had no way to move
   a window across workspaces (router.js's whole point). Added
   `DetachLeaf`/`GraftLeaf` tree functions and the `move-leaf` Apply case:
   destination named by `op.Workspace`, optional `op.Target` leaf to
   split, `op.Dir`; the leaf **keeps its id**, which is exactly why
   frames survive the move (frames are keyed by leaf id — reconciliation
   needs zero new code). Moving a workspace's only leaf leaves a fresh
   launcher leaf behind instead of an empty tree. Target validated
   *before* detaching so failure never mutates. Three new unit tests.
2. `backend.go`: the `Backend` interface (Tree/Windows/Apply/Bind) — the
   seam that makes one module serve both attachment points — plus
   `IPCBackend` over `wmx11.QueryIPC` with context-bounded calls, and
   `ErrNoKeybindings` pointing at rc.js.
3. `module.go`/`sugar.go`: queries (tree/windows/focused/leaves),
   mutations that all compile to Ops (apply/split/close/setApp/setRatio/
   swap/moveSplit/moveLeaf), `wm.split` ratio handling (split-leaf
   reports the new *leaf*, so the module finds the new parent split in a
   tree fetch before issuing set-ratio), the fluent
   `wm.workspace(name)` (find-by-name-or-create; methods switch/rename/
   remove/clone/adopt each one Op; resolved id owned by Go), `wm.on`
   via the shared EventFan, `wm.bind` (posts only; IPC throws).
4. **EventFan refactor**: the P1 event pump moved from pbuimod to
   `pkg/jsmod/eventfan.go` so pbui.on and wm.on share one broker
   subscription per process (two subscriptions would have raced over the
   client's single events channel).
5. `run.go` wires `wm` + `--wm-socket`; `window.managed` now carries the
   workspace id.
6. Tests: sugar→exact-Op-sequence assertions, fluent workspace
   (re-lookup mints no ops), adopt-across-workspaces, bind error text,
   failed-op-mutates-nothing, and the **replay property**: the op stream
   a session records, replayed onto a fresh desktop, must serialize
   identically (it does).
7. Live in Xvfb :78: `golden.js` built editor|((trace)/(listener)) with
   ratio 0.62 and self-asserted via `wm.tree()` (exit 0; screenshot 01 —
   the trace tiles show the script's own ops as events);
   `router.js` + `xterm -T "Mozilla Firefox"` → the window was adopted
   into a freshly created "web" workspace, frame intact (screenshot 02).

### What worked

- The Backend seam did its job on the first try: the entire test suite
  runs against `fakeBackend` (a real `wmcore.Desktop` + op recorder), no
  X anywhere, and the same module code then drove the live WM unchanged.
- `move-leaf` slotted into the WM with zero X-side changes — frames
  keyed by leaf id meant reconciliation was already correct.

### What didn't work

- **goja exposes Go struct *field names*, not json tags**: `wm.tree()`
  returned `{Workspaces: …}` and golden.js crashed on `d.workspaces`.
  Fixed with `jsmod.ToPlain` (JSON round trip) on every query result —
  scripts see wire shapes, always.
- **pkill self-match, terminally**: even a kill script is not enough if
  the *outer* shell's eval string mentions the pattern (the parent's
  cmdline matches). The only reliable pattern: the Bash call that runs
  the kill script must contain nothing but the script path.

### Bugs found and fixed (pre-existing)

- `add-workspace` defaulted the first leaf's app to the literal string
  `"launcher"`, but the WM renders builtins only for `""` or
  `builtin:*` — every workspace created by Mod4-n or script showed a
  blank, unpaintable tile. Fixed both sides: the op keeps `""` (the
  launcher convention), and `isBuiltinLeaf` also accepts `"launcher"`.

### Code review instructions

`pkg/wmcore/ops.go` move-leaf case first (check the validate-before-
detach ordering and the only-leaf branch), then `wmmod/sugar.go`
(`findParentSplit` — it scans all workspaces), then the EventFan
(`pkg/jsmod/eventfan.go`) for the no-blocking chain. Run
`go test ./pkg/jsmod/wmmod/ -run TestReplay -v` for the ops-as-data
property.

## Related

- design-doc/02 — the implementation guide this diary executes.
- GGWM-001 reference/02 — the diary of the WM build itself.
