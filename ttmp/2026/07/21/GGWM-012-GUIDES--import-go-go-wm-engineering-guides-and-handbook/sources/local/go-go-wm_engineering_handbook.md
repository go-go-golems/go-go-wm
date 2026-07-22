# Building go-go-wm as a Programmable Presentation-Based Desktop

## Architecture review, resize-performance plan, JavaScript runtime design, rich REPL design, and an intern's implementation guide

**Repository reviewed:** `github.com/go-go-golems/go-go-wm` at the merged `main` history available on 2026-07-21  
**Primary comparison:** i3's X11 architecture and interactive resize path  
**Conceptual lineage:** CLIM and Genera Dynamic Windows, Smalltalk systems, HyperCard-style end-user programming, and contemporary scriptable window managers  
**Audience:** contributors who know Go or JavaScript but are new to window-manager internals

---

## Scope and evidence

This handbook reviews the current implementation, the dated `GGWM-*` design and implementation records from July 18-20, the original self-contained PBUI shell sketch, the basketball analytics sketch, and the relevant `go-go-goja` runtime mechanisms. The public PARC pages named in the request could not be fetched from the research environment. The repository contains the corresponding dated ticket workspaces, design documents, diaries, profiles, smoke tests, and examples, so those in-repository records are used as the authoritative chronology.

This is not a request to replace the current design. The code already contains several strong architectural choices: a pure layout model, operations as data, a single X11 owner loop, a separate JavaScript owner loop, a broker-level presentation protocol, immutable render snapshots, bounded event ingestion, and a rich REPL prototype. The recommendations below preserve those choices and formalize the seams between them.

The document follows a foundational-first teaching style: it explains the contracts an X11 window manager must satisfy, then traces the current code, then derives performance and scripting changes from that evidence. Code sketches are intentionally concrete, but proposed APIs are design material rather than a compatibility promise.

---

# Executive assessment

## The central judgment

go-go-wm is not slow because Go is unsuitable for window managers, because software rendering is inherently too slow, or because the binary split tree is complex. The most visible resize cost comes from performing expensive, dimension-dependent work during every accepted pointer update:

1. The split ratio changes.
2. The entire workspace layout is recomputed.
3. changed frames and their clients are resized.
4. every changed frame is repainted.
5. an exact-size RGBA buffer and exact-size X image or MIT-SHM pixmap are replaced whenever dimensions differ.
6. every configured client is asked to redraw its own interior.

The existing performance work removed per-paint allocation, inefficient fills, duplicate Expose rendering, and socket copies for same-size repaints. Those changes are valid and measurable. They do not remove the dominant structural fact of a divider drag: dimensions usually change on every frame, so exact-size caches miss on every frame.

The first performance recommendation is therefore direct:

> **Make preview-only resizing the default. Move a lightweight divider indicator during the drag; commit the new ratio and configure client windows once on release. Add adaptive live resizing later as an optional policy.**

This is not a retreat from responsiveness. i3 uses this design for graphical tiled resizing: it moves a helper bar while the pointer is held and updates container percentages after the drag completes. The user sees immediate feedback, while clients avoid a stream of costly geometry changes.

## What should remain

The following current decisions are good foundations and should remain visible in the architecture:

| Current decision | Why it is valuable |
|---|---|
| `wmcore` is pure and X-free | Layout, replay, neighbor selection, and operations can be tested without a display server. |
| Every layout mutation is an `Op` | Keyboard, mouse, IPC, JavaScript, tests, traces, and replay use one vocabulary. |
| X11 state is owned by one WM loop | Focus, fullscreen, frame maps, and X resources do not need scattered locks. |
| Each Goja VM has an owner loop | Asynchronous native modules can settle Promises without concurrent VM access. |
| JS UI render functions produce normalized snapshots | Host render loops never call JavaScript and can keep the last good snapshot after a script error. |
| PBUI objects and verbs are data on a broker | Presentation-based interaction crosses process and application boundaries. |
| Event ingestion is bounded before entering the VM | A producer cannot block the broker reader indefinitely. |
| The REPL derives rich, typed views | Evaluation results can become desktop objects rather than formatted strings. |

## What should change next

The highest-value changes are ordered below. “Immediate” means they reduce current user-visible latency or remove a correctness risk without requiring the full future architecture.

| Priority | Change | Intended result |
|---|---|---|
| Immediate | Preview-only divider resize | Stops exact-size render/X-resource churn and client repaint storms during ordinary drags. |
| Immediate | Replace timestamp dropping with a latest-pointer mailbox and render scheduler | The WM always paints the newest pointer state instead of processing stale motion events. |
| Immediate | Add stage-level resize telemetry and repeatable slow-client tests | Performance work becomes evidence-driven and regressions become visible. |
| Immediate | Replace goroutine-per-event emission with one bounded outbox | Prevents unbounded goroutine growth during high-rate activity. |
| Near term | Separate desired X state from applied X state and diff them | Makes geometry, mapping, focus, and stacking updates explicit and batchable. |
| Near term | Render client decorations separately from client-sized interiors | External client frames stop requiring a full-pane software bitmap. |
| Near term | Introduce supervised, per-runtime JS identities and capability manifests | JS becomes an OS policy layer with lifecycle and failure boundaries. |
| Medium term | Replace flat `uispec` rows with retained keyed scene trees | Enables component state, nested layout, scrolling, overlays, virtualization, and dirty-region rendering. |
| Medium term | Add a surface/portal manager | Bars, taskbars, menus, modals, popovers, notifications, and command palettes share one lifecycle and stacking model. |
| Medium term | Extend PBUI from exact ptype strings to a type registry and translators | Accept and context actions gain CLIM-like composability without hard-coded application coupling. |
| Medium term | Promote the rich REPL into the desktop shell and debugger | Users can inspect, compose, profile, transact, persist, and hot-reload the running desktop. |

## The target architecture in one sentence

**Go owns mechanisms, validation, X resources, rendering, and supervision; JavaScript owns policy, composition, and domain behavior; PBUI owns the semantic object protocol that lets every surface and runtime cooperate.**

---

# Part I. Foundations: what a window manager must do

## 1. The X11 mental model

An X11 window manager is not the display server. The X server owns windows, routes input, stores properties, manages pixmaps, and performs drawing requests. Applications are X clients. The window manager is another privileged X client that selects `SubstructureRedirect` on the root window and therefore receives requests that would otherwise alter top-level windows directly.

A normal top-level client lifecycle looks like this:

1. A client creates a window and sets ICCCM/EWMH properties such as title, class, normal hints, transient leader, protocols, and window type.
2. The client asks to map it.
3. The WM receives `MapRequest`, inspects properties, and decides whether the window tiles, floats, or should be ignored.
4. A reparenting WM creates a frame, reparents the client into it, chooses geometry, and maps the frame and client in a controlled order.
5. Focus is set with X input-focus operations and reflected through EWMH properties.
6. The WM handles `ConfigureRequest`, `DestroyNotify`, `UnmapNotify`, `PropertyNotify`, `ClientMessage`, focus events, and input events until teardown.

The distinction between **requested state**, **desired WM state**, and **applied X state** is essential. A tiled client can request a size, but the layout owns its geometry. The WM may refuse the request; when the resulting geometry does not change, ICCCM requires a synthetic `ConfigureNotify` so the client knows the authoritative coordinates. A floating client is different: its size hints and configure requests usually participate in placement and resizing.

This creates a basic ownership table:

| Concern | Tiled client | Floating client | WM-owned surface |
|---|---|---|---|
| Outer geometry | Layout model | Float placement policy and client request | Surface manager |
| Client interior | Frame geometry minus decorations | Float geometry minus decorations | Not applicable |
| Decorations | WM | WM | WM |
| Stacking | Mostly non-overlapping; WM controls | WM controls float band | WM controls portal layer |
| Focus | WM routes to client | WM routes to client | WM routes to surface handler |
| Repaint | Client paints interior; WM paints decoration | Same | WM renderer paints content |

A new contributor should keep two rules in view:

- **Do not derive policy from incidental X events.** Put policy in explicit state machines and decision functions, then make X handlers translate events into those decisions.
- **Do not make X calls while discovering what the state should be.** Compute desired state first; then diff and apply it.

These rules are visible in mature WMs. i3's render pass computes rectangles in memory without X calls. Its X layer keeps a representation of what X currently sees and pushes only required changes.

## 2. Tiling is a model problem before it is an X problem

The current `wmcore` binary tree is a sound minimal tiling model. A leaf represents a tile; a split represents a row or column division with a ratio and two children. `Layout` maps that tree and a workspace rectangle to pixel rectangles. An operation mutates the tree, and the X shell reconciles windows to the resulting layout.

The tree is small enough to understand but already supports the important properties:

- every tile has a stable node ID;
- splits have explicit orientation and ratio;
- close collapses a sibling into the removed space;
- split-dock and cross-workspace moves can preserve leaf identity;
- serialization and replay are possible;
- geometry-dependent commands such as directional focus are derived from one layout function.

The model should continue to be stricter than the UI. A menu, tooltip, modal, or dialog does not need to become a tree node. Floating transients already demonstrate the correct distinction: the tiling tree remains replayable, while shell-side overlay state handles geometry that is not part of persistent tiling policy.

### 2.1 Invariants worth making executable

A window manager becomes easier to change when its invariants are named and tested. Recommended invariants include:

1. Every managed tiled frame belongs to exactly one live leaf.
2. Every live leaf has at most one managed external client and exactly one app identity.
3. A frame ID is stable across geometry changes and workspace moves that preserve the leaf.
4. Exactly one focus target is active: tiled client, floating client, fullscreen target, or WM surface.
5. Fullscreen owns geometry and focus until an explicit exit condition.
6. Hidden workspaces have no mapped tiled frames or floats, unless a future sticky policy says otherwise.
7. An operation either fails before mutation or produces an event describing the successful mutation.
8. Replaying a successful operation stream from the same initial desktop produces an equivalent desktop.
9. The desired X state can be recomputed from authoritative model and shell state.
10. JavaScript cannot mutate WM-owned state without entering through a validated host operation.

Several of these are already present informally or through focused regression tests. The next step is to collect them into package-level tests and diagnostic assertions.

## 3. Presentation-based UI: the semantic layer

A presentation-based interface associates a visible region with a typed object. The object is not inferred later from pixels or text; the rendering operation records the relationship at presentation time. This changes input from “parse a string produced by another application” to “select a typed object that is already on the desktop.”

The current PBUI design contains three central mechanisms:

- A **presentation object** has `ptype`, JSON value, label, and mouse documentation.
- An **accept session** asks for one or more ptypes; matching regions become eligible answers across processes and workspaces.
- A **verb** is a type-directed action registered as data and routed to its owning process.

The original shell sketch makes the interaction contract explicit: left click answers a compatible pending accept; otherwise it performs a primary action; otherwise it opens the object's menu. Right click always opens the object menu. The Go `apps.Resolve` function preserves this contract in a pure, testable form.

### 3.1 The CLIM concepts that matter here

The relevant CLIM ideas are concrete, not ornamental history:

| CLIM concept | Current go-go-wm analogue | Missing extension |
|---|---|---|
| Presentation | `pbui.Object` plus an `apps.Region` | Stable semantic node in a retained output tree |
| Presentation type | Exact `Ptype` string | Type hierarchy, parameters, predicates, versioned descriptors |
| Input context | Broker accept session | Nested contexts, scopes, deadlines, supersession, completion |
| Presentation translator | Type-directed gesture/action | Explicit source-to-target conversion graph |
| Output history | Rendered presentations remain semantically known | Persistent scene/output records and query APIs |
| Command table | Broker verbs | Namespaces, priorities, predicates, capabilities, discoverability |
| Mouse documentation | `Doc` and status line | Structured gesture documentation and keyboard equivalents |

The important upgrade is not to copy CLIM's API. It is to preserve the semantic separation:

1. Rendering declares what object a region represents.
2. Input declares what type of object is needed.
3. A type system decides compatibility.
4. Translators can convert a visible source presentation into an acceptable target.
5. Commands and menus are derived from types and context, not application identity.

### 3.2 What the basketball sketch teaches

The basketball workbench is valuable because it moves beyond chips and buttons. Player names in tables, scatter-plot bubbles, radar legends, trend points, teams, and games are all typed presentations. One focused player drives several independent views. A watched object remains live and usable from a different pane.

That sketch suggests a practical widget strategy:

- Tables, scatter plots, line charts, radar plots, shot charts, and small multiples should be host-rendered **data widgets**, not arbitrary JavaScript pixel loops.
- Every visual mark can carry a presentation object. A plotted point is not merely a coordinate; it can be a `<game>` or `<player>` region with hover documentation, accept behavior, and verbs.
- Shared selection and focus should be explicit state or signals, not direct references between widgets.
- Large datasets require virtualization, level-of-detail decisions, and bounded presentation-region generation.

The forgotten UX pattern becomes useful precisely because it composes with modern analytical widgets: semantic marks can be selected by commands, re-presented in a watchlist or REPL, and acted on by services that did not create the original chart.

---

# Part II. Review of the current implementation

## 4. Current architecture map

The current system can be read as five cooperating layers:

1. `pkg/wmcore`: the pure desktop tree, operations, layout, neighbor selection, and serialization.
2. `pkg/wmx11`: X11 lifecycle, frames, floats, focus/fullscreen state, input, IPC, builtins, launcher, and painting.
3. `pkg/pbui`, broker, and client: typed objects, verbs, accept sessions, events, and cross-process routing.
4. `pkg/apps`, `pkg/apps/uispec`, and `pkg/draw`: pure software-rendered surfaces and hit regions.
5. `pkg/jsmod`, `pkg/repl`, and `go-go-goja`: JavaScript modules, runtime ownership, UI snapshots, event fan-out, and REPL sessions.

![Figure: Current architecture](ggwm_assets/current_arch.png)

The separation is more mature than the repository's small README suggests. In particular, the JavaScript path does not directly call X, and the renderer does not enter the VM. Those are the right boundaries for a programmable desktop.

## 5. `wmcore`: strong foundation, small next steps

### 5.1 What is already right

`wmcore.Op` is the most important architectural success in the codebase. It makes mutation explicit and serializable. `Apply` is the single model mutation entry point, and `Result` reports generated IDs. The same vocabulary can be used by keyboard handlers, mouse gestures, IPC, JavaScript sugar, testing, recording, and replay.

The layout function is deterministic in purpose and compact. Neighbor selection uses geometry rather than tree position, which aligns user expectations with what is on the screen. Stable leaf IDs allow a frame and its client to survive moves without unnecessary X teardown.

### 5.2 Improvements

