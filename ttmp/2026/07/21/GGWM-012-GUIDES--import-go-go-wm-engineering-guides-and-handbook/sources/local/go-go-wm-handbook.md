# Building go-go-wm

## X11 performance, interactive resizing, and a fully scriptable presentation-based UI

**Architecture review and implementation guide for new contributors**  
**Repository reviewed:** `go-go-golems/go-go-wm`, `main` at merge commit `5b73c9f37c97538f6767ecdc3ece4fb599932377`  
**Project-notes window reviewed:** 18-20 July 2026  
**Prepared:** 21 July 2026

---

## Scope and method

This document answers four questions:

1. How should a new developer reason about building an X11 window manager systematically?
2. What is already strong in go-go-wm, and where do its present implementation boundaries create correctness or performance risk?
3. Why is interactive resizing slow, and which changes are most likely to improve it substantially?
4. How should the scripting and rendering architecture evolve so JavaScript can define menus, bars, taskbars, modals, plots, tables, and arbitrary PBUI widgets without placing JavaScript or unbounded work on the X11 event loop?

The review combines five forms of evidence:

- A static review of the current Go implementation, especially `pkg/wmx11`, `pkg/xshm`, `pkg/draw`, `pkg/apps`, `pkg/apps/xapp`, `pkg/apps/uispec`, `pkg/jsmod`, and `pkg/wmcore`.
- The repository's project diaries and design documents for GGWM-001 through GGWM-011, including the existing CPU profiles and the implementation notes for the rendering and MIT-SHM work.
- The original self-contained React prototype and the basketball prototype supplied with this request. These are treated as semantic specifications, not merely mockups.
- Primary X11 sources: ICCCM, EWMH, the MIT-SHM and X Synchronization protocols, and XCB guidance.
- Primary implementation references from i3, AwesomeWM, and dwm.

The public PARC pages were not retrievable from this execution environment. The corresponding dated documents mirrored under `ttmp/2026/07/18`, `ttmp/2026/07/19`, and `ttmp/2026/07/20` in the repository were reviewed instead. The performance conclusions below are therefore a code-and-document review supported by the project's existing profiles; they are not presented as a new live profile captured on the author's machine.

---

# Executive findings

The central conclusion is direct: **the split-tree algorithm is not the principal reason resizing feels slow**. The current resize path turns each admitted pointer sample into too much work at too many layers. A divider drag can perform a durable tree mutation, a full layout traversal, divider reconciliation, frame geometry changes, full-frame software rasterization, MIT-SHM resource destruction and creation, and full-buffer pixel conversion. The resized PBUI client may then independently render and upload its entire window. The result is work amplification: one human gesture causes two immediate-mode full-surface pipelines to run at motion-event cadence.

The implementation already contains valuable first-order fixes. GGWM-005 and GGWM-006 changed pixel fills and conversion order, cached frame images, avoided redundant Expose renders, throttled drag work, repainted only changed frames, introduced shared pixmaps, and batched durable operations. The recorded CPU time fell substantially. Those changes should remain. The next gains require changing **what work exists**, not merely making the existing full-frame loop slightly faster.

The highest-value performance work is:

1. **Coalesce pointer motion by identity, not only by time.** Keep the latest coordinate in a one-slot mailbox and allow at most one resize update in flight. Stale events must be replaced, not processed later. i3 explicitly drains queued motion events and invokes its drag callback only with the newest sample.
2. **Separate preview state from committed model state.** During a drag, store a transient ratio override. Commit one `OpSetRatio` on release. Durable operation logs and broker events should describe committed state; sampled preview telemetry can be a separate stream when needed.
3. **Offer outline and adaptive resizing.** i3's graphical tiled resize moves a thin helper window while the pointer is down and changes the tree once on release. go-go-wm should support that low-cost mode and an adaptive mode that performs live resizing only while its frame budget and client synchronization permit.
4. **Stop painting a client frame as a full-window bitmap.** For a normal reparented client, the client covers nearly all of the frame. The WM should render a title-bar-sized surface and use X window backgrounds, border pixels, or thin child windows for the remaining decoration. A 1920 x 1080 frame should not require a 7.9 MiB decoration buffer merely to draw a 20-pixel title strip.
5. **Eliminate resize-time X resource churn.** A dimension change currently destroys and recreates full-size shared-memory resources. Client decorations should no longer need such resources; PBUI content surfaces should use retained capacity, pooling, double buffering, or deferred reallocation.
6. **Fix the managed-client `ConfigureRequest` path.** Denying a tiled client's requested geometry should not trigger a full relayout and repaint. The WM should answer with the correct synthetic `ConfigureNotify` required by ICCCM and leave rendering untouched unless model geometry changed.
7. **Repair the PBUI client shell.** `pkg/apps/xapp` currently rerenders, creates an X image, uploads it, and destroys it on every size notification. It needs size-event coalescing, cached upload resources, separated measure/layout/paint invalidation, and eventually retained layers and damage.

The UI-scripting conclusion is equally clear: **do not expose X11 drawing or synchronous JavaScript callbacks as the widget API**. Preserve the successful boundary already established by `uimod`: JavaScript owns state, composition, and handlers on its owner loop; Go owns normalization, layout, hit testing, rasterization, resource management, and X11 commits. Evolve the present flat row/segment format into a keyed, declarative, retained **Presentation Scene IR**. A presentation node should be able to wrap any visual mark, including a table cell, plot point, shot marker, swatch, text span, or composite widget. The host should diff snapshots, classify invalidation, retain clean layers, and deliver serialized events back to the JavaScript loop.

The recommended implementation sequence is intentionally conservative:

- **P0: observability and resize-event coalescing.** This provides trustworthy measurements and immediate responsiveness without changing rendering semantics.
- **P1: preview/commit separation, outline/adaptive resize, and ICCCM-correct ConfigureRequest handling.** This removes high-frequency durable work and supplies a robust fallback for slow clients.
- **P2: thin client decorations and resource-lifetime redesign.** This removes the largest avoidable pixel and SHM costs from the WM.
- **P3: cached/coalesced PBUI client rendering and a retained scene engine.** This fixes the other half of the resize feedback loop and creates the foundation for real widgets.
- **P4: generalized surfaces, capabilities, hot reload, and developer tools.** This enables custom bars, taskbars, menus, modals, and application-like scripted surfaces safely.

---

# Part I. Foundations: what an X11 window manager actually does

## 1. The window manager is an X client with exclusive responsibilities

An X11 window manager is not the display server. It is an ordinary X client that asks the server for a special class of events on the root window. The decisive request is selection of `SubstructureRedirect` on the root. Only one client can successfully select that mask at a time. Once selected, map and configure requests for top-level windows are redirected to the WM, which decides how those windows should be mapped, positioned, sized, framed, stacked, and focused.

That fact produces the first architectural rule for go-go-wm:

> X11 is an external state machine. The WM owns a model of intended desktop state and continuously reconciles X server state to that model.

The model is not optional. Without it, event handlers become a collection of local reactions: a MapRequest maps a window, a button changes a rectangle, a property event changes a label. That style becomes inconsistent as soon as workspaces, fullscreen, floating transients, scripted operations, or crash recovery are added. go-go-wm is correct to place the authoritative tiling model in `pkg/wmcore` and to make `pkg/wmx11` an adapter that reconciles windows to it.

A minimal reparenting lifecycle is:

1. The client creates a top-level window and asks the server to map it.
2. The server redirects the MapRequest to the WM.
3. The WM reads initial properties such as title, class, size hints, window type, and transient leader.
4. The WM creates a frame window, adds the client to the save set, reparents the client into the frame, and configures frame and child geometry.
5. The WM maps the frame and client, updates focus and EWMH state, and emits its own higher-level event.
6. DestroyNotify or UnmapNotify eventually tears the relationship down and updates the model.

Every step has failure modes. A client can disappear between property queries. It can unmap itself. It can request an unexpected stack mode. It can advertise a transient leader that no longer exists. Checked X requests can introduce round trips. A robust WM treats each operation as reconciliation against potentially changing server state rather than as a linear transaction that cannot be interrupted.

### 1.1 The save set is not cleanup trivia

When a reparenting WM takes ownership of a client, it should add that client to its save set before reparenting. If the WM exits unexpectedly, the X server can reparent the client back to an ancestor and preserve it instead of leaving the application stranded inside a dead frame. go-go-wm already follows this pattern. New frame types, including scripted host windows or floating wrappers, must use the same lifecycle discipline.

### 1.2 One owner for X-facing mutable state

`WM.Run` uses one goroutine to own the desktop, frame maps, focus state, drag state, surfaces, and X event dispatch. External goroutines post closures through `WM.Post`. This is the correct default. XCB itself can issue requests asynchronously, but the application-level maps and invariants are easier to reason about when one loop owns them.

The rule is stronger than “avoid data races.” It means:

- Every state transition has a total order.
- Input events and posted operations cannot interleave inside a mutator.
- Tests can reason about one operation stream.
- Focus, fullscreen, mapping, and geometry invariants can be centralized.
- The JavaScript runtime can use the same owner-loop discipline without either loop directly calling the other.

The current `focusState` and `fullscreenState` refactors are an example of the right response when a one-loop system still develops bugs: do not add locks; encapsulate the state machine and reduce the number of legal mutation points.

## 2. Requests, replies, events, and round trips

X11 performance is often misunderstood because an API call is not equivalent to completed server work. Most X requests are queued into the connection buffer and sent asynchronously. Calls that require a reply, checked-request error collection, or explicit synchronization can force the client to wait for the server. On a local Unix-domain connection, round trips are cheaper than on a remote display, but they are still serial dependencies and they still drain parallelism.

XCB was designed to make request pipelining explicit. A client can issue several requests, retain their cookies, and collect replies later. In a WM, that suggests a practical policy:

- Query properties at lifecycle boundaries, not during interactive rendering.
- Issue independent property requests together and collect their replies afterward.
- Avoid checked requests in a motion-event hot path unless the error must be handled immediately.
- Batch `ConfigureWindow`, map/unmap, stack, and property requests, then flush once after reconciliation.
- Never call a synchronization primitive merely to “make sure the screen updated” inside a drag loop.

A flush is not a server round trip. It sends buffered requests. A reply wait, `GetInputFocus`, `GetGeometry(...).Reply()`, a checked request that collects errors, or `Sync`-style operation can create a round trip. Instrument them separately.

### 2.1 Why queued motion events cause perceived lag

Assume the pointer produces 500 motion samples per second while the WM can complete only 40 expensive resize updates per second. A time gate that ignores samples arriving within 16 ms prevents some work, but it does not necessarily discard events already waiting in the X queue. If each accepted sample still takes 25 ms, the loop can remain behind the pointer. The user releases the button, yet the WM may continue applying stale coordinates before reaching the release event.

The correct rule is **latest state wins**. Pointer motion describes the current desired divider coordinate; intermediate coordinates are not durable commands. i3's drag loop polls all pending X events, retains only the latest MotionNotify, handles other event types, and invokes the drag callback once per drain. A one-slot mailbox produces the same semantics in go-go-wm without requiring a wholesale event-loop rewrite.

## 3. Reparenting geometry and the ownership of size

A reparenting WM creates two relevant rectangles:

- The frame rectangle in root coordinates.
- The client rectangle in frame-local coordinates.

In go-go-wm, a normal client is placed below a title strip and inside a border. The frame may be `W x H`, while the child is approximately:

```text
client.x = BorderW
client.y = TitleH
client.w = frame.w - 2 * BorderW
client.h = frame.h - TitleH - BorderW
```

The geometry ownership rule depends on window kind:

| Window kind | Who owns geometry? | ConfigureRequest policy |
|---|---|---|
| Tiled application | `wmcore` layout | Deny or translate request; report actual geometry correctly. |
| Floating application | WM floating state, constrained by client hints | Honor reasonable position/size requests unless another state, such as fullscreen, owns geometry. |
| Fullscreen window | Fullscreen state machine | Ignore conflicting client geometry until fullscreen exits. |
| Override-redirect popup | The creating client or WM surface manager | The normal WM management path does not redirect it. |
| Internal PBUI tile | `wmcore` layout | Host renders into the assigned content rectangle. |
| Bar/taskbar/dock | Surface manager and monitor work-area policy | Placement may reserve work area with EWMH struts. |

This table should become executable policy. A single `handleConfigureRequest` function should classify the target and delegate to a method that owns the relevant invariant. The recent fullscreen/focus work moved in this direction; geometry should receive the same treatment.

### 3.1 Synthetic ConfigureNotify is part of the contract

A tiled client may request a position or size that the WM refuses because the tree owns placement. ICCCM requires the WM to tell the client what geometry it actually has. When a request is denied or modified without a real server-generated notification that carries the required root-relative values, the WM sends a synthetic `ConfigureNotify` to the client.

Calling a full `relayout()` merely to “reassert” the current rectangle is both expensive and semantically indirect. The correct path is:

```go
func (w *WM) rejectTiledConfigure(f *frame, ev ConfigureRequestEvent) {
    // Handle stack-only requests separately if policy permits them.
    sendSyntheticConfigureNotify(
        client = f.client,
        rootX = f.rect.X + BorderW,
        rootY = f.rect.Y + TitleH,
        width = f.clientWidth(),
        height = f.clientHeight(),
        borderWidth = 0,
        aboveSibling = 0,
    )
}
```

No layout traversal, frame paint, buffer allocation, or SHM operation is needed. This change is small, improves compatibility, and prevents a misbehaving or resize-happy client from turning ConfigureRequests into WM-wide repaint storms.

## 4. ICCCM and EWMH are behavioral specifications

ICCCM describes the contracts between clients and the WM: lifecycle, size hints, focus models, transient relationships, protocols such as `WM_DELETE_WINDOW`, and configure behavior. EWMH adds interoperable desktop conventions: active window, client lists, desktops, work areas, fullscreen, window types, struts, and synchronization requests.

These specifications should not be implemented as a checklist of atoms. Each property belongs to a state transition. For example:

- `_NET_ACTIVE_WINDOW` follows focus ownership. It is not set independently of the focus state machine.
- `_NET_WM_STATE_FULLSCREEN` follows the fullscreen transition. Geometry, stacking, bar visibility, and restoration are part of the same transition.
- `_NET_WM_WINDOW_TYPE_DIALOG` and `WM_TRANSIENT_FOR` influence map-time classification and stacking.
- `_NET_WM_STRUT_PARTIAL` changes monitor work areas and therefore layout input rectangles.
- `_NET_WM_SYNC_REQUEST` changes the resize scheduler by giving the WM a client-readiness signal.

The design principle is: **protocol properties should be projections of authoritative state or inputs to an explicit state machine, never a second competing model.**

## 5. Interactive performance is a latency budget

A resize interaction is successful when three things hold:

1. The visible divider or outline stays close to the pointer.
2. The event loop remains responsive to release, cancel, key, map, and unmap events.
3. The final committed geometry is exact and consistent.

At 60 Hz, a display interval is 16.67 ms. The WM should not assume it owns that entire interval. The X server must process requests; managed clients may redraw; a compositor may compose. A useful initial target is:

| Stage | Target during live resize |
|---|---:|
| Event queue lag at scheduler entry | under 8 ms median, under 24 ms p99 |
| WM preview layout and diff | under 1 ms for ordinary trees |
| X request construction and flush | under 1 ms |
| Decoration paint/upload | under 2 ms total for affected frames |
| Total WM update | under 4-6 ms p95 |
| Client acknowledgement when sync protocol is used | bounded by 1-2 display intervals before fallback |

These are engineering targets, not promises about every machine. The important change is to make the budget explicit. Once an update exceeds budget, the scheduler should degrade work, not accumulate debt. Outline mode, lower preview cadence, skipped semantic animation, or old-content preservation are valid degradations. Processing stale motion samples is not.

### 5.1 Pixel volume explains why “small” code is expensive

A 1920 x 1080 RGBA image contains 2,073,600 pixels and occupies about 7.9 MiB. Filling it, converting all channels, and making the server consume it 60 times per second means touching roughly 475 MiB per second per surface before counting allocations, title rendering, copies, or client work. Two resized windows plus a PBUI client can push that into gigabytes of memory traffic.

By contrast, a 1920 x 22 title strip is 42,240 pixels, about 165 KiB. At 60 Hz it is roughly 9.7 MiB per second. The ratio is the point: **the best optimization is to stop representing decoration as a full client-sized bitmap.**

---

# Part II. The current go-go-wm architecture

## 6. The semantic core inherited from the prototypes

The supplied `pbui-shell` prototype defines more than a visual style. It establishes the system's semantic contract:

- A presentation associates a presentation type, a value, a visual face, and interaction metadata.
- A pending `accept` changes the meaning of every compatible presentation across the desktop.
- A right-click menu is assembled from verbs applicable to the presentation type, regardless of which process registered them.
- A binary split tree owns tile geometry.
- Applications share a desktop-level world of output, inspection, and tracing rather than behaving as isolated rectangles.
- The same data can be rendered in different places while remaining the same live object.

This is close to the CLIM model. CLIM defines a presentation as an association among an object, a presentation type, and displayed output. Presentation types define how objects are presented and accepted and form an inheritance lattice. go-go-wm translates that idea into JSON values, a broker, process ownership, and screen regions.

