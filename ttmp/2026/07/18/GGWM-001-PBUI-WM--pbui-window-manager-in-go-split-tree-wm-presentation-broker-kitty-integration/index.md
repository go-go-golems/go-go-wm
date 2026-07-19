---
Title: 'PBUI window manager in Go: split-tree WM + presentation broker + kitty integration'
Ticket: GGWM-001-PBUI-WM
Status: active
Topics:
    - wm
    - pbui
    - x11
    - broker
    - kitty
DocType: index
Intent: long-term
Owners: []
RelatedFiles: []
ExternalSources: []
Summary: ""
LastUpdated: 2026-07-18T20:05:22.897634863-04:00
WhatFor: ""
WhenToUse: ""
---

# PBUI window manager in Go: split-tree WM + presentation broker + kitty integration

## Overview

Design and implement **go-go-wm**: an X11 tiling window manager in Go that
ports the PBUI shell prototype (`sources/pbui-shell.jsx` — a CLIM/Genera
"Dynamic Windows" style shell where every object is a typed presentation with
an accept protocol and type-directed menus) into a real system: a reparenting
split-tree WM, a display-agnostic presentation broker on a Unix socket, and
kitty terminal integration via OSC 8 links and a custom kitten. Single binary
on the glazed framework; designed for later go-go-goja JS scripting
(ops-as-data, event bus, verb registry).

Documents:

- `design-doc/01-pbui-wm-design-and-implementation-guide.md` — **primary**:
  intern-level architecture, wire protocol, glazed command surface, decision
  records, phased plan (Phases 0–8).
- `design-doc/02-pbui-application-layer-builtin-apps-demo-clients-presentation-surfaces.md`
  — the app layer: Region/click-contract framework, WM-embedded
  trace/listener/inspector, six demo clients, divider states.
- **Status 2026-07-18**: Phases 0–6 plus the app layer are implemented and
  verified live in Xvfb (see diary entry 2 and `various/build-screenshots/`).
- `reference/01-preliminary-research-x11-presentations-kitty-testing.md` —
  cleaned-up research and rationale.
- `reference/02-investigation-diary.md` — chronological session diary.
- `sources/` — the prototype JSX plus two look-reference screenshots.

## Key Links

- **Related Files**: See frontmatter RelatedFiles field
- **External Sources**: See frontmatter ExternalSources field

## Status

Current status: **active**

## Topics

- wm
- pbui
- x11
- broker
- kitty

## Tasks

See [tasks.md](./tasks.md) for the current task list.

## Changelog

See [changelog.md](./changelog.md) for recent changes and decisions.

## Structure

- design/ - Architecture and design documents
- reference/ - Prompt packs, API contracts, context summaries
- playbooks/ - Command sequences and test procedures
- scripts/ - Temporary code and tooling
- various/ - Working notes and research
- archive/ - Deprecated or reference-only artifacts
