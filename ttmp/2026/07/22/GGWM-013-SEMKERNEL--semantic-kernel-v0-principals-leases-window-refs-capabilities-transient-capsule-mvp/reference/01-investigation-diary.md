---
Title: Investigation diary
Ticket: GGWM-013-SEMKERNEL
Status: active
Topics:
    - wm
    - goja
DocType: reference
Intent: long-term
Owners: []
RelatedFiles: []
ExternalSources: []
Summary: ""
LastUpdated: 2026-07-22T22:48:11.628303622-04:00
WhatFor: ""
WhenToUse: ""
---

# Investigation diary

## Goal

<!-- What is the purpose of this reference document? -->

## Context

<!-- Provide background context needed to use this reference -->

## Quick Reference

<!-- Provide copy/paste-ready content, API contracts, or quick-look tables -->

## Usage Examples

<!-- Show how to use this reference in practice -->

## Related

<!-- Link to related documents or resources -->

# Diary

## Goal

Implementation journey of semantic kernel v0 (M1-M5): principals, leased resources, window refs, one capability, and the Explain-window capsule.

## Step 1: M1+M2 — principals and the leased resource registry

The broker now assigns every connection a `principal:conn/<n>` identity in the welcome frame; client names become display labels. A new registry tracks every leased side effect — verbs (implicit), subscriptions (implicit), and explicit registrations — and revokes everything a principal owns on disconnect or `lease.close`, emitting `lease.ended` facts.

### Prompt Context

**User prompt (verbatim):** "make a decent MVP out of this, than we can later extend." then (interrupt) "sorry i meant, reduce the list of tasks / features to a MVP. Don't start yet" then `/goal M1 M2 M3 M4 M5`.

**Assistant interpretation:** Implement the five-piece MVP scoped in design-doc/01: M1 principals, M2 leases, M3 window refs, M4 one capability, M5 the Explain-window capsule.

**Inferred user intent:** A small, extensible semantic-kernel foundation landed with tests, not a research prototype.

**Commit (code):** see below.

### What I did

- `pkg/pbui/wire.go`: `Principal` on welcome; `Resource` struct; `resource.register` / `resource.list` / `lease.close` / `resource.listing` message types.
- `pkg/pbui/object.go`: `Verb.OwnerPrincipal` — routing and cleanup key; `Owner` stays the label.
- `pkg/pbui/broker/broker.go`: per-conn principal; `resources` registry; verbs/subscriptions create registry entries; `revokeResource` (kind-specific cleanup + `lease.ended`); disconnect revokes by principal; verb.invoke routes by principal; ownership checks on re-register and lease.close.
- `pkg/pbui/client/client.go`: capture principal from welcome; `Principal()`, `RegisterResource`, `ListResources`, `CloseLease`.
- `pkg/pbui/broker/resources_test.go`: four tests — principal uniqueness, same-name verb cleanup (the M1 pin), resource lifecycle with foreign-owner rejection and idempotent close, disconnect-revokes-all with three `lease.ended` facts.

### Why

- Names were the cleanup key: `removeConn` swept verbs by `v.Owner != c.name`, so two clients sharing a name lost each other's verbs, and verb.invoke routed to whichever conn matched the name first.

### What worked

- All prior broker tests pass unchanged — the protocol additions are purely additive, and the client's default reply routing handles the new `resource.listing` frame without changes.

### What didn't work

- First test run failed to build: new test file used `package broker` while the existing helpers live in `package broker_test`. One-line fix.

### What I learned

- The registry could reuse the existing emit/event machinery wholesale; `lease.ended` is just a fact on the existing bus, which means the WM's existing event drainer can react to it with no new plumbing (relevant for M5 cleanup).

### What was tricky to build

- Verb identity: verbs are upserted by (principal, id) but the registry keys resources by a single string. `resourceIDForVerb` = `verb/<principal>/<verbID>` keeps the two views consistent; `revokeResource` filters `b.verbs` through the same function so they cannot drift.

### What warrants a second pair of eyes

- `revokeResource` iterates `b.resources` while `removeConn` deletes from it via the same function — Go map iteration with deletion of the *current* key is safe, but a reviewer should confirm no future revoke path deletes a *different* key mid-iteration.

### What should be done in the future

- Peer-cred authentication behind the principal; launcher remote commands into the registry (documented trim).

### Code review instructions

- Start: `pkg/pbui/broker/broker.go` (`revokeResource`, `putResource`, the `TRegister`/`TResourceRegister`/`TLeaseClose` cases). Validate: `go test ./pkg/pbui/... -count=1`.