The basketball prototype adds an important requirement that the first prototype only hints at: a presentation is not necessarily a text chip. It may be a shot marker on a court, a point on a trend line, a radar polygon vertex, a bubble, a table cell, a team-color swatch, or a composite row. A future widget system must therefore allow a presentation wrapper around **any retained visual node or group**, with a precisely computed hit region and view-specific face.

HyperCard contributes a different but compatible lesson: interface objects carry scripts and participate in an event/message system. The useful principle is not to copy HyperTalk syntax. It is to make the desktop inspectable and alterable at the level the user sees: bars, fields, menus, cards/surfaces, commands, and data objects. JavaScript should compose these elements without receiving raw XIDs or needing to implement window-system mechanics.

## 7. Package boundaries and data flow

The current package structure is unusually good for a young WM:

| Package | Responsibility | Review assessment |
|---|---|---|
| `pkg/wmcore` | Pure desktop, split tree, operations, layout, neighbor queries | Strong foundation. Keep X11 and scripts out. |
| `pkg/wmx11` | Reparenting shell, frames, focus, floating, fullscreen, input, bars, menus, launcher, IPC | Correct owner-loop direction; currently carries too much immediate rendering policy. |
| `pkg/pbui` and broker/client | Typed objects, accepts, verbs, menus, event bus, wire protocol | Semantically distinctive and well factored. Needs richer type/view metadata over time. |
| `pkg/draw` | Theme and software drawing primitives, X image conversion | Testable and deterministic; should become a backend for retained layers rather than the public widget abstraction. |
| `pkg/apps` | Region/click contract and embedded app vocabulary | Useful bridge; flat regions and full-surface render functions are now the main limitation. |
| `pkg/apps/uispec` | Declarative row/segment IR | Correct direction; insufficient hierarchy, invalidation, state, and general hit semantics. |
| `pkg/apps/xapp` | Standalone X client shell for PBUI apps | Functional, but its full redraw/upload/destroy loop is a major performance gap. |
| `pkg/jsmod` | Goja bridges, event fan, queues, `wm`, `pbui`, and `ui` modules | Owner-loop and data normalization rules are strong. Needs generation/capability/surface orchestration for full UI scripting. |
| `pkg/xshm` | Shared-pixmap upload path | Valuable optimization, but current size-coupled lifetime makes interactive resizing expensive. |

The normal model-to-screen path is:

```text
input / IPC / JavaScript
        |
        v
    wmcore.Op
        |
        v
  Desktop mutation
        |
        v
 wmcore.Layout(area)
        |
        v
 X frame reconciliation + rendering
        |
        v
      X server
```

The PBUI semantic path is orthogonal:

```text
application or script registers verbs / emits objects
        |
        v
      broker
        |
        +---- accept mode broadcast ----> all surfaces highlight compatible objects
        |
        +---- menu query ----------------> verbs by type and owner
        |
        +---- verb.run ------------------> owning process / runtime
```

These two paths should remain orthogonal. Layout operations should not know about presentation menus; PBUI objects should not know about X rectangles. The surface layer is where they meet: a rendered presentation produces hit geometry and a broker object, while the WM supplies placement and input.

## 8. What the 18-20 July project entries accomplished

The dated repository notes describe a rapid but coherent evolution.

### 8.1 GGWM-001: the native system

The initial implementation translated the React sketch into a pure binary split engine, a PBUI broker and client protocol, a software-drawn reparenting WM, embedded applications, standalone demos, and CLI tools. The durable-operation design was established early. This is important: keyboard bindings, mouse actions, IPC, and JavaScript can all express the same mutation vocabulary.

### 8.2 GGWM-002: scripting attachment points

The scripting design correctly separates three attachment points:

- In-process configuration in the WM process.
- Standalone scripts controlling the WM over IPC.
- An interactive REPL using the same modules.

The decisive concurrency rule is that goja has its own owner loop and the WM loop never executes JavaScript. Calls cross through posted closures and bounded waits. `accept` is promise-shaped because it is a one-shot rendezvous; verbs and events are callback streams. The `wm` and `pbui` modules are separated because they carry different authority.

### 8.3 GGWM-003: the first declarative UI module

`uimod` introduced a normalized data-only specification, rendered by Go into an image and a list of regions. Script handlers run on the JS loop, produce a new snapshot, and post a repaint. The render host reads only a mutex-protected normalized snapshot and never calls JavaScript. This is the most important invariant to preserve while expanding widget power.

### 8.4 GGWM-004: themes, i3-derived configuration, and onboarding

The theme work centralized palette lookup and exposed a practical lesson: paint-time configuration must not be captured as init-time values, and palette mutation must have one writer. The i3-style JavaScript configuration demonstrated that the operation API can support a familiar workflow without parsing i3's configuration language.

### 8.5 GGWM-005: measured paint-path improvements

The performance ticket used CPU profiles rather than intuition. Before optimization, frame painting, pixel conversion, fills, duplicate Expose handling, and garbage collection dominated. The implementation changed fills to row copies, conversion to row-major traversal, cached frame images, reused Expose buffers, limited drag updates, and repainted only frames whose geometry changed. Recorded CPU time and startup time fell substantially.

### 8.6 GGWM-006: shared pixmaps and batching

MIT-SHM shared pixmaps removed repeated PutImage transfer for frame buffers. Operation batching prevented repeated model-to-X reconciliation during multi-operation scripts. Parallel conversion and bar caching addressed remaining hot spots. Damage tracking was deliberately deferred.

### 8.7 GGWM-007 through GGWM-009: missing desktop layers

The transient-window design correctly keeps floating dialogs out of the pure tiling tree. The launcher design introduces one command registry behind popup and tile surfaces. The rich REPL recognizes that PBUI already supplies the right ontology for rich values: a result should be a real `color`, `number`, `dataset`, or domain object, not a wrapper that loses desktop-wide verbs and accept compatibility.

### 8.8 GGWM-010 and GGWM-011: correctness and state ownership

PR review exposed focus/fullscreen bugs caused by related state spread across raw fields and files. The follow-up created explicit `fullscreenState` and `focusState` owners and display-free decision functions. This is the right structural response. Similar ownership types should be introduced for interactive resize, surface stacking/input scope, and geometry policy.

## 9. What is already architecturally strong

A performance review should not flatten the system into a list of problems. Several choices should be defended during refactoring.

### 9.1 Pure layout and operations as data

`wmcore` can be fuzzed, property-tested, serialized, replayed, and queried without X. Keeping mutation in `Apply` makes behavior consistent across keyboard, mouse, IPC, scripts, and tests. The long-term opportunity is to distinguish durable operations from transient previews, not to abandon operations as data.

### 9.2 Single-owner loops

Both the WM and goja runtime use owner loops. This allows asynchronous composition without pervasive locks and makes boundaries visible. Retained UI snapshots fit naturally into the same design.

### 9.3 VM-free rendering

The current script-tile renderer reads a normalized snapshot and invokes no JavaScript. This protects the X loop from a runaway script, a slow promise, garbage collection in the VM, or reentrant UI calls. Every future widget and surface should obey the same rule.

### 9.4 Broker symmetry

The WM participates in the same PBUI protocol as other processes. It does not secretly own a second type/action mechanism. This permits terminal presentations, standalone apps, scripts, bars, and REPL output to interoperate.

### 9.5 Profiling before optimization

The project has already recorded before/after profiles and documented why each change matters. Preserve this discipline. The next phase should add latency and event-queue measurements, not replace profiling with architectural speculation.

### 9.6 Recent focus/fullscreen encapsulation

The current `focusState` and `fullscreenState` make invariants testable without a display and give one owner to related state. Resize mode, preview state, and surface input scopes deserve analogous types.

## 10. Current implementation risks

The main risks are not isolated bugs. They are boundaries that will become more expensive as widget power and surface count grow.

| Area | Current behavior | Consequence |
|---|---|---|
| Rendering | Immediate-mode full-surface `image.RGBA` output | Any small state change can repaint and upload all pixels. |
| Client frame | Decoration and content share a full frame-sized buffer | Ordinary client resize churns megabytes of decoration data that is mostly hidden by the child. |
| Resize model | Every preview applies a durable ratio operation | Allocation, event emission, and observers are coupled to pointer frequency. |
| Motion control | Time gate without a latest-state mailbox | Stale events can remain queued; latency can grow under load. |
| X resources | Shared surfaces are dimension-coupled and recreated on size change | SysV SHM, X pixmap, and checked-request churn occurs in the hottest interaction. |
| `xapp` | Full render plus new upload object on every ConfigureNotify | Managed PBUI clients double the total resize work. |
| `uispec` | Flat rows and segments | No general nesting, retained identity, clipping, scrolling protocol, modal focus, or fine invalidation. |
| Regions | Flat reverse linear scan | Adequate for small demos; expensive and semantically weak for tables/plots/large scenes. |
| Modal state | Separate booleans/pointers for menu, launcher, accept, drag | Input routing and focus restoration become increasingly ad hoc. |
| Accept styling | `repaintAllFrames` on mode transitions | A semantic overlay change triggers full rasterization of every frame. |
| Script lifecycle | Handler IDs are not a complete generation-stamped resource model | Hot reload can leave stale events or resources without a central surface generation contract. |
| Capabilities | Process/spawn checks exist, but UI authority is coarse | Global bars, input grabs, notifications, and overlays need explicit permission boundaries. |

The rest of this document develops concrete replacements for these boundaries.

---

# Part III. Performance: making resize responsive

## 11. The current divider-resize path, step by step

A useful performance analysis begins with one concrete trace. In the current code, a divider press creates drag state. Each frame MotionNotify routes to `handleMotion`, which routes divider drags to `dividerMotion`. The motion handler checks elapsed time, derives a ratio from the current pointer position, applies `OpSetRatio`, and calls `relayoutResized`. Release removes the throttle and executes a final motion update.

The simplified shape is:

```go
func (w *WM) dividerMotion(rootX, rootY int) {
    if time.Since(w.drag.lastPaint) < 16*time.Millisecond {
        return
    }

    ratio := ratioFromPointer(w.drag, rootX, rootY)
    _, _ = w.Apply(Op{Op: OpSetRatio, Node: w.drag.split, Ratio: ratio})
    w.relayoutResized()
    w.drag.lastPaint = time.Now()
}
```

The actual code contains snapping and drag-kind logic, but the cost structure is captured here. `relayoutResized` computes a complete layout, synchronizes divider windows, loops through all layout items, changes geometry for frames whose rectangles differ, configures client children, and paints resized frames. A frame whose dimensions changed replaces its RGBA image and invalidates its cached X image or shared-memory surface. The SHM path then allocates a new System V segment, attaches it to X, creates a shared pixmap, writes the full image, and clears/copies it to the window.

For a managed PBUI client, the geometry change is only the midpoint. The client receives ConfigureNotify, calls its application `Render(w, h, accepting)`, converts the resulting full image into a new X image, draws/paints it, and destroys the X image. The diagram below shows the combined path.

![Current resize work amplification](figures/current-resize.png)

*Figure 1. One admitted pointer sample can trigger durable model work, full WM-side decoration rendering, resize-time SHM resource creation, and an independent full client redraw.*

### 11.1 The throttle reduces frequency, not cost or queue age

A 16 ms guard can cap the number of updates executed by a handler that is called promptly. It cannot guarantee that the handler is seeing the latest coordinate. It also does not bound the duration of an admitted update. If one update takes 30 ms, the system is already below 34 Hz before the client redraw is counted.

There are three independent variables:

- **Input rate:** how frequently the server queues motion samples.
- **Admission rate:** how frequently the WM chooses to run a preview update.
- **Completion rate:** how frequently the WM and clients can finish the admitted work.

A good scheduler keeps admission at or below completion and replaces stale work. The current gate reduces admission but does not explicitly replace queued work. It also computes the next admission decision only after the previous work returns, which means it has no direct measure of queue lag.

### 11.2 The operation path is doing two jobs

`OpSetRatio` currently serves as both:

1. The durable statement “this split now has ratio 0.62.”
2. The transient preview “the pointer is currently here.”

Those are not the same event. Durable operations should be replayable and meaningful to automation. A pointer preview is lossy by nature. If the UI displayed 0.603, 0.607, 0.616, and 0.620 over four frames, a replay only needs the final committed 0.620 unless a dedicated interaction recording is being captured.

Conflating the two makes every preview pay for persistent tree replacement, operation emission, observers, and possible future journaling. It also makes it harder to drop a preview under pressure, because dropping a durable operation sounds like corruption even though dropping an intermediate pointer coordinate is correct.

### 11.3 The full layout is not the dominant cost, but it amplifies other work

For ordinary desktop trees, a complete binary-tree traversal and map allocation is likely smaller than pixel conversion and X resource creation. It should still be improved, but only after the pixel and event problems. The more important issue is that full layout drives broad reconciliation: every divider is synchronized, every visible frame is considered, and several maps/tree lookups are repeated.

The priority order matters. Replacing `map[NodeID]LayoutItem` with a clever arena while still recreating 8 MiB SHM buffers will not make resizing feel good. Removing full-frame decoration buffers can produce a visible improvement before subtree layout exists.

## 12. A cost model for the current renderer

Performance work becomes easier when every stage has a unit.

### 12.1 CPU and memory units

For each resize update, record:

- Nodes visited and layout items produced.
- Frames whose position changed.
- Frames whose size changed.
- Divider windows moved and repainted.
- RGBA bytes filled.
- Pixels converted from RGBA to the X server's byte order.
- Images allocated and bytes allocated.
- SHM segments attached/detached.
- Pixmaps created/freed.
- ConfigureWindow, MapWindow, UnmapWindow, ClearArea, and property requests issued.
- Flushes, reply waits, and checked-request waits.
- Client ConfigureNotify events produced.

The expected correlation is more useful than a single CPU percentage. For example, if p95 resize time rises with `shm_surface_creates` and `rgba_bytes_written`, the structural diagnosis is confirmed. If it rises with `x_reply_wait_ns`, a hidden round trip is more important.

### 12.2 Latency units

Each pointer sample should carry or be associated with:

- Event timestamp from X where available.
- Monotonic receipt time.
- Mailbox replacement time.
- Scheduler start time.
- X flush time.
- Optional client sync acknowledgement time.
- Commit time on release.

Derived metrics:

```text
queue_lag        = scheduler_start - event_receipt
preview_latency  = x_flush - event_receipt
wm_work          = x_flush - scheduler_start
client_wait      = sync_ack - x_flush
release_latency  = final_commit - release_receipt
stale_distance   = abs(pointer_latest - pointer_rendered)
```

`stale_distance` is especially useful. A system can report 60 updates per second while visibly trailing the pointer if those updates apply old coordinates.

### 12.3 A frame-level budget table

A resize update should be instrumented as spans rather than one timer:

| Span | Suggested name | Notes |
|---|---|---|
| Input admission | `wm.resize.admit` | Include queue lag and number of replaced samples. |
| Ratio calculation | `wm.resize.ratio` | Include snap target and mode. |
| Preview model | `wm.resize.preview_model` | Zero durable ops expected. |
| Layout | `wm.resize.layout` | Record nodes visited and whether full/subtree. |
| Geometry diff | `wm.resize.diff` | Record changed positions/sizes. |
| X request build | `wm.resize.xbuild` | Request counts by opcode. |
| Decoration render | `wm.resize.decor_paint` | Bytes and affected surfaces. |
| Upload/SHM | `wm.resize.upload` | Reuse/create/destroy counters. |
| Flush | `wm.resize.flush` | Should not imply a reply wait. |
| Client readiness | `wm.resize.client_sync` | Counter value, timeout, fallback. |
| Final commit | `wm.resize.commit` | One per successful drag. |

The implementation can expose this through Logcopter, `runtime/trace`, Prometheus-style counters, or a debug IPC query. The format matters less than stable names and low overhead.

## 13. Preserve the gains from GGWM-005 and GGWM-006

The existing performance work should be understood before further refactoring, because a rewrite can accidentally reintroduce solved problems.

### 13.1 Row-oriented fills and conversion

An `image.RGBA` stores rows with a stride. Filling or converting one pixel at a time through general color interfaces adds method calls, bounds checks, and poor locality. The current code writes row-major byte slices and uses row copies for solid fills. Keep that strategy in low-level raster backends.

### 13.2 Cached image objects

The earlier renderer allocated and destroyed X image objects on every paint. Cached images and SHM surfaces reduced object churn for stable dimensions. The target design should retain caches, but attach them to smaller layers and give them capacity-aware lifetimes.

### 13.3 Expose repair without rerender

The frame's background pixmap allows the server to repair exposed regions. The Expose handler now avoids a full render when a current shared surface exists and reblits a cached X image otherwise. Retained surfaces extend this principle: Expose should be a repair or damage-copy operation, not a request to rerun application logic.

### 13.4 Changed-frame-only paint

`relayoutResized` paints frames whose size changed rather than all frames. Preserve and strengthen this diff. Position-only changes do not require content repaint. Decoration focus changes affect only old and new focus layers. Accept-mode changes should affect semantic overlay layers, not application content.

### 13.5 MIT-SHM as a transport optimization

