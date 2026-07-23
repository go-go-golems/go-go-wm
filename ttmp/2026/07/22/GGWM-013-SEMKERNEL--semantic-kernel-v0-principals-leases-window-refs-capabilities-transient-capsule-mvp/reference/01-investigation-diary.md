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