The next changes should support systematic reconciliation and transactions rather than add new layout features immediately.

**Add model versions.** A monotonically increasing desktop version makes queries, transactions, previews, and stale-write detection precise.

```go
type Desktop struct {
    Version    uint64
    Workspaces []Workspace
    Current    string
    // ...
}
```

**Return a change summary.** `Apply` currently returns minted IDs. A richer result can report affected workspaces, nodes, focus implications, and whether geometry/topology changed. This avoids broad shell reconciliation after every operation.

```go
type ChangeSet struct {
    BeforeVersion uint64
    AfterVersion  uint64
    Topology      bool
    Geometry      bool
    Visibility    bool
    AffectedNodes []NodeID
    AffectedWS    []string
}
```

**Make batch semantics atomic or explicitly prefix-committing.** The current batch stops at the first error and retains the successful prefix. That is useful for boot scripts but should be named. Provide both:

- `ApplyPrefix(ops)` for current behavior;
- `ValidateBatch` plus `ApplyAtomic(ops)` for transactions that require all-or-nothing semantics.

**Separate transient preview state from committed desktop state.** Divider previews should not mutate the authoritative tree on every pointer movement. A `LayoutWithOverride(splitID, ratio)` helper can compute preview geometry without producing operations or events.

**Add property-based tests.** Generate trees and operations, then assert ID uniqueness, ratio bounds, layout coverage, non-overlap, replay equivalence, and failure non-mutation.

## 6. `wmx11`: correct ownership, expensive reconciliation

### 6.1 Threading model

The WM loop owns X calls and focus/fullscreen state. IPC and JavaScript callers post functions back to that loop. The recent focus-state refactor makes the “exactly one focus target” rule structural instead of maintaining coupled fields. This is the correct direction: each cross-cutting state machine should have one owner type, read methods, and mutation methods.

The pattern should be repeated for:

- applied X geometry/map/stack state;
- active drag/gesture state;
- surface/portal state;
- client lifecycle state;
- runtime registrations leased by a script owner.

### 6.2 Reconciliation today

A successful operation calls `afterOp`, which can reap orphan frames, synchronize builtins, perform a full relayout, update EWMH, repaint bars, and refocus on workspace changes. `ApplyBatch` reduces this to one reconciliation pass for a burst, which was a meaningful startup optimization.

The remaining weakness is that reconciliation is organized around broad procedures rather than an explicit change set. A ratio update and a theme swap both eventually call painting code, but they have different invalidation requirements. A close operation, focus change, bar text update, and workspace switch also differ.

Recommended internal phases:

```text
model mutation
    ↓
ChangeSet
    ↓
compute desired shell state
    ↓
diff desired vs applied X state
    ↓
issue batched X requests
    ↓
mark scene/layout/paint damage
    ↓
render and upload dirty surfaces
    ↓
publish sequenced events
```

This sequence creates named places for profiling and testing. It also prevents accidental full repaints from becoming the default response to every state change.

### 6.3 Client lifecycle

The code has already learned several hard X11 lessons: float classification, transient leaders, fullscreen ownership, focus restoration, per-client event hookup, and click-to-focus. Continue extracting display-free decisions such as `shouldFloat`, `computeFocusDecision`, and configure-request policy into pure functions.

A useful next abstraction is a `ManagedClient` state machine:

```go
type ClientKind uint8
const (
    ClientTiled ClientKind = iota
    ClientFloating
    ClientUnmanaged
)

type ManagedClient struct {
    XID       xproto.Window
    FrameXID  xproto.Window
    Kind      ClientKind
    Lifecycle LifecycleState
    Workspace string
    Leaf      wmcore.NodeID
    Hints     NormalHintsSnapshot
    Props     ClientProperties
    Desired   ClientGeometry
    Applied   ClientGeometry
}
```

This does not require moving all code into one struct. It requires making lifecycle and geometry ownership queryable, so handlers stop re-deriving the same distinctions.

## 7. Rendering and regions

`pkg/apps` has a clean small contract: renderers are pure functions that return an `image.RGBA` and regions. `RegionAt` resolves topmost hit testing, and `Resolve` implements presentation/action/menu precedence. Golden tests can validate pixels without X.

The limitation is that the result is already flattened into pixels and rectangles. Once flattened, the host cannot know that only a title changed, that a table row scrolled, or that a field cursor moved. It cannot preserve local widget state by key, perform incremental layout, or virtualize unseen rows. That limitation affects performance and expressiveness simultaneously.

Treat the current `uispec.Spec` as a useful wire format prototype, not the final widget system. It proves that JavaScript can safely describe UI as data. The next version should retain the semantic tree until after reconciliation, layout, and damage calculation.

## 8. PBUI broker and event flow

### 8.1 Strengths

The broker unifies accepts, verbs, event publication, and ownership. Registrations are process-owned and disappear on disconnect. This is the right substrate for cross-application composition. The event fan creates one subscription per script process, uses a bounded queue, reports drops, batches deliveries, and posts JavaScript dispatch to the runtime owner.

### 8.2 Risks

**Events are best-effort and lack a global sequence.** That is acceptable for hover and advisory status. It is insufficient for stateful automation, notebook replay, or a taskbar that must recover after disconnection.

**`emitEvent` launches a goroutine for every event.** During a drag, trace append, window storm, or misbehaving script, this can create an unbounded number of goroutines even if downstream delivery is bounded.

**One slow JavaScript handler delays every later JS event in that runtime.** The owner-loop rule is correct; the scheduling policy needs priorities, budgets, and coalescing.

**Handlers are append-only.** A long-running development session needs unsubscribe handles and owner-scoped replacement.

Recommended changes:

1. One bounded WM event outbox, drained by a fixed goroutine.
2. A global event sequence assigned on the WM loop for authoritative events.
3. Per-topic retention policies: none, latest, bounded ring, or durable log.
4. `subscribe({from, filter, coalesce})` with a snapshot/resync path.
5. JS subscription objects with `close()` and automatic runtime lease cleanup.
6. Priority classes: input settlement, host-call completion, UI action, authoritative state event, telemetry.
7. Handler duration metrics and warnings when a callback exceeds its budget.

## 9. JavaScript attachment points

The current system supports three useful modes:

- an in-process trusted rc runtime with direct WM backend access;
- a standalone daemon runtime that talks through IPC and the PBUI broker;
- one-shot scripts and a REPL.

The mode distinction is valid. It should become an explicit deployment model rather than remain a property of which command constructed the runtime.

| Profile | Typical use | Trust and failure model |
|---|---|---|
| System runtime | core bindings, default bar, launcher policy | Trusted; in process; restartable without restarting X ownership |
| User runtime | personal automation and widgets | Capability-limited; preferably independently restartable |
| App runtime | one script-defined application | Own identity, surfaces, verbs, and event leases |
| REPL runtime | interactive experiments and inspection | Explicit capabilities per notebook/session; interruptible |
| External agent | networked or heavy integration | Separate process; broker/API only |

The current `xgojaprovider` shared runtime state is a warning sign: module factories must not accidentally share clients, event fans, or backend state across VMs. Every runtime needs a unique instance record and runtime-scoped module state.

## 10. The UI module

`ui.app` correctly treats Goja callables as VM-owned. `rerenderOnLoop` calls JS, normalizes exported data, and swaps a mutex-protected snapshot. X app and tile renderers copy the snapshot and remain VM-free. Action and key dispatch post back to the owner, then rerender and repaint.

This is the right concurrency law:

> **JavaScript produces state and event responses on its owner loop. Host renderers consume immutable, VM-free snapshots.**

The next limitations are visible in the implementation:

- every action reruns the whole render function;
- every live host surface redraws after the snapshot swap;
- rows and segments have no stable identity;
- local field, scroll, expansion, hover, and selection state have nowhere to live except application JS;
- there is no unmount or effect cleanup protocol;
- there is no layout-vs-paint invalidation distinction;
- there is no error boundary below the whole app snapshot;
- the renderer receives full dimensions and returns a full image.

The retained-widget design later in this handbook keeps the owner-loop law while solving these limitations.

## 11. The rich REPL

The REPL is already more than a terminal evaluator. It has persistent session semantics, `Out(n)` and `$_`, console capture, rich value derivation, a `__pbui__` opt-in, table/image/field views, PBUI verbs, and a standalone UI. The most important design choice is that a result uses its real ptype. A color result is a `<color>`, not a generic “REPL value,” so it can answer a color accept and receive color verbs.

The REPL should become the main programmable shell for the desktop, but several kernel and editor responsibilities are still missing:

- interrupt and deadline control;
- completion and signature help;
- Unicode text input and IME support;
- multiline editing and structured history search;
- bounded result retention and explicit live/snapshot semantics;
- notebook persistence with code, capability manifest, source maps, object provenance, and event cursors;
- runtime inspection and attachment;
- transaction preview and commit;
- integrated traces, profiles, logs, and permission prompts.

These are not cosmetic editor features. They define whether the REPL can safely operate the desktop as a long-running system.

## 12. Architectural risk register

| Risk | Current evidence | Consequence | Recommended containment |
|---|---|---|---|
| Resize resource churn | Exact-size RGBA and XSHM/ximage replacement in `paintFrame` | Lag and allocator/X server overhead during every dimension change | Preview resize first; decoration-only surfaces; explicit frame scheduler |
| Client repaint storm | `ConfigureWindow` on every accepted drag tick | Slow clients determine perceived WM latency | Commit on release; optional sync-aware live mode |
| Event goroutine growth | Goroutine created per emitted event | Memory/scheduler pressure under bursts | Fixed bounded outbox |
| Runtime identity leakage | Provider/module state can be shared across VMs | Cross-runtime events or registrations | Per-runtime instance registry |
| Timed-out host call still executes | Posted WM operation may outlive caller timeout | Script believes mutation failed while state changes later | Operation IDs, cancellation-before-start, eventual completion record |
| Flat UI IR | Rows/segments flattened per render | No diff, lifecycle, virtualization, overlays, or partial damage | Retained keyed scene tree |
| Exact ptype matching | `TypeMatches` supports `any` or equality | No subtype or translator composition | Type registry and conversion graph |
| Coarse execution permission | `--allow-exec` style gate | Scripts receive more authority than required | Granular capability manifest and scoped resources |
| Single giant rc runtime | Many system services can share one failure domain | One exception or hot reload affects unrelated policy | Supervised runtime actors |
| Unbounded REPL history | Raw values and views remain reachable | Long sessions retain memory and live resources | Retention classes, pinning, weak handles, persisted snapshots |

---
# Part III. Performance: making resize and rendering predictable

## 13. A systematic performance method

Window-manager performance must be measured under input and with real clients. An idle WM profile says little. A useful method combines four views of the same interaction:

1. **Input trace:** pointer-event arrival, coalescing, scheduled frames, and release.
2. **WM stage timings:** model apply, layout, X-state diff, X requests, raster, conversion, upload, event publication.
3. **Client behavior:** configure requests sent, sync acknowledgements, expose/damage, and client CPU.
4. **User-visible timing:** pointer-to-preview latency, pointer-to-client-geometry latency, frame misses, and final commit latency.

The repository already includes `pprof` entry points and scripted drag harnesses. Extend them rather than start over.

### 13.1 Test environments

Use at least four environments because each reveals a different cost:

| Environment | Purpose |
|---|---|
| Xvfb | Deterministic CI; validates model/X request counts and catches gross regressions. |
| Xephyr on the development desktop | Includes a real nested server and compositor path while remaining disposable. |
| Bare local Xorg session | Measures the actual deployment path and input latency. |
| Remote/forwarded X without SHM | Validates fallback behavior and prevents local shared-memory assumptions. |

### 13.2 Fixture clients

A WM needs controlled clients, not only whatever applications happen to be installed.

- **Fast client:** accepts resize and repaints a solid fill immediately.
- **Slow client:** sleeps a configurable time on configure before repaint.
- **Sync client:** implements `_NET_WM_SYNC_REQUEST` and acknowledges after repaint.
- **Hint client:** exercises min/max/base/increment/aspect hints.
- **Transient client:** creates dialogs and updates transient/type properties late.
- **Event-storm client:** creates, maps, renames, and destroys windows repeatedly.

The existing `testwin` command can grow into this fixture family.

### 13.3 Metrics and budgets

Suggested initial budgets are targets for engineering discussion, not claims about current measurements:

| Metric | Initial target |
|---|---|
| Pointer-to-preview p95 | under 16 ms |
| Preview frame cadence | stable 60 Hz when inexpensive; stable 30 Hz under load |
| Release-to-final-configure p95 | under 25 ms before client repaint |
| WM-loop longest task during resize | under 4 ms |
| XSHM/pixmap allocations during preview-only drag | zero |
| Client `ConfigureWindow` requests during preview-only drag | zero until release |
| Stale pointer states painted | zero; scheduler paints latest known state |
| Event-outbox queue occupancy | bounded and observable |
| JavaScript callback warning threshold | configurable, initially 8-16 ms |

### 13.4 Trace schema

Instrument one gesture with a shared `gesture_id`:

```json
{"kind":"input.motion","gesture":"g42","seq":181,"x":714,"y":390,"ts_ns":...}
{"kind":"resize.preview_scheduled","gesture":"g42","frame":23,"latest_seq":181}
{"kind":"resize.preview_painted","gesture":"g42","frame":23,"duration_us":340}
{"kind":"resize.commit","gesture":"g42","ratio":0.621,"desktop_before":91,"desktop_after":92}
{"kind":"x.apply","gesture":"g42","configure_frames":2,"configure_clients":2,"duration_us":610}
{"kind":"paint.damage","gesture":"g42","surfaces":2,"pixels":18432}
```

This trace should be available in three forms: structured logs, a ring buffer query for the REPL/inspector, and counters/histograms for long runs.

## 14. Current divider-resize path

The current path is clear in `pkg/wmx11/input.go` and `pkg/wmx11/manage.go`:

1. `MotionNotify` reaches `dividerMotion`.
2. Updates less than 16 ms after the last accepted paint are discarded.
3. The current layout is computed to find the split rectangle.
4. Pointer position is converted to a ratio and snapped.
5. `wmcore.OpSetRatio` mutates the desktop.
6. `relayoutResized` recomputes the whole workspace layout.
7. Each changed frame receives `MoveResize`; its client receives `ConfigureWindow`.
8. Each changed frame calls `paintFrame`.
9. If dimensions changed, exact-size host and X resources are replaced.
10. The full frame is filled and converted; builtins/script tiles rerender their full content.
11. The X server and every client process the geometry change.
12. Release repeats the final position to compensate for discarded motion.