Shared memory removes an explicit image payload from each X request. It does not make full-surface rendering free. Pixel generation, cache misses, server reads, pixmap allocation, and synchronization still exist. Treat SHM as the last transport stage after the amount of damage has been minimized.

## 14. Replace the time gate with a latest-wins resize scheduler

The first implementation change should be small enough to land independently.

### 14.1 The mailbox

Add one resize-controller object owned by the WM loop:

```go
type resizeController struct {
    active       bool
    split        wmcore.NodeID
    mode         ResizeMode
    latestX      int
    latestY      int
    latestSeq    uint64
    renderedSeq  uint64
    scheduled    bool
    inFlight     bool
    released     bool
    lastStart    time.Time
    lastDuration time.Duration
}
```

Motion handling does not perform layout. It updates `latestX`, `latestY`, and `latestSeq`. If no update is scheduled, it schedules one. Multiple motion events before the scheduled function runs only replace coordinates.

```go
func (r *resizeController) Motion(x, y int) {
    r.latestX, r.latestY = x, y
    r.latestSeq++
    if !r.scheduled && !r.inFlight {
        r.scheduled = true
        r.wm.Post(r.step)
    }
}
```

Because X callbacks and posted closures execute on the same WM loop, the actual implementation may schedule with a timer or a loop-owned deadline rather than posting to the same queue immediately. The semantic requirement is one wake token, not one queued function per event.

### 14.2 The step function

The step function snapshots the latest sequence and coordinates, performs at most one preview update, and then checks whether a newer sample arrived while work was in progress.

```go
func (r *resizeController) step() {
    r.scheduled = false
    if !r.active {
        return
    }

    seq, x, y := r.latestSeq, r.latestX, r.latestY
    r.inFlight = true
    started := time.Now()
    r.applyPreview(x, y)
    r.lastDuration = time.Since(started)
    r.lastStart = started
    r.renderedSeq = seq
    r.inFlight = false

    if r.released {
        r.commitLatest()
        return
    }
    if r.latestSeq != r.renderedSeq {
        r.scheduleNext(r.nextDelay())
    }
}
```

`nextDelay` uses the target interval and measured duration. If work took 18 ms, scheduling another live update immediately may be acceptable if the latest coordinate is far away and the event loop is otherwise empty, but repeated over-budget work should switch to outline or reduce cadence. The controller should never enqueue multiple pending steps.

### 14.3 Release and cancel semantics

ButtonRelease stores the release coordinates and sets `released`. It does not wait for a throttle interval. If no step is in flight, it commits immediately; otherwise, the in-flight step sees `released` and drains the latest coordinate before commit. Escape restores the original ratio and exits without a durable operation.

The controller should retain:

```go
type resizeTransaction struct {
    split         wmcore.NodeID
    originalRatio float64
    previewRatio  float64
    startedAt     time.Time
    mode          ResizeMode
}
```

That transaction makes cancellation, tracing, and one-shot commit explicit.

### 14.4 Coalescing policy differs by event class

The existing `boundedQueue` in `pkg/jsmod` deliberately drops new events when full because subscribers value the contiguous prefix they received. Pointer motion has the opposite policy: the newest state is more valuable than a contiguous history of stale coordinates. These should be separate queue types with explicit semantics:

| Event class | Backpressure policy |
|---|---|
| Pointer motion, hover, window-size preview | Replace old pending item with newest. |
| Durable operations, lifecycle events, accept results | Preserve order; never silently replace. |
| High-volume telemetry | Sample or aggregate; include lost count. |
| Script state snapshots for a surface | Keep newest complete snapshot; discard superseded pending snapshot. |
| Text/key input | Preserve order with bounded queue and explicit overflow failure. |

Naming these policies prevents accidental reuse of the wrong queue.

## 15. Separate preview ratios from committed operations

The preview model can be implemented without changing `wmcore.Node` immediately.

### 15.1 Minimal implementation: layout override

Add an optional ratio override to `Layout` or to a drag-specific layout function:

```go
type LayoutOverrides struct {
    Ratios map[wmcore.NodeID]float64
}

func LayoutWithOverrides(root *Node, area Rect, gap int, o LayoutOverrides) LayoutResult
```

During a drag, the authoritative desktop remains at the original ratio. The render reconciliation receives an override for the active split. On release:

```go
_, err := w.Apply(wmcore.Op{
    Op:    wmcore.OpSetRatio,
    Node:  tx.split,
    Ratio: tx.previewRatio,
})
```

Only that call emits the durable operation and broker event.

### 15.2 Better implementation: a preview tree view

If several interactive operations eventually need previews—drag docking, workspace layout previews, animated transitions—introduce a read-only `LayoutView`:

```go
type LayoutView interface {
    Root() *wmcore.Node
    Ratio(node wmcore.NodeID) float64
    LeafApp(node wmcore.NodeID) string
}
```

The durable desktop implements it directly. A preview wrapper delegates everything except a small overlay map. Layout consumes the interface. This avoids cloning trees and keeps preview concerns out of the serialized model.

### 15.3 Observer semantics

Some scripts may want live resize information. Do not force that requirement back into durable ops. Emit an explicitly lossy event at a controlled rate:

```json
{
  "event": "window.resize-preview",
  "data": {
    "split": "n17",
    "ratio": 0.618,
    "sequence": 42,
    "mode": "live"
  }
}
```

Subscribers must treat it as telemetry. The committed `wm.op` event follows once on release. This distinction makes recording and replay coherent.

## 16. Outline, live, and adaptive resizing

There is no universal correct resize mode. The WM should make the policy explicit and scriptable.

### 16.1 Outline mode

Outline mode moves a thin helper or divider window while the pointer is down. It does not resize application frames. On release, the final ratio is committed and the normal reconciliation runs once.

Advantages:

- Pointer tracking remains responsive even for slow Electron, Java, remote X, or graphics-heavy clients.
- The WM avoids client redraw feedback entirely during the drag.
- The operation log contains one change.
- Implementation is simple and mirrors i3's graphical resize path.

Costs:

- Content does not resize continuously.
- The final release can produce one visible jump and a large redraw.
- The outline must clearly communicate the prospective boundary.

For an experimental WM, outline mode is not a regression. It is a reliable baseline and a diagnostic tool. If outline mode is smooth while live mode is slow, the remaining bottleneck is definitively geometry/client rendering rather than input handling.

### 16.2 Live mode

Live mode applies preview geometry continuously. It should be reserved for clients and surfaces that can keep up. Even in live mode, it must use latest-wins coalescing, transient ratios, changed-subtree geometry, and thin decoration rendering.

### 16.3 Adaptive mode

Adaptive mode begins live and changes behavior based on measured conditions. A concrete policy:

```text
start drag:
    mode = live
    budget = 6 ms WM work per update

for each preview:
    if queue_lag > 24 ms:
        use outline for this and following updates
    else if last_wm_work > 10 ms twice:
        use outline
    else if any sync-capable client has not acknowledged prior resize:
        do not issue another live client resize
    else:
        perform live update

on release:
    apply final geometry regardless of preview mode
```

A less abrupt policy can alternate: render the outline at pointer rate and perform a live application resize at 20-30 Hz when budget allows. The outline shows the current intended boundary; content follows at a controlled cadence.

### 16.4 Per-surface and per-client policy

Expose a small rule vocabulary:

```js
wm.resizePolicy({ default: "adaptive", targetHz: 60 });
wm.rule({ class: /mpv|gamescope/i, resize: "outline" });
wm.rule({ class: /kitty|Alacritty/i, resize: "live" });
ui.configure({ resizeQuality: "adaptive" });
```

The policy should be advisory. The scheduler can degrade a live request under load. A surface may also declare `fastResize: true` or support the synchronization protocol.

### 16.5 `_NET_WM_SYNC_REQUEST`

EWMH defines a resize synchronization protocol using XSync counters. A supporting client advertises `_NET_WM_SYNC_REQUEST` in `WM_PROTOCOLS` and exposes `_NET_WM_SYNC_REQUEST_COUNTER`. Before a resize, the WM sends a synchronization request with a new counter value and then configures the window. The client updates its counter after it has redrawn for that request.

The protocol gives the WM a readiness signal. It should be integrated as backpressure:

- Do not send an unbounded sequence of live resizes to a client with an outstanding counter value.
- Keep displaying the latest outline while waiting.
- When the counter advances, send the newest pending size, not every skipped size.
- Time out and continue in outline/final mode if the client is broken or slow.
- Support clients without the protocol through ordinary coalescing and adaptive fallback.

The protocol is not a reason to block the WM loop. XSync alarm events or polled state should post readiness into the resize controller.

## 17. Redesign normal client decorations as thin layers

This is the largest structural performance opportunity in `pkg/wmx11`.

### 17.1 The current mismatch

A `frame` stores a full-size `*image.RGBA`, a full-size fallback X image, and a full-size shared-memory surface. `paintFrame` fills the entire frame background, draws a title strip and border, and uploads the whole frame. For a normal client, the reparented child covers the content area. Most uploaded pixels are never visible.

The representation should match what the WM owns visually:

- Title strip.
- Outer border or focus ring.
- Optional resize handles and buttons.
- Empty background only for internal PBUI surfaces.

### 17.2 Recommended window hierarchy

Use separate child windows or server-rendered primitives:

```text
frame (root child; owns placement and clipping)
├── title window       y=0, height=TitleH
├── client window      y=TitleH, fills content
├── left border        optional thin InputOutput/InputOnly child
├── right border       optional thin child
└── bottom border      optional thin child
```

Possible implementations:

1. **Title child plus frame background/border pixel.** The frame uses `CwBackPixel` and X border width for simple borders. Only the title child owns an RGBA/SHM surface.
2. **Four thin decoration children.** This supports custom focus rings and hit areas without a full bitmap.
3. **Core X or XRender drawing.** Draw rectangles and text directly into a title pixmap/window. Text rendering may still use a cached image or glyph backend.

The first option is sufficient for the current paper-and-ink style.

### 17.3 Geometry reconciliation

A client frame resize becomes:

```go
frame.MoveResize(x, y, w, h)
title.MoveResize(0, 0, w, TitleH)
client.Configure(BorderW, TitleH, w-2*BorderW, h-TitleH-BorderW)
```

Only title width changes. If title rendering supports clipping and a capacity buffer, even the title upload can avoid reallocation on every pixel change. Position-only changes require no paint.

### 17.4 Internal PBUI surfaces are different

A builtin or script tile has no client child covering the content. It needs a content surface. Do not force client frames and internal surfaces through one buffer shape merely because both have a title.

Split the record conceptually:

```go
type frameChrome struct {
    frameWin  *xwindow.Window
    titleWin  *xwindow.Window
    titleBuf  *Surface
    borderWin ...
}

type clientContent struct {
    client xproto.Window
}

type pbuiContent struct {
    contentWin *xwindow.Window
    scene      *CompiledScene
    layers     *LayerCache
}
```

A frame composes chrome with exactly one content host. The X shell can still expose one higher-level `frame` type during migration, but painting and resource lifetime should be delegated.

### 17.5 Focus and accept-mode repaint become cheap

A focus change usually alters title/border appearance. In the thin-layer design, repaint only the old and new title/focus layers. Accept mode should not repaint ordinary client chrome at all unless tile-title presentations are compatible accept targets. A semantic overlay can highlight those title regions independently.

### 17.6 Memory impact

Assume four 1920 x 1080 client frames. Full-size RGBA buffers alone occupy about 31.6 MiB; matching SHM surfaces double that order of magnitude before X pixmaps. Four 1920 x 22 title buffers occupy about 660 KiB. The difference reduces allocation pressure, cache pollution, SHM limits, and resize-time creation cost simultaneously.

## 18. Redesign buffer and MIT-SHM lifetime

MIT-SHM is useful when the application repeatedly updates an image of stable dimensions. Interactive resizing violates that stability if the buffer is tied exactly to the current window size. The current `xshm.New(w, h)` path performs several expensive lifecycle operations: `shmget`, `shmat`, extension attach, IPC removal marking, shared pixmap creation, and later pixmap/detach cleanup. Doing that repeatedly during a drag converts a transport optimization into resource churn.

### 18.1 First rule: remove buffers that should not exist

Thin client decorations solve most WM-side resizing without inventing a more complex SHM allocator. Do that first.

### 18.2 Capacity buffers for PBUI content

A PBUI content surface can reserve capacity larger than its logical viewport:

```go
type PixelBuffer struct {
    CapacityW int
    CapacityH int
    ViewW     int
    ViewH     int
    Pix       []byte
    Surface   *xshm.Surface
}
```

Growth uses buckets rather than exact dimensions:

```text
requested 641 x 421  -> capacity 768 x 512
requested 770 x 512  -> capacity 1024 x 512
requested 1000 x 700 -> capacity 1024 x 768
```

Shrinking changes only the viewport. Growth recreates resources occasionally, not for every pixel. Choose moderate buckets or geometric growth with a maximum waste ratio. A surface that remains much smaller for several seconds can be compacted outside an interaction.

Shared pixmaps have dimensions, so a larger pixmap cannot simply be used as the background pixmap of a smaller window in every desired way without clipping policy. A practical approach is a content child window/pixmap with the capacity size, clipped by its frame, or use `ShmPutImage` from a capacity buffer into the logical drawable. Benchmark both. The key is that the backing memory and conversion workspace need not be exact-size or repeatedly allocated.

### 18.3 Double buffering and synchronization

The current shared-pixmap path writes directly into memory the server may read. The project notes explicitly accept benign tearing. As the UI becomes richer, use one of these schemes:

- Two shared pixmaps: render into the inactive buffer, then install/copy it and swap.
- One shared image plus `ShmPutImage`, waiting for the MIT-SHM completion event before modifying the same region again.
- Per-layer buffers where small layers are copied atomically enough for the visual style, with generation checks.

Do not wait synchronously on the WM loop. Completion events should mark a buffer reusable; the scheduler can skip or use another buffer when none is available.

### 18.4 Pooling

Maintain pools by backend and capacity class:

```go
type SurfacePool interface {
    Acquire(kind SurfaceKind, minW, minH int) *Surface
    Release(*Surface)
    Trim(budgetBytes int64)
}
```

Pool policy must include memory budgets and hidden-workspace behavior. Existing code drops frame buffers when workspaces are hidden. Keep that idea, but distinguish:

- Cheap title buffers, which may remain cached.
- Large content surfaces, which are candidates for release.
- Immutable resources such as glyph atlases or icons, which use separate caches.

### 18.5 Avoid redundant copies

The current builtin path often renders a content image and then copies it into the frame image. A retained content surface should render directly into its layer target. If an app still returns an image, composite only the damaged rectangle into the content buffer. The title is a separate layer.

## 19. Make reconciliation a diff, not a redraw command

`relayout` currently means several things at once: compute geometry, synchronize dividers, map/unmap workspace windows, resize frames, paint them, synchronize floats, update focus-related appearance, and sometimes repair state. As features accumulate, such a function becomes difficult to call safely.

Introduce a reconciliation plan:

```go
type ReconcilePlan struct {
    Geometry []GeometryChange
    Visibility []VisibilityChange
    Stacking []StackChange
    Decoration []DecorationDamage
    Content []ContentDamage
    Divider []DividerChange
    Focus *FocusTransition
}
```

The flow becomes:

```text
model + previous snapshot + preview overrides
        |
        v
compute desired snapshot
        |
        v
diff snapshots -> ReconcilePlan
        |
        v
execute X request batch and render damage
```

### 19.1 Stable desired-state snapshots

Keep a snapshot keyed by frame/surface ID:

```go
type FrameSnapshot struct {
    Rect       wmcore.Rect
    Visible    bool
    StackBand  StackBand
    Focused    bool
    TitleHash  uint64
    ThemeGen   uint64
    AcceptGen  uint64
}
```

A position-only rectangle change produces one frame move. A size change produces frame/client geometry and possibly content/layout invalidation. A focus change produces decoration damage. An unchanged visible flag produces no Map request.

### 19.2 Do not map an already mapped window on every relayout

Repeated MapWindow requests may be harmless, but they are noise and obscure traces. Track visibility transitions. The same applies to floats in `syncFloats` and divider windows in `syncDividers`.

### 19.3 Divider windows do not need repaint on movement

A divider's appearance changes on theme, hover, active drag state, orientation, or thickness. Its position and length can change without regenerating a pixmap if the server background is a solid pixel or a reusable pattern. Replace `paintDivider` in the drag path with one of:

- Set a background pixel once and use `ClearArea` after size changes.
- Use one cached pixmap per orientation/state.
- Draw a small repeated pattern and tile it.

The divider synchronization plan should distinguish `move`, `resize`, `map`, `unmap`, and `appearance`.

### 19.4 Batch X geometry requests

XCB requests are naturally asynchronous. Build all geometry changes, issue them in a deterministic order, then flush once. Avoid helper methods that hide a flush or checked reply. The execution order should generally be:

1. Unmap surfaces that must disappear before overlap changes.
2. Configure parent frames and internal decoration children.
3. Configure client children.
4. Configure dividers and overlays.
5. Map newly visible surfaces.
6. Apply stacking changes.
7. Install/copy damaged pixmaps.
8. Update EWMH properties whose values changed.
9. Flush.

