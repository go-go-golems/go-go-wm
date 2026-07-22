---
Title: Investigation diary
Ticket: GGWM-012-GUIDES
Status: active
Topics:
    - wm
    - performance
DocType: reference
Intent: long-term
Owners: []
RelatedFiles: []
ExternalSources: []
Summary: Chronological record of the guide analysis and codebase investigation.
LastUpdated: 2026-07-21T23:06:19.310827482-04:00
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

Capture the chronological investigation behind GGWM-012-GUIDES: importing three externally-authored go-go-wm engineering guides, reading them with a performance lens, verifying their claims against the live codebase, and synthesizing an intern-facing performance design and implementation guide.

## Step 1: Ticket creation, guide import, and workspace orientation

The first request was purely logistical: stand up a docmgr ticket in the `go-go-wm` repository, pull the three long-form guides that had been left in `/tmp/` into the ticket's `sources/` tree so they become durable, indexed workspace artifacts rather than scratch files, and confirm that the i3 configuration port actually lives in this workspace. Getting the sources under docmgr control matters because everything downstream — the analysis, the diary, the reMarkable bundle — should cite files that live inside the ticket, not paths in `/tmp/` that will vanish on the next reboot.

Orientation also mattered: this repository already carries eleven tickets (GGWM-001 through GGWM-011), so the new ticket had to follow the established `GGWM-NNN-SLUG` identifier convention and reuse existing vocabulary slugs rather than inventing new ones.

### Prompt Context

**User prompt (verbatim):** "Create a new docmgr ticket in go-go-wm and import the go-go-wm guides from /tmp/ into the sources directory of the ticket, and make sure you can find i3 in the workspace."

**Assistant interpretation:** Create a docmgr ticket workspace in the `go-go-wm` repo, use `docmgr import file` to copy the `/tmp/go-go-wm*` guide documents into the ticket's `sources/` directory, and then locate the i3-related code in the workspace to confirm it is present.

**Inferred user intent:** Establish a durable, indexed home for the three engineering guides so that subsequent analysis work has a citable workspace, and confirm the i3 config port is available as reference material for that analysis.

### What I did

- Inspected the repository docs root: `.ttmp.yaml` declares `root: ttmp` with `vocabulary: ttmp/vocabulary.yaml`.
- Listed the existing eleven tickets with `docmgr list tickets` to learn the `GGWM-NNN-SLUG` naming convention and read `ttmp/vocabulary.yaml` to pick valid topic slugs.
- Created the ticket:
  ```bash
  docmgr ticket create --ticket GGWM-012-GUIDES \
    --title "Import go-go-wm engineering guides and handbook" \
    --topics wm,goja
  # created GGWM-012-GUIDES at ttmp/2026/07/21/GGWM-012-GUIDES--import-go-go-wm-engineering-guides-and-handbook (9 dirs, 4 files)
  ```
- Imported all three guides:
  ```bash
  for f in /tmp/go-go-wm-engineering-guide.md \
           /tmp/go-go-wm-handbook.md \
           /tmp/go-go-wm_engineering_handbook.md; do
    docmgr import file --ticket GGWM-012-GUIDES --file "$f"
  done
  ```
- Searched the workspace for i3.

### Why

`docmgr import file` does more than copy: it writes provenance into `.meta/sources.yaml` and appends the source to the ticket `index.md` frontmatter as `ExternalSources: local:<name>`. A plain `cp` would have produced the same bytes with none of the indexing, and `docmgr doctor` would later have had no record of where the material came from.

### What worked

- All three guides landed in `sources/local/` and were registered in both `.meta/sources.yaml` and `index.md`:
  - `go-go-wm-engineering-guide.md` (151 KB)
  - `go-go-wm-handbook.md` (154 KB, ~3220 lines)
  - `go-go-wm_engineering_handbook.md` (132 KB, ~2880 lines)
- i3 is present in the workspace. The primary artifact is `examples/scripts/i3.js` (187 lines) — a line-by-line port of the user's `~/.config/i3/config` to the go-go-wm JavaScript API, delivered under GGWM-004. Its header documents the mapping table (`set $mod Mod4` → `MOD` constant, `bindsym $mod+Return exec kitty` → `wm.bind` + `wm.exec`, and so on) and warns that it must be run with `--no-default-binds` because the script owns the keyboard i3-style; double-grabbed combos would fire twice.
- Supporting i3 material: `pkg/jsmod/wmmod/i3_test.go` (tests for the port), i3-influenced logic in `pkg/jsmod/wmmod/module.go` and `pkg/jsmod/wmmod/rules.go`, `pkg/wmcore/neighbor.go` (directional focus, the i3 `focus <dir>` semantics), several `pkg/wmx11/*.go` files, and doc topics under `pkg/doc/topics/`.

### What didn't work

- `docmgr list tickets` output and the directory glob interacted badly with the shell in one call: `ls -d ttmp/2026/07/21/GGWM-012-GUIDES--*` returned `No such file or directory (os error 2)` even though the directory existed and an earlier `cd` into the same glob had succeeded. Working around it by using the fully spelled-out directory name (and absolute paths thereafter) was faster than diagnosing the quoting behavior.

### What I learned

- The repository's ticket convention embeds a short semantic suffix in the identifier itself (`GGWM-011-FOCUS-FS`, `GGWM-010-PR1-REVIEW`), not just in the slug. `GGWM-012-GUIDES` follows it.
- `ttmp/vocabulary.yaml` already carries `wm`, `x11`, `performance`, `goja`, `pbui`, `concurrency`, and others, so no `docmgr vocab add` was required for this work.

### What was tricky to build

Nothing structural — this step was mechanical. The only sharp edge was that `docmgr import file` is a per-file command with no multi-file form, so importing three guides required a shell loop; each invocation rewrites `.meta/sources.yaml` and `index.md`, so running them sequentially (rather than in parallel) avoids clobbering the metadata file.

### What warrants a second pair of eyes

- Whether all three guides should be retained. `go-go-wm-handbook.md` and `go-go-wm_engineering_handbook.md` overlap heavily in scope (both cover X11 foundations, resize performance, and a scriptable PBUI runtime); a future reader may want the ticket to declare which is canonical.

### What should be done in the future

- Consider a `docmgr doc relate` pass tying `examples/scripts/i3.js` to the ticket if i3 compatibility becomes a workstream of its own.

### Code review instructions

- Start at `ttmp/2026/07/21/GGWM-012-GUIDES--import-go-go-wm-engineering-guides-and-handbook/index.md` and confirm the three `ExternalSources` entries.
- Validate with:
  ```bash
  docmgr doctor --ticket GGWM-012-GUIDES --stale-after 30
  ls ttmp/2026/07/21/GGWM-012-GUIDES--*/sources/local/
  ```

### Technical details

Import destinations (all under the ticket root):

```
sources/local/go-go-wm-engineering-guide.md
sources/local/go-go-wm-handbook.md
sources/local/go-go-wm_engineering_handbook.md
.meta/sources.yaml          # provenance records
index.md                    # ExternalSources: local:<name>
```