![Figure: Current resize path](ggwm_assets/current_resize.png)

### 14.1 Why the 16 ms gate is not true coalescing

A time gate says “ignore this event because another event was recently processed.” It does not remove older events already waiting in the X/event queue, and it does not guarantee that the next accepted event is the newest pointer state. If painting or client interaction stalls the loop, the WM may later accept a stale event and visibly trail the pointer.

True coalescing has different semantics:

- every motion updates a single latest-state slot;
- at most one render task is pending;
- the render task reads the newest slot when it runs;
- intermediate positions are overwritten rather than queued;
- release commits the final pointer position synchronously.

### 14.2 Exact-size caches fail during size changes

The current cache policy is correct for same-size damage and ordinary repaint:

- reuse `image.RGBA` if dimensions match;
- reuse the X image or SHM surface if dimensions match;
- let Expose re-blit server-side content.

A resize changes dimensions, so the policy deliberately invalidates those resources. The XSHM path then performs operations such as `shmget`, `shmat`, server attach, pixmap creation, background-pixmap replacement, old pixmap free, and detach. XSHM removes the bulk pixel transfer through the X socket; it does not make surface creation free.

The fallback path has the same logical problem: the old `xgraphics.Image` is destroyed, a new pixmap-backed image is created, and the frame is rebound.

### 14.3 External-client frames pay for pixels the client covers

The frame image is the full outer frame size. For an external client, most of that rectangle is occupied by the reparented client window. The WM needs to paint the title strip, border, focus state, and perhaps resize affordances. It does not need a full client-area bitmap for those decorations.

This suggests a structural optimization stronger than buffer pooling:

- represent decorations as separate narrow windows or a set of decoration surfaces;
- let the client window occupy the interior without a full-pane WM background pixmap;
- resize/repaint title and border resources according to decoration dimensions, not client-area pixels.

For a 1200×800 pane with a 22 px title and 2 px border, decoration pixels are a small fraction of full-pane pixels. This also reduces dirty areas when title/focus changes.

### 14.4 Client repaint can dominate

Even a zero-cost WM repaint would not make live resizing cheap for every application. Each configure changes the client's drawable size. Toolkits may relayout complex widget trees, rebuild backing stores, rerasterize text, or synchronize with GPU/compositor pipelines. Terminal grids, browsers, IDEs, and remote applications have different costs.

A WM should not assume that “60 geometry changes per second” means “60 responsive frames per second.” EWMH defines `_NET_WM_SYNC_REQUEST` so a WM can coordinate interactive resizing with a client-maintained counter. Supporting it allows a live-resize mode to avoid outrunning clients that advertise the protocol.

## 15. Recommended resize architecture

### 15.1 Mode 1: preview-only, the default

During pointer motion:

- grab the pointer;
- compute a preview ratio from the latest pointer state;
- draw or move a thin divider/overlay;
- show snap state and optional numeric percentages;
- do not mutate the committed desktop;
- do not configure frames or clients;
- do not repaint client-sized surfaces;
- do not emit a model operation event.

On release:

- compute the final ratio from the release coordinates;
- apply one `set-ratio` operation;
- reconcile geometry once;
- repaint affected decorations and WM surfaces once;
- publish one committed operation event plus gesture telemetry.

Cancellation restores nothing because committed model state never changed.

This mode gives the best latency and is the safest baseline for unknown clients.

### 15.2 Mode 2: adaptive live resize

Some users prefer live content. Provide it as a policy with explicit pacing:

- motion is coalesced into a latest-pointer mailbox;
- a frame scheduler chooses 30 or 60 Hz based on previous WM task duration;
- clients with `_NET_WM_SYNC_REQUEST` receive at most one unacknowledged resize;
- clients without sync support receive a configurable maximum rate;
- if WM-loop latency or acknowledgement time exceeds a threshold, fall back to preview-only for the remainder of the gesture;
- final release always commits the exact final position.

### 15.3 Mode 3: WM-surface live resize

Builtin and script-defined surfaces are under host control. They can use a separate policy:

- layout updates live at the scheduler cadence;
- retained widgets reuse state and layout caches;
- only dirty decoration/content regions repaint;
- expensive data views may render at reduced detail during the gesture and refine on release.

This allows a fluid native workbench without imposing the same policy on arbitrary X clients.

### 15.4 Proposed gesture state

```go
type ResizeMode uint8
const (
    ResizePreview ResizeMode = iota
    ResizeAdaptiveLive
)

type DividerGesture struct {
    ID              uint64
    Split           wmcore.NodeID
    Mode            ResizeMode
    StartRatio      float64
    LatestPointer   atomic.Pointer[PointerSample]
    PreviewRatio    float64
    LastApplied     float64
    FramePending    bool
    ClientSync      map[xproto.Window]*SyncState
    BeganAt         time.Time
}
```

`atomic.Pointer` is illustrative. Because X input and gesture state are already WM-loop-owned, the simplest implementation may be a normal field plus one timer/task flag. The semantic requirement is “latest replaces previous,” not a particular synchronization primitive.

### 15.5 Proposed sequence

![Figure: Proposed resize scheduler](ggwm_assets/proposed_resize.png)

A minimal implementation can be smaller than the current live path:

```go
func (w *WM) dividerMotion(g *DividerGesture, x, y int) {
    g.Latest = PointerSample{X: x, Y: y, Seq: g.Latest.Seq + 1}
    if !g.FramePending {
        g.FramePending = true
        w.schedulePreviewFrame(g.ID)
    }
}

func (w *WM) runPreviewFrame(id uint64) {
    g := w.gestures[id]
    if g == nil { return }
    g.FramePending = false

    ratio, snapped := w.previewRatio(g.Split, g.Latest.X, g.Latest.Y)
    g.PreviewRatio = ratio
    w.previewLayer.ShowDivider(g.Split, ratio, snapped)

    if g.Mode == ResizeAdaptiveLive && w.liveResizeBudgetAllows(g) {
        w.applyPreviewGeometry(g.Split, ratio) // not a committed Op/event
    }
}

func (w *WM) endDividerGesture(g *DividerGesture, x, y int) {
    ratio, _ := w.previewRatio(g.Split, x, y)
    w.previewLayer.Hide()
    _, _ = w.Apply(wmcore.Op{Op: wmcore.OpSetRatio, Node: g.Split, Ratio: ratio})
}
```

### 15.6 Preview geometry should not be an operation

Operations describe committed desktop history. A transient pointer position is not desktop history. Emitting hundreds of `set-ratio` events makes traces noisy, rules ambiguous, replay expensive, and undo semantics unclear.

Provide a pure override path:

```go
func LayoutPreview(root *Node, area Rect, gap int, override RatioOverride) map[NodeID]LayoutItem
```

or a shell-side function that clones only the path to the split. The preview layer can show affected rectangles without altering `Desktop.Version`.

## 16. Desired/applied X state

A mature WM separates geometry calculation from protocol emission. Introduce a compact X-facing state representation:

```go
type WindowState struct {
    Rect       wmcore.Rect
    Mapped     bool
    StackBand  StackBand
    Above      xproto.Window
    InputFocus bool
    Background xproto.Pixmap
}

type XState struct {
    Windows map[xproto.Window]WindowState
    Active  xproto.Window
}
```

After model or surface changes:

1. compute `desired XState`;
2. compare it to `applied XState`;
3. produce a request plan;
4. issue requests in an order that preserves focus/stacking/map invariants;
5. update applied state after successful enqueue/check policy.

Benefits:

- moving without resizing does not repaint;
- unchanged clients receive no configure request;
- workspace switches can map/focus before unmapping underlying windows where required;
- bars and portals can update stacking independently of layout;
- request counts become testable without a live X server;
- batch boundaries and flushes are intentional.

A pure `DiffXState(desired, applied) []XRequest` package can receive extensive table tests.

## 17. Rendering architecture after resize

### 17.1 Classify surface species

Do not force every visible thing through one full-frame bitmap model.

| Surface species | Content owner | Recommended rendering strategy |
|---|---|---|
| External tiled client | Client interior; WM decorations | Separate decoration surfaces; no full-interior WM bitmap |
| External float | Client interior; WM decorations | Same, with float grip and shadow/border policy |
| Builtin tile | WM | Retained scene, dirty regions, cached layers |
| Script tile | JS data; WM renderer | Retained scene snapshots, dirty regions, host widgets |
| Bar/taskbar | WM/JS data | Persistent surface, row-level or item-level damage |
| Menu/popover/modal | WM/JS data | Short-lived portal surface; retained while open |
| Preview/drag overlay | WM | Tiny reusable overlay windows or compositor layer |
| Standalone PBUI app | App host | Same retained renderer, independent X shell |

### 17.2 Decoration-only frame design

There are several X implementation options. Measure before choosing, but keep the semantic target clear.

**Option A: multiple decoration windows.** Title, left/right/bottom borders are child or sibling windows around the client. Each is small and independently resized/painted. Input regions are natural X windows.

**Option B: shaped decoration frame.** A frame surface covers only decoration areas using Shape/XFixes input regions. More extension complexity.

**Option C: one frame window with server-side fills and a narrow title pixmap.** Avoid a full `image.RGBA`; use X rectangles for borders/background and an image only for the title strip. This may be the best first step because the current drawing stack already produces title images.

The choice should be driven by an experiment comparing X request count, resource churn, code complexity, and visual correctness under compositors.

### 17.3 Retained layers for WM surfaces

A WM-owned pane can be decomposed into layers:

1. static background;
2. title/chrome;
3. widget content;
4. focus/accept highlight;
5. transient hover/selection overlay.

A focus change should not rerender a table or chart. An accept-mode highlight should not rerun application JavaScript. A blinking field cursor should not rebuild a large scene.

Maintain dirty flags and rectangles:

```go
type Damage struct {
    Layout bool
    Paint  bool
    Rects  []image.Rectangle
}
```

For XSHM surfaces, add rectangular conversion and clear operations only after the retained scene can report honest dirty rectangles. Partial upload without semantic damage tracking merely moves complexity.

### 17.4 Grow-only buffers and pools

If exact-size frame buffers remain in some paths, reduce allocator churn by separating capacity from logical bounds:

- retain a CPU backing allocation at least as large as the current logical surface;
- grow geometrically when capacity is insufficient;
- clear only logical/dirty regions;
- pool small common overlay and title-strip buffers;
- avoid retaining screen-sized buffers for hidden workspaces.

For X pixmaps and SHM shared pixmaps, oversized reuse is more constrained because drawable dimensions and background semantics matter. Do not assume a larger pixmap can transparently substitute for an exact window-sized one. Prototype and verify with pixel tests before adopting a size-class pool.

### 17.5 Do not render arbitrary JS pixels in the hot path

A general canvas API is attractive but creates three problems:

- unbounded script execution during rendering;
- no semantic presentation regions unless separately reconstructed;
- poor opportunities for diffing, virtualization, and accessibility.

Prefer host-rendered data widgets and a narrow image node for already-produced media. A chart specification should describe marks, scales, series, and presentation payloads. The host decides detail level, hit testing, and damage.

## 18. Input and event scheduling

### 18.1 Input priority

The WM loop should process work in priority order:

1. pointer/key/button events needed to maintain direct manipulation;
2. focus and client lifecycle events;
3. completion of already-started host calls;
4. scheduled render frame;
5. authoritative model events;
6. script telemetry and low-priority refreshes.

This does not require a complex real-time scheduler. It requires avoiding long synchronous work in event handlers and making queued work visible.

### 18.2 WM event outbox

Replace:

```go
go func() { _ = broker.Emit(ctx, event, data) }()
```

with one bounded structure:

```go
type EventOutbox struct {
    q       *boundedQueue[Event]
    dropped atomic.Uint64
}

func (w *WM) emitEvent(ev Event) {
    if !w.outbox.TryPush(ev) {
        w.metrics.EventDrops.Add(1)
    }
}
```

Events can have a coalescing key. Pointer previews, hover docs, and focus telemetry can replace older pending events. Committed operations and lifecycle events should not be silently replaced; if their reliable queue is full, the system should surface a health fault and require subscriber resync.

### 18.3 JavaScript budgets

A Goja owner is single-threaded by design. One callback can still monopolize it. Add:

- enqueue timestamp and start/end duration for every owner task;
- queue depth and oldest-task age;
- warning events for long callbacks;
- optional cooperative yield primitives for script loops;
- deadlines/interrupts for REPL cells and selected event handlers;
- per-runtime restart/quarantine policy after repeated fatal errors.

Do not kill arbitrary system rc callbacks merely because they exceed 16 ms. Start with observability and explicit interruptible job types. The supervisor section defines the policy boundary.

## 19. Performance validation plan

### 19.1 Benchmarks

Create named, repeatable scenarios:

- `resize-preview-10s`: rapid divider sweeps with two fast clients.
- `resize-slow-client`: one client delays 40 ms per configure.
- `resize-four-pane`: nested split where only an ancestor path should change.
- `resize-script-dashboard`: tables and plots in WM-rendered tiles.
- `focus-storm`: alternate tiled/float/fullscreen focus repeatedly.
- `workspace-batch`: create, name, and populate N workspaces atomically.
- `events-10k`: publish an event burst and verify bounded memory/drop reporting.
- `repl-long-session`: evaluate and discard thousands of cells under a retention policy.

### 19.2 Assertions beyond wall time

Wall time alone can hide regressions. Record:

- number of model mutations per gesture;
- number of layouts computed;
- X configure/map/unmap/focus requests;
- pixmaps/SHM segments created and destroyed;
- bytes rasterized, converted, and uploaded;
- client configure acknowledgements;
- WM-loop max task duration;
- Go allocations and GC pause time;
- JavaScript owner queue depth;
- event drops and resyncs.

### 19.3 Shipping gate for resize work

A resize patch should not merge until:

1. unit tests cover preview ratio, snap, cancellation, final commit, and no model mutation during preview;
2. X request tests prove no client configure during preview mode;
3. scripted smoke tests compare final geometry to the old path;
4. slow-client tests show pointer preview remains responsive;
5. pprof and stage metrics are archived before and after;
6. fallback without MIT-SHM remains correct;
7. fullscreen, float, workspace switch, and accept highlighting remain correct;
8. no SHM segments or pixmaps leak on normal shutdown or crash-oriented tests.

---
# Part IV. JavaScript and PBUI as an operating-system substrate

## 20. The three-plane architecture

The system becomes easier to reason about when responsibilities are organized into three planes.

### 20.1 Mechanism plane

The mechanism plane owns facts that must remain correct even when every script is stopped:

- X11 selection and protocol compliance;
- client lifecycle, reparenting, geometry, focus, fullscreen, stacking, and EWMH;
- pure desktop model and validated operations;
- resource ownership and teardown;
- input normalization;
- drawing primitives, font shaping, image upload, and damage;
- runtime supervision and capability enforcement;
- durable identifiers, versions, and authoritative event sequence.

It is implemented in Go because it needs deterministic ownership, low-level protocol access, and a small trusted computing base.

### 20.2 Presentation plane

The presentation plane turns data into interactive semantic output:

- retained widget trees;
- layout and hit testing;
- presentation objects and output records;
- type registry, views, verbs, translators, and input contexts;
- surface and portal lifecycle;
- focus scopes, keyboard routing, accessibility descriptions;
- themes and style tokens;
- damage and render plans.

JavaScript can describe and update presentation trees, but Go validates, reconciles, lays out, and paints them.

### 20.3 Policy plane

The policy plane decides what the desktop should do:

- keybindings and modes;
- workspace conventions and project layouts;
- window rules;
- taskbar and menu composition;
- commands and automation;
- launch policy;
- domain applications and integrations;
- REPL/notebook sessions;
- user-defined type translators and verbs where permitted.

This is the natural home for JavaScript. Policy can be reloaded, inspected, and replaced without endangering X ownership.

![Figure: Target architecture](ggwm_assets/target_arch.png)

### 20.4 A strict dependency rule

Dependencies should point downward:

```text
policy → presentation → mechanism
```

The mechanism plane publishes events and host interfaces upward, but it never imports a particular bar, launcher, task manager, or user workflow. The presentation plane knows how to render a command entry, not which commands the user has. The policy plane composes those mechanisms.

This rule makes “replace the taskbar with a REPL-generated one” a configuration change rather than a WM fork.

## 21. Runtime actors and supervision

### 21.1 Why one rc runtime is not enough

A single trusted `rc.js` is an effective bootstrap. It becomes fragile when it owns unrelated services:

- keybindings;
- bar rendering;
- launcher extensions;
- filesystem watchers;
- network integrations;
- long-running automation;
- application widgets;
- experimental REPL code.

These services have different permissions, restart policies, performance budgets, and state. Sharing one VM means one unhandled error, runaway loop, provider-state bug, or reload can disturb all of them.

The target is a set of supervised runtime actors. Each actor owns one Goja runtime and one mailbox. It receives host calls and events through runtime services, and it owns leases for every registration or surface it creates.

### 21.2 Runtime record

```go
type RuntimeID string

type RuntimeRecord struct {
    ID          RuntimeID          // unique incarnation
    AppID       string             // stable logical identity
    Profile     RuntimeProfile
    State       RuntimeState
    Manifest    CapabilityManifest
    Owner       *runtimeowner.RuntimeOwner
    Mailbox     *PriorityMailbox
    StartedAt   time.Time
    Generation  uint64
    Restart     RestartPolicy
    Leases      LeaseSet
    Metrics     RuntimeMetrics
    Source      SourceDescriptor
}
```

`AppID` identifies “the top bar” across reloads. `RuntimeID` identifies one actual VM incarnation. Registrations use `RuntimeID` for exact ownership and may carry `AppID` for state migration and user-facing labels.

### 21.3 Lifecycle

![Figure: Runtime lifecycle](ggwm_assets/runtime_lifecycle.png)

The states should be explicit:

- **Defined:** manifest and source are known.
- **Starting:** runtime is constructed, modules installed, code loaded, and a first valid snapshot/registration set prepared.
- **Running:** events and host calls are accepted.
- **Draining:** new external work is refused while in-flight jobs settle or cancel.
- **Stopped:** leases and resources are released.
- **Failed:** startup or runtime fault is recorded.
- **Quarantined:** restart budget was exceeded; manual action or code change is required.

### 21.4 Lease ownership

Every side effect created by a runtime should be a lease:

- keybinding;
- event subscription;
- PBUI verb;
- presentation type descriptor;
- command registry entry;
- UI surface or portal;
- timer;
- file watcher;
- network listener;
- child process;
- retained object handle;
- status contribution.

A lease has an owner runtime and a `Close` operation. Runtime shutdown closes the complete set even if script cleanup code does not run.

```go
type Lease interface {
    ID() string
    Kind() string
    Close(context.Context) error
}
```

This is the difference between “hot reload usually works” and “hot reload has deterministic cleanup.”

### 21.5 Restart policy

System services can use supervised restart with backoff. User apps may stop after one failure. REPL cells should fail independently without restarting the session runtime unless the VM itself is corrupt or unresponsive.

```go
type RestartPolicy struct {
    Mode          string        // never, on-failure, always
    MaxRestarts   int
    Window        time.Duration
    BackoffMin    time.Duration
    BackoffMax    time.Duration
    PreserveState bool
}
```

A repeated startup failure should not create a tight loop that blocks the desktop. Quarantine the runtime, leave the previous generation active when safe, and surface a typed `runtime-failure` presentation in the bar/inspector/REPL.

### 21.6 Hard isolation boundary

A Goja runtime in the WM process can be scheduled and interrupted, but it is not a complete security process boundary. A script with powerful native modules can allocate memory, create goroutines indirectly, invoke expensive host operations, or exploit a host bug.

Use two trust classes:

- **In-process:** trusted system/user scripts with narrow module/capability sets and supervised owner loops.
- **Out-of-process:** untrusted extensions, network-facing agents, heavy computation, or code that requires hard CPU/memory/OS limits.

Both classes can expose the same PBUI and WM protocol. Placement changes; application code need not.

## 22. Capabilities and authority

### 22.1 Capabilities are more precise than module names

Module middleware in `go-go-goja` already supports safe/only/exclude policies. go-go-wm should add operation-level capabilities because one module can contain both read and write authority.

Recommended capability families:

| Capability | Example authority |
|---|---|
| `wm.read.tree` | Query workspaces and leaves. |
| `wm.read.windows` | Query managed clients and properties. |
| `wm.mutate.layout` | Split, close, move, set ratio, apply layout transactions. |
| `wm.mutate.focus` | Change focus and workspace. |
| `wm.mutate.client` | Close, float, fullscreen, move external clients. |
| `wm.bind.keys` | Register global keybindings in an allowed namespace. |
| `wm.spawn` | Launch a process through a constrained launcher. |
| `ui.surface.create` | Create tiles or standalone windows. |
| `ui.portal.create` | Create menus, modals, bars, notifications. |
| `pbui.publish` | Publish objects/output records. |
| `pbui.accept` | Open an input context. |
| `pbui.verb.register` | Register verbs for allowed ptypes. |
| `pbui.type.register` | Register type descriptors/translators. |
| `events.subscribe` | Subscribe to specified topics/filters. |
| `fs.read:<root>` | Read within a mounted root. |
| `fs.write:<root>` | Write within a mounted root. |
| `net.client:<scope>` | Connect to allowed hosts/services. |
| `process.manage:self` | Inspect/stop only child processes owned by the runtime. |
| `runtime.inspect:<scope>` | Inspect metrics/logs of another runtime. |

### 22.2 Manifest

```yaml
app_id: user.taskbar
profile: user-service
entry: ./taskbar.js
capabilities:
  - wm.read.tree
  - wm.read.windows
  - wm.mutate.focus
  - ui.portal.create:panel
  - pbui.publish
  - events.subscribe:window.*
  - events.subscribe:workspace.*
resources:
  max_event_queue: 512
  callback_warn_ms: 12
  max_surfaces: 2
restart:
  mode: on-failure
  max_restarts: 3
  window: 30s
```

The manifest is parsed before the runtime is created. Module loaders see the granted capability set and omit or wrap functions accordingly. Host methods also validate at call time; hiding a method is not enforcement by itself.

### 22.3 Scoped object capabilities

Some authority is best represented by an object handle rather than a global permission. `wm.workspace("dev")` can return a handle whose methods are limited to that workspace. A launcher command can receive a process-launch token with a fixed executable and argument schema instead of arbitrary shell access.

```js
const dev = wm.workspace("dev");          // handle scoped to one workspace
await dev.switch();
await dev.layout.apply("project", vars); // capability checked on handle creation and call
```

### 22.4 Permission interaction

The REPL should show a permission prompt when code requests authority beyond its notebook manifest. The prompt itself is a PBUI modal with a typed capability presentation, origin, requested duration, and exact operation. Decisions can be:

- deny;
- allow once;
- allow for this cell;
- allow for this session;
- update the notebook manifest after explicit confirmation.

System rc code should not produce interactive prompts during boot. Its manifest is provisioned as configuration.

## 23. Host-call semantics

### 23.1 Promise-first design

Host operations that can block, post to another loop, touch X, use IPC, or perform I/O should return Promises. Synchronous functions should be limited to pure local data transformations and immutable snapshot reads.

```js
const snapshot = wm.snapshot();       // local immutable cache, synchronous
await wm.focus("left");               // crosses to WM loop
const answer = await pbui.accept(...); // user interaction
```

### 23.2 Operation identity and eventual outcome

A timeout does not guarantee a posted operation never executed. Every cross-loop mutation should have an operation ID and an eventual completion record.

```go
type HostOperation struct {
    ID          string
    Runtime     RuntimeID
    SubmittedAt time.Time
    Deadline    time.Time
    State       OperationState
    Result      json.RawMessage
    Error       *StructuredError
}
```

Semantics:

- cancellation before the WM starts the operation prevents execution;
- cancellation after start may only cancel if the operation declares a safe cancellation point;
- caller timeout detaches waiting but does not erase the eventual record;
- `runtime.operations()` and the REPL can inspect the final outcome;
- idempotency keys prevent accidental duplicate retries.

### 23.3 Avoid owner-loop deadlocks

The `go-go-goja` owner guidance is important: do not execute a synchronous owner task that waits on work which must post back to the same owner. The host API should make this structurally difficult.

Bad shape:

```text
JS owner callback
  → synchronous Go function waits on worker
      → worker posts Promise settlement to JS owner
          → deadlock
```

Good shape:

```text
JS owner callback creates Promise and returns
  → worker runs
      → worker posts settlement to JS owner
```

The same rule applies between JavaScript and the WM loop. Never hold WM-owned state or block X dispatch while waiting for JavaScript.

## 24. Transactions and operations as first-class values

The existing operation vocabulary should become visible in the scripting model rather than hidden behind only convenience methods.

### 24.1 Transaction API

```js
const tx = wm.transaction({
  name: "open project",
  baseVersion: wm.snapshot().version,
});

tx.workspace("go-go-wm").ensure();
tx.workspace("go-go-wm").applyLayout("dev", {
  editor: "emacs",
  terminal: "kitty",
});
tx.focusWorkspace("go-go-wm");

const preview = await tx.preview();
// preview is a typed <wm-transaction-preview> presentation.
await tx.commit();
```

`preview()` validates all operations, computes the resulting tree and geometry, and returns a diff without mutating X. The REPL can render before/after trees, affected windows, required capabilities, and possible conflicts.

### 24.2 Concurrency

A transaction records a base desktop version. Commit policies:

- `strict`: fail if version changed;
- `rebase`: re-resolve symbolic selectors and validate against current state;
- `force`: privileged policy for explicit administrative use.

Avoid transactions that identify leaves only by volatile UI position. Prefer stable IDs or declarative selectors with a clear resolution result.

### 24.3 Undo

Not every WM operation is naturally reversible: closing an external client cannot be undone. Distinguish:

- **model undo:** reverse layout-only operations when the same clients still exist;
- **compensating action:** create a new workspace or restore a saved layout;
- **irreversible action:** close/kill/process execution, requiring explicit labeling.

Transaction previews should mark irreversible steps.

## 25. Sequenced events and state recovery

### 25.1 Event classes

| Class | Examples | Delivery contract |
|---|---|---|
| Authoritative state | window managed/unmanaged, op committed, workspace switched, runtime state | Global sequence; replay ring or durable log; resync supported. |
| Input/context | accept opened/answered, focus changed, command invoked | Ordered; usually bounded retention. |
| Coalescible UI | pointer preview, hover doc, resize preview, progress | Latest value per key. |
| Telemetry | paint duration, queue depth, callback duration | Sampled or aggregated; drops acceptable and counted. |
| Diagnostic | script error, invariant violation, resource leak warning | Retained until acknowledged or bounded by policy. |

### 25.2 Subscription API

```js
const sub = events.subscribe({
  topics: ["window.*", "workspace.*"],
  from: checkpoint.sequence,
  filter: { workspace: "dev" },
  delivery: "authoritative",
});

for await (const ev of sub) {
  // serialized on this runtime owner
}
```

The implementation can still dispatch callbacks, but an async-iterator model makes backpressure and cancellation explicit. A subscription has a lease and `close()`.

### 25.3 Snapshot plus cursor

A taskbar or automation daemon starts from a consistent pair:

```json
{
  "snapshot": {"desktop_version": 912, "windows": [...]},
  "event_cursor": 18422
}
```

The service renders the snapshot, then consumes events after the cursor. If events were evicted, it requests a new snapshot rather than silently continuing from an unknown state.

### 25.4 Event schemas

Use versioned data schemas, not arbitrary maps forever.

```go
type EventEnvelope struct {
    Seq       uint64
    Time      time.Time
    Type      string
    Version   uint16
    Source    string
    Runtime   RuntimeID
    Operation string
    Data      json.RawMessage
}
```

Generate TypeScript declarations for event payloads and host modules from the same descriptors used by validation and documentation.

## 26. PBUI object identity, types, and translators

### 26.1 Values versus handles

The current `pbui.Object` copies a JSON value. That is ideal for immutable scalars such as colors, numbers, paths, IDs, and small records. A live window, dataset, process, stream, or runtime should use an object handle with explicit lifetime and version.

```go
type ObjectRef struct {
    Ptype     string          `json:"ptype"`
    ObjectID  string          `json:"object_id"`
    Version   uint64          `json:"version"`
    Label     string          `json:"label,omitempty"`
    Doc       string          `json:"doc,omitempty"`
    Snapshot  json.RawMessage `json:"snapshot,omitempty"`
    Owner     string          `json:"owner,omitempty"`
}
```

Rules:

- scalar immutable values may omit `ObjectID` and travel by value;
- live objects have stable IDs and monotonic versions;
- a presentation can opt into a snapshot for disconnected display;
- invoking a verb resolves the current object through its owner/registry;
- stale versions are detectable;
- owner death changes the presentation to an unavailable snapshot rather than leaving a silent dangling reference.

### 26.2 Type descriptors

```go
type TypeDescriptor struct {
    Name        string
    Version     uint16
    Parents     []string
    Parameters  []ParameterSpec
    Schema      json.RawMessage
    DefaultView string
    Doc         string
}
```