Exact ordering can vary, but centralizing it makes X traces comprehensible.

## 20. Limit live layout to the affected split subtree

A divider belongs to a split node. Changing its ratio changes only the rectangles in that split's descendant subtree. Ancestors keep their rectangles; unrelated branches keep theirs.

### 20.1 Maintain parent/index information

`wmcore.Node` is currently navigated through recursive searches. Add an ephemeral index when a desktop snapshot is installed:

```go
type TreeIndex struct {
    Parent map[NodeID]NodeID
    Node   map[NodeID]*Node
    Depth  map[NodeID]int
}
```

This index is derived data and need not be serialized. Durable operations produce a new root and rebuild or incrementally update the index. During a drag, the controller can locate the split and its previously assigned rectangle directly.

### 20.2 Subtree layout API

```go
type LayoutSlice struct {
    Items []LayoutItem // stable order, including leaves and dividers
}

func LayoutSubtree(
    split *Node,
    splitRect Rect,
    gap int,
    overrides LayoutOverrides,
    dst []LayoutItem,
) LayoutSlice
```

The previous snapshot already contains `splitRect`. The result replaces entries for descendants only. Use a reusable slice or scratch arena owned by the resize controller to avoid per-frame maps.

### 20.3 Stable order instead of map iteration

A stable depth-first order simplifies diffing and deterministic X request construction. Use an index map only for direct lookup. This also removes repeated `Root.Find` calls from divider synchronization, because each `LayoutItem` can carry its node kind, split direction, and parent metadata.

### 20.4 Preserve the simple full layout path

Do not make every operation incremental immediately. Full `Layout` remains the correctness reference for workspace switches, monitor changes, startup, deserialize, and tests. Add a property test:

```text
For every generated tree, area, gap, and valid split ratio override:
merge(previousFullLayout, LayoutSubtree(activeSplit)) == FullLayoutWithOverride
```

The incremental path should prove equivalence to the full path.

## 21. Repair the PBUI client shell

Even a perfect WM cannot make live resize smooth if the managed client performs unbounded full redraws on every ConfigureNotify. `pkg/apps/xapp` currently does exactly that:

```go
func (a *shell) redraw() {
    img, regions := a.app.Render(a.w, a.h, a.accepting)
    a.regions = regions
    ximg := draw.ToXImage(a.X, img)
    ximg.XSurfaceSet(a.win.Id)
    ximg.XDraw()
    ximg.XPaint(a.win.Id)
    ximg.Destroy()
}
```

### 21.1 Immediate fixes

Implement these before the retained scene engine:

1. **Coalesce ConfigureNotify.** Store the newest width and height; schedule one redraw. Do not redraw for every intermediate notification already queued.
2. **Cache the Go image and X upload object.** Reuse at stable size; grow by capacity or recreate only when required.
3. **Add a redraw invalidation flag.** Expose and multiple state changes before the next turn should produce one redraw.
4. **Measure render, convert, upload, and allocation separately.** Client timing should be visible to the WM test harness.
5. **Support a fast resize renderer.** Applications may render simplified content while `interactiveResize=true` and produce final quality on commit.

A minimal shell scheduler:

```go
type redrawScheduler struct {
    dirty       bool
    sizeDirty   bool
    pendingW    int
    pendingH    int
    scheduled   bool
    inFlight    bool
}
```

ConfigureNotify only updates pending size and marks `sizeDirty`. Application state changes mark `dirty`. A single scheduled redraw consumes both.

### 21.2 Add a private resize protocol for go-go-wm surfaces

EWMH sync supports compatible general clients. PBUI clients can do more through the broker or an X property/client message:

```text
resize.begin(surface, initialSize, policy)
resize.preview(surface, size, sequence)
resize.commit(surface, finalSize, sequence)
resize.cancel(surface, originalSize)
```

The client can respond with capabilities:

```json
{
  "fastResize": true,
  "retainedScene": true,
  "maxPreviewHz": 60,
  "supportsScaleOldContent": false
}
```

This is not required for correctness. It allows the WM and PBUI shell to coordinate quality and backpressure more precisely than arbitrary X clients can.

### 21.3 Keep old content during expensive resize

A retained shell may preserve the old pixmap while layout for the new size is pending. Choices include clipping, centering, anchoring top-left, or scaling. Scaling can be visually poor for text and should not be the only mode. A paper-and-ink UI can instead preserve the old content and fill newly exposed areas with the surface background until the new scene is ready.

### 21.4 Final-quality commit

A resize commit must guarantee one final layout/render at the exact final size, even if previews were skipped. This is the same “lossy preview, exact commit” contract as the WM model.

## 22. Move `uispec` from immediate rows toward retained scenes

The present `uispec.Spec` is useful because it is data-only and easy to normalize. Its renderer, however, allocates a full surface and walks all rows each time. It also returns a flat region list whose geometry is rebuilt from scratch.

The migration should be evolutionary:

### 22.1 Give every segment a stable key

```go
type Seg struct {
    Key    string
    Kind   Kind
    // existing fields...
}
```

Rows also receive keys. A missing key can be generated from position for backward compatibility, but stable explicit keys are required for efficient updates and state preservation.

### 22.2 Split measure, layout, and paint

```go
type MeasuredSpec struct { ... }
type LaidOutSpec struct { Nodes []LayoutNode; Bounds Rect; ... }
type DisplayList struct { Ops []DrawOp; Regions *HitIndex; ... }

func Measure(spec Spec, env MeasureEnv) MeasuredSpec
func Layout(measured MeasuredSpec, constraints Constraints) LaidOutSpec
func Paint(layout LaidOutSpec, invalid Invalidation, cache *LayerCache) DisplayList
```

Text measurement, table column widths, and image intrinsic sizes belong in measure. Placement belongs in layout. Pixel output belongs in paint. A hover or accept highlight should usually skip measure and layout.

### 22.3 Retain layers

A layer is a cached raster or draw-command group with bounds and dependencies:

```go
type Layer struct {
    Key        string
    Bounds     image.Rectangle
    Generation uint64
    Surface    *PixelBuffer
    Dirty      Region
}
```

For example, a table background, static labels, data cells, and accept overlay can be separate layers. A selection change damages the affected rows and overlay, not the whole window.

### 22.4 Build a spatial hit index

Replace reverse linear scan for large scenes with a simple index. Options:

- Uniform grid buckets for mostly rectangular UI.
- R-tree for large irregular scenes.
- Hierarchical scene traversal with clipping and z-order.

Start with hierarchical traversal plus optional grid acceleration. Every hit record should include node key, local transform, presentation object, action/handler ID, cursor, and documentation string.

### 22.5 Virtualize large collections

The basketball leaders table, rich REPL history, trace listener, launcher results, and file browsers can grow. A widget protocol should compute only visible children plus overscan. Virtualization requires stable keys, scroll state, row height policy, and a way to ask the data source for a range. It should not be implemented as a giant pre-rendered image.

## 23. Performance test and observability plan

The next optimization cycle should ship with a reproducible test harness.

### 23.1 Test environments

Use at least:

- Xvfb for deterministic integration and CI.
- Xephyr for nested interactive testing and visual capture.
- A normal local Xorg session without compositor.
- A common compositor session.
- At least one high-DPI/multi-monitor setup after RandR support is active.
- Optional remote or deliberately delayed X proxy to expose round-trip assumptions.

### 23.2 Client corpus

Exercise:

- A terminal with cheap resize.
- Firefox or Chromium/Electron.
- GTK and Qt dialogs/transients.
- A Java/AWT application.
- mpv or another video client.
- `go-go-wm demo` PBUI clients.
- Purpose-built clients that flood ConfigureRequests, delay redraw, ignore sync, or destroy themselves mid-drag.

### 23.3 Automated resize scenario

A test driver can synthesize a press, N motion positions, and release over a fixed duration. Record:

```json
{
  "scenario": "two-1920x1080-pbui-clients",
  "mode": "adaptive",
  "input_samples": 240,
  "preview_updates": 58,
  "coalesced_samples": 182,
  "durable_ops": 1,
  "p50_wm_ms": 2.1,
  "p95_wm_ms": 4.8,
  "p99_queue_lag_ms": 12.0,
  "shm_creates_during_drag": 0,
  "rgba_megabytes_written": 19.4,
  "release_to_final_ms": 17.3
}
```

The exact target depends on hardware; regressions are visible when the same CI worker changes.

### 23.4 X request tracing

Use an X protocol tracer to count requests and identify reply waits. The desired outline-mode trace for one drag is nearly constant regardless of motion event count:

```text
N MotionNotify received
M helper ConfigureWindow requests, M << N due to coalescing
1 final frame/client geometry batch
1 durable ratio event
0 SHM create/destroy during drag
```

The desired live-mode trace contains changed geometry only and no repeat Map requests for stable visibility.

### 23.5 CPU, allocation, and runtime trace

Keep `pprof` CPU and heap profiles, but add `runtime/trace` around one scripted resize. CPU profiles answer “where was CPU time spent?” Runtime traces show goroutine scheduling, GC pauses, blocking, and timer behavior. Correlate them with resize span IDs.

### 23.6 Golden and property tests

Rendering changes need:

- Golden tests for title strips, borders, focus, accept overlays, menus, bars, tables, fields, plots, and clipping.
- Property tests that subtree layout equals full layout.
- Replay tests that only the final resize operation is required to reproduce committed state.
- Tests that release/cancel always terminate the transaction even when windows disappear.
- Tests that hidden surfaces do not retain large buffers beyond policy.
- Tests that a malformed script snapshot leaves the previous valid scene visible.

### 23.7 Definition of done for resize performance

A performance change is complete when:

- It has before/after traces for at least outline, live terminal, live PBUI client, and slow-client scenarios.
- Motion coalescing and release latency are measured.
- No checked request or reply wait appears in the admitted motion hot path without an explicit design reason.
- Durable operations per drag equal one on success and zero on cancel.
- Resource creation during stable-capacity drag is zero.
- Correctness tests cover destroy/unmap/fullscreen/workspace switch during drag.


---

# Part IV. A fully scriptable presentation-based UI

## 24. Define the semantic target before defining widgets

A widget API can easily become a list of drawing calls and callbacks. That would miss the system's distinguishing idea. The target is not “React in Goja” and not “let scripts draw into X windows.” The target is a desktop where visible objects preserve identity, type, actions, documentation, and acceptance semantics across every surface.

A complete PBUI node can answer these questions:

- What object does this output represent?
- What presentation type is it using in this view?
- Which other types can accept it through subtyping or coercion?
- Which verbs are applicable, and which process owns each verb?
- What visual face should be used in this context?
- What region is pointer-sensitive?
- How does keyboard focus reach it?
- What documentation should appear on hover?
- How is it serialized when crossing a process boundary?
- Which state updates when it is activated?

CLIM's presentation types combine display, input acceptance, and type relationships. HyperCard made visible objects scriptable and routed messages through an object hierarchy. Smalltalk environments made inspection and modification part of ordinary development. The go-go-wm contribution is to combine those properties with process isolation, X11 window management, typed broker objects, and a modern embedded JavaScript runtime.

### 24.1 Five non-negotiable semantics

**First, presentations are not limited to controls.** A visual node, text span, plot point, row, image region, or composite group can carry a presentation.

**Second, `accept` is a desktop input mode, not a modal dialog owned by the requesting app.** Compatible presentations across processes become candidate input. The requestor receives a typed object, not coordinates or widget identity.

**Third, verbs are open and type-directed.** A process can attach a verb to a type it did not define. Menus are assembled from the registry at use time.

**Fourth, views are distinct from values.** A `player` can appear as a compact chip, a table row, a radar summary, an inspector card, or a shot-chart selection. The object identity and type remain stable.

**Fifth, scripting cannot compromise the host loop.** JavaScript may describe state, trees, handlers, and effects, but Go owns normalization, layout, hit testing, drawing, X resources, focus, grabs, and final commit.

### 24.2 The basketball prototype as a requirements test

A generic widget proposal is incomplete unless it can express the basketball prototype naturally:

- The leaders table has sortable columns, selectable player rows, numeric alignment, and presentation-sensitive names.
- The shot chart has court geometry plus hundreds of typed shot markers and shared player selection.
- The radar chart has axes, labels, polygons, comparison colors, and typed player series.
- Trends have plots with hover/click targets.
- Standings contain team presentations nested in rows.
- The watchlist stores object references and re-presents them in another view.
- Inspector and Trace surfaces react to desktop-wide interactions.

If the API requires each application to flatten all of those into ad hoc rectangular buttons, it is not a PBUI widget system. It is the current region list with more syntax.

## 25. Review of the current `ui` module

The current `require("ui")` API has the correct safety boundary:

- JavaScript builders produce plain data.
- `app({render, actions, verbs, onKey})` owns JS callables on the runtime loop.
- Render output is normalized into a Go `uispec.Spec`.
- The last normalized snapshot is protected by a mutex.
- WM tile and standalone host renderers read the snapshot without entering the VM.
- Actions post to the JS loop, execute handlers, rerender, install the new snapshot, and request a host redraw.

This design should be generalized, not replaced.

### 25.1 Limitations of the current row/segment IR

The current IR can represent text, hints, object chips, buttons, tables, images, and fields. It cannot yet express:

- Nested row/column/stack composition with stable identity.
- Constraints, alignment, wrapping, min/max sizes, or intrinsic measurement protocols.
- Clipping, scrolling, virtualization, transforms, or z-order inside a surface.
- A presentation wrapper around arbitrary visual children.
- Reusable style classes and state-dependent styles.
- Focus traversal and text-editing ownership.
- Menus, popovers, modals, tooltips, and surfaces as first-class objects.
- Fine invalidation or retained layers.
- Asynchronous data/resource states.
- Component-local state that survives keyed tree updates.

The direct solution is a hierarchical scene description with a small host-owned widget protocol.

## 26. The Presentation Scene IR

Call the normalized form `SceneSpec` and its installed, host-owned form `CompiledScene`.

### 26.1 Scene node shape

A practical normalized node:

```go
type SceneNode struct {
    Key       string
    Kind      NodeKind
    Children  []SceneNode

    Layout    LayoutProps
    Style     StyleRef
    Visual    VisualProps

    Present   *PresentationSpec
    Input     InputProps
    Handlers  HandlerRefs
    Semantics SemanticProps

    Resource  *ResourceRef
}
```

Every node has a stable key within its parent. `Kind` selects a Go-owned protocol implementation. Properties are normalized structs rather than untyped maps in the compiled form.

### 26.2 Core node kinds

Keep the initial vocabulary compact but composable.

| Category | Node kinds | Purpose |
|---|---|---|
| Layout | `row`, `column`, `stack`, `grid`, `padding`, `align`, `spacer`, `scroll`, `virtual-list`, `split` | Compute child constraints and placement. |
| Visual | `text`, `rect`, `border`, `line`, `path`, `image`, `icon`, `canvas`, `clip` | Produce draw operations. |
| Data display | `table`, `tree`, `sparkline`, `bar-chart`, `plot`, `markdown` | Host-owned composite renderers with data contracts. |
| Controls | `button`, `field`, `checkbox`, `radio`, `slider`, `select`, `tabs`, `menu-item` | Standard interaction and focus semantics. |
| PBUI | `present`, `object-face`, `accept-overlay`, `doc-region` | Associate typed objects and semantic interaction with any subtree. |
| Surface anchors | `menu-anchor`, `popover-anchor`, `tooltip-anchor`, `drag-region` | Connect scene geometry to surface manager behavior. |

`canvas` should not be an unrestricted imperative drawing callback. It should contain a data-only list of primitives or reference an immutable host resource. This keeps rendering deterministic and VM-free.

### 26.3 Presentation wrapper

A presentation node can wrap any child:

```js
ui.present(
  { ptype: "player", value: player.id, label: player.name,
    doc: `${player.team} · ${player.pts} PPG` },
  ui.group({
    key: `shot-${shot.id}`,
    children: [
      ui.circle({ cx: shot.x, cy: shot.y, r: 4,
                  class: shot.made ? "shot-made" : "shot-miss" }),
      shot.selected && ui.ring({ cx: shot.x, cy: shot.y, r: 7 })
    ]
  })
)
```

The compiler records the union of visible child bounds after transforms and clipping. The presentation supplies object, documentation, accept compatibility, menu target, and default activation policy. The child supplies its face.

For non-rectangular marks, hit policy can be:

```js
ui.present(obj, mark, { hit: "painted" })
ui.present(obj, mark, { hit: { shape: "circle", cx, cy, r: 8 } })
ui.present(obj, mark, { hit: { path } })
ui.present(obj, mark, { hit: "bounds" })
```

The host compiles these into a spatial hit index.

### 26.4 Immutable snapshots and stable keys

JavaScript renders an immutable tree. A new tree replaces the prior desired tree. Stable keys let the host match nodes across snapshots and retain:

- Measured text and intrinsic size.
- Scroll position.
- Field selection/cursor state when host-owned.
- Layer caches.
- Resource handles.
- Animation state, if later supported.
- Accessibility/focus identity.

A key must be semantically stable, not an array index when rows can reorder. Development mode should warn on duplicate or unstable keys.

### 26.5 Normalization