## Step 2: M3+M4+M5 — refs, capabilities, and the Explain-window capsule, end to end

Window refs with tombstones (M3), the capability store (M4), and the transient capsule (M5) landed in three commits, and the whole loop was validated end-to-end in Xephyr on the first full harness run: verb over the broker → capability minted → constrained runtime spawned → broker lease visible in resource.list → tile placed and rendering → tile closed → zero residue.

### Prompt Context

**User prompt (verbatim):** (see Step 1 — `/goal M1 M2 M3 M4 M5`)

**Commits (code):** M3 `297c86a`, M4 `f0c50a8`, M5 this commit.

### What I did

- M3 `pkg/wmx11/refs.go`: `wm.window/0x<xid>` refs, `describeWindow`, bounded tombstones (64) recorded at the top of `unmanage` (single entry point for tiled and floating teardown); IPC `{"q":"describe"}`; `ScriptBackend.Describe`.
- M4 `pkg/wmx11/caps.go`: capability store (128-bit random IDs), `mintCapability`/`checkCapability`/`revokeCapabilitiesFor`, and `ScriptBackend.DescribeWith` — the gated read the capsule uses.
- M5:
  - `pkg/jsmod/semmod`: the capsule's ONLY module besides ui — `describe()` bound to closures the host built, so missing authority is unrepresentable in JS (research axiom 5).
  - `pkg/wmx11/capsule.go`: `explainWindow` verb handler (mint → spawn → place), `reapCapsules` hooked into `syncBuiltins` (tile-closed detected on the op that closed it), idempotent `teardownCapsule`, `{"q":"sem"}` dump (capability IDs deliberately excluded — they are bearer tokens).
  - `pkg/cmds/capsule.go`: the injected spawner — wmx11 stays goja-free (U-D3); the capsule runtime gets exactly `ui` + `sem`, no wm module, no exec; its broker connection registers a `wm.capsule` resource so the lease is observable; embedded JS renders the inspector tile.
  - Verb `window.explain` on tile presentations (`pbui.go`).
- Tests: refs round-trip/tombstone bound, capability mint/check/revoke, capsule reap/teardown/unplaced-guard — all display-free. E2E: `scripts/ggwm-capsule-e2e.sh` (7 stages, all assertions held; screenshots `images/capsule-e2e-*.png`).

### Why

- The MVP's proof obligation was composition: every primitive (principal, lease, ref, capability) is exercised by one user-visible feature, not four disconnected APIs.

### What worked

- The rc.go runtime-construction pattern transplanted cleanly: the capsule spawner is rc.js minus wm/pbui/exec plus sem.
- `syncBuiltins` as the reap hook means capsule teardown needs no new lifecycle plumbing — it rides the existing after-op reconciliation.

### What didn't work

- N/A — the E2E harness passed on its first complete run; unit-test build failures along the way were package-name and helper-collision fixes recorded in Step 1's pattern.

### What I learned

- Late-binding the WM pointer into `SpawnCapsule` via OnReady is the clean way to inject a runtime builder into a Config that is consumed before the WM exists.

### What was tricky to build

- Ordering in `explainWindow`: mint on the WM loop, spawn off-loop, then *post back* for placement — placement must happen after `app.tile()` registered the renderer, and `cs.placed` must only be set after the split op succeeds, or `reapCapsules` would tear down an in-flight capsule (pinned by TestUnplacedCapsuleNotReaped).

### What warrants a second pair of eyes

- `teardownCapsule` runs `cs.close()` on a goroutine; if the WM shuts down at that instant, runtime close and WM ctx cancellation race — benign today (both paths are idempotent closes) but worth a look.
- The capsule's `render()` posts to the WM loop (DescribeWith) from the JS loop; deadlock-free because nothing on the WM loop ever waits on the JS loop, but that invariant is implicit.

### What should be done in the future

- Tier 2: out-of-process spawner behind the same CapsuleSpec. Powerbox prompt before minting. Capsule refresh-on-event (subscribe to window facts) instead of a manual button.

### Code review instructions

- Read in order: `pkg/wmx11/refs.go` → `caps.go` → `capsule.go` → `pkg/cmds/capsule.go` → `pkg/jsmod/semmod/module.go`.
- Validate: `go test ./pkg/... -count=1`; `GO_GO_WM=<bin> PARENT=:0 bash scripts/ggwm-capsule-e2e.sh` → "ALL PASS".