Examples:

- `number` parent of `integer` and `percentage`;
- `path` parent of `file` and `directory`;
- `window` parent of `tiled-window` and `floating-window`;
- `sequence<T>` parameterized by element type;
- `dataset<row-schema>` parameterized by a schema descriptor.

Keep the first implementation modest. A directed acyclic parent graph plus parameter predicates covers most needs.

### 26.3 Compatibility

`accept("number")` should accept an `integer` directly. `accept({type:"file", readable:true})` should evaluate a predicate against type parameters or object metadata. Exact type equality remains a fast path.

```go
func Compatible(want Constraint, have ObjectRef, registry Registry) Match
```

`Match` can report direct compatibility, translator candidates, rejection reason, and confidence/priority.

### 26.4 Translators

A translator converts a source presentation into a target object under a gesture or command context.

Examples:

- `file → text` by reading a bounded preview;
- `window → workspace` by returning its current workspace;
- `git-commit → URL` for a configured forge;
- `dataset → selection<row>` through an interactive row picker;
- `command → process` by launching it, when permitted.

```go
type Translator struct {
    ID        string
    From      Constraint
    To        Constraint
    Gesture   GestureSpec
    Priority  int
    Capability string
    Owner     RuntimeID
}
```

A translator may be pure, asynchronous, or interactive. The accept session displays the translator being proposed and can require confirmation for side effects.

### 26.5 Accept state machine

![Figure: PBUI accept state](ggwm_assets/pbui_accept.png)

A complete accept session needs:

- session ID and owner runtime;
- requested constraints;
- prompt and mouse documentation;
- surface/workspace scope;
- direct-vs-translated policy;
- keyboard focus policy;
- timeout/deadline;
- nested parent session;
- status: requested, active, answered, cancelled, timed out, superseded;
- answer provenance: source presentation, translator chain, gesture, timestamp.

Nested input contexts matter. A verb can accept a color, then invoke a file picker, then return to the previous context. The surface manager and status line should render the active stack rather than one global nullable variable.

### 26.6 Presentation registry

A type descriptor alone does not know every view or command. Maintain registries for:

- concise label formatter;
- mouse documentation;
- inspector sections;
- named views;
- verbs;
- translators;
- completion providers;
- serializers/snapshotters;
- accessibility role/name/value;
- drag representations.

Registrations are leased and owner-scoped. Multiple providers can contribute views or verbs to one type, ordered by priority and user preference.

---
## 27. Retained widget trees

### 27.1 Why retained state is the next UI step

The current UI module proved that JavaScript can return declarative data and that Go can render it without entering the VM. Keep that boundary. Change the data from a flat list of rows into a retained tree with stable keys.

A retained tree provides four capabilities that are otherwise difficult to add independently:

1. **Reconciliation:** compare old and new descriptions and update only changed nodes.
2. **Lifecycle:** mount, update, unmount, and resource cleanup are explicit.
3. **Local host state:** focus, hover, scroll, expansion, cursor, selection, and measured size survive application rerenders.
4. **Damage:** the renderer knows whether a change affects layout, paint, or neither.

### 27.2 Do not reproduce a browser DOM

The desktop does not need HTML, CSS, browser event compatibility, or a large virtual-DOM framework. It needs a deterministic scene graph specialized for PBUI and desktop surfaces.

A minimal node record:

```go
type Node struct {
    Kind      string
    Key       string
    Props     json.RawMessage
    Children  []*Node
    HandlerID string
    Object    *pbui.ObjectRef
}
```

The host normalizer expands convenient JS syntax into this canonical form. Every sibling that may be reordered needs a stable key. Handler IDs refer to VM-owned callables stored in the runtime actor; render hosts never hold a `goja.Callable`.

### 27.3 Core node inventory

Start with nodes that cover the existing shell and the analytical sketches:

**Layout:** `Row`, `Column`, `Stack`, `Grid`, `Spacer`, `Separator`, `Padding`, `Align`, `Scroll`, `VirtualList`.

**Content:** `Text`, `RichText`, `Presentation`, `IconGlyph`, `Image`, `Table`, `Tree`, `Code`, `Markdown`.

**Input:** `Button`, `Toggle`, `Field`, `TextArea`, `ListBox`, `Slider`, `Tabs`, `Disclosure`.

**Data visualization:** `BarStrip`, `Sparkline`, `LinePlot`, `ScatterPlot`, `RadarPlot`, `CourtPlot` or a more general `Marks` node.

**Surface control:** `Portal`, `MenuAnchor`, `TooltipAnchor`, `ModalScope`, `FocusScope`.

**Diagnostics:** `ErrorBoundary`, `Loading`, `TraceTimeline`, `JsonViewer`, `RenderedJsonToggle`.

The textbook and basketball sketches suggest several immediately useful developer widgets: timeline traces, JSON/rendered toggles, invariant checklists, sortable tables, inline bars, scatter plots, trend lines, radar comparisons, watchlists, and inspector cards.

### 27.4 Data widgets preserve presentation semantics

A chart node should carry data and semantic object mappings:

```js
ui.scatter({
  key: "efficiency",
  x: { field: "usage", label: "USG%" },
  y: { field: "trueShooting", label: "TS%" },
  radius: { field: "points" },
  data: players,
  markObject(row) {
    return pbui.object("player", row.id, { label: row.name });
  },
  selection: model.focusedPlayer,
});
```

The host builds spatial indices and presentation regions. During an accept, matching marks can highlight without rerunning the script. A large plot can lower point detail or aggregate when zoomed out.

### 27.5 State split

State belongs in one of three places:

| State kind | Owner | Examples |
|---|---|---|
| Domain state | Application/runtime | selected player ID, watchlist contents, query result |
| View state with semantic meaning | Application or shared signal | chosen statistic, active comparison set |
| Ephemeral interaction state | Host widget instance | hover, scroll offset, field cursor, open disclosure, pressed state |

Do not force JavaScript to store every cursor blink or hover transition. Do not hide domain choices inside host widgets where scripts cannot inspect or persist them. The node API should make controlled vs uncontrolled state explicit.

### 27.6 Reconciliation algorithm

For each parent:

1. match old and new children by `(kind, key)`;
2. preserve host instance state for matches;
3. validate prop changes and compute invalidation flags;
4. mount new children;
5. unmount removed children and release resources;
6. reorder instances without remounting when keys remain;
7. propagate layout invalidation only as far as required.

Key mistakes should fail visibly in development mode. Duplicate keys or missing keys in a reorderable list should produce a structured UI error and retain the previous good tree.

### 27.7 Layout versus redraw

AwesomeWM's widget system exposes a useful distinction: a widget can signal that its geometry requirements changed or only that its pixels changed. Make this distinction first-class.

```go
type Invalidation uint8
const (
    InvalidateNone Invalidation = 0
    InvalidatePaint Invalidation = 1 << iota
    InvalidateLayout
    InvalidateHitMap
    InvalidateAccessibility
)
```

Examples:

- focus ring changed: paint only;
- label text changed: measure/layout and paint;
- row selection changed: paint and accessibility, not layout;
- list data appended below the viewport: model update, perhaps no immediate paint;
- scroll offset changed: paint/hit map; child measurements can remain cached.

### 27.8 Render pipeline

![Figure: Retained widget pipeline](ggwm_assets/widget_pipeline.png)

The pipeline is:

1. JS returns VNode data on its owner loop.
2. Host validates and normalizes against capability and schema rules.
3. Reconciler updates retained instances.
4. Layout computes geometry using constraints and cached measurements.
5. Painter emits a render plan and semantic hit map.
6. Damage compares old/new painted bounds and invalidation.
7. Renderer updates dirty rectangles and uploads them.
8. Input dispatch targets the retained path and posts handler IDs to JS.

Each stage exposes timing and counts.

### 27.9 Text and input are infrastructure

A credible OS-level widget system needs more than ASCII key names. Plan for:

- Unicode text events distinct from physical key events;
- compose sequences and input methods;
- grapheme-aware cursor movement and deletion;
- text selection and clipboard integration;
- multiline layout and scrolling;
- focus traversal and focus scopes;
- accelerators/mnemonics;
- accessibility role, name, value, and action metadata.

Global WM chords should be resolved before focused-surface text input, but the policy must be configurable for modes and modal surfaces. A `KeyEvent` should carry physical code, keysym, text, modifiers, repeat, and consumed state.

### 27.10 Scrolling and virtualization

Scrolling is not a decoration around rendering; it changes which nodes should exist and which presentation regions are active.

`VirtualList` should receive:

- item count;
- stable item key;
- estimated or measured height;
- renderer function or precomputed item VNodes;
- overscan;
- selection/focus model.

The host retains only visible and overscan instances. A 100,000-row dataset can remain a typed object with a table view without allocating 100,000 rectangles or JS nodes.

### 27.11 Error boundaries

An error in one custom widget should not replace an entire bar or desktop. `ErrorBoundary` retains the previous child snapshot or displays a typed error presentation with retry/reload verbs.

Errors should include:

- runtime and generation;
- component/node key path;
- source location/source map;
- handler or render phase;
- input event/operation ID;
- stack trace;
- previous good snapshot version.

## 28. Surface and portal manager

### 28.1 Why surfaces need one subsystem

Menus, context menus, tooltips, modals, launchers, bars, taskbars, notifications, and drag previews currently look like separate features. They share the same hard problems:

- X window creation and destruction;
- stacking layer;
- anchor and placement;
- monitor/workspace scope;
- focus transfer and restoration;
- outside-click and Escape dismissal;
- pointer and keyboard grabs;
- theme and scale;
- owner runtime lifecycle;
- PBUI presentations and accepts;
- reserved screen area/struts;
- animation or timing policy.

A portal manager turns those common problems into one mechanism.

### 28.2 Surface kinds

```go
type SurfaceKind string
const (
    SurfaceTile         SurfaceKind = "tile"
    SurfaceWindow       SurfaceKind = "window"
    SurfacePanel        SurfaceKind = "panel"
    SurfaceMenu         SurfaceKind = "menu"
    SurfacePopover      SurfaceKind = "popover"
    SurfaceTooltip      SurfaceKind = "tooltip"
    SurfaceModal        SurfaceKind = "modal"
    SurfaceNotification SurfaceKind = "notification"
    SurfacePalette      SurfaceKind = "command-palette"
    SurfaceOverlay      SurfaceKind = "overlay"
)
```

### 28.3 Descriptor

```go
type SurfaceSpec struct {
    ID           string
    Kind         SurfaceKind
    Owner        RuntimeID
    Workspace    string
    Monitor      string
    Layer        Layer
    Anchor       AnchorSpec
    Placement    PlacementSpec
    Size         SizeSpec
    Focus        FocusPolicy
    Dismiss      DismissPolicy
    Modal        bool
    ReservedEdge *StrutSpec
    SceneRoot    string
}
```

The spec is data. The manager resolves it into an X window, retained scene root, focus scope, and lease.

### 28.4 Panels and taskbars

A script-defined panel should not manually position an override-redirect window and separately teach the WM to avoid it. `ReservedEdge` updates the work area and, when interoperating with external tools, the relevant EWMH strut properties.

A taskbar is then ordinary policy:

```js
const panel = ui.mountSurface({
  id: "main-taskbar",
  kind: "panel",
  edge: "bottom",
  height: 26,
  reserve: true,
  workspace: "all",
}, () => Taskbar({
  windows: wm.windowsSignal(),
  workspaces: wm.workspacesSignal(),
}));
```

Window buttons are `<window>` presentations. Workspace chips are `<workspace>` presentations. Right-click menus come from type verbs plus panel-local actions.

### 28.5 Menus

A context menu should be assembled from:

- applicable PBUI verbs for the object and type hierarchy;
- surface-local actions;
- keyboard accelerators;
- user policy that hides, groups, or reorders commands;
- disabled reasons and capability checks;
- nested accepts or parameter prompts.

The menu is a retained portal, not a one-off pixel list. It can support search, submenus, keyboard navigation, documentation, and asynchronous enabled-state checks with deadlines.

### 28.6 Modals and focus scopes

A modal establishes:

- a focus scope with traversal contained inside it;
- an input barrier for underlying surfaces;
- explicit focus restoration target;
- owner runtime and cancellation semantics;
- nested-modal policy;
- accept-session integration.

A runtime crash closes its modal through lease cleanup and restores focus. A modal cannot leave the desktop with an invisible grab.

### 28.7 Notifications

Notifications should be typed objects with lifecycle and verbs, not only text bubbles:

```json
{
  "ptype": "notification",
  "object_id": "n-182",
  "snapshot": {
    "severity": "warning",
    "title": "Script callback exceeded 50 ms",
    "runtime": "user.taskbar#7"
  }
}
```

Verbs can inspect the runtime, open the trace, disable the subscription, or acknowledge the notification.

## 29. A JavaScript UI API that preserves host control

### 29.1 Design goals

The JavaScript API should be:

- declarative and serializable at the VM boundary;
- typed enough to generate TypeScript declarations and runtime validators;
- stable-keyed for reconciliation;
- explicit about controlled state and handlers;
- capability-checked;
- usable from plain JS and a JSX transform;
- small enough to learn without browser concepts.

### 29.2 Plain function form

```js
const ui = require("ui");
const pbui = require("pbui");

ui.defineComponent("WindowRow", ({ win, focused }) =>
  ui.row({ key: win.id, gap: 6 },
    ui.presentation({
      key: "window",
      object: pbui.ref("window", win.id, { label: win.title }),
      onActivate: "focus-window",
    }, ui.text(win.title, { bold: focused })),
    ui.spacer(),
    ui.text(win.class, { tone: "faint" }),
  )
);
```

### 29.3 JSX form

```jsx
function Taskbar({ model }) {
  return (
    <Row gap={6}>
      {model.workspaces.map(ws =>
        <Presentation key={ws.id} object={ws.ref} onActivate="switch-workspace">
          <Chip selected={ws.current}>{ws.name}</Chip>
        </Presentation>
      )}
      <Spacer />
      <VirtualList axis="horizontal" items={model.windows} itemKey={w => w.id}>
        {w => <WindowButton key={w.id} window={w} />}
      </VirtualList>
    </Row>
  );
}
```

JSX is only syntax. The output is the same validated node data.

### 29.4 Handler model

Handlers should be registered once and referred to by IDs in snapshots:

```js
const app = ui.app({
  id: "taskbar",
  handlers: {
    "focus-window": async ({ object }) => wm.window(object.objectId).focus(),
    "switch-workspace": async ({ object }) => wm.workspace(object.objectId).switch(),
  },
  render(model) { return Taskbar({ model }); },
});
```