Normalization occurs on the JS loop immediately after `render()` returns. It should:

- Convert builder objects and plain objects into one schema.
- Validate required fields and ranges.
- Validate ptypes and JSON-serializable values.
- Resolve style class names and resource handles to stable IDs.
- Replace handler functions with runtime-owned handler IDs.
- Stamp the runtime/surface generation.
- Enforce node and depth limits.
- Produce source-coordinate paths for errors, such as `root.children[3].rows[41].cells[2]`.

The normalized snapshot contains no `goja.Value`, function, channel, file descriptor, XID, or Go pointer to script-owned mutable state.

### 26.6 Compilation

Compilation is host-side and may occur on a worker for expensive pure work, provided installation and X resources remain loop-owned. It produces:

```go
type CompiledScene struct {
    Generation uint64
    Root       *CompiledNode
    KeyIndex   map[NodeKey]*CompiledNode
    FocusGraph *FocusGraph
    HitIndex   *HitIndex
    Layers     *LayerTree
    Resources  []ResourceLease
    Size       Size
}
```

A failed compile leaves the previous valid scene installed and emits a script diagnostic. This is essential for live editing: one malformed frame should not blank a bar or modal that the user needs to recover.

## 27. Diffing and invalidation

A retained scene engine is valuable only if it can decide what did not change.

### 27.1 Invalidation classes

Each property belongs to one or more classes:

| Class | Examples | Required work |
|---|---|---|
| Structure | node kind, child list, key | Recompile affected subtree; layout and paint. |
| Layout | text/font metrics, padding, constraints, visibility, table columns | Measure/layout affected ancestors and descendants; paint changed bounds. |
| Paint | color, border, selected state, glyph content at same metrics | Repaint affected layer; no layout if bounds stable. |
| Semantic | ptype/value/doc/verb target, accept compatibility | Rebuild semantic index/overlay; often no base repaint. |
| Input | handler ID, focusable, cursor, drag policy | Rebuild hit/focus metadata; no paint unless state face changes. |
| Resource | image handle generation, font asset | Re-resolve resource; paint and perhaps layout if intrinsic size changed. |

The node implementation declares which fields have which effect. Scripts do not decide invalidation manually.

### 27.2 Layout propagation

If a text label changes width inside a horizontal row, the text node and ancestors up to the nearest constraint boundary become layout-dirty. Siblings may move. A background color change remains paint-dirty at that node. A selected row can repaint its row layer without recomputing column widths.

The compiler should track dependency edges:

```text
child intrinsic size -> parent layout -> sibling positions -> parent bounds
resource intrinsic size -> image measure -> ancestor layout
accept generation -> semantic overlay only
```

### 27.3 Damage accumulation

Paint produces damage rectangles in surface coordinates. Merge rectangles when the additional overdraw is below a threshold; otherwise preserve separate regions. A simple algorithm is sufficient initially:

```text
for each new rect:
    merge with an existing rect if union_area <= 1.4 * sum_area
cap region count at 32; above cap, use bounding box
```

Damage is clipped by scroll and clip nodes. Each retained raster layer receives local damage; clean layers are composited without rerendering.

### 27.4 Semantic overlays

Accept-mode highlighting should not ask every script to rerender and should not repaint full content. The compiled scene already knows presentation regions and ptypes. When broker accept state changes:

1. Increment `AcceptGeneration`.
2. Query presentation regions compatible with accepted types.
3. Damage only the overlay bounds.
4. Paint a host-defined highlight, cursor, or badge layer.

A script may supply style tokens such as `acceptable`, `accept-hover`, and `accept-selected`, but the host applies them. This preserves desktop-wide consistency and turns the current `repaintAllFrames` into small overlay damage.

### 27.5 Text and glyph caches

Text is common in bars, menus, tables, and REPL output. Cache shaping/measurement by `(font, size, text, options)` and raster glyphs or complete short labels as appropriate. Theme color changes should not invalidate geometry. If the renderer stores alpha masks for glyph runs, recoloring can avoid reshaping.

## 28. Presentation types, views, and translators

The current `pbui.Object` contains `Ptype`, JSON value, label, and documentation. This is an effective wire object. A full PBUI system needs a registry around it.

### 28.1 Type descriptor

```go
type PresentationType struct {
    Name        string
    Parents     []string
    Validate    JSONSchemaRef
    DefaultView string
    Views       map[string]ViewDescriptor
    Coercions   []CoercionDescriptor
    URI         URIDescriptor
}
```

Registration can be process-owned and broker-mediated, with a core set supplied by the WM. Type names remain stable slugs. Values remain JSON for wire safety.

### 28.2 Subtyping

Accept compatibility should support more than exact string equality. Examples:

```text
player        <: person
team          <: organization
file-path     <: pathname
git-commit    <: git-object
wm.tile       <: desktop-object
command       <: executable-object
```

A request for `person` should make `player` presentations acceptable. Multiple inheritance should be used cautiously but is valuable for domain types, as CLIM demonstrated. The broker can compute a type lattice from registered descriptors and reject cycles.

Compatibility:

```go
func Accepts(requested []TypeSpec, offered TypeSpec) Compatibility
```

The result can contain exact/subtype/coercion rank. Menus and highlights may show when a coercion will occur.

### 28.3 Coercions and translators

CLIM presentation translators convert an input presentation into another type in context. A process-safe PBUI equivalent is a registered translator:

```go
type Translator struct {
    ID       string
    From     []string
    To       string
    Label    string
    Owner    string
    Priority int
}
```

Examples:

- `player -> team` by selecting the player's team.
- `file-path -> directory` by taking the parent directory.
- `git-commit -> git-ref` by resolving a suitable ref, if available.
- `window -> workspace` by reading its current workspace.

A translator may be pure and broker-executable when expressed as a safe data transform, or owner-executed like a verb. `accept("team")` could allow clicking a player and offer or automatically apply the unambiguous translator, depending on policy.

Do not introduce implicit conversion everywhere initially. Start with subtyping and explicit translator menus, then add opt-in automatic coercion where the user can predict it.

### 28.4 Views

A type can expose named views:

```js
pbui.type({
  name: "player",
  parents: ["person"],
  views: {
    chip: player => ui.playerChip(player),
    row: player => ui.playerRow(player),
    card: player => ui.playerCard(player),
    compact: player => ui.text(player.name)
  }
});
```

The functions execute on the registering runtime to produce normalized scene data, not during host paint. View results can be cached by `(type, value hash, view, theme generation, view options)` if they are declared pure. A surface can request `ui.object(obj, {view: "chip"})`; if no view is registered or the owner is unavailable, the host uses `Label` or a standard JSON face.

For cross-process robustness, consider two view classes:

- **Portable view descriptors:** data templates or host-known view kinds stored by the broker.
- **Owner-rendered views:** asynchronous snapshots produced by a process. They require lifecycle and timeout handling.

Start with core host views and in-process JS views. Add remote owner-rendered views only when a concrete use case justifies the complexity.

### 28.5 Verbs

The existing verb registry already has the right open-world ownership model. Extend descriptors with:

```go
type Verb struct {
    ID          string
    Label       string
    Ptypes      []string
    Accepts     []string
    Owner       string
    Group       string
    Order       int
    EnabledWhen PredicateRef
    Result      *TypeSpec
    Effects     EffectClass
    Shortcut    string
}
```

`EnabledWhen` should remain data/predicate-based or be evaluated asynchronously on the owner loop with caching. Menus must remain responsive if an owner is slow; show disabled/loading state rather than blocking X input.

### 28.6 Object identity and values

JSON values are sufficient for immutable value objects. Some desktop objects represent live entities: a window, surface, REPL cell, or process. Use stable opaque IDs in the JSON value and resolve them through a host/service registry. Never serialize raw pointers or XIDs as authority. An XID may appear as diagnostic data, but operations should address a generation-stamped logical ID such as `window:42@7` so stale references fail safely.

## 29. Input routing, focus, and modal scopes

A fully scriptable desktop cannot route input through a collection of special cases. Menus, launchers, modals, text fields, floating dialogs, bars, drag operations, and accept mode all compete for pointer and keyboard interpretation. Introduce an explicit input-scope stack.

### 29.1 Input scope

```go
type InputScope struct {
    ID            ScopeID
    Kind          ScopeKind
    Surface       SurfaceID
    Owner         OwnerID
    Parent        *ScopeID
    Modal         bool
    PointerPolicy PointerPolicy
    KeyPolicy     KeyPolicy
    Dismiss       DismissPolicy
    Restore       FocusRestoreToken
    Generation    uint64
}
```

Examples:

- The desktop base scope routes global bindings, tile focus, frame clicks, and ordinary surface events.
- A menu scope consumes arrow/enter/escape and pointer events inside the menu; outside click dismisses it and may replay the click according to policy.
- A launcher scope owns typed input while open.
- A modal scope limits activation to one surface subtree and its allowed child popups.
- A divider-drag scope owns pointer motion/release and Escape cancel.
- An accept scope changes semantic activation but does not necessarily block ordinary navigation.

The topmost scope receives first refusal. A scope returns one of `consumed`, `pass`, `dismiss-and-retry`, or `defer`. This eliminates scattered checks such as “if launcher != nil else if menu != nil else if accepting...” from unrelated event handlers.

### 29.2 Focus target

Extend the successful `focusState` pattern to distinguish:

```go
type FocusTarget struct {
    Kind      FocusKind // tile client, float client, internal node, overlay node, none
    Surface   SurfaceID
    Node      NodeKey
    Client    xproto.Window
    Generation uint64
}
```

A text field in a WM-rendered tile focuses a scene node, while X input focus remains on the host frame/window. A normal client focuses its client X window. A popup field focuses the popup X window and a scene node. The focus manager owns restoration when scopes close.

### 29.3 Key routing

A key event passes through:

1. Global passive grabs and hard WM emergency bindings.
2. Active input scope bindings, such as Escape to close a modal.
3. Focused scene node editing/activation.
4. Surface-level `onKey` fallback.
5. Optional desktop default behavior.

The policy from the launcher design remains sound: modifier chords reserved by the WM do not enter ordinary surface text input. Make the reserved modifier set configurable and visible to scripts.

### 29.4 Pointer routing

The compiled scene returns a z-ordered hit path:

```text
surface -> clipped ancestors -> presentation wrapper -> control -> visual leaf
```

Pointer dispatch can use capture and bubble phases, but keep the first API small. A normalized event envelope:

```go
type EventEnvelope struct {
    Surface     SurfaceID
    Node        NodeKey
    Generation  uint64
    Type        string
    LocalX      float64
    LocalY      float64
    RootX       int
    RootY       int
    Button      int
    Key         string
    Code        string
    Modifiers   []string
    Object      *pbui.Object
    TimestampMs int64
}
```

The event contains data only. Dispatch posts it to the JavaScript owner. The host never calls a handler synchronously while holding X-facing state.

### 29.5 Default actions

A presentation can define default activation without replacing control semantics. For example:

- Left-click a player shot marker: set shared focus to that player.
- Right-click: request the broker menu for the player object.
- In accept mode: left-click answers the accept instead of running the ordinary activation, unless the accept policy permits an alternate modifier.
- Hover: update mouse documentation and optional inspector focus.

The host resolves precedence consistently. Scripts can configure policies, but each surface should not invent its own accept behavior.

### 29.6 Drag gestures

Drag regions should use host-owned gesture recognition:

```js
ui.dragRegion({
  key: "column-resize",
  axis: "x",
  threshold: 4,
  cursor: "col-resize",
  onStart: "resizeStart",
  onMove: "resizeMove",
  onEnd: "resizeEnd",
  onCancel: "resizeCancel"
}, child)
```

The host coalesces move events and sends latest-wins envelopes. Script callbacks should not receive 1000 events per second. For host-owned behaviors such as window moving or split resizing, the handler ID can select a native action rather than crossing into JS.

## 30. Surface manager: tiles, windows, bars, menus, modals, and taskbars

A `SceneSpec` describes content. A `Surface` describes where that content lives and how it participates in X11, stacking, focus, work areas, lifetime, and security.

![Target retained PBUI pipeline](figures/pbui-scene.png)

*Figure 2. JavaScript owns state and composition. The host owns a VM-free normalization, diff, layout, damage, surface, input, and X11 path.*

### 30.1 Surface descriptor

```go
type SurfaceSpec struct {
    ID          string
    Kind        SurfaceKind
    Title       string
    Root        SceneSpec
    Placement   PlacementSpec
    Size        SizePolicy
    Stack       StackPolicy
    Input       InputPolicy
    Lifetime    LifetimePolicy
    WorkArea    WorkAreaPolicy
    Capabilities []string
}
```

Surface kinds:

| Kind | X representation | Typical use |
|---|---|---|
| `tile` | Content child inside a managed tile frame | Script apps, REPL, dashboards, inspectors. |
| `window` | Normal top-level X client or WM-owned standalone host | Independent PBUI applications. |
| `bar` / `dock` | Override-redirect or EWMH dock window, monitor-anchored | Menu bar, taskbar, status line. |
| `menu` | Short-lived override-redirect popup | Type-directed verbs, application menus. |
| `popover` | Anchored popup with owner relationship | Detail panels, completion lists. |
| `tooltip` | Non-focusable short-lived popup | Documentation and hover details. |
| `modal` | Overlay/pop-up plus modal input scope | Confirmation, form, command palette. |
| `notification` | Timed top-band surface | Desktop notifications and script status. |
| `overlay` | General WM chrome layer | Drag outlines, accept hints, debug visualizations. |

### 30.2 Stacking bands

Do not let each surface call `Stack(Above)` independently. Maintain ordered bands and one restacker.

![Surface stacking bands](figures/surface-stack.png)

*Figure 3. Surface kinds belong to explicit stacking bands and input scopes. Exact ordering within a band is managed centrally.*

A reasonable order from top to bottom:

1. Emergency/debug overlays and active drag indicators.
2. Global menus, launchers, notifications, and chrome that must remain reachable.
3. Modal surfaces and their child popovers.
4. Ordinary popovers, menus, and tooltips.
5. Focused float and other floats.
6. Tiled frames and internal tile surfaces.
7. Root background.

Fullscreen is a state transition that temporarily moves one application frame into the fullscreen band; explicit menus may remain above according to policy.

### 30.3 Bars and taskbars

A script-defined bar should not manually calculate root coordinates. It declares monitor anchor, thickness, and work-area reservation:

```js
ui.surface({
  id: "main-topbar",
  kind: "bar",
  placement: { monitor: "primary", edge: "top", thickness: 28 },
  workArea: { reserve: true },
  input: { focusable: false },
  root: renderTopBar()
});
```

The surface manager:

- Resolves monitor geometry through RandR.
- Places one window per monitor or one spanning window according to spec.
- Sets EWMH window type to dock when appropriate.
- Publishes `_NET_WM_STRUT_PARTIAL` so work areas exclude reserved space.
- Recomputes `wmcore` layout areas when bars appear, disappear, or change thickness.
- Keeps global bars above floats according to stacking policy.
- Gives scripts logical monitor IDs, not raw CRTC/output internals.

A taskbar is just a bar whose scene subscribes to window/workspace state. It should read a snapshot or signal store rather than querying the WM during paint.

### 30.4 Menus

The current verb menu creates a new window, renders a complete image, and rerenders/reuploads on hover. The target menu surface retains row layers. Hover damages the old and new row. Keyboard navigation, submenu ownership, outside-click dismissal, and focus restoration live in the menu input scope.

Broker menu assembly remains type-directed. The surface API should also support application-defined menus whose items may be verbs, commands, toggles, or nested menus.

```js
ui.menu.open({
  anchor: event.nodeBounds,
  items: [
    ui.menuItem({label: "Inspect", verb: "any.inspect", object}),
    ui.separator(),
    ui.submenu({label: "Open with", items: openWithItems})
  ]
});
```

The host validates that the script has `ui.overlay.menu` capability and owns the initiating surface or has global-menu authority.

### 30.5 Modals

A modal has three independent properties:

- Visual placement and stacking.
- Input scope and allowed descendants.
- Completion protocol.

Use a promise-like API because a modal has one completion result:

```js
const result = await ui.modal({
  id: "delete-workspace",
  title: "Delete workspace?",
  owner: currentSurface,
  root: renderConfirmation(),
  resultType: "boolean"
});
```

The host opens a modal scope, remembers focus, installs the scene, and resolves the promise on `close(result)` or cancellation. Closing due to script reload, owner death, or workspace disappearance resolves with a typed cancellation outcome. The X loop never waits for the promise.

### 30.6 Popovers and tooltips

Placement is a host service. The script supplies an anchor rect and preferred placements. The host flips, slides, and clips against monitor work area. A tooltip should not take focus. A popover may. Transient ownership is represented logically and, where useful, through X transient hints.

### 30.7 Lifetime

Surface lifetime policies:

- `persistent`: restored after script runtime restart if the owner reclaims the ID.
- `owner`: destroyed when owner disconnects.
- `scope`: destroyed when an input scope closes.
- `timeout`: notification/tooltip expiration.
- `manual`: explicit close.

Every surface has a generation. Events for a destroyed generation are dropped before reaching JS.

## 31. JavaScript ownership, backpressure, capabilities, and hot reload

