---
Title: 'Semantic Kernel v0 MVP: scope and implementation'
Ticket: GGWM-013-SEMKERNEL
Status: active
Topics:
    - wm
    - goja
DocType: design-doc
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://pkg/cmds/capsule.go
      Note: M5 injected runtime spawner and capsule JS
    - Path: repo://pkg/pbui/broker/broker.go
      Note: M1+M2 principals and resource registry
    - Path: repo://pkg/wmx11/caps.go
      Note: M4 capability store
    - Path: repo://pkg/wmx11/capsule.go
      Note: M5 capsule lifecycle
    - Path: repo://pkg/wmx11/refs.go
      Note: M3 window refs and tombstones
ExternalSources: []
Summary: 'MVP slice of the programmable-semantic-desktop research: broker-assigned principals, a leased resource registry, live window refs with tombstones, one minted-and-checked capability, and a transient ''Explain window'' capsule that composes all four. Everything else from the research docs is explicitly deferred.'
LastUpdated: 2026-07-22T22:48:11-04:00
WhatFor: Define exactly what semantic-kernel v0 ships and how each piece maps onto existing code.
WhenToUse: Before extending the kernel (capability policy, sandboxes, scene v2) or reviewing the M1-M5 commits.
---


# Semantic Kernel v0 MVP: scope and implementation

The research corpus (GGWM-012 `sources/local/go-go-wm-programmable-{semantic-desktop,presentation-environment}.md`, ~15.5k lines) converges on one thesis: make the operating-system concepts already implicit in go-go-wm — typed objects, verbs, accept, script tiles — explicit as identities, capabilities, leases, and schemas. This ticket ships the smallest coherent slice of that: five pieces, each landable with tests, each the seed of the corresponding full concept.

One sentence: **make every script-created thing owned and revocable, make windows referenceable, gate one read path behind a capability, and prove it all with a single transient app** — on the machinery that already exists (uispec tiles, the Goja engine builder from `pkg/cmds/rc.go`, the 468-line broker), with no new protocol family, no sandbox, no scene rewrite.

## The five pieces

### M1 — Principals (minimal)

The broker assigns every connection `principal:conn/<n>`; the client-supplied `name` becomes a display label. Verbs and resources key on principal, not name. This kills the name-collision class: today two clients can claim the same name, cleanup drops the wrong verbs, and `roles:["wm"]` is self-asserted (`pkg/pbui/broker/broker.go:214-235, 239-245`). Deferred: peer-cred authentication, service tokens, role removal.

### M2 — Leased resource registry

One broker-side registry: `{id, kind, owner principal, label, descriptor}`. Kinds covered natively in v0: **verbs** (created implicitly by `register`), **subscriptions** (by `subscribe`), and **generic explicit resources** (`resource.register`, used by the capsule). Disconnect or explicit `lease.close` revokes everything owned, idempotently, emitting `lease.ended` per resource. `resource.list` returns the registry. This generalizes the cleanup the launcher already does for remote commands by name.

**Scope trim, documented:** launcher remote commands stay on their existing name-keyed `client.disconnected` path (`pkg/wmx11/launcher.go:417-447`, `builtin.go:363`). Routing them through the registry requires attributing a WM-IPC registration to a broker principal — an identity-mapping question that belongs to the capability-policy phase, not v0.

### M3 — Live window refs

`wm.window/<xid>` resolvable to a typed snapshot (title, class, geometry, workspace, focused) via a `describe` IPC query and `ScriptBackend.Describe`. Destroyed windows leave a tombstone (last snapshot + destroyed-at) rather than disappearing silently. Deferred: revisions, watch streams, non-window object kinds, broker-routed describe.

### M4 — One capability, one check

`{id, holder, action:"wm.window.read", ref}` records owned by the WM loop, minted when the user invokes the Explain verb, required by the capsule's `sem.describe`, revoked when the capsule's lease ends. The IPC `describe` query stays capability-free — it is the trusted debug surface. What v0 proves is the mint → check → revoke loop existing at all; policy, attenuation, powerbox prompts, and rate limits are the extension.

### M5 — The proving capsule: "Explain this window"

A verb on tile presentations. Invoking it mints the capability (M4), spawns a **separate in-process Goja runtime** (Tier 1 — a concurrency and API boundary, explicitly not a security boundary) with only two modules: `ui` (existing, tile host) and a new `sem` module exposing `describe()` bound to the minted capability. The capsule renders an inspector tile through the existing uispec path. Closing the tile ends the lease: runtime disposed, capability revoked, broker resources released. A `{"q":"sem"}` IPC query dumps capsules + capabilities (+ broker resources via `resource.list`) so the whole lifecycle is observable.

## Deferred (the extension map)

Envelope v2 and schema registry · out-of-process sandboxes (the capsule spawn path is where Tier 2 plugs in) · `ui.scene.v2` · ptype/translator registry · accept v2 · REPL cell records, plans, undo · receipts/idempotency · device mesh · self-hosting shell surfaces. Each deferred item extends an M-piece rather than replacing it.

## Invariants preserved

- No JavaScript in the WM paint path or X callbacks; capsule surfaces render from uispec snapshots.
- Single-owner loops: broker state on the broker loop, WM state on the WM loop, each VM on its own owner.
- Every capsule-created side effect carries an owner and an idempotent cleanup path (Axiom 8 of the research docs).

## Validation

- Broker unit tests: principal assignment, resource lifecycle, revoke-on-disconnect, lease.close idempotency, verb cleanup by principal not name.
- WM tests where display-free logic allows (tombstones, capability mint/check/revoke).
- Xephyr end-to-end: spawn capsule on an xterm tile, screenshot, close tile, assert `{"q":"sem"}` shows zero residue.