The host event includes:

```ts
interface UIEvent {
  type: string;
  surfaceId: string;
  nodePath: string[];
  handlerId?: string;
  object?: PBUIObjectRef;
  local: {x: number; y: number};
  root: {x: number; y: number};
  button?: number;
  key?: KeyEvent;
  modifiers: string[];
  timestamp: number;
}
```

### 29.5 State and effects

Avoid implementing a large React-compatible hook system initially. Provide a small deterministic model:

- application state is ordinary JS objects/signals;
- `ui.signal(initial)` creates an owner-loop observable value;
- `ui.computed(fn)` derives values;
- `ui.effect(fn)` creates a leased effect with explicit dependencies or subscription handles;
- host widget state remains host-owned;
- every effect returns cleanup or owns leases tracked by the runtime.

```js
const selected = ui.signal(null);
const windows = wm.signal.windows();
const visible = ui.computed(() => windows.get().filter(w => !w.skipTaskbar));
```

Signals do not permit VM access from foreign goroutines. Host updates enqueue owner tasks; multiple updates can coalesce before render.

### 29.6 Render scheduling

A state change should mark the app dirty, not immediately rerender recursively. The runtime scheduler:

1. coalesces dirty signals;
2. runs at most one render task per turn/frame budget;
3. creates a new tree snapshot;
4. hands it to the host reconciler;
5. posts redraw only for affected surfaces.

A handler can await host operations; the last committed app state remains visible while it waits. Loading/progress is explicit application state.

### 29.7 Hot reload API

```js
export function saveState() {
  return { selected: selected.get(), filters: filters.get() };
}

export function restoreState(snapshot) {
  selected.set(snapshot?.selected ?? null);
  filters.set(snapshot?.filters ?? defaultFilters);
}
```

The supervisor loads the new generation in isolation, validates the first tree and registrations, restores bounded JSON state, then atomically swaps leases and scenes. The old generation drains after the new one is visible.

## 30. The rich REPL as the desktop shell

### 30.1 The REPL's role

A conventional shell starts processes and pipes text. A presentation-based desktop shell should additionally:

- query and mutate the live desktop through typed operations;
- display results through multiple semantic views;
- publish results as presentations usable by other commands;
- inspect runtimes, windows, operations, events, and widget trees;
- attach watchers and traces;
- preview and commit transactions;
- persist notebooks with provenance and permissions;
- hot-reload apps and system services;
- act as an editor for user-level OS policy.

The existing rich REPL already establishes the key principle: `Out[n]` is a live PBUI presentation.

### 30.2 Cell model

Extend the current cell record:

```go
type Cell struct {
    ID            string
    N             int
    Source        string
    SourceMap     *SourceMap
    Status        CellStatus
    SubmittedAt   time.Time
    StartedAt     time.Time
    EndedAt       time.Time
    CapabilityUse []CapabilityUse
    OperationIDs  []string
    Console       []LogRecord
    Result        *pbui.ObjectRef
    ResultMode    ResultMode // snapshot, live, pinned
    Views         []ViewRef
    SelectedView  string
    EventCursor   uint64
    Error         *StructuredError
    Metrics       CellMetrics
}
```

### 30.3 Cell lifecycle

![Figure: REPL cell lifecycle](ggwm_assets/repl_lifecycle.png)

A cell is a supervised job inside the REPL runtime:

1. Edit with Unicode, multiline support, history, and completion.
2. Compile/transform with source maps and capability preflight where possible.
3. Optionally preview a WM transaction or permission request.
4. Evaluate with deadline and interrupt handle.
5. Derive or accept a rich PBUI result.
6. Register live watchers if requested.
7. Persist source, metadata, snapshots, and cursors according to policy.
8. On error, retain logs, stack, trace, and retry/revise actions.

### 30.4 Interrupt and cancellation

The REPL cannot be the OS shell until a user can stop a cell. Requirements:

- every cell has a context and deadline;
- Goja interrupt is wired to an explicit Stop action;
- host operations inherit the cell operation context where appropriate;
- asynchronous resources created by the cell are leased to the cell or promoted explicitly to the session/runtime;
- interrupting a cell closes cell-scoped timers, subscriptions, and pending accepts;
- the VM remains usable after a normal interrupt; fatal owner failure triggers session recovery.

### 30.5 Completion

Completion should combine:

- JavaScript lexical/scope information from the REPL engine;
- generated declarations for native modules;
- PBUI type registry names and verb IDs;
- WM object handles and operation schemas;
- command registry entries;
- notebook symbols and `Out[n]`;
- user-defined app/component exports.

Completion results are themselves typed presentations with documentation, signature, origin, required capability, and insert text.

### 30.6 Inspection verbs

Every result should gain generic shell verbs when applicable:

- `inspect` — open structured inspector;
- `watch` — rerun or subscribe and update the cell;
- `pin` — retain live handle beyond normal history eviction;
- `snapshot` — convert a live handle to a bounded immutable value;
- `copy-as-input` — insert a reproducible expression;
- `publish` — expose the object on a named shelf/listener;
- `trace` — show operations/events contributing to the result;
- `profile` — rerun with CPU/allocation/host-call timing;
- `open-source` — jump to script/component definition;
- `grant/revoke` — inspect relevant authority.

Type-specific verbs from the global registry appear alongside these automatically.

### 30.7 Desktop tools as REPL values

Examples:

```js
wm.tree()                    // <wm-desktop> with outline and geometry views
wm.windows()                 // <window-set> with table and workspace views
runtime.list()               // <runtime-set> with health/queue/lease views
events.query({since: 18400}) // <event-stream-slice> with timeline and JSON views
ui.inspect("main-taskbar")  // <scene-tree> with layout, damage, and source views
profile.resize({seconds: 5}) // <performance-profile> with flamegraph and counters
```

The result view can contain live window, runtime, event, and node presentations. A developer can accept one into a later command without copying IDs.

### 30.8 Drag and accept into the editor

When a REPL command is waiting for a value, ordinary PBUI accept behavior applies. The editor can also accept a presentation as source:

- drop a `<window>` to insert `wm.window("...")`;
- drop a `<file>` to insert an escaped file-handle expression;
- drop an `Out[n]` result to insert `Out(n)`;
- drop a `<color>` to insert a literal or object reference according to user choice.

The insertion provider belongs to the type registry. This is a modern form of direct manipulation without converting the system back to strings.

### 30.9 Persistence and provenance

A notebook file should include:

- cell source and stable IDs;
- runtime/module versions;
- capability manifest;
- desktop/event version at evaluation;
- operation IDs;
- immutable result snapshots within configured size limits;
- references to live objects with owner and version;
- selected views and folded state;
- logs and structured errors;
- optional attachments/images stored by content hash.

On reopen, live references are resolved if possible and shown as stale/unavailable otherwise. Replaying side-effectful cells is never automatic without an explicit notebook policy and transaction preview.

### 30.10 Retention

Provide result classes:

- **ephemeral:** eligible for eviction after the cell leaves the retention window;
- **snapshot:** bounded JSON/image stored with the notebook;
- **live:** handle and subscription valid while owner lives;
- **pinned:** explicit user request; counts against a visible resource budget.

The REPL status bar should show retained bytes, live subscriptions, timers, and handles. Eviction should close cell-scoped leases.

### 30.11 Attach and debug

With permission, the REPL can attach to another runtime's diagnostics—not directly enter its VM concurrently. The supervisor exposes safe operations:

- list queued owner tasks and ages;
- inspect leases and subscriptions;
- read recent logs and structured errors;
- request a heap/CPU profile at host level;
- request application-exported state snapshot;
- trigger hot reload or restart;
- pause new events and drain;
- inspect the last rendered scene tree.

Direct arbitrary evaluation inside a system runtime should be a high-authority debugging capability and still execute through that runtime's owner.

## 31. Hot reload and state transfer

### 31.1 Two-generation reload

A safe reload sequence:

1. Detect code or manifest change.
2. Create generation `N+1` with a fresh runtime ID.
3. Load modules and code under the new manifest.
4. Ask generation `N` for a bounded JSON `saveState`, with a deadline.
5. Call `restoreState` in `N+1`.
6. Produce and validate initial scenes, bindings, commands, verbs, types, and subscriptions in a staging registry.
7. Atomically swap staging registrations and scene roots into active ownership.
8. Mark generation `N` draining; stop new events.
9. Cancel or settle in-flight work according to policy.
10. Close all old leases and runtime resources.

If steps 2-6 fail, keep generation `N` active and present the new error in the REPL/inspector.

### 31.2 State schema

State transfer must be JSON-like and versioned:

```js
export const stateVersion = 3;
export function saveState() { ... }
export function migrateState(oldVersion, value) { ... }
export function restoreState(value) { ... }
```

Do not attempt to serialize arbitrary closures, Promises, Go values, timers, or VM object graphs. Those are resources/leases and must be recreated.

### 31.3 Filesystem watching

Use the existing event-emitter/fswatch infrastructure through a supervisor-owned watcher. Debounce changes, coalesce paths, and ignore generated/output directories. A script runtime should not create a new OS watcher on every render.

### 31.4 Reload scopes

Support:

- reload one component module while retaining app runtime, for development;
- reload one app/runtime generation;
- reload all user policy runtimes;
- replace system policy generation;
- full WM restart only for mechanism changes.

The first form is convenient but more complex because module cache and closure state remain. Implement full runtime-generation reload first; add module-level HMR only after lifecycle semantics are solid.

## 32. Security and failure isolation

### 32.1 Threats to model

Even a single-user desktop should model:

- accidental infinite loops;
- unbounded event or render production;
- resource leaks on reload;
- scripts executing arbitrary shell commands;
- filesystem access beyond intent;
- network-facing integrations receiving hostile input;
- one runtime spoofing another's registration owner;
- stale object handles invoking actions on a changed object;
- broker clients emitting forged invocation events;
- malformed UI specs consuming excessive memory;
- expensive rich-value derivation over huge data.

### 32.2 Controls

- per-runtime identity authenticated by connection/host, not caller-provided strings alone;
- capability checks at host calls;
- schema and size bounds for all exported UI/value data;
- mailbox limits with per-class policies;
- deadlines and interruption for jobs that support it;
- lease cleanup on owner death;
- out-of-process profile for hard isolation;
- operation IDs and audit records;
- object owner/version validation;
- broker message authorization: only broker/WM can emit reserved invocation topics;
- bounded derivation and virtualization for data;
- no JS execution in render/X loops;
- health UI that makes drops, restarts, and quarantines visible.

### 32.3 Security is part of the UX

A capability failure should produce a useful typed error:

```text
<capability-denied>
runtime: user.taskbar#7
operation: wm.exec("curl ...")
required: wm.spawn
manifest: /home/user/.config/go-go-wm/apps/taskbar.yaml
```

The object menu can show “Inspect request,” “Allow once,” or “Edit manifest,” subject to policy. Silent denial and generic exceptions make a programmable OS difficult to debug.

## 33. Observability as a built-in application

The desktop should expose its own execution model through PBUI apps:

### Runtime inspector

- runtime state/generation/restart count;
- owner queue depth and oldest task;
- callback duration histogram;
- capabilities;
- leases grouped by kind;
- recent errors/logs;
- buttons/verbs: reload, drain, stop, restart, quarantine, open source.

### X state inspector

- managed clients and properties;
- desired versus applied geometry/map/stack/focus;
- pending sync-resize acknowledgements;
- X request counts;
- frame/pixmap/SHM resources;
- invariant violations.

### Scene inspector

- retained node tree with keys and bounds;
- component/source path;
- layout and paint invalidations;
- dirty rectangles;
- presentation object attached to each node;
- handler IDs and focus state;
- render/measure cost by subtree.

### Event timeline

- authoritative sequence;
- operation correlation;
- runtime deliveries and queue delays;
- dropped/coalesced events;
- accept and translator lifecycles;
- filters by window/workspace/runtime/gesture.

### Performance dashboard

- WM-loop latency;
- paint, conversion, upload, layout histograms;
- resize gesture summaries;
- client configure/sync statistics;
- Go memory/GC;
- runtime queue and callback metrics.

These views are ideal PBUI demonstrations: every runtime, event, window, node, and operation is itself a typed presentation that can be accepted into REPL commands.

---
# Part V. Implementation roadmap

## 34. Sequencing principles

The project is novel, but the implementation should proceed through narrow vertical slices. Each phase must end in a usable desktop and leave explicit metrics and invariants behind.

Use these sequencing rules:

1. Fix input responsiveness before adding more widgets.
2. Make ownership and lifecycle explicit before hot reload.
3. Add retained trees before partial rendering.
4. Add a portal manager before independently implementing bars, taskbars, modals, and advanced menus.
5. Add runtime identities and leases before allowing many independent system scripts.
6. Add object handles and versions before treating processes, windows, datasets, and runtimes as durable REPL values.
7. Add an authoritative event sequence before stateful JS services depend on event replay.
8. Keep every phase testable without JavaScript where possible, then add one end-to-end JS proof.

## 35. Phase 0: measurement and resize preview

**Goal:** make resizing feel immediate and establish a trustworthy performance baseline.

### Tasks

1. Add gesture IDs and stage timers to divider resize.
2. Add test clients: fast, slow, and optionally sync-aware.
3. Add pointer trace replay and X request counters.
4. Implement `DividerGesture` with committed and preview ratios.
5. Draw/move a lightweight preview divider and snap indicator.
6. Apply exactly one `set-ratio` operation on release.
7. Add `resize.mode = preview | adaptive-live` configuration, default preview.
8. Replace goroutine-per-event publication with a bounded outbox.
9. Archive before/after profiles and gesture traces.

### Acceptance criteria

- No desktop version change during preview motion.
- No client `ConfigureWindow` request during preview motion.
- No frame-sized RGBA/XSHM/ximage allocation during preview motion.
- Final geometry and snap behavior match current semantics.
- Pointer-to-preview p95 remains under one display frame in test environments.
- Slow clients do not degrade preview movement.

### Intern learning outcome

The contributor can explain X event ownership, drag state, committed versus transient state, and why event throttling differs from coalescing.

## 36. Phase 1: desired/applied X state

**Goal:** turn broad reconciliation into a testable state diff.

### Tasks

1. Define pure desired state for frame/client map, geometry, stacking, and focus.
2. Snapshot currently applied state.
3. Implement and test `DiffXState`.
4. Route relayout/workspace/focus/fullscreen through the diff plan incrementally.
5. Make synthetic configure notifications explicit in the plan.
6. Add request ordering tests for map/focus/unmap transitions.
7. Add counters by X request type.