Full scriptability increases the number of asynchronous crossings. The current owner-loop law should be written as a public contract.

### 31.1 Concurrency contract

1. A `goja.Runtime` is touched only by its owner loop.
2. The WM/X loop never invokes JavaScript.
3. Foreign loops post serialized closures or envelopes to the owner.
4. Render hosts consume normalized immutable snapshots only.
5. No X-facing operation waits indefinitely for JS.
6. Promise settlement posts to the owner loop.
7. Every callback and resource is generation-stamped.
8. Queue semantics are chosen by event class and are observable.

### 31.2 Per-surface latest-snapshot mailbox

A surface renderer usually cares about the newest complete desired scene. If a script produces scenes A, B, and C before the host installs A, B can be discarded provided effects and input ordering are handled separately.

```go
type SnapshotMailbox struct {
    pending *NormalizedScene // at most one
    wake    chan struct{}      // one token
    replaced uint64
}
```

Handlers and state updates remain ordered on the JS loop. Snapshot delivery is latest-wins. The host exposes replacement counts in developer diagnostics.

### 31.3 Event queues

Separate queues:

- `inputOrdered`: keys, button press/release, composition events; bounded and ordered.
- `pointerLatest`: motion/hover per surface or gesture; latest-wins.
- `brokerOrdered`: lifecycle, accept result, verb run; ordered with overflow error.
- `telemetrySampled`: resize previews, mouse docs, traces; sampled/aggregated.
- `snapshotLatest`: newest complete scene.

A single generic queue cannot implement all of these correctly.

### 31.4 Script time budgets

A handler may run too long even on its own loop. Measure handler duration and queue lag. Development mode should report:

```text
surface main-topbar handler onClick:refresh took 84 ms
JS event queue lag: 137 ms
3 pointer-move envelopes replaced
2 pending scene snapshots replaced
```

Optional runtime policies:

- Warn at 16 ms.
- Mark surface degraded at 100 ms repeated.
- Interrupt or restart runtime at a configurable hard limit where goja interruption is safe.
- Keep last valid scene visible during recovery.

Do not freeze the desktop because a status widget entered an infinite loop.

### 31.5 Capabilities

Scripts should receive only declared authority. Suggested capabilities:

| Capability | Authority |
|---|---|
| `ui.basic` | Create content inside surfaces already owned by the runtime. |
| `ui.window` | Create standalone normal windows. |
| `ui.overlay.menu` | Open owner-anchored menus/popovers/tooltips. |
| `ui.overlay.modal` | Open modal scopes associated with owned surfaces. |
| `ui.global.bar` | Create bars/docks and reserve work area. |
| `ui.global.notification` | Create desktop notifications. |
| `wm.query` | Read tree, windows, monitors, focus, themes. |
| `wm.mutate` | Apply layout/focus/workspace operations. |
| `wm.bind` | Register global keybindings. |
| `process.spawn` | Execute processes. |
| `filesystem.read` / `filesystem.write` | Access host filesystem through scoped APIs. |
| `network` | Make network requests through a controlled module. |
| `pbui.register-type` | Register type/view/translator metadata. |
| `pbui.register-verb` | Register verbs. |

The in-process rc runtime may be trusted by default, while downloaded or project-local scripts use manifests and prompts. Capabilities should be checked during normalization and at effect execution, not merely at module import.

### 31.6 No raw X authority

Do not expose raw X connection objects, window IDs as mutable handles, pixmap creation, pointer grabs, or arbitrary property writes to JavaScript. Provide narrow host operations. This protects invariants and makes a future non-X11 backend possible.

### 31.7 Resource handles

Images, fonts, icons, and large data use opaque handles:

```js
const logo = await ui.resources.image.fromFile(path);
ui.image({resource: logo, fit: "contain"});
```

The handle is owner- and generation-scoped. The resource manager enforces byte limits, decoding limits, and supported formats. A normalized scene contains the handle ID, not file bytes or Go objects.

### 31.8 Hot reload

A runtime reload proceeds as a transaction:

1. Start a new runtime generation.
2. Load modules and validate manifest/capabilities.
3. Let the new runtime claim persistent surface IDs and optionally receive serialized state from the old generation.
4. Normalize and compile initial scenes.
5. Atomically switch each successfully claimed surface.
6. Close unclaimed old surfaces according to policy.
7. Drop events and callbacks from the old generation.
8. Release old resources after in-flight host work completes.

A bad reload leaves old surfaces visible until the new generation proves it can render. This is more useful than destroying the desktop UI before evaluating the new script.

### 31.9 State migration

Keep host-owned UI state—scroll offsets, focus within a keyed scene, table column widths—separate from application state. Application state can opt into serialization:

```js
ui.app({
  id: "basketball",
  initialState,
  serializeState(state) { return {...state}; },
  migrateState(old, oldVersion) { ... }
});
```

The state payload must be JSON and size-bounded.

## 32. Proposed JavaScript API

The API below is illustrative. The important aspect is the boundary: builders create data; effects return promises or handles; the host compiles and renders.

### 32.1 Application and state

```js
const ui = require("ui");
const pbui = require("pbui");
const wm = require("wm");

const app = ui.app({
  id: "basketball",
  title: "Basketball Lab",
  version: 1,
  state: {
    selectedPlayer: "p-23",
    sort: {column: "pts", direction: "desc"},
    watchlist: []
  },

  render(ctx) {
    const state = ctx.state;
    return ui.column({
      key: "root",
      class: "app",
      children: [
        renderToolbar(state),
        ui.split({
          key: "main-split",
          direction: "row",
          ratio: 0.42,
          first: renderLeaders(state),
          second: renderShotChart(state)
        })
      ]
    });
  },

  handlers: {
    selectPlayer(ctx, ev) {
      ctx.update(s => ({...s, selectedPlayer: ev.object.value}));
    },
    toggleWatch(ctx, ev) {
      ctx.update(s => togglePlayer(s, ev.object.value));
    }
  }
});

app.tile({workspace: "sports"});
```

`ctx.update` changes runtime state, schedules at most one render for the turn, normalizes the result, and submits the newest snapshot.

### 32.2 A presentation-rich table

```js
function renderLeaders(state) {
  return ui.table({
    key: "leaders",
    columns: [
      {key: "rank", label: "#", width: 36, align: "right"},
      {key: "player", label: "Player", flex: 1},
      {key: "pts", label: "PTS", width: 64, align: "right"},
      {key: "reb", label: "REB", width: 64, align: "right"}
    ],
    rows: sortedPlayers(state).map((p, index) => ({
      key: p.id,
      selected: p.id === state.selectedPlayer,
      cells: {
        rank: ui.text(String(index + 1)),
        player: ui.present(
          pbui.object("player", p.id, {label: p.name, doc: playerDoc(p)}),
          ui.row({children: [
            ui.swatch({tone: teamTone(p.team)}),
            ui.text(p.name),
            ui.tag(p.team)
          ]}),
          {onActivate: "selectPlayer"}
        ),
        pts: ui.number(p.pts, {precision: 1}),
        reb: ui.number(p.reb, {precision: 1})
      }
    })),
    virtual: {rowHeight: 25, overscan: 8},
    sort: state.sort,
    onSort: "sortLeaders"
  });
}
```

The table owns column measurement, clipping, scrolling, keyboard row navigation, and virtualization. The player cell remains a real `player` presentation.

### 32.3 Plot presentations

```js
function renderShotChart(state) {
  const shots = shotsFor(state.selectedPlayer);
  return ui.plot({
    key: "shots",
    xDomain: [0, 500],
    yDomain: [0, 470],
    background: courtPrimitives(),
    marks: shots.map(shot =>
      ui.present(
        pbui.object("shot", shot.id, {
          label: shot.made ? "Made shot" : "Missed shot",
          doc: `${shot.distance} ft · ${shot.period}Q`
        }),
        ui.circleMark({
          key: shot.id,
          x: shot.x,
          y: shot.y,
          r: shot.id === state.hoverShot ? 6 : 4,
          class: shot.made ? "made" : "miss"
        }),
        {hit: {shape: "circle", radius: 8}, onHover: "hoverShot"}
      )
    )
  });
}
```

The plot compiles marks into retained primitives and a spatial index. Hover damages only the old and new marks plus documentation line.

### 32.4 A custom top bar

```js
const bar = ui.surface({
  id: "devbar",
  kind: "bar",
  placement: {edge: "top", monitor: "each", thickness: 26},
  workArea: {reserve: true},
  capabilities: ["ui.global.bar", "wm.query"],
  render(ctx) {
    const snap = ctx.signals.wm;
    return ui.row({
      key: "bar",
      class: "topbar",
      children: [
        ui.workspaceList({
          key: "workspaces",
          workspaces: snap.workspaces,
          current: snap.current,
          onActivate: "switchWorkspace"
        }),
        ui.spacer({flex: 1}),
        ui.present(
          pbui.object("window", snap.focused.id, {label: snap.focused.title}),
          ui.text(snap.focused.title),
          {onContextMenu: "objectMenu"}
        ),
        ui.clock({format: "HH:mm"})
      ]
    });
  }
});
```

Signals are snapshots pushed to the runtime. The bar does not synchronously query the WM during render.

### 32.5 Type-directed modal composition

```js
pbui.verb({
  id: "player.compare",
  label: "Compare with...",
  ptypes: ["player"],
  async run(player) {
    const other = await pbui.accept("player", {
      prompt: `Compare ${player.label} with another player`
    });
    if (!other) return;

    await ui.modal({
      id: "player-comparison",
      title: "Player comparison",
      root: renderComparison(player, other),
      resultType: "void"
    });
  }
});
```

The verb owner composes broker-level accept and surface-level modal APIs. Neither blocks the X loop.

### 32.6 Commands and effects

Effects should be explicit:

```js
await wm.apply(wm.op.split({leaf, direction: "row"}));
await ui.clipboard.writeText(text);
await ui.notifications.show({title, body, object});
await ui.resources.image.fromFile(path);
```

A render function must remain pure with respect to host effects. Development mode should detect or reject effect calls while rendering.

## 33. Developer tools are part of the widget architecture

A novel retained PBUI system will be difficult to debug without first-class inspection.

### 33.1 Scene inspector

A built-in inspector should show:

- Surface and runtime generation.
- Scene tree with keys and kinds.
- Normalized properties.
- Measured and laid-out bounds.
- Clip and transform chain.
- Layout, paint, semantic, and resource dirty flags.
- Layer cache size and damage.
- Presentation object/type and compatible accepts.
- Handler IDs and owner runtime.
- Focus and hit-test path.

The inspector itself should use PBUI presentations so a node, surface, type, or resource can be right-clicked and acted upon.

### 33.2 Paint flashing and damage visualization

Debug overlays:

- Flash repainted rectangles.
- Draw layout bounds and baselines.
- Draw hit regions and z-order numbers.
- Show clipped versus unclipped bounds.
- Display current input scope stack.
- Show frame budget and queue lag in a small overlay.

These overlays must be host-owned and cheap enough to trust.

### 33.3 Event trace

For one interaction, record:

```text
#481 X ButtonPress frame=tile:n17 local=(431,9)
#482 hit node=split-grip object=<wm.split:n9>
#483 scope push drag:divider:n9 generation=74
#484 MotionNotify seq=1 replaced=0
#490 MotionNotify seq=7 replaced=6
#491 resize preview ratio=.603 mode=live wm=3.2ms
#508 resize preview ratio=.618 mode=outline reason=client-sync-pending
#527 ButtonRelease final=.625
#528 OpSetRatio n9 .625
#529 scope pop drag:divider:n9; focus restored tile:n17
```

This connects X events, hit testing, scope transitions, JS callbacks, PBUI semantics, and model operations in one timeline.

### 33.4 Script diagnostics

Normalize errors should be structured:

```text
surface devbar generation 18 rejected
root.children[2].rows[41].cells["player"].present.value
  expected JSON value for ptype "player"; received function
previous generation 17 remains installed
```

Handler errors should include surface, node key, event sequence, and source stack. Repeated errors can disable one handler while preserving the surface.

### 33.5 Performance inspector

Per surface:

- JS handler p50/p95 and queue lag.
- Snapshots produced/installed/replaced.
- Normalize, compile, measure, layout, paint, composite, and X commit timing.
- Dirty rectangle area versus surface area.
- Layer and resource bytes.
- Hit-test count and cost.
- Events delivered/coalesced/dropped.

The inspector should make a slow bar or plot attributable to a node and stage, not merely to the process.


---

# Part V. Systematic implementation plan

## 34. Build the WM by invariants and layers

A new contributor should not begin by editing the event handler that happens to produce the visible bug. Start by identifying the owner of the invariant, then trace the event/model/X path.

### 34.1 Core invariants

Write these into package documentation and tests:

1. **One goroutine owns WM and X-facing mutable state.** Foreign goroutines post work.
2. **Every managed X client belongs to at most one host record and one lifecycle owner.** Reverse maps agree or teardown fails loudly in development mode.
3. **The tiling tree is mutated only by `wmcore.Apply` and contains no floating or transient shell state.**
4. **A durable operation stream reproduces committed desktop state.** Transient previews are separate.
5. **Exactly one focus target is active; focus restoration is explicit.** Fullscreen and modal scopes may temporarily override it.
6. **Fullscreen owns its frame's geometry and focus until it exits.**
7. **A surface's render hot path never enters JavaScript.**
8. **No reply wait, checked request, process execution, filesystem call, or unbounded queue drain occurs in an interactive X hot path.**
9. **Reconciliation is idempotent.** Applying the same desired snapshot twice produces no additional geometry, mapping, or paint work.
10. **Hidden workspace surfaces are unmapped and large resources are released according to policy.**
11. **A pointer preview may be coalesced or dropped; a committed operation, key press, lifecycle event, or accept result may not be silently replaced.**
12. **Every callback, scene, surface, resource, and logical window reference is generation-stamped when its owner can reload or die.**

A change that cannot state which invariant it preserves or extends is not ready to merge.

### 34.2 Layer order for a new WM

For onboarding and future ports, build and validate in this order:

1. **Connection and ownership.** Open X, inspect screen/root, select SubstructureRedirect, fail cleanly if another WM owns it.
2. **Lifecycle.** MapRequest, save set, frame creation, reparent, map, destroy/unmap teardown.
3. **ICCCM basics.** Titles, classes, normal hints, transients, `WM_DELETE_WINDOW`, focus model, synthetic ConfigureNotify.
4. **Pure layout.** Desktop/tree/ops/layout with no X dependency.
5. **Reconciliation.** Desired geometry snapshot to idempotent X request plan.
6. **Focus and input.** Passive grabs, click-to-focus, focus state, key routing, pointer gestures.
7. **Workspaces and EWMH.** Client lists, desktops, active window, work areas.
8. **Floating and fullscreen.** Explicit state owners and stacking bands.
9. **Rendering.** Thin chrome, internal content surfaces, caching, Expose repair.
10. **Interactive scheduling.** Latest-wins resize/move previews and exact commits.
11. **IPC and scripts.** Serializable operations, queries, events, capabilities.
12. **Presentation surfaces.** Typed objects, hit regions, scene compiler, overlays.
13. **Multi-monitor and work-area management.** RandR, per-monitor bars, struts, topology changes.
14. **Compatibility and recovery.** Startup adoption, crash survival, session restore, hostile clients.

The order keeps correctness observable. A complex renderer cannot compensate for a broken ConfigureRequest contract, and a rich script API cannot compensate for unclear focus ownership.

### 34.3 Review method for one feature

For a feature such as “scripted modal dialog,” walk through:

- Model state: is it durable, transient, or owner-scoped?
- X representation: which windows, properties, event masks, and stack band?
- Input scope: what receives keys/pointer, what dismisses, what restores focus?
- JavaScript boundary: which data crosses, which loop runs handlers, what timeout applies?
- PBUI semantics: which presentations, verbs, accepts, and documentation regions?
- Rendering invalidation: what changes layout, paint, semantic overlay, or resources?
- Teardown: what happens on owner death, workspace switch, reload, X DestroyNotify, and cancellation?
- Tests and metrics: how will correctness and latency be observed?

This method prevents a feature from being implemented only along its happy path.

## 35. Prioritized roadmap

![Target resize scheduler](figures/target-resize.png)

*Figure 4. The target resize path replaces stale motion, keeps preview state transient, limits geometry to the affected subtree, and chooses live or outline work from an explicit budget.*

### Phase P0: measure and coalesce

**Goal:** make the current path observable and ensure the WM always works on the newest pointer state.

Changes:

- Add `resizeController` with one-slot latest-coordinate mailbox.
- Add sequence numbers, queue-lag measurement, and one update in flight.
- Add stage spans and counters described in Chapter 12.
- Add an IPC debug query for current resize transaction and recent statistics.
- Add an automated Xephyr/Xvfb drag scenario.
- Keep current `OpSetRatio` and renderer temporarily, so this phase isolates scheduling effects.

Acceptance criteria:

- A burst of 1000 synthetic motion events produces far fewer preview updates and no post-release stale replay.
- Release always commits the newest coordinate.
- Existing behavior and screenshots remain otherwise unchanged.

Expected impact: high perceived responsiveness under overload; low implementation risk.

### Phase P1: transient preview and resize modes