### Acceptance criteria

- Existing focus/fullscreen/float regression suite remains green.
- Unchanged geometry produces no configure request.
- Moving without resizing produces no decoration reraster.
- Workspace switch request order is deterministic and tested.
- Debug query can show desired and applied state for any window.

## 37. Phase 2: decoration rendering split

**Goal:** stop painting external-client interiors in WM-owned full-pane bitmaps.

### Tasks

1. Prototype title-only pixmap plus server-side border fills.
2. Compare with multiple decoration windows under common compositors.
3. Choose the simpler measured approach.
4. Rework frame hit testing for title, grip, border, and client interior.
5. Retain full surfaces only for builtins/script tiles.
6. Add pixel/golden and X integration tests.
7. Verify fallback without XSHM and on remote X.

### Acceptance criteria

- External-client focus/title change touches decoration-sized pixels only.
- Live/adaptive resize has no full-pane WM conversion for external clients.
- No flicker or exposed stale background under tested compositors.
- Frame extents and client geometry remain ICCCM/EWMH correct.

## 38. Phase 3: runtime supervisor and capabilities

**Goal:** turn scripts into managed OS services.

### Tasks

1. Introduce `RuntimeRecord`, unique runtime IDs, stable app IDs, and states.
2. Make provider/module state per runtime.
3. Implement lease registry and automatic cleanup.
4. Move current rc, daemon, and REPL construction behind runtime profiles.
5. Add capability manifest parsing and operation-level checks.
6. Add mailbox metrics, callback timing, and structured errors.
7. Add stop/restart/quarantine commands and inspector surface.
8. Restrict reserved broker events and registration ownership.

### Acceptance criteria

- Two VMs cannot observe or overwrite each other's module state accidentally.
- Killing/reloading a runtime removes every binding, verb, command, subscription, surface, timer, and watcher it owns.
- Capability denial is structured and visible.
- A failing taskbar runtime can restart without restarting the WM or unrelated runtimes.
- Runtime inspector shows queue, leases, capabilities, errors, and generation.

## 39. Phase 4: retained scene core

**Goal:** replace flat `uispec` snapshots with keyed retained trees while preserving the current API through an adapter.

### Tasks

1. Define canonical node schema, stable keys, handler IDs, and validators.
2. Implement reconciler with mount/update/unmount and local instance state.
3. Implement constraint layout for current row/column/table/field/image needs.
4. Separate layout, paint, hit-map, and accessibility invalidation.
5. Adapt existing `uispec.Spec` into scene nodes so current demos continue to work.
6. Implement damage reporting and scene inspector.
7. Add error boundaries and previous-good-tree retention.
8. Generate TypeScript declarations.

### Acceptance criteria

- Existing JS color app, counter tile, launcher, and REPL render through the adapter.
- Reordering keyed rows preserves field/selection state.
- Focus change does not rerun application render or relayout unrelated content.
- Scene inspector displays bounds, keys, invalidations, handlers, and presentations.
- Duplicate/missing key diagnostics are actionable.

## 40. Phase 5: fields, scroll, virtualization, and data widgets

**Goal:** make the UI system sufficient for developer workbenches and the rich REPL.

### Tasks

1. Unicode/IME-aware text input and grapheme editing.
2. Multiline field/text area and selection/clipboard.
3. Scroll containers and wheel/page/key routing.
4. `VirtualList` and virtual table.
5. Host data widgets: bars, sparkline, line, scatter, radar/marks.
6. Semantic mark hit testing and accept highlighting.
7. Shared focus/selection signals.
8. Accessibility metadata and keyboard navigation.

### Acceptance criteria

- Basketball-style leaders table, shot/trend/scatter/radar workbench can be expressed without custom pixel JS.
- Every visible chart mark can be a typed presentation.
- 100,000-row virtual table has bounded scene instances and regions.
- REPL editor handles Unicode and multiline input.

## 41. Phase 6: portal manager and system UI in JavaScript

**Goal:** express launcher, menus, bars, taskbar, modals, and notifications through one surface mechanism.

### Tasks

1. Implement surface kinds, anchors, placement, layers, focus, dismissal, and leases.
2. Port current object menu and launcher popup to portal scenes.
3. Implement panels with work-area reservation/struts.
4. Build a default JS top/bottom bar and taskbar as dogfood.
5. Implement modal/focus scopes and permission prompts.
6. Implement notifications and health badges.
7. Add multi-monitor/workspace scope before relying on panels broadly.

### Acceptance criteria

- Current launcher and object menu behavior is preserved.
- Runtime crash cannot leave a pointer/keyboard grab or modal barrier.
- A JS taskbar can switch workspaces, focus windows, open type menus, and show runtime health.
- Removing/reloading the taskbar releases its panel and work-area reservation atomically.

## 42. Phase 7: PBUI type registry and durable object protocol

**Goal:** move from exact strings and copied values to a composable semantic object system.

### Tasks

1. Add type descriptors, parent graph, constraints, and compatibility queries.
2. Add immutable scalar values versus live object handles.
3. Add object owner/version/liveness resolution.
4. Add translator registry and accept provenance.
5. Add nested input contexts, timeouts, scopes, and supersession.
6. Add named views/inspectors/completion/drag insertion providers.
7. Add unavailable/stale presentation rendering.
8. Add TypeScript descriptors and registry inspector.

### Acceptance criteria

- `integer` directly satisfies `number` accepts.
- A configured translator can satisfy an accept and the UI displays the conversion.
- Closing an owner changes live presentations to explicit unavailable snapshots.
- Nested accepts cancel and restore focus correctly.
- Registry inspector can explain why a presentation matches or does not match an input context.

## 43. Phase 8: REPL shell, hot reload, and notebooks

**Goal:** make the rich REPL the operational shell for the programmable desktop.

### Tasks

1. Wire cell contexts, deadlines, interrupt, and cell-scoped leases.
2. Add completion/signature help from generated declarations and registries.
3. Add transaction preview/commit views.
4. Add runtime attach/inspect/restart/reload commands.
5. Add notebook save/load with provenance and capability manifests.
6. Add live/snapshot/pinned result retention.
7. Implement two-generation runtime hot reload with state transfer.
8. Add profile/trace/watch/publish generic REPL verbs.
9. Add drag/accept insertion into the editor.

### Acceptance criteria

- A user can create and hot-reload a taskbar from the REPL without restarting the WM.
- A cell can preview and commit a project-layout transaction.
- Interrupting a cell closes its pending accept/subscriptions/timers and leaves the session usable.
- Reopened notebooks show reproducible snapshots and explicit stale live handles.
- Runtime and scene errors are navigable to source.

## 44. Proposed backlog tickets

A practical ticket breakdown:

| Ticket | Title | Depends on |
|---|---|---|
| GGWM-012 | Divider gesture preview and latest-motion scheduler | current main |
| GGWM-013 | Resize telemetry, slow/sync test clients, regression harness | GGWM-012 in parallel |
| GGWM-014 | Bounded authoritative event outbox and sequence | current broker |
| GGWM-015 | Desired/applied X state diff | GGWM-012 |
| GGWM-016 | Decoration-only rendering experiment and implementation | GGWM-015 |
| GGWM-017 | Runtime supervisor, identities, leases | current go-go-goja owner APIs |
| GGWM-018 | Capability manifests and host-call audit | GGWM-017 |
| GGWM-019 | Retained scene schema and reconciler | current `uispec` adapter |
| GGWM-020 | Layout/paint invalidation and scene inspector | GGWM-019 |
| GGWM-021 | Unicode fields, scrolling, virtualization | GGWM-019 |
| GGWM-022 | PBUI data-widget pack | GGWM-019/021 |
| GGWM-023 | Portal/surface manager | GGWM-019, GGWM-015 |
| GGWM-024 | JS bar/taskbar dogfood | GGWM-023, GGWM-017 |
| GGWM-025 | PBUI type descriptors and object handles | GGWM-014/017 |
| GGWM-026 | Translators and nested accept contexts | GGWM-025, GGWM-023 |
| GGWM-027 | REPL interrupt, completion, and cell leases | GGWM-017/021 |
| GGWM-028 | Transaction preview and notebook persistence | GGWM-014/025/027 |
| GGWM-029 | Two-generation hot reload and state migration | GGWM-017/019 |

Each ticket should include: purpose, current trace, invariants, design decisions, implementation phases, tests, observability, and a “what this deliberately does not solve” section.

## 45. Testing strategy

### 45.1 Test pyramid

**Pure unit tests** should dominate:

- tree operations, layout, neighbor selection;
- change summaries and transactions;
- X desired-state computation and diff;
- type compatibility and translator selection;
- retained reconciliation and invalidation;
- portal placement;
- capability checks;
- event queue/coalescing/replay;
- rich-value derivation and retention.

**Golden tests** cover:

- titles, borders, focus/accept states;
- widgets and charts;
- menus, bars, modals, notifications;
- error/unavailable presentations;
- scenes under themes and scale factors.

**X integration tests** cover:

- map/reparent/configure/unmap lifecycle;
- focus and fullscreen ordering;
- float/transient classification;
- EWMH state/struts/sync-resize;
- portal stacking and grabs;
- SHM and fallback paths.

**End-to-end tests** prove one user story each:

- drag preview and final commit;
- JS app presents an object and answers cross-process accept;
- runtime reload swaps a taskbar without losing work area;
- REPL cell previews a transaction and commits;
- owner crash cleans up all leases;
- notebook reload shows stale/live results accurately.

### 45.2 Model-based tests

Build a small abstract desktop model and generate command sequences. Compare:

- model state;
- `wmcore` state;
- serialized/replayed state;
- desired X state;
- observed debug query state after integration execution.

Focus/fullscreen/float bugs often emerge from combinations rather than one command. Stateful property tests are appropriate.

### 45.3 Fault injection

Add controlled failures:

- broker disconnect during accept;
- runtime death with open modal and keybinding;
- XSHM allocation failure;
- renderer returns invalid node or oversized image;
- event queue overflow;
- host call times out before start and after start;
- hot-reload `saveState`, `restoreState`, or first render throws;
- client destroys itself during configure;
- sync-resize client never acknowledges;
- compositor appears/disappears if that path is supported.

The expected result must be explicit: cleanup, fallback, quarantine, resync, or user-visible error.

### 45.4 Manual exploratory checklist

Automated tests cannot fully judge interaction. Keep a short repeatable checklist:

- Resize rapidly across snap zones with terminal, browser, IDE, and WM-native tiles.
- Cancel drag and verify unchanged tree.
- Switch workspaces while menus/accepts/modals are open.
- Open/close fullscreen tiled and floating clients; verify focus restoration.
- Reload bar/taskbar and inspect resource counts.
- Create two independent JS apps with identical module names but different runtime IDs.
- Flood events and verify desktop input remains responsive.
- Use REPL to accept objects from another workspace and from a chart mark.
- Interrupt long REPL code and then run a normal cell.
- Kill a runtime owning a menu/modal and verify grabs/focus recover.

## 46. Documentation and generated contracts

The codebase already benefits from detailed ticket documents. Reduce drift by generating reference material:

- TypeScript declarations for `wm`, `pbui`, `ui`, `runtime`, `events`, and launcher modules;
- JSON schemas for operations, events, node specs, manifests, type descriptors, and notebook files;
- capability reference from the enforcement descriptors;
- event catalog from payload types;
- widget catalog with state/invalidation/accessibility properties;
- REPL completion metadata from the same descriptors;
- protocol version compatibility tables.

Narrative guides should explain why and workflows; generated references should enumerate exact fields.

---

# Part VI. Intern curriculum and implementation playbooks

## 47. First-week reading path

A new contributor should not begin by changing `handleMotion` or adding a widget. Use this order:

1. Read `pkg/wmcore/tree.go`, `layout.go`, and `ops.go`; run pure tests.
2. Trace one external window from `MapRequest` to frame creation, focus, and teardown.
3. Trace one `wm.apply` call from JS through runtime owner, WM post, `wmcore.Apply`, X reconciliation, event publication, and Promise settlement.
4. Trace one PBUI accept from request through broker, region click, answer, and Promise resolution.
5. Trace one `ui.app` action through handler dispatch, rerender, snapshot swap, host repaint, and region update.
6. Trace one REPL result through evaluation, derivation/`__pbui__`, view rendering, and PBUI presentation.
7. Run current smoke scripts under Xvfb/Xephyr and inspect logs/profiles.
8. Read the focus/fullscreen regression tests to see how display-free decisions protect protocol code.

## 48. Learning lab 1: pure layout and operations

**Objective:** understand persistent tree mutation and geometry.

Tasks:

- construct nested row/column trees;
- calculate layouts at several screen sizes;
- apply split, close, move, and ratio operations;
- serialize and replay;
- add a property test for no overlap and complete area accounting minus divider gaps;
- implement a non-mutating preview ratio override.

Evidence panel:

- before/after tree JSON;
- operation stream;
- generated rectangles;
- invariant checklist.

## 49. Learning lab 2: one X client lifecycle

**Objective:** understand why protocol ordering matters.

Tasks:

- run a controlled test client;
- record MapRequest, property reads, frame creation, reparent, configure, map, focus, synthetic configure, unmap, and destroy;
- draw a sequence diagram from logs;
- change one ordering under a test flag and observe the failure;
- restore and encode the expected order in a test or assertion.

Key question: which state is authoritative at each step—client request, WM model, or applied X state?

## 50. Learning lab 3: resize profiling

**Objective:** distinguish input scheduling, WM paint, X resource, and client repaint costs.

Tasks:

- replay the same pointer trace against fast and slow clients;
- disable SHM and compare;
- record allocations, pixmap/SHM creation, configure request count, and loop latency;
- implement preview-only mode;
- prove that the preview produces zero client configure requests;
- compare final geometry.

The report should state which costs disappeared and which remain after release.

## 51. Learning lab 4: PBUI accept and translator

**Objective:** understand semantic input.

Tasks:

- create a JS app that presents `integer` values;
- add `integer` as a child of `number` in a test registry;
- open `accept("number")` from the REPL and answer with an integer mark in a chart;
- add a pure translator `integer → percentage`;
- display provenance in the answer inspector;
- cancel a nested accept and verify the parent context resumes.

## 52. Learning lab 5: retained widget reconciliation

**Objective:** preserve host state and minimize invalidation.

Tasks:

- implement keyed `Row`, `Text`, `Button`, and `Field` nodes;
- type text in a field;
- rerender with siblings reordered;
- prove cursor/text state remains attached to the keyed field;
- change only focus and prove layout is not recomputed;
- remove the field and prove unmount cleanup runs once.

Evidence:

- old/new trees;
- reconciliation operations;
- invalidation flags;
- rendered before/after images;
- lifecycle trace.

## 53. Learning lab 6: supervised JS service

**Objective:** understand runtime ownership and leases.

Tasks:

- create a runtime that registers one keybinding, one command, one event subscription, and one panel;
- list its leases;
- trigger an intentional exception;
- observe restart policy;
- reload with state transfer;
- kill the runtime and prove all leases disappear and focus/work area recover.

## 54. Learning lab 7: REPL transaction

**Objective:** use the REPL as an OS shell rather than a debug console.

Tasks:

- query windows and workspace tree as rich values;
- select a window presentation from a table;
- build a layout transaction;
- render a preview diff;
- commit with a strict base version;
- provoke a version conflict and rebase;
- persist the notebook and reopen it with snapshots and provenance.

## 55. Code-review checklist

### WM/X11 change

- What invariant changes?
- Is policy in a pure decision function?
- What is desired state, and what X requests are emitted?
- Are unchanged windows skipped?
- Are map/focus/unmap and stacking order tested?
- Does tiled/floating/fullscreen ownership remain unambiguous?
- Is cleanup complete on destroy, runtime failure, and shutdown?
- Are metrics and trace correlation present?

### JavaScript/native-module change

- Which runtime owns each callable and resource?
- Can any foreign goroutine touch the VM?
- Is the API synchronous only when truly local and nonblocking?
- Which context and cancellation semantics apply?
- Which capability is enforced?
- What happens if the caller times out but execution continues?
- Are module/provider states per runtime?
- Are callbacks, subscriptions, and resources leased and removable?

### Widget/surface change

- Does every reorderable node have a stable key?
- Which changes invalidate layout, paint, hit map, and accessibility?
- Is ephemeral state host-owned and domain state script-owned?
- Can data volume be bounded or virtualized?
- Are semantic marks presentations?
- Are focus, keyboard, dismissal, and owner death handled?
- Can the component render without calling JS from the host paint loop?

### PBUI change

- Is the object immutable by value or live by handle?
- What are its owner, version, and stale behavior?
- How does type compatibility work?
- Are verbs/translators capability-checked and owner-scoped?
- Can an accept be cancelled, timed out, nested, or superseded?
- Is answer provenance recorded?

### REPL change

- Can the cell be interrupted?
- Which resources are cell-scoped versus session-scoped?
- Is output bounded and retention explicit?
- Is the operation reproducible or clearly side-effectful?
- Are source maps, capability use, operations, and event cursor recorded?
- Does failure leave the session usable?

---

# Appendix A. Package map

| Package/path | Responsibility | Architectural direction |
|---|---|---|
| `pkg/wmcore` | Pure desktop model, layout, operations, neighbors | Add versions, change sets, transactions, preview overrides. |
| `pkg/wmx11` | X11 shell, clients, focus, fullscreen, floats, input, rendering | Add desired/applied X state, gesture scheduler, decoration split, portal integration. |
| `pkg/draw` | Software drawing, themes, images, plots | Become renderer backend for retained plans; add dirty-rect operations. |
| `pkg/apps` | Pure surface render + presentation/action regions | Preserve click contract; adapt regions from retained semantic nodes. |
| `pkg/apps/uispec` | Flat declarative UI IR | Maintain compatibility adapter; supersede with keyed scene schema. |
| `pkg/pbui` | Objects, verbs, wire types | Add object refs, versions, type descriptors, translators, accept state. |
| `pkg/pbui/broker` | Routing and ownership | Add authorization, sequences, replay/resync classes, lease integration. |
| `pkg/jsmod` | Shared JS bridges, queues, event fan | Add per-runtime instances, priority mailbox, unsubscribe, metrics. |
| `pkg/jsmod/wmmod` | WM JS API and sugar | Promise-first operations, transactions, handles, generated declarations. |
| `pkg/jsmod/pbuimod` | PBUI JS API | Object refs, nested accepts, registry queries, provenance. |
| `pkg/jsmod/uimod` | Script-defined apps and snapshots | Retained trees, handler IDs, signals, surfaces, hot reload. |
| `pkg/xgojaprovider` | Runtime module provider | Remove shared runtime state; construct runtime-scoped services. |
| `pkg/repl` | Rich value derivation/session model | Add cell contexts, retention, provenance, generated completion. |
| `pkg/cmds/replui.go` | Standalone rich REPL UI | Migrate to retained editor/widgets; share shell model with tile host. |
| `pkg/launcher` | Command registry, match, frecency | Keep pure; expose command presentations and portal surface. |
| `pkg/xshm` | Shared-pixmap upload | Keep as backend; add lifecycle metrics and validated format handling. |
| `go-go-goja/pkg/runtimeowner` | Goja owner scheduling/lifecycle | Use as base for runtime actors and context-aware host calls. |

# Appendix B. Proposed API sketches

## B.1 Runtime service

```js
const runtime = require("runtime");

runtime.info();
runtime.capabilities();
runtime.leases();
runtime.onHealth(ev => ...);
await runtime.reload("user.taskbar");
await runtime.stop("app.weather");
```

## B.2 WM handles and transactions

```js
const wm = require("wm");

const snap = wm.snapshot();
const win = wm.window("0x03a00017");
await win.focus();

const tx = wm.transaction({ baseVersion: snap.version });
tx.split(win.leaf(), "row", { app: "terminal" });
const preview = await tx.preview();
await tx.commit({ conflict: "strict" });
```

## B.3 PBUI registry

```js
const pbui = require("pbui");

pbui.defineType({
  name: "integer",
  parents: ["number"],
  schema: { type: "integer" },
});

pbui.registerView("integer", {
  id: "number.hex",
  label: "Hex",
  render(obj) { return ui.code("0x" + obj.value.toString(16)); },
});

pbui.registerTranslator({
  id: "integer.to-percentage",
  from: "integer",
  to: "percentage",
  translate(obj) { return pbui.object("percentage", obj.value); },
});
```

## B.4 Surfaces

```js
const ui = require("ui");

const handle = ui.mountSurface({
  id: "project-palette",
  kind: "command-palette",
  anchor: { monitor: "focused", gravity: "center" },
  focus: "exclusive",
  dismiss: ["escape", "outside-click", "owner-stop"],
}, () => ProjectPalette());

handle.close();
```

## B.5 REPL shell commands

```js
const p = await profile.resize({ duration: 5000 });
await pbui.publish(p, { shelf: "performance" });

const runtimeRef = await pbui.accept("runtime");
await runtime.attach(runtimeRef.objectId).openInspector();

const tx = wm.transaction();
tx.workspace("research").applyLayout("analysis");
await repl.preview(tx);
await tx.commit();
```

# Appendix C. Recommended architectural decisions

## ADR-1: Preview-only tiled resize is the default

**Decision:** interactive tiled resize moves a preview divider and commits geometry on release. Adaptive live resize is optional.

**Reason:** avoids repeated client relayout, exact-size surface churn, and stale-event processing while preserving direct feedback.

**Consequence:** content does not resize continuously by default. The preview must be clear and snap-aware.

## ADR-2: JavaScript never runs on the X or render loop

**Decision:** all VM access occurs through a runtime owner. Render hosts consume VM-free snapshots and dispatch handler IDs asynchronously.

**Reason:** preserves input responsiveness and avoids Goja concurrency violations.

**Consequence:** UI APIs are data-oriented; synchronous DOM-like callbacks during paint are unavailable.

## ADR-3: Runtime side effects are leases

**Decision:** every registration, surface, timer, watcher, process, and subscription created by a runtime is owned by a lease set.

**Reason:** deterministic reload, crash cleanup, and inspection.

**Consequence:** native modules must expose closeable resources and register them with the runtime.

## ADR-4: Retained keyed scenes supersede flat row specs

**Decision:** `uispec` remains a compatibility input but normalizes into a retained keyed scene.

**Reason:** local state, lifecycle, virtualization, and dirty-region rendering require identity beyond flattened rows.

**Consequence:** scripts must provide stable keys for reorderable children.

## ADR-5: PBUI distinguishes values from live handles

**Decision:** immutable scalars travel by JSON value; live entities use owner/versioned object references.

**Reason:** windows, runtimes, datasets, and streams require identity, liveness, stale detection, and owner failure behavior.

**Consequence:** verbs and views resolve current objects through registries; snapshot fallback is explicit.

## ADR-6: The REPL is an operational shell

**Decision:** the rich REPL receives interrupt, completion, capability, transaction, profiling, runtime-inspection, and persistence features.

**Reason:** the programmable desktop needs a first-class environment to create, inspect, debug, and persist policy.

**Consequence:** REPL cells are supervised jobs with provenance and resource ownership, not only calls to `Eval`.

## ADR-7: One portal manager owns shell overlays

**Decision:** menus, bars, taskbars, modals, popovers, notifications, launchers, and previews use one surface lifecycle/stacking/focus mechanism.

**Reason:** independently solving grabs, focus restoration, owner cleanup, placement, and layers creates inconsistent failure modes.

**Consequence:** current menu/launcher implementations migrate to the portal API before advanced variants are added.

# Appendix D. Glossary

**Accept session:** A PBUI input context requesting an object of a specified type or constraint.

**Applied X state:** The geometry, mapping, stacking, background, and focus state believed to have been sent to the X server.

**Capability:** Explicit authority granted to a runtime for a class of host operations or resources.

**Change set:** Structured summary of what a model operation changed and which reconciliation phases are required.

**Damage:** The region or semantic invalidation requiring repaint or upload.

**Desired X state:** The X-facing state computed from the authoritative desktop, clients, portals, and focus/fullscreen models.

**Lease:** A host resource owned by a runtime/cell that is automatically closed when the owner stops.

**Live object handle:** PBUI reference to an owner-resolved entity with stable ID, version, and liveness.

**Mechanism plane:** Go subsystem that owns X protocol, authoritative models, resources, rendering, and enforcement.

**Owner loop:** The single serialized execution context allowed to access a Goja VM or, separately, the WM's X state.

**Policy plane:** Scriptable layer that composes keybindings, layouts, rules, commands, and user applications.

**Portal:** A shell-owned surface outside ordinary tile layout, such as a menu, panel, modal, tooltip, or notification.

**Presentation:** A visible semantic association between a region/node and a typed object.

**Presentation translator:** A registered conversion from a source presentation type/constraint to a target input type/constraint.

**Preview state:** Transient interaction state that affects feedback but is not committed to desktop history.

**Retained scene:** Host-owned tree of keyed UI instances that survives across script render descriptions.

**Runtime actor:** One supervised Goja VM, owner loop, mailbox, capability manifest, leases, and metrics.

**Snapshot:** Immutable VM-free data handed from a JavaScript owner to host-side rendering or persistence.

**Surface:** A host-visible UI root with placement, stacking, focus, dismissal, and owner lifecycle.

**Transaction:** Validated group of operations with a base version, preview, conflict policy, and commit result.

# Appendix E. Source trail

## Repository evidence

Primary files reviewed include:

- `pkg/wmcore/layout.go`, `tree.go`, `ops.go`, `neighbor.go`
- `pkg/wmx11/wm.go`, `manage.go`, `input.go`, `divider.go`, `float.go`, `fullscreen.go`, `focus_state.go`, `scripting.go`, `scripttiles.go`, `pbui.go`, `launcher.go`
- `pkg/apps/apps.go`, `pkg/apps/uispec/uispec.go`, `pkg/apps/xapp/xapp.go`
- `pkg/draw/widgets.go`, `ximage.go`, `plot.go`, `theme.go`
- `pkg/xshm/xshm.go`
- `pkg/pbui/object.go`, broker and client packages
- `pkg/jsmod/bridge.go`, `queue.go`, `eventfan.go`, `wmmod`, `pbuimod`, `uimod`
- `pkg/xgojaprovider/provider.go`
- `pkg/repl/session.go`, `derive.go`, `value.go`
- `pkg/cmds/rc.go`, `run.go`, `repl.go`, `replui.go`
- `examples/scripts/i3.js`, `js-colors.js`, `rc-tile.js`, `project-switcher.js`

The dated `GGWM-002` through `GGWM-011` workspaces were reviewed for design intent, implementation chronology, profiles, smoke tests, review findings, and explicitly deferred prototype limitations.

## Attached design evidence

- The original PBUI shell sketch defines typed live presentations, accept across tiles/workspaces, type-directed object menus, split-tree workspaces, and singleton app views.
- The basketball sketch demonstrates presentation semantics inside tables and analytical visualizations, shared focus across independent panes, and live watchlists.
- The textbook-authoring guidance requests foundational explanations, concrete code and traces, diagrams, and complete prose rather than analogies or vague summaries.

## External primary references

- i3 source: `src/resize.c`, `src/render.c`, `src/x.c`, and the i3 user/developer documentation at `https://github.com/i3/i3` and `https://i3wm.org/docs/`.
- ICCCM: X.Org Inter-Client Communication Conventions Manual, including configure-request and synthetic configure semantics.
- EWMH: freedesktop.org Extended Window Manager Hints, including active window, window types, struts, and `_NET_WM_SYNC_REQUEST`.
- McCLIM manual and CLIM documentation: presentation types, input contexts, accept, presentation translators, command tables, and output history.
- AwesomeWM widget documentation: declarative widget hierarchies and separate layout/redraw invalidation signals.
- Qtile command graph and shell documentation: addressable command objects for groups, layouts, windows, bars, widgets, screens, and core.
- herbstluftwm documentation: runtime configuration through an IPC command interface.
- `go-go-golems/go-go-goja`: runtime factory, runtime owner, runtime services, module middleware, contexts, async Promise settlement, and explicit lifecycle.

---

# Closing

The project already contains the core insight that makes it distinctive: desktop output is semantic, and commands can consume visible objects instead of forcing every interaction through text. The next engineering step is not to add more isolated demonstrations. It is to make that semantic layer durable and efficient enough to host the desktop itself.

The immediate performance fix is to model resizing as a gesture with preview and commit, not as a stream of committed full repaints. The immediate scripting fix is to model every script as a supervised runtime with identity, capabilities, leases, and metrics. The immediate UI architecture fix is to retain keyed semantic trees so the host can preserve state and calculate damage. The immediate REPL direction is to make cells interruptible, permissioned, inspectable operations over the live system.

With those foundations, custom bars, menus, modals, taskbars, analytical workbenches, and OS automation become compositions over one small set of mechanisms: operations, events, runtime actors, retained scenes, portals, presentations, accepts, verbs, and translators. That is the point at which go-go-wm stops being only a window manager with scripting and becomes a programmable presentation-based desktop.