**Goal:** remove durable model/event work from preview cadence and provide a cheap fallback.

Changes:

- Add `resizeTransaction` with original and preview ratios.
- Add layout ratio overrides.
- Commit one `OpSetRatio` on release; zero on cancel.
- Add outline helper surface and `outline|live|adaptive` policy.
- Add adaptive budget transitions.
- Fix tiled client ConfigureRequest to send synthetic ConfigureNotify without relayout.
- Treat divider windows as reusable appearance resources; stop repainting every divider on movement.

Acceptance criteria:

- Durable ops per completed drag: exactly one.
- Outline drag creates no client ConfigureNotify until release.
- ConfigureRequest floods do not produce paints or full layout passes.
- Replay of committed operations reproduces the final tree.

Expected impact: very high; moderate implementation risk.

### Phase P2: thin chrome and resource lifetime

**Goal:** remove full-frame decoration surfaces and resize-time SHM churn for normal clients.

Changes:

- Split frame chrome from content host.
- Introduce title child window and server-side background/border policy.
- Migrate title rendering and hit regions to the chrome object.
- Keep full content surfaces only for builtin/script tiles.
- Add capacity-aware/pool-backed content buffers.
- Add double-buffer or completion-safe SHM lifecycle.
- Make map/unmap/stack transitions diff-based.

Acceptance criteria:

- Normal client resize creates/destroys no full-size RGBA or SHM surface.
- Decoration bytes scale with title/border area, not client area.
- Focus/theme changes paint title/focus layers only.
- Memory use for four large client frames falls by an order of magnitude.

Expected impact: very high; higher implementation risk because frame lifecycle changes.

### Phase P3: PBUI client scheduler and retained scene core

**Goal:** prevent PBUI clients from recreating a second full-surface bottleneck and establish widget identity/invalidation.

Changes:

- Coalesce ConfigureNotify and state redraws in `xapp`.
- Cache upload resources and add resize begin/preview/commit support.
- Add stable keys to `uispec` rows/segments.
- Split normalize/measure/layout/paint.
- Add retained layer cache, damage region, and semantic accept overlay.
- Add hierarchical hit/focus representation.
- Convert menu hover and bar updates to damage-based painting.

Acceptance criteria:

- PBUI xapp performs at most one pending redraw and installs exact final size.
- Hovering one menu row repaints only old/new rows.
- Accept-mode transition does not rerun application render or repaint unaffected base layers.
- Existing `ui.row/text/object/button` scripts remain source-compatible through adapters.

Expected impact: high and foundational; staged implementation required.

### Phase P4: generalized widget and surface system

**Goal:** allow safe custom bars, taskbars, menus, popovers, modals, notifications, and rich domain applications.

Changes:

- Add hierarchical SceneSpec node vocabulary.
- Add surface manager and stack bands.
- Add input scope stack and scene-node focus.
- Add capabilities/manifests and opaque resource handles.
- Add type descriptors, subtyping, views, and translators.
- Add per-surface latest snapshot mailboxes and queue classes.
- Add transactional hot reload and generation-stamped events/resources.
- Add inspector, paint flashing, event trace, and performance panels.

Acceptance criteria:

- The basketball prototype can be implemented with typed table cells and plot marks without raw region plumbing.
- A script can create a top bar that reserves work area on each monitor.
- A script can open a modal and nested menu without directly manipulating X focus/grabs.
- Reloading a broken script preserves the previous valid surface and leaves global recovery controls usable.

Expected impact: enables the novel product direction; implement only after P0-P3 make the host predictable.

### Phase P5: advanced protocol and backend work

This phase is optional and should be driven by measured needs:

- Broader `_NET_WM_SYNC_REQUEST` support and client capability database.
- RandR topology transactions and monitor-aware workspace policies.
- XRender or alternate text/compositing backend if software raster remains measured bottleneck.
- A non-X11 surface backend, enabled by keeping SceneSpec and PBUI independent of raw X.
- Accessibility bridges and keyboard navigation metadata.
- Persisted desktop/surface sessions.

Do not begin with a compositor, OpenGL renderer, or generalized animation engine. The current bottleneck is unnecessary work and resource churn, not absence of GPU drawing.

## 36. Code change map

The following map translates the roadmap into repository areas.

### 36.1 `pkg/wmx11/input.go`

- Replace direct divider work with `resizeController.Motion`.
- Route press/release/cancel through `Begin`, `Release`, and `Cancel`.
- Remove `lastPaint` as the sole admission policy.
- Ensure global Escape dispatches through input scopes once available.

### 36.2 New `pkg/wmx11/resize.go`

Own:

- Resize transaction and mode.
- Latest-coordinate mailbox and scheduler.
- Preview ratio calculation and snapping.
- Budget state and adaptive transitions.
- Optional client sync tracking.
- Final commit/cancel.
- Resize metrics.

Keep X execution delegated to reconciliation; keep tree mutation delegated to `WM.Apply` on commit.

### 36.3 `pkg/wmcore/layout.go`

- Add ratio override support.
- Add stable slice result or reusable output path.
- Add subtree layout API after correctness baseline.
- Add metadata to layout items so consumers do not re-find nodes.

### 36.4 `pkg/wmcore/tree.go` and desktop indexing

- Add ephemeral `TreeIndex` outside serialized node structs.
- Keep immutable operation semantics.
- Add tests for index rebuild and subtree equivalence.

### 36.5 `pkg/wmx11/manage.go`

- Split desired-state calculation from execution.
- Replace tiled `ConfigureRequest -> relayout()` with synthetic ConfigureNotify.
- Delegate float/fullscreen geometry to policy owners.
- Migrate frame creation to chrome/content host structure.
- Avoid map/unmap calls without state transitions.

### 36.6 `pkg/wmx11/divider.go`

- Separate divider geometry from appearance.
- Use background pixel or cached pixmap.
- Repaint only on appearance generation changes.
- Optionally reuse the active divider as outline helper.

### 36.7 `pkg/wmx11/wm.go`

- Add `resizeController`, `surfaceManager`, and future `inputScopes` owners.
- Replace broad `relayout` call sites with requests for reconciliation reasons.
- Preserve `ApplyBatch` and operation events.
- Expose metrics/debug queries without letting callers touch state.

### 36.8 `pkg/wmx11/frame_chrome.go` and content hosts

New code should own title windows, border policy, hit regions, title buffers, focus/accept appearance, and chrome lifecycle. Client and PBUI content hosts implement a small interface:

```go
type ContentHost interface {
    Configure(Rect)
    SetVisible(bool)
    Invalidate(Invalidation)
    DropLargeResources()
    Destroy()
}
```

### 36.9 `pkg/xshm`

- Separate backing memory from exact logical viewport where practical.
- Add buffer state (`free`, `drawing`, `server-reading`) and completion handling.
- Add pool integration and memory counters.
- Keep fallback when MIT-SHM or shared pixmaps are unavailable.
- Validate size multiplication and X protocol integer conversions.

### 36.10 `pkg/apps/xapp`

- Add redraw scheduler and latest pending size.
- Cache image/upload resources.
- Add private resize protocol hooks.
- Move from `App.Render` to compiled scene host over time.
- Expose client-side timing counters.

### 36.11 `pkg/apps/uispec`

- Add keys and compatibility adapters.
- Refactor renderer into normalize/measure/layout/paint.
- Add compiled hit/focus structures.
- Keep table/image/field but reimplement as scene nodes.
- Add golden tests per node and invalidation tests.

### 36.12 `pkg/jsmod/uimod`

- Replace flat builder-only vocabulary with scene builders while retaining old exports.
- Normalize function handlers into IDs and generation stamps.
- Add per-surface snapshot mailbox.
- Add app state/update transaction and pure-render guard.
- Add surface effect APIs through a capability-checked backend.

### 36.13 `pkg/pbui`

- Add optional type descriptors, parents, views, and translators as versioned protocol extensions.
- Keep `Object` wire compatibility.
- Add compatibility query APIs and semantic generation events.
- Make registry ownership and disconnect cleanup explicit.

### 36.14 `pkg/wmx11/pbui.go`, bars, launcher, menus

- Move menu/launcher/bar windows under surface manager.
- Replace full rerenders on hover/accept with scene invalidation.
- Keep broker semantics and command registry; change presentation only.

## 37. Testing strategy by layer

### 37.1 Pure model tests

- Operation table tests for every success and error case.
- Generated tree layout invariants: no negative rectangles; children partition parent subject to gaps; deterministic output.
- Replay property for committed ops.
- Preview override does not mutate serialized desktop.
- Subtree layout equivalence.
- Neighbor total-order determinism.

### 37.2 State-machine tests without X

Follow the current focus/fullscreen pattern:

- Resize transaction begin/motion/release/cancel.
- Latest-wins coalescing under arbitrary event sequences.
- Adaptive mode transitions from timing/sync inputs.
- Surface stack ordering.
- Input scope push/pop/dismiss/focus restoration.
- Generation rejection of stale events.
- Capability decisions.

### 37.3 Xvfb integration tests

- Become-WM exclusivity.
- Manage/reparent/save-set lifecycle.
- Synthetic ConfigureNotify contents.
- Client destroy/unmap during resize.
- Float/fullscreen/workspace transitions.
- `_NET_ACTIVE_WINDOW`, client lists, desktops, window state.
- Bar struts and work-area recomputation.
- Modal/menu stacking and focus.
- MIT-SHM fallback and completion behavior.

### 37.4 Script tests

- Builder normalization with precise error paths.
- Handler owner-loop enforcement.
- Snapshot replacement and queue policies.
- Broken render preserves previous scene.
- Hot reload claim/migrate/swap/cleanup.
- Capability denial at normalization and effect time.
- PBUI view/type/translator registration and disconnect cleanup.

### 37.5 Scene tests

- Stable-key diff classification.
- Text change that is paint-only versus layout-changing.
- Scroll clipping and hit testing.
- Non-rectangular plot mark hit geometry.
- Virtual list visible range.
- Semantic accept overlay without base repaint.
- Focus traversal and field editing.
- Damage merge thresholds.

### 37.6 Compatibility smoke suite

Automate startup of common clients, map dialogs, toggle fullscreen/floating, switch workspaces, resize, close via WM_DELETE, and kill the WM to verify save-set recovery. Store protocol traces and screenshots for regressions.

### 37.7 Fuzzing

Good fuzz targets:

- Deserialize arbitrary desktop JSON and apply random valid/invalid ops.
- Normalize arbitrary SceneSpec JSON with depth/node limits.
- Hit test random nested clips/transforms against a slow reference.
- Parse broker messages and PBUI URIs.
- Process random X lifecycle event sequences through display-free state machines.
- Parse `.desktop` entries and script manifests.

## 38. Intern curriculum and implementation exercises

The system is large enough that onboarding should produce small, verifiable changes rather than ask a new developer to “read the WM.”

### Exercise 1: trace one client lifecycle

**Goal:** understand reparenting and ownership.

Tasks:

- Run a purpose-built test window under Xephyr.
- Record MapRequest, property reads, frame creation, reparent, map, focus, UnmapNotify, and teardown.
- Add a debug query that returns frame/client/leaf/generation relationships.
- Explain why the save set is installed before reparenting.

Evidence: annotated X trace and a lifecycle diagram.

### Exercise 2: make ConfigureRequest correct and cheap

**Goal:** learn ICCCM geometry contracts.

Tasks:

- Write a test client that repeatedly requests random sizes while tiled.
- Assert the WM sends synthetic ConfigureNotify with current geometry.
- Remove the full relayout from that path.
- Measure paint/layout counts before and after.

Evidence: request trace showing zero relayouts and correct notifications.

### Exercise 3: implement latest-wins motion

**Goal:** learn event backpressure.

Tasks:

- Build a display-free mailbox/controller test with 1000 motion samples and one release.
- Integrate it into divider drag without changing rendering.
- Add queue-lag and replaced-sample counters.
- Demonstrate that release latency remains bounded under an artificial 20 ms paint delay.

Evidence: before/after timeline.

### Exercise 4: add outline resize

**Goal:** separate preview from commit.

Tasks:

- Create an outline helper window.
- Keep durable tree ratio unchanged during drag.
- Commit one operation on release and restore on cancel.
- Add replay and operation-count tests.

Evidence: one-drag operation log containing one `set-ratio`.

### Exercise 5: split frame chrome

**Goal:** understand X window hierarchy and rendering ownership.

Tasks:

- Add a title child window for normal clients.
- Move title hit regions and paint to it.
- Remove full-frame client decoration buffer.
- Compare allocated bytes and SHM lifecycle counts.

Evidence: memory profile and golden title screenshot.

### Exercise 6: coalesce xapp redraw

**Goal:** understand client-side backpressure.

Tasks:

- Add pending-size mailbox and one redraw token.
- Cache the X image at stable dimensions.
- Instrument render/convert/upload.
- Resize the PBUI basketball prototype and compare total redraw count.

Evidence: client trace where ConfigureNotify count exceeds redraw count and final size is exact.

### Exercise 7: add keys to `uispec`

**Goal:** prepare retained identity without changing visuals.

Tasks:

- Add optional keys to rows and segments.
- Generate compatibility keys for old scripts.
- Warn on duplicates.
- Write a diff that classifies unchanged/moved/changed nodes.

Evidence: unit tests for reorder preserving player-row identity.

### Exercise 8: semantic accept overlay

**Goal:** connect PBUI semantics to retained damage.

Tasks:

- Compile presentation regions by ptype.
- On accept-mode change, generate overlay damage only.
- Verify no script render callback runs and base image hash remains unchanged.

Evidence: paint-flash capture and counters.

### Exercise 9: script-defined bar

**Goal:** learn surfaces, EWMH work area, and capabilities.

Tasks:

- Implement a minimal `bar` surface kind.
- Place it on one monitor and reserve a top strut.
- Render workspace objects and a clock.
- Gate creation behind `ui.global.bar`.

Evidence: EWMH property dump, layout area change, and capability-denial test.

### Exercise 10: plot-mark presentations

**Goal:** prove the widget architecture against the basketball case.

Tasks:

- Add retained plot primitives and a spatial index.
- Wrap shot markers as `shot` presentations and players as `player` presentations.
- Make desktop `accept("player")` answer from a selected plot mark.
- Add right-click verbs and hover documentation.

Evidence: interaction trace connecting plot hit to broker accept result.


---

# Appendices

## Appendix A. X11 event and request cheat sheet for contributors

This appendix is intentionally operational. It states what each event means in the WM and which mistakes are common.

| Event/request | Meaning in go-go-wm | Correct response | Common mistake |
|---|---|---|---|
| `MapRequest` | A non-override top-level client wants to become visible. | Classify, manage or float, reparent, map, update state. | Mapping immediately before reading transient/type/class policy. |
| `ConfigureRequest` | A client requests geometry or stacking. | Delegate by geometry owner; honor floats, deny/report tiles, ignore fullscreen conflicts. | Calling full relayout for a denied tiled request. |
| `DestroyNotify` | The X resource is gone. | Idempotent teardown, remove maps, clear focus/fullscreen/scope references. | Assuming Unmap always arrives first. |
| `UnmapNotify` | A window became unmapped; may be client withdrawal or WM action. | Distinguish expected WM unmap from withdrawal; teardown if appropriate. | Destroying a frame on the WM's own workspace hide. |
| `ReparentNotify` | Window changed parent. | Usually diagnostic for managed clients; verify lifecycle assumptions. | Treating every reparent as hostile. |
| `PropertyNotify` | Title, hints, state, type, protocols, or class changed. | Read only the relevant property; update projection/state machine. | Refetching every property and repainting everything. |
| `ClientMessage` | EWMH/ICCCM/application protocol message. | Parse atom and route to explicit state transition. | Directly toggling unrelated raw fields. |
| `FocusIn/FocusOut` | Server focus changed. | Reconcile with focus state, account for detail/mode. | Treating every notification as user intent. |
| `EnterNotify` | Pointer entered a window. | Update docs/hover/focus according to policy. | Focusing on enter without considering grabs and popups. |
| `Expose` | Server needs drawable content repaired. | Reuse background pixmap/cached layer; rerender only if cache invalid. | Calling application render unconditionally. |
| `MotionNotify` | Pointer state sample. | Latest-wins coalescing in active gesture/hover class. | Preserving every sample or applying durable ops. |
| `ButtonPress` | Start activation, focus, menu, or gesture. | Hit-test; push input scope or invoke semantic action. | Starting drag before threshold or failing to restore focus. |
| `ButtonRelease` | Complete active gesture. | Drain latest coordinate, exact commit, pop scope. | Applying a stale last-admitted motion coordinate. |
| `KeyPress` | Ordered keyboard input. | Global binding first, then active scope/focus node. | Coalescing or dropping keys like pointer motion. |
| `MappingNotify` | Keyboard/modifier mapping changed. | Rebuild grabs/key translation. | Keeping stale keycodes. |
| RandR events | Monitor/output topology changed. | Recompute logical monitors, bars/struts, work areas, fullscreen, layout. | Handling each low-level event as a separate visible transition. |
| XSync alarm/counter | A synchronized client reached a requested redraw state. | Mark client ready and send newest pending live resize. | Blocking the event loop waiting for counter advancement. |

### A.1 Checked versus unchecked requests

Use checked requests when immediate error attribution matters at a lifecycle boundary, such as creating a critical window or attaching SHM. In a hot path, unchecked requests plus connection/error monitoring are usually preferable. Record any checked request used during drag in the performance review.

### A.2 Map/unmap bookkeeping

Maintain explicit desired and observed visibility. A workspace switch intentionally unmaps frames; the resulting events must not be mistaken for client withdrawal. Use generation or suppression tokens around WM-initiated unmaps if the X library/event details do not make the distinction sufficiently clear.

### A.3 Window IDs and logical identity

XIDs can be reused after resources are destroyed. Never make long-lived script identity equal to an XID alone. Pair logical identity with generation and owner.

## Appendix B. Recommended metrics and debug query

A development build can expose:

```json
{
  "q": "perf",
  "window": "5s",
  "data": {
    "resize": {
      "active": false,
      "input": 812,
      "updates": 96,
      "coalesced": 716,
      "commits": 3,
      "cancels": 0,
      "modeTransitions": {"liveToOutline": 2},
      "queueLagMs": {"p50": 1.2, "p95": 6.8, "p99": 15.4},
      "wmWorkMs": {"p50": 2.4, "p95": 5.1, "p99": 9.7},
      "releaseMs": {"p50": 8.2, "p95": 18.6}
    },
    "render": {
      "rgbaBytes": 201326592,
      "convertedPixels": 50331648,
      "damagePixels": 9437184,
      "fullSurfacePaints": 4,
      "titlePaints": 192,
      "shmCreate": 0,
      "shmDestroy": 0,
      "ximageCreate": 0,
      "ximageDestroy": 0
    },
    "x11": {
      "configure": 384,
      "map": 0,
      "unmap": 0,
      "clear": 192,
      "property": 3,
      "flush": 99,
      "replyWait": 0,
      "checkedWait": 0
    },
    "scripts": {
      "eventsDelivered": 44,
      "pointerReplaced": 182,
      "snapshotsProduced": 31,
      "snapshotsInstalled": 12,
      "snapshotsReplaced": 19,
      "handlerMsP95": 3.4
    }
  }
}
```

The debug API should return bounded aggregates, not an unbounded trace. Full traces can be written to a file or consumed by the trace app.

### B.1 Counter naming

Suggested internal names:

```text
wm_resize_input_total
wm_resize_preview_total
wm_resize_motion_replaced_total
wm_resize_commit_total
wm_resize_cancel_total
wm_resize_mode_transition_total{from,to,reason}
wm_resize_queue_lag_seconds
wm_resize_work_seconds{stage}
wm_reconcile_change_total{kind}
wm_x_request_total{opcode}
wm_x_wait_seconds{kind}
wm_render_pixels_total{kind}
wm_render_damage_pixels_total{surface}
wm_surface_resource_bytes{kind}
wm_surface_snapshot_total{result}
wm_script_event_total{queue,result}
```

Labels must remain bounded. Do not use window titles, script paths, node keys, or arbitrary ptypes as metric labels; place those in traces.

## Appendix C. Recommendation matrix

| Priority | Recommendation | Expected impact | Effort | Risk | Verification |
|---:|---|---:|---:|---:|---|
| 1 | Latest-wins motion mailbox | Very high perceived latency gain | Small | Low | Queue lag, release latency, coalesced count |
| 2 | One durable ratio op on release | High CPU/event reduction | Small-medium | Low | Operation count and replay |
| 3 | Outline/adaptive resize | Very high for slow clients | Medium | Low-medium | Smooth outline, one final geometry batch |
| 4 | Synthetic ConfigureNotify for denied tile requests | High compatibility and storm prevention | Small | Low | Protocol test client |
| 5 | Thin title/border layers for client frames | Very high pixel/memory reduction | Large | Medium | Bytes, SHM lifecycle, goldens |
| 6 | Stop divider repaint on movement | Medium hot-path reduction | Small | Low | Paint counters |
| 7 | Diff map/unmap/geometry/stacking | Medium broad reduction | Medium | Medium | X request trace/idempotence |
| 8 | Capacity/pool content buffers | Medium-high for PBUI resize | Medium | Medium | Resource create count and memory budget |
| 9 | xapp ConfigureNotify coalescing | Very high for PBUI clients | Small | Low | Notify-to-redraw ratio |
| 10 | xapp cached upload resources | High allocation/upload reduction | Medium | Medium | Heap/X resource trace |
| 11 | `_NET_WM_SYNC_REQUEST` backpressure | High for supporting clients | Medium-large | Medium | Delayed test client and timeout |
| 12 | Subtree layout | Medium for large trees | Medium | Medium | Equivalence property test |
| 13 | Stable keyed `uispec` diff | Foundational | Medium | Medium | Reorder/invalidation tests |
| 14 | Semantic accept overlay | High for PBUI mode transitions | Medium | Low-medium | No base rerender/hash unchanged |
| 15 | Retained layers and damage | High for rich widgets | Large | Medium-high | Damage/paint metrics and goldens |
| 16 | Surface manager and input scopes | Foundational correctness | Large | Medium-high | State-machine/Xvfb tests |
| 17 | Capabilities and generation-stamped hot reload | High safety/reliability | Large | Medium | Failure/reload tests |
| 18 | Type lattice/views/translators | High PBUI expressiveness | Medium-large | Medium | Broker compatibility tests |
| 19 | GPU/compositor backend | Unknown until measured | Very large | High | Only after retained CPU path profile |

The ordering is deliberate. A compositor or GPU backend does not remove stale-event processing, durable preview operations, ConfigureRequest storms, or duplicated client redraws.

## Appendix D. Performance and correctness review checklist

### D.1 Before editing an X event handler

- Which state machine owns the event?
- Is the event ordered, durable, replaceable, or sampleable?
- Can the handler perform a reply wait or checked request?
- Can it call JavaScript, disk, process, network, or broker synchronously?
- Which maps/fields may change, and does one method own them?
- What happens if the target window disappears during the operation?
- What events will the WM's own X requests generate in response?

### D.2 Before adding a render call

- Did geometry, content, style, semantic state, or only position change?
- Can the change be expressed as damage on an existing layer?
- Is the buffer sized to visible ownership or to an unnecessarily large parent?
- Does this call allocate Go memory, SHM, pixmaps, GCs, fonts, or X images?
- Will Expose repair from a cache afterward?
- Can multiple invalidations before the next turn be coalesced?
- Is the final exact render guaranteed after lossy previews?

### D.3 Before adding a JS callback

- Which owner loop runs it?
- Is the event serialized and generation-stamped?
- What queue/backpressure policy applies?
- What happens if the callback throws, hangs, or returns malformed data?
- Does the previous scene remain visible?
- Which capability authorizes the effect?
- Can the result be superseded by a newer snapshot?

### D.4 Before adding a PBUI type or verb

- Is the value JSON and bounded?
- Is the type stable and namespaced appropriately?
- What is its parent type, if any?
- Which default and named views exist?
- What documentation appears on hover?
- Which accepts should match it?
- Are translators explicit and predictable?
- Which process owns verbs and how are they withdrawn on disconnect?

### D.5 Before adding a surface kind

- Which X window type and override-redirect policy apply?
- Which stacking band owns it?
- Can it take focus?
- Which input scope is pushed?
- How is outside-click/Escape handled?
- What focus is restored on close?
- Does it reserve work area?
- What happens on monitor change, workspace switch, owner death, and reload?

## Appendix E. Common wrong turns

### E.1 “Use more goroutines in the WM loop”

Parallel pixel conversion can help stable independent buffers, but parallelizing mutable X/WM state creates ordering problems. First reduce work and keep X commits centralized. Pure scene compilation or image decoding can use workers with generation cancellation.

### E.2 “Increase the drag throttle”

A lower update rate can mask overload but worsens responsiveness and does not fix stale queues. Implement latest-wins semantics and adaptive modes. Then choose cadence from measured budget.

### E.3 “Use MIT-SHM everywhere”

SHM improves transport; exact-size shared resource creation during resize can be worse than a smaller ordinary path. Minimize damage and stabilize lifetime first.

### E.4 “Implement dirty rectangles before changing frame buffers”

Damage tracking on a full-size client decoration buffer adds complexity while preserving the wrong representation. Split chrome from client content first; then damage matters for PBUI surfaces.

### E.5 “Let JavaScript draw directly”

Direct drawing callbacks couple VM latency to Expose/resize and make caching, validation, resource budgets, and future backends difficult. JavaScript should emit data-only scene snapshots or data primitives.

### E.6 “Copy React's component model exactly”

React solves browser DOM reconciliation. go-go-wm needs X surfaces, focus/grabs, desktop accepts, type-directed verbs, work-area struts, process ownership, and strict VM isolation. Stable keys and declarative trees are useful; browser assumptions are not the architecture.

### E.7 “Treat every object as a button”

PBUI presentations include passive output that becomes active only in context, plot marks, text spans, table cells, and groups. Controls and presentations overlap but are not identical.

### E.8 “Make one global event queue”

Pointer motion, keys, operation logs, snapshots, and telemetry require different backpressure semantics. A universal queue silently chooses the wrong correctness model for some class.

### E.9 “Store floats in the tiling tree”

The current design correctly keeps floats as shell state. Their geometry, stacking, and transient lifecycle are not replayable tiling structure. Keep tree purity.

### E.10 “Expose all authority to the rc script because it is local”

The trusted rc runtime can receive broad capabilities, but the API should still be narrow. This prevents accidental invariant violations and permits the same module vocabulary to run under reduced authority elsewhere.

## Appendix F. Glossary

**Accept.** A typed input rendezvous. A requester specifies presentation types; compatible visible presentations can answer with a typed object.

**Adaptive resize.** A policy that performs live resize while budget/client readiness permit and falls back to outline or lower cadence under load.

**Compiled scene.** The host-owned, validated, laid-out, hit-testable, retained representation of a JavaScript SceneSpec.

**Damage.** The region of a surface whose pixels need repaint or recomposition.

**Durable operation.** A serializable model mutation whose replay reproduces committed desktop state.

**Frame.** A WM-created parent around a client or internal content host, responsible for placement and chrome.

**Generation.** A monotonically changing identity component that distinguishes a current runtime/surface/resource from a destroyed predecessor with the same logical ID.

**Input scope.** An explicit routing context that temporarily owns keys/pointer interpretation and focus restoration, such as a menu, modal, drag, or launcher.

**Latest-wins queue.** A one-slot pending state where a new sample replaces an older unprocessed sample.

**Presentation.** An association among a typed object, a visual face, and interactive semantics such as acceptance, menus, activation, and documentation.

**Presentation type.** A semantic type used for display/input matching, view selection, verbs, subtyping, and optional translation.

**Preview state.** Lossy transient interaction state used to display an in-progress gesture; it is not part of the durable operation log.

**Reconciliation.** Computing and applying the minimal X/render changes required to make observed desktop state match desired state.

**Retained layer.** Cached draw output or commands that remain valid across scene updates until invalidated.

**SceneSpec.** An immutable, keyed, data-only UI tree produced by a script.

**Surface.** A hosted UI instance with content plus placement, X representation, stacking, input, lifetime, and capability policy.

**Translator.** A registered conversion from one presentation type to another in an input or command context.

**Verb.** A type-directed action registered by an owner process and offered for compatible presentations.

**View.** A named visual representation of a typed object in a specific context.

## Appendix G. Source notes and bibliography

### Project and supplied artifacts

**[P1]** go-go-golems, *go-go-wm*, current repository reviewed at merge commit `5b73c9f37c97538f6767ecdc3ece4fb599932377`. https://github.com/go-go-golems/go-go-wm

**[P2]** `ttmp/2026/07/18/GGWM-002-GOJA-DSL.../design-doc/01-wm-scripting-dsl-design-modules-primitives-script-kinds.md`.

**[P3]** `ttmp/2026/07/18/GGWM-003-UI-MODULE.../design-doc/01-ui-module-design-spec-ir-app-adapters-script-tiles-xgoja-provider.md`.

**[P4]** `ttmp/2026/07/19/GGWM-004-THEMES-I3.../design-doc/02-go-go-wm-from-scratch-an-intern-s-guide-to-the-system.md`.

**[P5]** `ttmp/2026/07/19/GGWM-005-PERF.../design-doc/01-paint-path-performance-analysis-and-fixes.md` and `02-the-rendering-pipeline-under-the-microscope...md`.

**[P6]** `ttmp/2026/07/19/GGWM-006-XSHM.../design-doc/01-from-putimage-to-shared-pixmaps...md`.

**[P7]** `ttmp/2026/07/19/GGWM-007-TRANSIENTS.../design-doc/01-floating-transients...md`.

**[P8]** `ttmp/2026/07/19/GGWM-008-LAUNCHER.../design-doc/01-the-launcher...md`.

**[P9]** `ttmp/2026/07/19/GGWM-009-RICH-REPL.../design-doc/01-the-rich-repl...md`.

**[P10]** `ttmp/2026/07/19/GGWM-010-PR1-REVIEW.../design-doc/01-pr-1-review-analysis-and-intern-implementation-guide.md`.

**[P11]** `ttmp/2026/07/20/GGWM-011-FOCUS-FS.../design-doc/01-fullscreen-focus-state-encapsulation-analysis-and-intern-implementation-guide.md`.

**[P12]** Supplied `pbui-shell(3).jsx`, the original self-contained presentation-based desktop prototype.

**[P13]** Supplied `pbui-basketball.jsx`, a domain-rich prototype with typed tables and plot marks.

**[P14]** Supplied `SKILL(6).md`, textbook-authoring guidance used for the explanatory structure of this document.

### X11 specifications and protocol guidance

**[X1]** X Consortium, *Inter-Client Communication Conventions Manual (ICCCM)*. https://www.x.org/releases/current/doc/xorg-docs/icccm/icccm.html

**[X2]** freedesktop.org, *Extended Window Manager Hints (EWMH), latest specification*. https://specifications.freedesktop.org/wm-spec/latest/

**[X3]** X.Org, *MIT-SHM Extension Protocol*. https://www.x.org/releases/current/doc/xextproto/shm.html

**[X4]** X.Org, *X Synchronization Extension Protocol*. https://www.x.org/releases/current/doc/xextproto/sync.html

**[X5]** XCB project, *Basic Graphics Programming With The XCB Library*. https://xcb.freedesktop.org/tutorial/

**[X6]** X.Org, *X Resize, Rotate and Reflect Extension (RandR) protocol and library documentation*. https://www.x.org/releases/current/doc/randrproto/randrproto.txt

### Window-manager implementation references

**[W1]** i3, `src/drag.c`, event draining and latest MotionNotify handling. https://github.com/i3/i3/blob/next/src/drag.c

**[W2]** i3, `src/resize.c`, graphical tiled resize using a helper resize bar and committing percentages after drag. https://github.com/i3/i3/blob/next/src/resize.c

**[W3]** AwesomeWM, `wibox.widget.base`, widget fit/layout and redraw/layout invalidation signals. https://awesomewm.org/doc/api/classes/wibox.widget.base.html

**[W4]** AwesomeWM, `awful.popup`, declarative popup placement and widget hosting. https://awesomewm.org/doc/api/classes/awful.popup.html

**[W5]** suckless, dwm source and resize throttling history. https://git.suckless.org/dwm/file/dwm.c.html

### Presentation-based and scriptable UI references

**[H1]** LispWorks, *Common Lisp Interface Manager 2.0 User Guide*, especially Chapters 6-8 on presentation types and translators and Chapter 14 on output recording and redisplay. https://www.lispworks.com/documentation/lww42/CLIM-W/html/climguide.htm

**[H2]** LispWorks, *Conceptual Overview of Defining a New Presentation Type*. https://www.lispworks.com/documentation/lw60/CLIM/html/climuser-118.htm

**[H3]** McCLIM project, implementation and specification resources. https://mcclim.common-lisp.dev/

**[H4]** Apple HyperTalk documentation preserved by the HyperCard Center, including event handlers and object scripts. https://hypercard.center/HyperTalkReference

---

# Closing assessment

The project is not slow because its idea is too ambitious or because a presentation-based desktop requires a heavyweight runtime. The present lag is explained by specific, removable work: stale motion processing, durable preview mutations, broad reconciliation, full-frame decoration buffers, resize-time shared-resource creation, and a second full redraw loop in PBUI clients.

The existing architecture already contains the principles needed to fix it: pure models, operations as data, owner loops, VM-free snapshots, open typed verbs, and measured profiling. Apply those principles more strictly to interactive state and rendering. Preview state should be lossy and replaceable. Committed state should be exact and replayable. Rendering should retain identity and clean layers. X resources should have stable lifetimes. JavaScript should describe interfaces and handle semantic events without entering the X hot path.

The resulting system would be unusual in a useful way. It would combine a small tiling WM, CLIM-like typed presentations, HyperCard-like scriptable visible objects, Smalltalk-like inspection, and process-safe modern scripting. That novelty does not require abandoning established X11 practice. It requires using those practices—event coalescing, protocol compliance, explicit state machines, narrow damage, cached resources, and backpressure—as the foundation on which the novel semantics can remain responsive.
