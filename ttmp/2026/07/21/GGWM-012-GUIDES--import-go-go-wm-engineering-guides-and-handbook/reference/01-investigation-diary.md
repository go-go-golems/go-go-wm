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

## Step 2: Parallel evidence gathering — three guides plus a live codebase map

The second request changed the shape of the work from logistics to analysis: read the three imported guides with a performance lens and produce an intern-facing design and implementation guide. The raw material is large — roughly 9,100 lines and 437 KB across the three documents — and it is *secondary* evidence: it describes the codebase rather than being the codebase. Treating it as ground truth without checking would be a mistake, because the guides were written against a specific commit and recommend work that may already have landed.

I therefore split the work four ways and ran the readers concurrently: one extraction agent per guide, and a fourth agent whose only job is to map the *live* code with `file.go:LINE` anchors so that every guide claim can be checked against what the repository actually does today. The fourth agent was briefed explicitly to say where the code already does the right thing, so the final deliverable does not recommend work that is already finished.

### Prompt Context

**User prompt (verbatim):** "Ok, analyze the engineering guides with an eye for performance improvements, and Create  a detailed analysis / design / implementation guide that is for a new intern, explaining all the parts of the system needed to understand what it is, with prose paragraphs and bullet point sand pseudocode and diagrams and api references and file references. It should be very clear and technical. Store in the ticket and the nupload to remarkable."

**Assistant interpretation:** Read the three guides focusing on performance, cross-check against the real codebase, then write a long-form, technically precise onboarding document — prose plus bullets plus pseudocode plus diagrams plus API and file references — into the ticket, and publish it to reMarkable.

**Inferred user intent:** Turn three overlapping external review documents into one authoritative, actionable, self-contained onboarding artifact that a new engineer can read on a reMarkable and then start implementing performance work from.

**Follow-up user prompt (verbatim):** "keep a diary as yu work, and should the agents, and commit at appropriate intervals. Make sure to load the right skills for diary format and itnern research documents and for committing"

**Assistant interpretation:** Maintain the investigation diary continuously rather than writing it at the end; have the subagents report their own methodology logs; commit the ticket at natural checkpoints; and load the `diary`, `full-blown-tech-research-design`, and `git-commit-instructions` skills so the formats are correct.

**Inferred user intent:** Ensure the work is auditable and resumable — that the reasoning trail, not just the conclusion, survives in the repository.

### What I did

- Loaded the governing skills before writing anything: `ticket-research-docmgr-remarkable` (pipeline), `diary` (this format), `full-blown-tech-research-design` (intern-guide output standard), `remarkable-upload` (delivery), `git-commit-instructions` (commit hygiene).
- Seeded a ten-item task list with `docmgr task add` and checked off the two Step 1 items.
- Extracted the heading structure of all three guides first, so the four agent briefs could name specific sections rather than asking for a vague summary:
  ```bash
  for f in *.md; do grep -n "^#\{1,2\} " "$f" | head -80; done
  ```
- Measured the codebase to size the fourth brief:
  ```bash
  for d in $(find pkg cmd -maxdepth 2 -type d); do n=$(cat $d/*.go 2>/dev/null | wc -l); [ "$n" -gt 0 ] && echo "$n $d"; done | sort -rn
  ```
  Result: ~19k LOC, dominated by `pkg/wmx11` (5326), `pkg/cmds` (2251), `pkg/jsmod/wmmod` (2097), `pkg/wmcore` (1541), `pkg/draw` (1175).
- Launched four background agents in a single message so they ran concurrently: guide-A extraction (`go-go-wm-handbook.md`), guide-B extraction (`go-go-wm_engineering_handbook.md`), guide-C extraction (`go-go-wm-engineering-guide.md`), and a codebase hot-path map.
- Sent each agent a follow-up asking it to append an `## Investigation log` section recording commands used, dead ends, unsupported claims, and what warrants a second pair of eyes — so their methodology feeds this diary rather than being lost.

### Why

Four sequential reads of 437 KB plus a codebase survey would have been slow and would have consumed context that is better spent on synthesis. The documents are independent of each other, so there is no ordering dependency; the codebase map is independent of all three. That is the exact shape where fan-out pays for its overhead.

Briefing by section number mattered more than briefing by topic. A brief that says "extract the performance recommendations" produces a summary; a brief that says "reproduce §14 latest-wins resize scheduler, §15 preview ratios vs committed operations, §17 thin decoration layers, verbatim where pseudocode is given" produces reusable material.

### What worked

- Two extractions returned with the level of fidelity requested — budgets, tables, ASCII diagrams, Go struct sketches, and phase acceptance criteria preserved rather than paraphrased.
- The two guides converge on the same root cause, which is strong evidence rather than a coincidence of authorship: **work amplification during divider drag**. One admitted pointer sample currently triggers a durable tree mutation, a full workspace layout, broad reconciliation, full-frame decoration rasterization, exact-size RGBA and MIT-SHM resource replacement, and an independent full client redraw.
- They also converge on the two cheapest high-value fixes: a **latest-wins one-slot pointer mailbox** (replacing the 16 ms time gate, which limits admission but does not drain stale queued events), and **preview-only resize** (transient ratio during drag, exactly one `OpSetRatio` on release).
- Concrete numbers survived extraction intact and will anchor the final document: a 1920x1080 RGBA frame is 2,073,600 pixels / ~7.9 MiB, so a 60 Hz full-frame decoration repaint moves ~475 MiB/s *per surface*; the 1920x22 title strip it actually needs is ~165 KiB, or ~9.7 MiB/s. Four large client frames hold ~31.6 MiB of RGBA versus ~660 KiB of title buffers.

### What didn't work

- `remarquee cloud account --non-interactive` does not honor `--non-interactive` for pairing: it looped printing `Enter one-time code (go to https://my.remarkable.com/device/browser/connect): ERROR: 2026/07/21 23:06:01 auth.go:76: Code has the wrong length, it should be 8` about twenty times before the command returned. There is no `~/.config/rmapi` token on this machine, so the device is genuinely unpaired.
- A pairing attempt piping a supplied one-time code (`printf '<code>\n' | remarquee cloud account --reauth`) exceeded the 120 s tool timeout and was moved to the background with an empty output file — inconclusive at the time of writing.
- One shell oddity recurred: `ls -d ttmp/2026/07/21/GGWM-012-GUIDES--*` failed with `No such file or directory (os error 2)` while `cd` into the same glob had succeeded moments earlier. Resolved by switching to absolute paths everywhere.

### What I learned

- The guides disagree on ADR count: one extraction reports the handbook's Appendix C as containing three ADRs, the other reports seven (C.1-C.7, adding retained keyed scenes, values-vs-live-handles, REPL as operational shell, and one portal manager). The seven-ADR reading came from the agent that read that appendix directly, so it is the one to trust.
- **A numbering collision exists.** `go-go-wm_engineering_handbook.md` §44 proposes a backlog in which **GGWM-012 is "Divider gesture preview and latest-motion scheduler"**, and continues through GGWM-029. This ticket already occupies `GGWM-012-GUIDES`. Anyone adopting that backlog verbatim will collide.
- The distinction the guides insist on and that the final document must teach clearly: **throttling limits admission; coalescing replaces stale state.** A 16 ms gate can report 60 updates/second while visibly trailing the pointer, because it never discards the events already queued in the X connection. i3 drains all pending events and invokes its drag callback once with only the newest sample.

### What was tricky to build

The hard part was brief design, not execution. An early draft of the agent briefs asked for "a summary of the performance sections"; that would have destroyed exactly the material worth keeping — the ms budgets, the byte counts, the `resizeController` struct, the R0-R7 and P0-P5 phase acceptance criteria. Rewriting each brief to enumerate the target sections by number and to demand verbatim preservation of numbers, tables, ASCII diagrams, and API signatures is what made the returned material directly usable as source for synthesis.

A second sharp edge: the guides are secondary sources written against merge commit `5b73c9f37c97538f6767ecdc3ece4fb599932377`, which is the current `main` merge in this repository's history. That is close enough that their claims are probably still true, but "probably" is not evidence, which is why the fourth agent exists and why the final document must mark each recommendation as already-done or outstanding.

### What warrants a second pair of eyes

- The GGWM-012 numbering collision above — someone should decide whether to renumber this ticket or the proposed backlog.
- Every claim inherited from the guides but not yet confirmed against `pkg/wmx11` at this commit. Until the codebase map returns, statements such as "`dividerMotion` applies `OpSetRatio` on every accepted sample" are *reported by the guides*, not *observed by me*.

### What should be done in the future

- Decide whether all three guides remain in `sources/`, or whether one is declared canonical (see Step 1).
- If the proposed GGWM-013..GGWM-029 backlog is adopted, file the tickets with `docmgr ticket create` rather than leaving them as prose in a source document.

### Code review instructions

- Nothing executable changed in this step. Review the agent briefs' fidelity by spot-checking a claim in the final design doc against the corresponding section of the guide in `sources/local/`.

### Technical details

Convergent root-cause chain reported by both guides for one admitted pointer sample during a divider drag:

```text
MotionNotify
  -> 16 ms time gate (admission only; queue not drained)
  -> full workspace Layout() to find the split rect
  -> OpSetRatio mutates the durable desktop tree
  -> relayoutResized(): full layout, all dividers synced, all frames considered
  -> changed frames: MoveResize + client ConfigureWindow
  -> paintFrame(): full-frame RGBA fill + colour conversion
  -> dimensions changed => exact-size cache miss
       => new image.RGBA + shmget/shmat/attach/pixmap-create/old-pixmap-free
  -> each client independently re-renders its entire interior
```

Stated latency budget to design against (60 Hz display interval = 16.67 ms, not all of it the WM's):

| Stage | Target during live resize |
|---|---|
| Event queue lag at scheduler entry | < 8 ms median, < 24 ms p99 |
| WM preview layout and diff | < 1 ms |
| X request construction and flush | < 1 ms |
| Decoration paint/upload | < 2 ms total |
| Total WM update | < 4-6 ms p95 |

## Step 3: Synthesis — and the finding all three guides missed

The four readers came back and the picture changed. The three guides converge on a diagnosis I could verify — divider resize does far too much work per pointer sample — but the codebase map contradicted them on several specifics, and in one case in a way that inverts the priority order. The guides all assert that the admitted motion path contains no synchronous X round trips. It contains four. That single correction is the most valuable thing this ticket produced, and it is the reason the deliverable is a new document rather than a summary of the existing three.

Writing then became an exercise in *subtraction* as much as synthesis. A large fraction of what the guides recommend has already landed under GGWM-005 and GGWM-006 — the motion throttle, the applied-geometry diff, the resize-only repaint mode, the server-side Expose repair, row-major parallel colour conversion, cached bar surfaces, `ApplyBatch` coalescing. Handing an intern a document that tells them to build things that already exist would waste weeks and destroy their trust in the document. So Part IV of the guide is an explicit already-done table, and every open item carries a `file.go:LINE` anchor.

### Prompt Context

**User prompt (verbatim):** (see Step 2)

**Assistant interpretation:** Synthesize the four extractions into one long-form, technically precise intern guide, storing it in the ticket and publishing it to reMarkable.

**Inferred user intent:** (see Step 2)

### What I did

- Read the four agent reports and reconciled them against each other, resolving three direct conflicts (see below).
- Wrote `design-doc/01-go-go-wm-performance-engineering-an-intern-s-guide-to-the-resize-and-render-path.md` — 1,807 lines, structured as: how to read it → executive summary → scope → Part I X11 primer → Part II evidence-anchored codebase map → Part III end-to-end drag trace and cost model → Part IV gap analysis → Part V target architecture with five decision records → Part VI seven-phase plan with exit criteria → Part VII test strategy → Part VIII risks and open questions → Part IX onboarding labs → Appendices A–E.
- Related twelve source files to the doc with `docmgr doc relate`, each with a note explaining *why* that file matters.
- Wrote three changelog entries, including one recording the central finding.
- Checked off tasks 5d9r, ukt0, 6vbj, 6nch, zayb.

### Why

Three overlapping 100+ KB review documents are not a usable onboarding artifact. They disagree with each other in places, they were written against a commit rather than against the working tree, and none of them carries line anchors. The value this ticket adds is not more analysis — it is *verification plus subtraction*: check every claim against the code, delete what is already done, anchor what remains, and order the rest by impact-to-risk.

The document is deliberately opinionated about sequencing. Phase 0 is measurement, and Phase 0 is explicitly gated on confirming or refuting the round-trip hypothesis, because if that hypothesis is wrong the whole priority order should change. Writing a plan whose first phase can invalidate its second is more honest than writing one that assumes its own diagnosis.

### What worked

- **The fan-out brief design.** Naming target sections by number and demanding verbatim preservation of numbers, tables, ASCII diagrams, and API signatures produced reusable source material rather than summaries. Every budget table, byte count, and struct sketch in the final document came through intact.
- **Briefing the codebase agent to say what is *already correct*.** This was the single most valuable instruction given to any agent. Without it the deliverable would have recommended re-implementing the motion throttle, the applied-geometry cache, and the Expose fast path.
- **The evidence anchors.** Every substantive claim in Parts II–IV carries a `file.go:LINE` reference, so a reviewer can falsify any of them in one keystroke.

### What didn't work

- The `remarquee cloud account --non-interactive` pairing loop from Step 2 was resolved when the user supplied an 8-character one-time code. Piping it in worked:
  ```bash
  printf '<code>\n' | remarquee cloud account --reauth
  ```
  The command exceeded the 120 s tool timeout and was backgrounded with an empty output file, so it looked like a failure — but it had in fact succeeded: `~/.config/rmapi/rmapi.conf` (1.5 KB) appeared at 23:09, and the background task later reported exit code 0. **Lesson: check for the side effect, not just the exit status, when a pairing command backgrounds.**
- One agent, resumed to append its methodology log, returned only the log on the second notification rather than the log appended to its original report. Harmless here because the first report was already captured, but worth knowing: resuming a completed agent gives you the *new* turn's output, not a re-emission of the old one.

### What I learned

Three conflicts between the guides and the code, resolved in favor of the code:

1. **Round trips.** The guides say the drag loop has none. `xshm.New` calls `shm.AttachChecked(...).Check()` (`pkg/xshm/xshm.go:92`) and `shm.CreatePixmapChecked(...).Check()` (`xshm.go:106-107`). Both are *checked* requests, therefore round trips. `paintFrame` destroys and recreates the surface on every dimension change (`manage.go:420-432`), and a divider drag changes dimensions every tick. Two panes change per tick, so roughly four round trips per tick, plus a `shmget`/`shmat`/`IPC_RMID` syscall triple each. The reason three independent reviews missed this is that the obvious audit is `grep '\.Reply()'`, and this is `.Check()`.
2. **The throttle.** The guides frame the 16 ms gate as leaving the final pointer position stale. It does not: `handleRelease` clears the gate and replays the release coordinates (`input.go:392-395`). The real defect is mid-drag lag and the absence of X-level motion compression — go-go-wm uses raw `MotionNotifyFun` rather than `mousebind.Drag`, which is xgbutil's compressing facility.
3. **The applied-state cache.** The guides recommend introducing one. `frame.rect` already is one, and it is correctly diffed before any `MoveResize` or client `ConfigureWindow` (`manage.go:332`). What is *not* diffed is visibility: `Map()`/`Unmap()` are issued unconditionally for every frame in the process on every relayout (`manage.go:359-366`).

A fourth, non-conflicting insight: the concurrency architecture is genuinely excellent and deserves to be defended rather than refactored. There is not a single mutex in `pkg/wmx11` (verified by grep) because one goroutine owns all X-facing state, and the mutual exclusion between X callbacks and posted ops falls out of xgbutil's unbuffered `pingBefore`/`pingAfter` handshake. JavaScript provably never touches the WM loop. Any performance proposal that adds a render thread would destroy this, which is why the guide includes a decision record recording "keep the single-owner loop" as *accepted existing practice*, specifically to stop it being re-litigated.

### What was tricky to build

The hardest judgment was **priority inversion**. Read naively, the guides put "thin decoration layers" first — it has the biggest arithmetic behind it (a 48× reduction in bytes touched) and all three documents lead with it. But it is also the highest-risk change in the codebase, because it rewrites frame lifecycle, and it is a *throughput* fix. The round trips are a *latency* fix, and latency is what the user perceives during a drag. A 7.9 MiB memcpy at memory bandwidth is on the order of a millisecond; four serial server round trips are a scheduling dependency that no amount of faster pixel code removes.

So the guide reorders: measure, then cheap structural wins, then kill the resource churn, then preview/commit, and only then the chrome split. Getting that ordering right required holding the pixel arithmetic and the round-trip cost in the same frame and deciding which one the user actually feels. I have flagged it as a hypothesis rather than a fact, because it is unmeasured — Phase 0 exists to settle it.

A second sharp edge: describing the event loop correctly required reading the *vendored* `xgbutil/xevent/eventloop.go` alongside `wm.go`, because the mutual-exclusion guarantee is not visible in go-go-wm's own code. It emerges from `MainPing` sending on an unbuffered channel before dequeuing each event. A reader who only reads `wm.go:236-281` sees a `select` and may reasonably conclude that X callbacks race with posted ops. They do not. That subtlety is now written down in §2.2 with the reasoning, because it is load-bearing for the "no mutexes" claim.

### What warrants a second pair of eyes

- **The round-trip claim itself.** It is read out of source, not measured. Someone should instrument `xshm.New` and count `Attach`/`CreatePixmap` calls per second during a real drag before Phase 2 is scheduled. Lab 3 in the guide exists precisely to reproduce it.
- **The capacity-buffer design in §5.5.** Shared pixmaps have fixed dimensions, so a larger pixmap cannot simply back a smaller window without a clipping policy. The guide flags this as needing an experiment; if the experiment fails, Phase 2 needs a different shape.
- **The Map/Unmap diffing change in Phase 1.** Workspace switching intentionally unmaps frames, and those events must not be confused with client withdrawal. This is the kind of change that looks trivial and breaks window visibility in a corner case.
- **Whether `mousebind.Drag` is a drop-in win.** It already compresses motion in xgbutil (`drag.go:97,116`) and might deliver most of the mailbox benefit for a fraction of the work — or conflict with the existing grab handling. Listed as open question 3.

### What should be done in the future

- File the Phase 0 work as its own ticket. Note the **GGWM-012 numbering collision**: the handbook's proposed backlog assigns GGWM-012 to "Divider gesture preview and latest-motion scheduler", but this ticket already holds that number. Renumber before filing GGWM-013…029.
- Decide which of the three source guides is canonical, or merge them (carried forward from Step 1).
- Consider adding a `--pprof` flag; profiling is currently env-gated only (`GO_GO_WM_PPROF`), which is a discoverability problem.

### Code review instructions

- **Start here:** `design-doc/01-…-resize-and-render-path.md`, Part IV §4.2 ("Where this guide corrects the source material"). That table is the ticket's actual contribution; if it is wrong, the rest needs re-reading.
- **Then spot-check the anchors.** Pick any three claims from Part II and verify them:
  ```bash
  sed -n '88,96p'   pkg/xshm/xshm.go        # the two .Check() round trips
  sed -n '316,369p' pkg/wmx11/manage.go     # relayoutPaint: the f.rect diff, the unconditional Map
  sed -n '330,353p' pkg/wmx11/input.go      # dividerMotion: gate, double Layout, durable op
  grep -rn 'sync\.\|Mutex' pkg/wmx11/*.go | grep -v _test   # expect: no output
  ```
- **Validate the ticket:**
  ```bash
  docmgr doctor --ticket GGWM-012-GUIDES --stale-after 30
  ```

### Technical details

Reconciled priority order, with the reasoning made explicit:

| Tier | Cost | Nature | Anchor |
|---|---|---|---|
| 1 | ~4 checked X round trips + syscalls per tick | **Latency** — serial server dependency | `xshm.go:92,106`; `manage.go:420-432` |
| 2 | ~32 MiB memory traffic per tick (2 panes, fill + convert) | **Throughput** — memory bandwidth | `manage.go:381-413`; `ximage.go:56-108` |
| 3 | Divider image + pixmap create/free per divider per tick | **X resource churn** | `divider.go:60,132,148`; `bars.go:110-115` |
| 4 | 2× full `Layout`, O(n²) `Find`, unconditional Map/Unmap | **Algorithmic waste** | `input.go:342`; `manage.go:321,325,359-366` |
| 5 | Client-side reflow after each configure | **Outside WM control** | — mitigated by outline mode / `_NET_WM_SYNC_REQUEST` |

## Step 4: Validation, and the reMarkable upload blocked on a missing toolchain

The ticket validates cleanly and the deliverable is committed. The final delivery step — publishing the bundle to reMarkable — is blocked on a missing local toolchain rather than on anything about the content, and the user chose to leave it that way rather than install several hundred megabytes of TeX.

### Prompt Context

**User prompt (verbatim):** (see Step 2 — "…and the nupload to remarkable")

**Assistant interpretation:** Publish the finished guide to the reMarkable device.

**Inferred user intent:** Read the guide on the device rather than in a terminal.

**User decision (verbatim option chosen):** "Skip the upload."

### What I did

- Ran `docmgr doctor --ticket GGWM-012-GUIDES --stale-after 30` → **all checks passed**, no vocabulary warnings (the topics used — `wm`, `performance`, `x11`, `goja` — were already in `ttmp/vocabulary.yaml`).
- Paired the reMarkable device successfully using the one-time code the user supplied.
- Attempted the bundle upload; diagnosed the failure; presented the options; recorded the user's choice to skip.
- Edited task `j43y` to record the blocker rather than silently leaving it unchecked.

### What worked

- `docmgr doctor` passed on the first run.
- Device pairing succeeded: `~/.config/rmapi/rmapi.conf` was written and the background pairing task exited 0.
- `remarquee upload bundle --dry-run` correctly resolved all five inputs, the bundle name, and the remote directory.

### What didn't work

Two distinct failures, in order:

1. **Relative paths silently fall through to the help text.** Invoking the bundle with repo-relative paths produced remarquee's help output with `**No examples found!**` and no error line. The same invocation with absolute paths dry-ran correctly. This is a genuinely confusing failure mode — the tool prints help rather than "file not found".

2. **The real blocker: no PDF toolchain.** Running without `--dry-run` produced:
   ```
   Error: pandoc failed: : exec: "pandoc": executable file not found in $PATH
   ```
   followed, again, by the full help text. Confirmed absent:
   ```bash
   which pandoc xelatex          # neither found
   pacman -Qq | grep -iE '^(pandoc|texlive)'   # not installed
   ```
   No alternative engine is available either — a scan for `typst`, `weasyprint`, `wkhtmltopdf`, `tectonic`, `lualatex`, and `pdflatex` found none. Only `groff` and `libreoffice` are present, and neither is a `--pdf-engine` remarquee accepts.

   The fix would be `sudo pacman -S --needed pandoc-cli texlive-xetex texlive-fontsrecommended` (available as `extra/pandoc-cli 3.6.1-18`), roughly 300–500 MB. The user chose not to install it.

### What I learned

- `remarquee` prints its help page on *any* error, including a missing binary and a bad path, with the actual error on the first line before the help. Always read the first line of output, not the tail — `| tail -20` hides the only useful part. This cost two wasted invocations.
- The reMarkable path has two independent prerequisites that fail differently: **auth** (rmapi token, fixed by the one-time code) and **rendering** (pandoc + a LaTeX engine). Verifying one says nothing about the other. `remarquee status` reported `ok` throughout, despite pandoc being absent.

### What was tricky to build

Nothing was built in this step. The tricky part was diagnosis order: because the pairing failure in Step 2 was loud and repetitive, it looked like the whole reMarkable path was an auth problem. It was not — auth was fixed within one command of getting the code, and the actual blocker was a silent toolchain gap that only surfaced once a non-dry-run reached the pandoc invocation.

### What warrants a second pair of eyes

- Nothing in this step. The content risks are all recorded in Step 3.

### What should be done in the future

- If reMarkable delivery is wanted from this machine, install `pandoc-cli` plus `texlive-xetex` and `texlive-fontsrecommended`, then re-run:
  ```bash
  T=$PWD/ttmp/2026/07/21/GGWM-012-GUIDES--import-go-go-wm-engineering-guides-and-handbook
  remarquee upload bundle \
    "$T/design-doc/01-go-go-wm-performance-engineering-an-intern-s-guide-to-the-resize-and-render-path.md" \
    "$T/reference/01-investigation-diary.md" \
    "$T/index.md" "$T/tasks.md" "$T/changelog.md" \
    --name "GGWM-012 go-go-wm Performance Engineering" \
    --remote-dir "/ai/2026/07/21/GGWM-012-GUIDES" \
    --toc-depth 2 --non-interactive
  ```
  Use **absolute paths** — relative ones fail with a help dump instead of an error.
- The guide contains several wide tables and ASCII diagrams in fenced blocks. When the PDF is eventually generated, check that the Part III trace diagram and the Appendix C file index survive xelatex's line breaking; `--layout editor` widens margins if they do not.

### Code review instructions

- Validate the ticket:
  ```bash
  docmgr doctor --ticket GGWM-012-GUIDES --stale-after 30    # expect: all checks passed
  docmgr task list --ticket GGWM-012-GUIDES                   # expect: 9/10 done, upload blocked
  ```

## Step 5: Unblocking the PDF toolchain and completing delivery

The user reversed the Step 4 decision and asked for the toolchain to be installed after all, in a tmux window so they could supply the sudo password themselves. The install turned out to need three rounds rather than one, because the LaTeX dependency chain for pandoc's default template is deeper than the obvious `pandoc + xetex` pair, and because the tool's default fonts are not present on this machine. All three failures were fixed without guessing, by reading the exact error and checking what was actually installed.

### Prompt Context

**User prompt (verbatim):** "open tmux with sudo to install tetex and pandoc"

**Assistant interpretation:** Open an interactive tmux window running the pacman install for TeX Live and pandoc, so the user can enter their sudo password, then complete the reMarkable upload.

**Inferred user intent:** Get the guide onto the reMarkable device after all, keeping the privileged install visible and under their control rather than hidden inside a tool call.

**Follow-up user prompt (verbatim):** "can you resent the sudo command to the window, I don't see it"

**Assistant interpretation:** Re-issue the command in the tmux window because it is not visible.

**Inferred user intent:** Make the prompt visible so they can act on it.

### What I did

1. Created window `8:install` in the already-attached `go-go-wm-dev` session and sent the pacman command.
2. On the re-send request, cancelled the pending `sudo` with `C-c` before re-sending — see below for why this mattered.
3. Confirmed the transaction (184.49 MiB download, 773.29 MiB installed, 245 packages).
4. Installed a second round after `xcolor.sty` was reported missing: `texlive-latexrecommended texlive-latexextra texlive-mathscience`.
5. Substituted available fonts rather than installing a third round.
6. Ran the upload successfully and updated task `j43y` and the changelog.

### Why

The tmux route was the right shape for this: `sudo` needs a TTY for the password prompt, and a backgrounded tool call cannot provide one. Putting it in a window of the session the user already had attached meant they could act on it without switching context, while I could still observe progress with `capture-pane` and `display-message -p '#{pane_current_command}'`.

For the font failure I deliberately did **not** install a third package. `fc-list` showed Noto Sans and JetBrains Mono already present, both of which are better choices for e-ink than DejaVu anyway. Checking before installing cost one command and saved a round trip through the sudo prompt.

### What worked

- Polling `#{pane_current_command}` is a reliable completion signal for a long pacman run: it reads `sudo` while the transaction is live and flips to `bash` when the shell returns. This is much better than sleeping a fixed interval and hoping.
- `kpsewhich <pkg>.sty` verified the LaTeX packages directly instead of re-running the multi-minute pandoc pipeline to find out.
- The final invocation, which is the one to reuse:
  ```bash
  remarquee upload bundle <absolute paths...> \
    --name "GGWM-012 go-go-wm Performance Engineering" \
    --remote-dir "/ai/2026/07/21/GGWM-012-GUIDES" \
    --toc-depth 2 \
    --mainfont "Noto Sans" --monofont "JetBrains Mono" \
    --non-interactive
  # OK: uploaded GGWM-012 go-go-wm Performance Engineering.pdf -> /ai/2026/07/21/GGWM-012-GUIDES
  ```
  The first upload used `--layout editor` for wider annotation margins; the user preferred the standard layout, so the document was re-rendered without it (see Step 6). Fonts stay overridden because DejaVu is absent.

### What didn't work

Three sequential failures, each needing a different fix:

1. **`tmux send-keys` reported `can't find window: install`** even though `new-window` had succeeded. `list-windows` showed the cause: an existing window named `install-` at index 6 made the name `install` ambiguous. Fixed by targeting the stable pane ID `@10` instead of the name.
2. **`! LaTeX Error: File 'xcolor.sty' not found.`** `texlive-xetex` plus `texlive-fontsrecommended` is not enough for pandoc's default template. Fixed with `texlive-latexrecommended texlive-latexextra texlive-mathscience`.
3. **`Package fontspec Error: The font "DejaVu Sans" cannot be found`.** remarquee defaults to DejaVu Sans / DejaVu Sans Mono, and `ttf-dejavu` is not installed here. Fixed by overriding with fonts that are.

### What I learned

- **Never blind-send text to a pane running `sudo`.** When the re-send was requested, the pane still had `sudo` waiting at `[sudo] password for manuel:`. Sending the command text at that moment would have submitted the entire pacman command line *as the password* — echoing a failed password attempt into the scrollback and possibly into logs. Checking `#{pane_current_command}` first, then sending `C-c` to dismiss the prompt, then re-sending, was the safe order. This is a general rule for driving interactive TTYs: read the pane state before writing to it.
- The password prompt had in fact already been satisfied by the time of the re-send (sudo credentials were cached), so the second attempt landed straight on `:: Proceed with installation? [Y/n]`. That is exactly the situation in which a blind send would have been most damaging — the pane looked idle but was not.
- pandoc-on-Arch needs a larger package set than intuition suggests. The working set for xelatex output is: `pandoc-cli`, `texlive-basic`, `texlive-latex`, `texlive-latexrecommended`, `texlive-latexextra`, `texlive-mathscience`, `texlive-xetex`, `texlive-fontsrecommended`. `selnolig.sty` is still absent but is guarded by `\ifLuaTeX` in pandoc's template and so is irrelevant under xelatex.

### What was tricky to build

Nothing was built. The genuinely tricky part was the send-keys race described above — a case where the obviously-correct action (do what the user asked: re-send the command) was unsafe given hidden pane state, and where the failure would have been silent rather than loud. The symptom the user reported ("I don't see it") was consistent both with "the window is empty" and with "the window is sitting at a prompt you have not looked at"; only inspecting the pane distinguished them.

### What warrants a second pair of eyes

- The rendered PDF should be checked on the device for the wide tables in Part IV and Appendix C, and for the ASCII trace diagram in Part III §3.1. Those were the layout risks flagged in Step 4. If they break, `--layout editor` is already applied, so the next lever is `--geometry margin=0.6in` or splitting the widest tables.

### What should be done in the future

- Consider `ttf-dejavu` if remarquee's defaults are wanted unmodified; otherwise record the Noto Sans / JetBrains Mono override as the house style for this repo's uploads.

### Code review instructions

- Verify delivery:
  ```bash
  remarquee cloud ls /ai/2026/07/21/GGWM-012-GUIDES --long --non-interactive
  docmgr task list --ticket GGWM-012-GUIDES        # expect 10/10 done
  docmgr doctor --ticket GGWM-012-GUIDES --stale-after 30
  ```

## Step 6: Re-render with the standard layout

A one-line preference change, recorded because it alters the delivered artifact and because it cost an irreversible overwrite.

### Prompt Context

**User prompt (verbatim):** "use the standard layout, not editor."

**Assistant interpretation:** Re-render the reMarkable PDF with remarquee's default markdown layout instead of the `editor` preset, and replace the document on the device.

**Inferred user intent:** They do not want the widened annotation margins; the standard layout fits more content per page and reads better for a long technical document.

### What I did

- Re-ran the bundle upload with `--layout editor` removed and `--force` added, keeping `--mainfont "Noto Sans" --monofont "JetBrains Mono"`.
- Result: `OK: uploaded GGWM-012 go-go-wm Performance Engineering.pdf -> /ai/2026/07/21/GGWM-012-GUIDES`.
- Patched the Step 5 entry, which had asserted `--layout editor` "was added deliberately", so the diary no longer documents a setting that is not in force.

### Why

`--layout editor` was my choice, not a requirement, made on the assumption that a document meant to be marked up wants annotation margins. The user's judgment overrides that: for a 1,800-line technical guide, page count and line length matter more than marginalia space.

Patching Step 5 rather than only appending Step 6 matters because the diary is meant to be read as a record of *what is true*, not just what happened in sequence. A future reader looking up the upload invocation would otherwise copy the wrong flags.

### What worked

Re-running with `--force` succeeded first try — the toolchain and font overrides from Step 5 carried over unchanged.

### What didn't work

Nothing failed. But an intermediate attempt without `--force` correctly refused:
```
SKIP: GGWM-012 go-go-wm Performance Engineering already exists in /ai/2026/07/21/GGWM-012-GUIDES (use --force to overwrite)
```
This is the right default — it means a re-upload cannot silently destroy annotations.

### What was tricky to build

Nothing, but there is a real hazard worth naming: **`--force` deletes the existing document together with any annotations on it.** In this case the document was minutes old and unannotated, so the cost was zero. Once the guide has actually been read and marked up on the device, a render-only change is no longer cheap — the annotations do not survive and are not recoverable. From this point on, a re-render should be treated as a decision, not a formatting tweak, and confirmed against whether the device copy has been written on.

### What warrants a second pair of eyes

- The standard layout is narrower per line than `editor`. The wide tables in Part IV and Appendix C, and the ASCII trace diagram in Part III §3.1, should be checked on the device. If they overflow, the next lever is `--geometry margin=0.6in`, not `--layout editor`.

### What should be done in the future

- Record the house style for this repo's reMarkable uploads: default layout, `--toc-depth 2`, Noto Sans / JetBrains Mono.

### Code review instructions

- Confirm the delivered artifact:
  ```bash
  remarquee cloud ls /ai/2026/07/21/GGWM-012-GUIDES --long --non-interactive
  ```

## Step 7: Implementing Phase 0 and Phase 1 — and being wrong about the index

This step turns the guide from a document into code. The VT2 experiment had already produced its first real finding before the harness even completed a run — `shared_pixmaps: false`, meaning this machine never takes the MIT-SHM path at all — so the work here covers both the instrumentation needed to settle the remaining questions and the Phase 1 changes that are safe to make without waiting for numbers.

The most useful thing that happened was being wrong in public. I implemented the tree index that the design doc recommends for removing an O(n²) loop, benchmarked it, and found it made the common case *slower*. Rather than quietly keep it, I measured a second variant and kept the change only once it was defensible — and the guide now has to be corrected, because it presented that item as an unambiguous win.

### Prompt Context

**User prompt (verbatim):** "ok, update the tasks and then work on all of this and make it faster, by yourself"

**Assistant interpretation:** Add the implementation work to the ticket's task list, then autonomously carry out the Phase 0 instrumentation and Phase 1 optimizations described in the design doc, verifying as I go.

**Inferred user intent:** Stop planning and start making the window manager measurably faster, with the ticket kept up to date so the work is reviewable.

**Follow-up user prompt (verbatim):** "Keep a detailed diary as you work, use the skill for the format"

**Assistant interpretation:** Maintain the investigation diary continuously through the implementation, in the `diary` skill's format.

**Inferred user intent:** Preserve the reasoning trail — especially the failures — so the implementation can be reviewed and continued by someone else.

### What I did

Added twelve tasks (`zps3`…`e4yb`) splitting the work into P0 measurement and P1 optimization, then implemented:

**Measurement (P0)**
- `pkg/wmx11/perf.go` — a `perfCounters` struct covering reconciliation, upload-path churn, drag scheduling, and wall clock, plus a `perfSnapshot` JSON shape.
- Counters wired into `relayoutPaint`, `paintFrame`, `dividerMotion`, `gripMotion`, `syncDividers`, and both upload branches. The ones that matter most: `shm_creates`/`shm_destroys` and `ximg_creates`/`ximg_destroys` — these are what settle the central hypothesis — plus `motion_events` vs `motion_admitted` to expose coalescing.
- `perf` and `perf-reset` IPC queries returning bounded aggregates.
- `GO_GO_WM_NO_RESIZE_PAINT=1`, which suppresses decoration paint during a divider drag while still committing geometry. This measures the upper bound of paint removal directly, without first restructuring frames into chrome + content.
- Benchmarks in `pkg/wmcore/layout_bench_test.go` and `pkg/draw/paint_bench_test.go`. The repository previously had **zero**.

**Optimization (P1)**
- `wmcore.Index` / `BuildIndex` / `BuildIndexInto` plus `index_test.go`, replacing the `Root.Find`-per-layout-item loops in `relayoutPaint` and `syncDividers`.
- Divider paint key: `dividerWin` now records the `(mode, dir, w, h)` it last painted and skips `paintDivider` when nothing visible changed.
- Divider map mirror: `d.mapped` stops a `MapWindow` request per divider per relayout.
- Frame map mirror: `frame.mapped` as a tri-state (`mapUnknown`/`mapMapped`/`mapUnmapped`) so Map/Unmap is issued only on a visibility transition.
- `handleConfigureRequest` now sends a synthetic `ConfigureNotify` instead of calling `relayout()`.
- Split-rect caching in `dragState`, halving the layout passes per drag tick.
- `gripMotion` given the same 16 ms admission control as `dividerMotion`.

### Why

The ordering was deliberate: instrumentation first, then only those optimizations that are provably safe without a display. Everything implemented here is verifiable by `go test` and `go test -bench`; nothing needed a running X server. That matters because the X-dependent parts of the plan (Phase 2's capacity buffers, Phase 4's chrome split) are exactly the risky ones, and they should not be attempted until the counters can show whether they helped.

The `GO_GO_WM_NO_RESIZE_PAINT` flag is a measurement tool rather than a feature. The design doc's Phase 2 asks for "a resize path that configures geometry without calling `paintFrame` … to quantify the upper bound of paint removal before restructuring chrome". An env-gated branch is the cheapest honest way to get that number.

### What worked

- **The synthetic `ConfigureNotify` fix was almost free.** The existing code carried the comment *"Re-assert our geometry (send a synthetic ConfigureNotify)"* directly above a call to `w.relayout()` — the comment described the correct behaviour and the code did something else, roughly a thousand times more expensive. Writing what the comment already said removed a whole-workspace relayout from a client-driven path with no rate limit.
- **The split-rect cache is exactly right by construction.** Changing a split's ratio moves its descendants' rectangles but never its own, so the rect captured at `beginDividerDrag` stays valid for the entire gesture. That removes one of the two full-tree layouts per tick without any approximation.
- **All 15 packages pass** after every change, including the existing `focus_state_test.go` and `float_test.go` regression suites.
- The divider paint guard is the change I expect to matter most on this machine, because `paintDivider` allocates an image *and* creates and frees a server pixmap through the uncached blit path, and it was firing once per divider per motion tick.

### What didn't work

**The tree index made the common case slower, and I nearly shipped it.**

The design doc lists "remove the O(n²) `Find`-in-loop" as a Phase 1 win. The first implementation allocated a fresh map per pass. Benchmarked (3 runs, 1 s each, ns/op):

| leaves | `Find` (old) | `BuildIndex` (new) | verdict |
|---:|---:|---:|---|
| 2 | 85 | 531 | **6.2× slower** |
| 4 | 230 | 690 | **3.0× slower** |
| 8 | 722 | 1179 | **1.6× slower** |
| 16 | 2732 | 3509 | **1.3× slower** |
| 32 | 11 896 | 8 014 | 1.5× faster |

A fresh `map[NodeID]*Node` costs more than the depth-first scans it replaces until roughly 24 leaves — and a real workspace holds two to eight tiles. The "obvious" algorithmic win was a regression across the entire realistic range.

The fix was `BuildIndexInto`, which refills one scratch map owned by the WM:

| leaves | `Find` | `BuildIndexInto` | verdict |
|---:|---:|---:|---|
| 2 | 85 | 178 | 2.1× slower (−93 ns) |
| 4 | 230 | 329 | 1.4× slower |
| 8 | 722 | 791 | 1.1× slower (−69 ns) |
| 16 | 2732 | 1540 | **1.8× faster** |
| 32 | 11 896 | 3212 | **3.7× faster** |

Crossover moves to ~10 leaves and the small-tree penalty becomes about 70 ns per pass — noise beside a `paintFrame` that the live log timed at **3.1 ms**. I kept it on those terms: it is insurance against pathological trees, not a win at typical sizes.

### What I learned

- **`draw.Text` is the dominant cost in title rendering.** New benchmarks: `TitleStrip.Render` at width 1272 takes **73.7 µs** and allocates 115 KB, of which `draw.Text` alone is **52.8 µs** (72%). Since the title string does not change while a pane is being resized — only its width does — this is cacheable, and it is a bigger single win than anything in the Phase 1 list.
- **`draw.Fill` is already optimal.** 108 µs for 1272×664 at **31 GB/s** is memory-bandwidth bound. GGWM-005's row-pattern fill left nothing on the table; do not revisit it.
- Putting those together: the measured cost of one `paintFrame` is ~182 µs of fill plus title, against the ~3.1 ms the live log recorded. **Roughly 94% of a frame paint is therefore neither fill nor title rendering** — it is BGRA conversion, upload, and X. That is a much sharper statement of where to look next than the design doc currently makes.
- Throttling `gripMotion` introduced a correctness obligation I nearly missed: unlike the divider path, grip release did **not** replay the final pointer position, so `d.over`/`d.zone` could be up to one frame stale and the drop would land on the wrong target. Added the same release-replay the divider path has had since GGWM-005.

### What was tricky to build

Two things.

**The divider paint key needed an escape hatch.** Appearance is a pure function of `(mode, dir, w, h)` *for a given palette*, and a theme swap changes the pixels without changing any of those. Worse, an `Expose` means the server has thrown the contents away, so the key describes something that is no longer on screen. Both needed explicit `invalidate()` calls — in `setTheme`'s divider loop and in the `Expose` handler. Caching keyed on a partial description of the output is exactly the kind of change that works in testing and produces stale pixels in the field weeks later; the two invalidation points are the whole correctness argument.

**The map mirror had to be a tri-state, not a bool.** A `bool` would default to "unmapped", which is wrong for frames adopted at startup by `manageExisting` — the WM has told the server nothing about them, and skipping the first Map because the field happens to read `false` would leave windows invisible. `mapUnknown` as the zero value forces the first pass to issue the request and makes the state machine honest about what it does and does not know.

### What warrants a second pair of eyes

- **`frame.mapped` versus workspace switching.** Workspace switches intentionally unmap frames, and `UnmapNotify` for a WM-initiated unmap must not be mistaken for client withdrawal. I updated the two sites that map frames (`handleMapRequest`, `manage`) but a reviewer should check whether any other path changes map state behind the mirror's back.
- **The divider `Expose` invalidation.** Dividers are created with `CwBackPixel`, so the server repaints the background itself; the grip mark is what needs redrawing. If the background pixel alone is ever deemed sufficient, the invalidate becomes unnecessary — but leaving it costs one paint per expose and is the safe direction.
- **The synthetic `ConfigureNotify` field values.** Root-relative X/Y are computed as `f.rect.X + BorderW` / `f.rect.Y + TitleH`. That matches how `relayoutPaint` configures the client child, but it deserves a protocol-level test against a real client rather than my reading.
- **Sharing one `idxScratch` between `relayoutPaint` and `gripMotion`.** Safe today because both run on the WM loop and never nest, but it is an invariant that is not enforced by anything.

### What should be done in the future

- **Cache the title text rendering.** 52.8 µs per paint, and the text is invariant during a resize. This is the next change I would make.
- Correct the design doc: Part IV lists the O(n²) removal as a straightforward win, and Part VI Phase 1 repeats it. Both need the benchmark table and the "only above ~10 leaves" caveat.
- Record the `shared_pixmaps: false` finding in the guide — on this host GGWM-006's shared-pixmap path never executes, so the Tier-1 hypothesis needs restating in terms of the `ximg` fallback.
- `wmcore.Layout` still allocates a fresh map per call (2.4 µs at 8 leaves). The same scratch-reuse trick applies but changes a public signature.

### Code review instructions

- **Start at** `pkg/wmx11/perf.go` to see what is now measurable, then `pkg/wmx11/manage.go:relayoutPaint` for the index, the move/resize split, and the map diff.
- **The behavioural changes to scrutinise** are `divider.go:paintDivider` (the cache guard and its two invalidation points), `input.go:handleRelease` (the new grip replay), and `manage.go:sendSyntheticConfigureNotify`.
- **Validate:**
  ```bash
  go build ./... && go test ./... -count=1
  go test ./pkg/wmcore/ -run XXX -bench 'FindAll|IndexReuseAll' -benchtime=1s -count=3
  go test ./pkg/draw/   -run XXX -bench 'Fill|TitleStrip|BenchmarkText' -benchtime=300ms
  ```
- **Observe the effect at runtime** — start the WM, drag a divider, then:
  ```bash
  echo '{"q":"perf-reset"}' | socat - UNIX-CONNECT:$XDG_RUNTIME_DIR/go-go-wm.sock
  # ... drag ...
  echo '{"q":"perf"}'       | socat - UNIX-CONNECT:$XDG_RUNTIME_DIR/go-go-wm.sock
  ```
  Expect `divider_paint_skipped` to dominate `divider_painted`, `motion_coalesced` to be large, and `map_requests_skipped` to be non-zero. `shm_creates` will be 0 on this host because `shared_pixmaps` is false; watch `ximg_creates` instead — it should equal the number of size changes.

### Technical details

Measured per-paint costs (this machine, `go test -bench`):

| Operation | 1272×664 | 1920×1080 | Allocations |
|---|---:|---:|---|
| `draw.Fill` | 108 µs | 373 µs | 0 |
| `TitleStrip.Render` | 73.7 µs (w=1272) | 91.6 µs (w=1920) | 12 / 115 KB |
| └ of which `draw.Text` | 52.8 µs | — | 2 |
| `draw.TextWidth` | 1.8 µs | — | 0 |
| Observed `paintFrame` (live log) | ~3.1 ms | — | — |

## Step 8: The biggest win was not on the plan — caching glyph runs

The benchmarks written in Step 7 immediately pointed somewhere the design doc never looks. `draw.Text` was 72% of a title-strip render, and a window's title does not change while its pane is being resized — only the pane's width does. Caching the rasterized run made it **7.2× faster**, which is a larger single improvement than every Phase 1 item combined, and it was invisible until there were benchmarks.

The cost was a real, measurable change in rendering output: 14 pixels out of 179,200 at one part in 255. Most of this step was deciding whether that was acceptable and making sure it could never quietly get worse.

### Prompt Context

**User prompt (verbatim):** (see Step 7 — "work on all of this and make it faster, by yourself")

**Assistant interpretation:** Continue the autonomous optimization work, following the evidence the benchmarks produce rather than only the pre-written plan.

**Inferred user intent:** Real, measured speedups — not just execution of a checklist.

**Commit (code):** `190e10c` — "perf(wmx11,wmcore,draw): instrument the resize path and cut per-paint work"

### What I did

- Added `pkg/draw/textcache.go`: a bounded cache (512 entries, wholesale clear on overflow) keyed by `(string, bold, size)`, storing each run as an **alpha mask** plus its baseline offset and advance.
- Rewired `draw.Text` to composite the cached mask through `image/draw.DrawMask` with a uniform colour source.
- Quantified the resulting golden-test failures rather than guessing at them, via a temporary Go diff harness (stdlib PNG decoding — the machine has no PIL).
- Replaced the throwaway probe with three real tests: `TestTextCacheMatchesDirect` (tolerance 1/255 against the original implementation, which is retained in the test file as the reference), `TestTextCacheAdvanceMatches`, and `TestTextCacheEviction`.
- Regenerated the two affected goldens with `go test ./pkg/apps -update`.
- Kept the diff harness as `pkg/apps/golden_delta_test.go`, a diagnostic that sizes any future golden failure instead of just reporting "differs".

### Why

Storing an **alpha mask** rather than coloured pixels was the key decision. The alternative — caching the rendered RGBA — would have keyed on colour too, so a focus change or a theme swap would miss the cache and re-rasterize. With a mask, geometry work happens once per `(string, bold, size)` and colour is applied at blit time. Focus changes recolour the same title constantly, so this matters in practice, not just in theory.

The bound exists because titles are unbounded input: windows come and go, and each new title is a new key. A wholesale clear at 512 entries is cruder than an LRU but the working set is one entry per visible title plus a handful of button glyphs, so the eviction path should essentially never run — and simple code that cannot leak beats clever code that might.

### What worked

| Benchmark | Before | After | Change |
|---|---:|---:|---|
| `draw.Text` (24-char title) | 53.9 µs | 7.46 µs | **7.2× faster** |
| `TitleStrip.Render` w=1272 | 73.7 µs | 38.8 µs | **1.9× faster** |
| `TitleStrip.Render` w=1920 | 91.6 µs | 50.7 µs | **1.8× faster** |

`pkg/draw`'s own golden tests — title strips, banners, menus, status lines, drop previews — passed **unchanged**, which was the first evidence the change was faithful.

### What didn't work

`pkg/apps` golden tests failed: `builtin-about` and `demo-colors`. Two of nine.

My first fidelity probe compared five representative strings and reported `maxdelta=0, bytesdiff=0` — pixel-identical — which contradicted the failures and briefly made me think the two suites disagreed. They did not; my probe simply had not hit the case that differs.

The actual measurement, once I wrote a proper PNG differ:

```
builtin-about   diff=14/179200 px (0.0078%)  maxdelta=1  bbox=(20,14)-(80,204)
demo-colors     diff=4/179200 px  (0.0022%)  maxdelta=1  bbox=(18,107)-(162,110)
```

**Cause.** `font.Drawer` blends each glyph onto the destination in turn:
`dst = over(over(dst, g₁), g₂)`. The cache blends glyphs into one mask and applies it once: `dst = over(dst, over(g₁, g₂))`. Those are algebraically identical for non-overlapping glyphs, and differ by one least-significant bit where antialiased glyphs overlap, because the alpha arithmetic rounds at a different point. Only certain kerned pairs at certain sizes overlap at all, which is why 7 of 9 goldens and all 5 of my probe strings were unaffected.

### What I learned

- **A "pixel-identical" spot check proves very little.** Five hand-picked strings all reported zero delta while a real corpus disagreed. The lesson generalizes: when a change *can* alter output, measure the whole corpus, don't sample it.
- The absolute cost is now clearer than the design doc states. One `paintFrame` at 1272×664 is roughly 105 µs of `Fill` plus 39 µs of title = **~144 µs**, against the **~3.1 ms** the live WM log recorded. So **over 95% of a frame paint is neither fill nor text** — it is BGRA conversion, upload, and X. That is a much sharper target than "rendering is slow", and it is where the next work belongs.
- `draw.Fill` at 32 GB/s is memory-bandwidth bound. GGWM-005 finished that job; there is nothing left there.

### What was tricky to build

The judgement call, not the code.

A 1/255 delta on 18 pixels across two images is invisible — but goldens exist precisely to catch unintended changes, and silently regenerating them is how a codebase loses its safety net one small justification at a time. Simply accepting the diff would have set a bad precedent even though this particular diff is harmless.

What made it acceptable was making the tolerance **explicit and enforced**: `TestTextCacheMatchesDirect` keeps the original implementation as a reference and fails if any channel drifts by more than 1. So the trade is no longer "we accepted a rendering change once" but "we accepted a bounded rendering change, and the bound is a test". A future change to the cache that costs 5/255 instead of 1/255 now fails loudly rather than quietly regenerating more goldens.

The second sharp edge was the mask's baseline. The mask stores glyphs with the baseline at `metrics.Ascent.Ceil()` from its top, and the blit places it at `y - originY`. Getting that rounding wrong shifts every glyph by a pixel — which would *not* have been a 1/255 difference, it would have been obvious. The `TextWidth` advance test guards the horizontal equivalent, since callers position subsequent runs from the returned advance.

### What warrants a second pair of eyes

- **The tolerance of 1.** It is a judgement, not a law. If the project would rather be bit-exact, revert `draw.Text` to the direct path and keep the cache for the title strip only — the speedup mostly survives, since the title is the expensive run.
- **Mask padding.** `rasterizeText` pads the mask width by `int(size)+2` for glyphs that overhang their advance. A face with extreme overhang could clip. The fidelity test would catch it for the strings it covers; it is not a proof.
- **Cache eviction under adversarial input.** A script that renames a window in a loop generates unbounded keys. The wholesale clear bounds memory, but the clear itself is O(n) on the WM loop. It has never fired in practice; worth knowing it exists.

### What should be done in the future

- **Follow the 95%.** Fill and text are now ~144 µs of a ~3.1 ms paint. Instrument `ConvertRows` and the upload separately — that is where the remaining time is, and the counters added in Step 7 do not yet break it down.
- Cache the whole title strip, not just its text, keyed by `TitleRenderKey` including width. During a resize the width changes every tick so it would miss — which is exactly the argument for the capacity-buffer approach in the design doc §5.5.
- Consider hoisting `image.NewUniform` out of the blit; it is a small allocation per call.

### Code review instructions

- **Start at** `pkg/draw/textcache.go`, then read `pkg/draw/textcache_test.go` — the reference implementation and the tolerance argument are both there.
- **The two regenerated goldens** are `pkg/apps/testdata/{builtin-about,demo-colors}.png`. To re-derive the delta yourself:
  ```bash
  git stash && go test ./pkg/apps/ -count=1        # regenerate .got.png against old goldens
  git stash pop
  go test ./pkg/apps/ -run TestGoldenDelta -v      # prints diff px, max delta, bbox
  ```
- **Validate:**
  ```bash
  go test ./... -count=1                            # 15 packages
  go test ./pkg/draw/ -run TestTextCache -v -count=1
  go test ./pkg/draw/ -run XXX -bench 'BenchmarkText$|TitleStrip' -benchtime=400ms
  ```

### Technical details

Cache entry shape:

```go
type textKey struct { s string; bold bool; size float64 }

type textMask struct {
    mask    *image.Alpha // coverage; colour applied at blit time
    originY int          // baseline offset within the mask
    advance int          // pen advance, must equal TextWidth
}
```

Paint-path cost after this step, 1272×664 pane:

```
draw.Fill            ~105 us   (32 GB/s — memory-bandwidth bound, done)
TitleStrip.Render     ~39 us   (was 73.7 us)
                     --------
subtotal             ~144 us
observed paintFrame  ~3100 us  (live WM debug log)
                     --------
unaccounted          ~2956 us  ->  BGRA conversion + upload + X
```

## Step 9: Measurement refutes the central hypothesis

The VT harness failed a second time, but the fixed logging finally explained why, and that pointed at a much better approach: run the whole thing in a nested Xephyr server on the user's existing display. That needs no console, cannot take down a real session, and can be driven entirely from a tool call. Within minutes it produced the numbers this ticket has been trying to get since the beginning.

The numbers say the design document's central hypothesis is **wrong**. Not imprecise — wrong in a way that reverses the recommended order of work.

### Prompt Context

**User prompt (verbatim):** "I ran the test again."

**Assistant interpretation:** Read the harness output from the user's second VT attempt and diagnose it.

**Inferred user intent:** Get the experiment unblocked and produce the measurements the plan depends on.

**Follow-up user prompt (verbatim):** "Goal set: continue as far as you can before you need my input / contribution again"

**Assistant interpretation:** Work autonomously to the point where a human contribution is genuinely required.

**Inferred user intent:** Stop round-tripping through the console; get results.

### What I did

- Read `harness.log` — the logging fix from Step 6 paid off immediately.
- Diagnosed the VT failure, then abandoned that approach in favour of Xephyr.
- Wrote `scripts/ggwm-xephyr-validate.sh`: nested server, real WM, scripted `xdotool` drag, perf counters dumped over IPC.
- Ran three conditions: default, `GO_GO_WM_NO_RESIZE_PAINT=1`, `GO_GO_WM_NO_SHM=1`.
- Extracted `paintFrame` duration distributions from the JSON logs.

### What worked

**Xephyr is the right harness and the VT was never necessary.** `:0` is reachable (3200×1800), so a nested `:7` at 1280×800 runs the real window manager with real X semantics, disposably, from a tool call. The whole VT/tty/startx path — three failed attempts across two sessions — was solving a problem that did not need solving.

The Phase 1 changes are confirmed working at runtime:

| Counter | Value | Meaning |
|---|---:|---|
| `map_requests_skipped` | 700 | The map-state mirror suppressed 700 redundant Map/Unmap requests |
| `divider_paint_skipped` | 350 | The paint guard skipped 350 of 531 divider paints (66%) |
| `motion_coalesced` | 294 of 644 | 46% of motion events dropped by admission control |
| `layout_calls` / `relayouts` | 351 / 350 | The split-rect cache holds: one layout per tick, not two |

No visual defects, no missing windows, no stale pixels. The three behavioural changes I flagged as unverifiable by unit test are now verified.

### What didn't work — and the finding that matters

**The Tier-1 hypothesis is refuted.**

The design document argues that `xshm.New`'s two *checked* X requests — synchronous round trips — paid on every tick because `paintFrame` recreates the surface on every dimension change, are the dominant cost of a drag. The counters confirm the *mechanism* exactly: `shm_creates = shm_destroys = frames_resized = 512`. One create/destroy pair per resized pane per tick. **1,024 round trips for a single drag.**

But the conclusion drawn from that mechanism does not survive contact with a measurement:

| Condition | `shm_creates` | `ximg_creates` | ms/paint | p50 | p95 |
|---|---:|---:|---:|---:|---:|
| default (shm) | 512 | 0 | **4.98** | 4.82 | 8.29 |
| `GO_GO_WM_NO_SHM=1` | 0 | 510 | **5.85** | 5.47 | 9.44 |

Disabling MIT-SHM **removes all 1,024 round trips and makes things 15% slower.** If the round trips dominated, the opposite would happen. They do not dominate. MIT-SHM earns its cost even while paying two synchronous round trips per frame.

**The second measurement is more decisive still.** With `GO_GO_WM_NO_RESIZE_PAINT=1`:

```
relayout_ms_total:  2595 ms  ->  13.8 ms     (188x)
ms per relayout:    7.416    ->  0.040
```

Reconciliation minus paint is **0.13 ms of a 7.4 ms relayout — 1.8%**. Layout, the tree index, the map diffing, geometry request construction, divider synchronisation: all of it together is under two percent of the cost of a relayout.

So the honest assessment of my own Phase 1 work is that it optimized the 1.8%. It was correct, it removed real waste, and it is worth keeping — but it was never going to be felt.

### What I learned

**Where the time actually goes.** A `paintFrame` averages 4.98 ms for ~470 kilopixels. `draw.Fill` benchmarks at 32 GB/s, which would cover 470 kpx in about 0.06 ms. Fill is therefore **~1%** of a paint. Combined with the glyph cache result from Step 8, the accounting is:

```
draw.Fill              ~0.06 ms    ~1%
TitleStrip.Render      ~0.04 ms    ~1%   (after the glyph cache)
                       ---------
everything else        ~4.9  ms    ~98%   BGRA conversion + upload + X server
```

**This reverses the plan's ordering.** The design doc puts Phase 2 (capacity buffers, eliminating per-tick surface recreation) before Phase 4 (chrome/content split), on the theory that round trips are the top cost. The measurement says the top cost is *pixels* — conversion and upload volume. The chrome/content split makes decoration paint scale with a 22-pixel title strip instead of a 664-pixel pane, roughly a 30× reduction in pixels touched. **Phase 4 should now come first.**

**Suppressing paint defers it rather than removing it.** Under `NO_RESIZE_PAINT`, `resize_paint_suppressed` reached 506 — yet `frames_painted` was still 506 and `paint_ms_total` was unchanged at 2601 ms. The paints simply moved to the `Expose` handler: skipping the paint leaves the window's background pixmap stale at the new size, so the server generates Expose and the frame repaints anyway. The flag still did its job as a *measurement* — `relayout_ms_total` is the number that matters — but as a design direction, "just don't paint during the drag" does not work without also solving what the window shows in the meantime. That is precisely the argument for retained content plus a preview, not for a paint skip.

### What was tricky to build

The environment differs from the deployment target in a way that must be stated with the results, or they will mislead:

- **Xephyr reports `shared_pixmaps: True`; the user's real Xorg reports `False`.** So this harness exercises the shm path, while the actual machine runs the PutImage fallback. The `noshm` condition is therefore the one that represents the live deployment, and it is the *slower* of the two.
- **Xephyr is a nested server**, so absolute timings are inflated by the parent server's own work. Corroboration: the live Xorg log recorded `paintFrame` at 3.1–3.5 ms against Xephyr's 4.8 ms p50 — the same order of magnitude, so the inflation is modest and the ratios are trustworthy.

Getting `ggwm-xephyr-validate.sh` to fail loudly rather than hang was the other piece of care: it polls for the WM process as well as the socket, so a WM that dies during startup reports its own last log lines instead of spinning for eight seconds and then claiming the socket never appeared.

### What warrants a second pair of eyes

- **The shm-versus-PutImage comparison should be repeated on bare Xorg**, where `shared_pixmaps` is false and the comparison cannot actually be made. That asymmetry means the live machine has no shm path to fall back *from*, so the 15% is not available to it at all.
- **The 4.98 ms/paint figure is Xephyr's**, not bare metal's. Conclusions about *proportions* are safe; the absolute budget is not.
- Whether `divider_painted = 181` is irreducible. It tracks legitimate mode changes as the pointer crosses snap zones (`dividerDragFeedback` toggles between dragging and snapped), so most of those paints are real appearance changes — but a cheaper representation for that state flash may exist.

### What should be done in the future

1. **Reorder the plan: Phase 4 before Phase 2.** Pixels dominate; round trips do not.
2. **Instrument `ConvertRows` and the upload separately.** They are ~98% of a paint and are still a single undifferentiated block. This is now the highest-value instrumentation left.
3. Re-run this harness after every subsequent change — it takes about ninety seconds and produces directly comparable counters.
4. Retire the VT harness (`ggwm-shm-ab.sh`) in favour of the Xephyr one, or fix it as a bare-metal cross-check for the cases where nesting distorts results.

### Code review instructions

- **Reproduce:**
  ```bash
  S=ttmp/2026/07/21/GGWM-012-GUIDES--*/scripts/ggwm-xephyr-validate.sh
  $S phase1                              # default
  GO_GO_WM_NO_RESIZE_PAINT=1 $S nopaint  # isolates paint from reconciliation
  GO_GO_WM_NO_SHM=1          $S noshm    # the live machine's actual path
  ```
  Results land in `~/ggwm-xephyr/<label>.{perf.json,jsonl,harness.log}`.
- **The two numbers that carry the argument** are `relayout_ms_total` under `nopaint` (13.8 ms vs 2595 ms) and `ms/paint` for shm vs no-shm (4.98 vs 5.85).

### Technical details

Full counter sets, one scripted drag of three sweeps each (644 motion events):

```
                        default    nopaint     noshm
relayouts                   350        347       345
layout_calls                351        348       346   (~1 per tick: split-rect cache)
frames_resized              512        506       510
frames_painted              512        506       510
map_requests_skipped        700        694       690
divider_painted             181        178       176
divider_paint_skipped       350        347       345
shm_creates / destroys      512        506         0
ximg_creates / destroys       0          0       510
paint_megapixels          240.7      237.9     239.8
motion_events               644        644       644
motion_admitted             350        347       345
motion_coalesced            294        297       299
resize_paint_suppressed       0        506         0
paint_ms_total             2550       2601      2984
relayout_ms_total          2595       13.8      3031
shared_pixmaps             True       True      False
```

## Step 10: Decomposing the paint — and correcting Step 9's correction

Step 9 concluded that the round-trip hypothesis was refuted, on the strength of an A/B: disabling MIT-SHM removes all 1,024 checked round trips and is 15% slower, therefore the round trips are not the bottleneck. That inference is wrong, and this step shows why. Swapping MIT-SHM for the PutImage fallback does not remove one component from a fixed system; it exchanges one bundle of components for a different bundle. The comparison said which of two implementations is faster. It said nothing about which component inside either one is expensive.

Decomposing the paint directly — compose, surface management, conversion, transfer — gives the answer that neither the code reading nor the A/B could.

### Prompt Context

**User prompt (verbatim):** "do as much work as possible, keeping a detailed diary, committing at appropriate intervals (see skills), until you need me."

**Assistant interpretation:** Continue autonomously: finish the measurement decomposition, act on it, and keep the ticket current.

**Inferred user intent:** Maximum progress without round-tripping through the console.

### What I did

- Added three timing spans inside `paintFrame`: `compose` (fill, title, border into the RGBA scratch), `surface` (surface destroy/create — the checked round trips on the shm path, `xgraphics.New` + `XSurfaceSet` on the fallback), and `convert` (RGBA to BGRA only). Transfer is the remainder.
- Ran the Xephyr harness against both upload paths.

### What the decomposition shows

Per paint, averaged over ~520 paints per run:

| Component | MIT-SHM path | share | PutImage fallback | share |
|---|---:|---:|---:|---:|
| compose (fill + title + border) | 0.85 ms | 16% | 0.74 ms | 11% |
| **surface management** | **2.99 ms** | **56%** | **1.75 ms** | **26%** |
| convert (RGBA→BGRA) | 1.44 ms | 27% | 0.93 ms | 14% |
| **transfer** (PutImage) | ~0.03 ms | ~1% | **3.22 ms** | **49%** |
| **total `paintFrame`** | **5.31 ms** | | **6.63 ms** | |

Two things follow immediately.

**Surface management dominates the shm path at 56%.** That is exactly the cost the original Tier-1 hypothesis named: the surface is destroyed and recreated because the pane's dimensions changed, and `xshm.New` pays two checked round trips to do it. The hypothesis was right about the shm path all along.

**Step 9's refutation was an inference error, not a measurement error.** The measurement in Step 9 is correct — no-shm really is 15% slower. What was wrong was concluding from it that surface recreation is cheap. It is not cheap; the alternative is simply more expensive still. Disabling shm trades 2.99 ms of surface management for 3.22 ms of PutImage transfer, and pays 1.75 ms of `ximg` recreation on top. Both paths are dominated by work that exists *only because the dimensions changed on this tick*.

### What I learned

**An A/B between two implementations does not isolate a component.** It swaps a bundle. To attribute cost to a component you must measure the component, not toggle the strategy that contains it. This is the second time in this ticket that a plausible inference survived until someone measured one level deeper — the first being the original code-read hypothesis. The pattern is the same: each level of evidence refutes the level above it, and only direct decomposition terminates the regress.

**Both paths share the same underlying defect.** Surface management is 26–56% depending on path, and it is incurred *per size change*. A divider drag changes dimensions on every tick, so it is incurred per tick. Nothing about that is specific to MIT-SHM; the fallback pays it too, as `ximg` recreation plus `XSurfaceSet`.

**The two remaining costs scale with pane pixels.** Conversion (14–27%) and transfer (1–49%) are both proportional to the number of pixels in the pane. That is what the chrome/content split addresses, by making decoration repaints touch a 1272×22 title strip instead of a 1272×664 pane.

### The revised plan, with both phases justified

The two big changes are now separately motivated by measured numbers rather than competing for the same justification:

1. **Capacity buffers (Phase 2)** eliminate surface recreation on size change. That is **56% of a paint on shm, 26% on the fallback**, and it applies to both. This is the larger, more contained win, and it is the one to do first.
2. **Chrome/content split (Phase 4)** reduces convert plus transfer, which is **28% on shm and 63% on the fallback**, by roughly 30× for decoration repaints.

Step 9 argued for reordering Phase 4 ahead of Phase 2. That was based on the mistaken belief that surface management was cheap. **The original ordering — Phase 2 first — is correct after all**, and now for a measured reason rather than an inferred one.

### What was tricky to build

Attributing the remainder. `upload` minus `surface` minus `convert` is the transfer, but only if nothing else hides in that span. On the shm path the remainder is `ClearAll`, which measures at ~0.03 ms and confirms that the shm transfer really is nearly free — the server composites from memory both sides already share. On the fallback the remainder is `XDraw` plus `XPaint`, i.e. PutImage pushing the whole surface through the socket, at 3.22 ms. Those two numbers are the clearest statement of what MIT-SHM buys and what it costs.

The other care point: `surfaceNanos` is accumulated on both paths but means slightly different things — shm destroy/create versus `xgraphics.New` plus `XSurfaceSet`. Both are "resource work incurred because the size changed", which is the quantity of interest, but the label is doing some work and the diary should say so.

### What warrants a second pair of eyes

- **The capacity-buffer clipping question is still unresolved** and is the main risk in Phase 2. A shared pixmap has fixed dimensions. Setting an oversized pixmap as a window's background should display its top-left region, because background pixmaps tile from the origin and the window clips — but "should" is doing real work in that sentence and it must be verified with a pixel test before the design depends on it.
- Whether `compose` at 0.85 ms is mostly the per-paint `image.NewRGBA` allocation. A 636×664 pane buffer is ~1.7 MB, reallocated every tick because the size changed; 520 of those is ~880 MB of allocation per drag. Capacity buffers would remove this too, which would make Phase 2 worth even more than the surface number alone suggests.

### What should be done in the future

- Implement capacity buffers next, covering `f.img`, `f.surf`, and `f.ximg` together, since all three are recreated by the same size-change condition.
- Re-run the harness after that change; `surface_ms_total` should approach zero during a steady drag and `shm_creates` should fall from ~520 to a handful.

### Code review instructions

- **Reproduce:**
  ```bash
  S=ttmp/2026/07/21/GGWM-012-GUIDES--*/scripts/ggwm-xephyr-validate.sh
  $S split2                       # MIT-SHM path
  GO_GO_WM_NO_SHM=1 $S split3noshm  # PutImage fallback
  ```
- **The numbers that carry the argument** are `surface_ms_total` (1578 ms of 2802 ms on shm; 912 ms of 3462 ms on the fallback) and the derived transfer remainder.

### Technical details

```
                     shm      fallback
frames_painted        528          522
paint_ms_total       2802         3462
compose_ms_total      450          386
upload_ms_total      2352         3075
  surface_ms_total   1578          912
  convert_ms_total    759          483
  transfer (derived)   15         1680
shm_creates           528            0
ximg_creates            0          522
```

## Step 11: Capacity buffers — a 2.35x win, and a bug the measurement caught

Step 10 identified surface management as 56% of a paint on the MIT-SHM path: resource work incurred solely because the pane's dimensions changed on this tick, which during a divider drag means every tick. This step implements the fix, and it produced the largest end-to-end improvement of the ticket. It also introduced a regression that the counters caught immediately and that no test would have.

### Prompt Context

**User prompt (verbatim):** (see Step 10 — "do as much work as possible…")

**Follow-up user prompt (verbatim):** "save the screenshots in the ticket as well so we can create a nice illustrated report later on and also see what you were dealing with"

**Assistant interpretation:** Capture screenshots of the running WM into the ticket so the report can be illustrated and rendering can be inspected rather than inferred.

### What I did

- Added `bucketSize` — pane dimensions rounded up to a 64-pixel granularity — and sized `f.img`, `f.surf` and `f.ximg` to the bucket rather than to the exact pane.
- Changed composition to draw into an explicit viewport rectangle instead of `img.Bounds()`, so the surplus capacity is never touched.
- Added screenshot capture to the Xephyr harness at three points (before drag, mid drag, after release), written into `images/` in the ticket.

### Why bucketing works at all

The window is now smaller than its backing store, and the backing store is installed as the window's background pixmap. X tiles a background pixmap from the window origin, so an oversized pixmap displays its top-left region and the surplus is clipped away. This was flagged in the design doc as an assumption requiring verification before anything depended on it, so it was verified by screenshot rather than by reasoning — see below.

Bucketing at 64 pixels keeps surplus memory under about 10% for ordinary panes while turning roughly 350 resource recreations per drag into about 10.

### What worked

MIT-SHM path, before and after:

| | before | after | change |
|---|---:|---:|---|
| `shm_creates` | 528 | 124 | **4.3x fewer** |
| surface ms/paint | 2.99 | 0.69 | **4.3x** |
| compose ms/paint | 0.85 | 0.50 | 1.7x |
| **ms per paint** | **5.31** | **2.26** | **2.35x** |
| **relayout_ms_total** | **2852** | **1155** | **2.47x** |

The compose improvement was not the target and is worth noting: `image.NewRGBA` for a 636×664 pane is about 1.7 MB, and it was being reallocated on every tick because the size changed. Roughly 880 MB of allocation per drag is now roughly 200 MB.

**The rendering is correct.** The screenshots confirm it directly rather than by inference: `capbuf2-3-after-release.png` shows two correctly sized panes with intact title strips, borders, divider and bars; `capbuf2-2-mid-drag.png` shows the divider in its snapped (mustard) state with the status bar reading `RESIZING`, no stale pixels and no artifacts from the oversized pixmap.

### What didn't work

**The first run of capacity buffers tripled the paint count.** `frames_painted` jumped from 528 to 1340 while `frames_resized` stayed at 496, so roughly 844 paints were arriving from somewhere other than reconciliation.

The cause was in the Expose fast path (`events.go`), which decides whether a frame's buffers are current:

```go
case f.surf != nil && f.surf.W == f.rect.W && f.surf.H == f.rect.H:
    // server-side repair; no client work
```

That compares the surface to the **viewport**. Once surfaces became capacity-sized the comparison could never succeed, so every Expose fell through to `default:` and ran a full `paintFrame` — precisely the cost that fast path exists to avoid, and which a comment records as having been ~27% of the profile when it was first fixed under GGWM-005. Comparing against the bucket instead restored it: `frames_painted` is now 492, exactly equal to `frames_resized`.

**No test would have caught this.** It is not a correctness bug — the screen looked right throughout, and every one of the 15 packages passed. It was visible only as a counter that did not match its sibling. That is a direct argument for the counters being part of the product rather than scaffolding.

**Capacity buffers help the fallback path much less.** On the PutImage path:

| | before | after |
|---|---:|---:|
| `ximg_creates` | 522 | 124 |
| surface ms/paint | 1.75 | 0.42 |
| transfer ms/paint | 3.22 | **4.09** |
| ms per paint | 6.63 | 5.84 |

Surface work fell by the expected factor, but **transfer got worse**, because the fallback pushes the whole backing store through `PutImage` and the backing store is now bucket-sized. Net improvement is 12% rather than 135%. The two paths respond differently because they are billed differently: shm pays per resource creation and its transfer is nearly free, while the fallback pays per pixel transferred.

This matters for this project specifically, because the development machine's Xorg reports no shared-pixmap support and therefore runs the fallback.

### What I learned

**A change can be a large win and a small win simultaneously, depending on which resource the path is billed for.** Capacity sizing removes creations; it does not remove pixels, and it slightly increases them. Any optimization that trades one resource for another needs measuring on every path that has a different price list.

The fix for the fallback is to transfer only the viewport rather than the whole capacity buffer — `SubImage` over the viewport rectangle before `XDraw`. That should make the fallback strictly better instead of a trade.

### What was tricky to build

The viewport/capacity split has to be applied consistently or it produces subtle wrongness rather than obvious breakage. Composition draws into `view`; the buffers are sized to `capW, capH`; the currency checks compare against `capW, capH`; and `paintBuiltin` already worked in `f.rect` terms so it needed no change. Missing any one of those produces either surplus work (filling the whole capacity) or a permanently-stale cache (the Expose bug). The Expose case is the instructive one because it failed silently in the direction of doing *more* work, which is invisible without instrumentation.

### What warrants a second pair of eyes

- **Bucket granularity is unmeasured.** 64 pixels was chosen to keep surplus under ~10%; 32 or 128 might be better on either axis. The counters make this cheap to sweep.
- **Memory ceiling.** Buffers now round up and are never shrunk except by `dropBuffers` on workspace hide or unmanage. A pane that is briefly large leaves a large buffer behind until it is hidden. A compaction policy for surfaces that stay small for some period is worth considering.
- **The Expose currency check now duplicates the bucketing rule.** If `bucketSize` changes, both call sites must agree. Deriving the check from a single helper on `frame` would remove the coupling.

### What should be done in the future

1. Transfer only the viewport on the fallback path (next step).
2. Sweep bucket granularity with the harness.
3. Then reassess whether the chrome/content split is still the right next major change — its value is reducing pixels, which is now the dominant remaining cost on both paths.

### Code review instructions

- **Start at** `pkg/wmx11/manage.go:paintFrame` for the viewport/capacity split and `bucketSize` at the end of the file, then `pkg/wmx11/events.go` for the currency check that must agree with it.
- **Verify rendering** by looking at the committed screenshots in `images/`, not by reasoning about background-pixmap tiling.
- **Reproduce:**
  ```bash
  S=ttmp/2026/07/21/GGWM-012-GUIDES--*/scripts/ggwm-xephyr-validate.sh
  $S capbuf2                        # shm path
  GO_GO_WM_NO_SHM=1 $S capbuf-noshm # fallback path
  ```
  Expect `frames_painted == frames_resized` (no Expose re-render) and `shm_creates` around 124 rather than ~520.

### Technical details

```
MIT-SHM path            before   after
frames_painted             528     492
frames_resized             528     492   (equal after the Expose fix)
shm_creates                528     124
paint_ms_total            2802    1111
  compose_ms_total         450     246
  surface_ms_total        1578     341
  convert_ms_total         759     511
relayout_ms_total         2852    1155

PutImage fallback       before   after
ximg_creates               522     124
paint_ms_total            3462    3094
  surface_ms_total         912     224
  convert_ms_total         483     459
  transfer (derived)      1680    2167   <-- regression: capacity-sized PutImage
relayout_ms_total         3518    3144
```

## Step 12: Uploading only what is visible — 2.9x on both paths

Step 11 left the two upload paths with different bottlenecks and different verdicts: MIT-SHM 2.35x faster, the PutImage fallback essentially unchanged. This step closes that gap with an observation that the decomposition made obvious and that neither the source reading nor the review documents had drawn: **for a frame holding a reparented client, almost everything the window manager converts and transfers is covered by the client window and never appears on screen.**

Acting on it brings both paths to roughly 2.9x faster than baseline, and it does so without creating a single new X window — which is the change the design document proposes as Phase 4 and describes as the highest-risk item in the plan.

### Prompt Context

**User prompt (verbatim):** (see Step 10 — "do as much work as possible…")

### The observation

A frame is a window the WM owns; the client is reparented inside it at `(BorderW, TitleH)`. The WM renders a title strip, a border, and — for builtin or script tiles only — content. When there is a client, the client window occupies everything except the strip and a 2-pixel border.

For a 636×664 pane that is about 20,000 visible WM pixels out of 422,000. Every paint was converting and uploading all 422,000.

```
+-- frame 636x664 -----------------------+
| title strip  636 x 22      <- visible  |
+----------------------------------------+
|B|                                    |B|   B = 2px border, visible
|o|   client window covers this        |o|
|r|   ~416,000 px, NEVER visible       |r|
|d|                                    |d|
+----------------------------------------+
| bottom border 636 x 2      <- visible  |
+----------------------------------------+
```

### What I did

- Added `WM.chromeRects(f)`, returning the four rectangles the WM actually shows for a client frame, and `nil` for builtin/script tiles whose whole surface is visible.
- Fallback path: `XDraw` each chrome sub-image, then `XPaintRects` for those rectangles instead of `XPaint` over the window.
- MIT-SHM path: added `Surface.WriteRGBARect` and converted only the chrome rows, then `ClearArea` per rectangle instead of `ClearAll`.
- Re-enabled capacity bucketing on both paths, since transfer volume no longer depends on the backing store's size.

### What worked

Per paint, against the Step 9 baseline:

| | baseline | after Step 11 | **after Step 12** | total |
|---|---:|---:|---:|---:|
| MIT-SHM | 5.31 ms | 2.29 ms | **1.82 ms** | **2.92x** |
| PutImage fallback | 6.63 ms | 6.09 ms | **2.27 ms** | **2.92x** |

WM-loop work for one scripted drag (`relayout_ms_total`):

| | baseline | **after** | |
|---|---:|---:|---:|
| MIT-SHM | 2852 ms | **944 ms** | **3.02x** |
| fallback | 3518 ms | **1150 ms** | **3.06x** |

The transfer collapse on the fallback is the headline component: **3.22 ms/paint to 0.38 ms — 8.5x.** That path is the one the development machine actually runs, since its Xorg reports no shared-pixmap support.

The cost profile is now flat. Nothing dominates:

```
                shm     fallback
compose        0.52 ms   0.48 ms
surface        0.65 ms   0.48 ms
convert        0.61 ms   0.85 ms
transfer       0.04 ms   0.45 ms
total          1.82 ms   2.27 ms
```

**Rendering verified by screenshot on both paths**, mid-drag and after release: title strips, borders, the snapped divider, and both bars all intact, with no artifacts from the uploaded region being smaller than the surface.

### What didn't work

The same sub-image technique, applied to the *whole viewport* in Step 11, was measurably worse: 5.79 ms/paint against 4.09. `xgraphics`' `xdraw` allocates a contiguous copy of the region on every call, and for a full viewport that copy costs more than the ~7% capacity surplus it avoids.

The technique is right; the region was wrong. For roughly 22 rows the copy is trivial and the saved transfer is enormous. **A mechanism that loses at one scale can win decisively at another**, which is an argument for measuring the specific application rather than forming a general opinion about the mechanism. The rejected variant is recorded in a comment at the call site so it is not retried on the strength of the general principle.

### What I learned

**This is Phase 4's benefit without Phase 4's risk.** The design document proposes splitting chrome from content by giving each frame a title child window, and identifies it as the highest-risk change in the plan because it rewrites frame lifecycle. The actual saving it targets is "stop touching pixels the client covers". That saving is available from the upload path alone, because the client already covers those pixels — the window hierarchy did not need to change to make them uninteresting, only the upload had to stop pretending they were visible.

The structural split still buys things this does not: composition and conversion still run over the full pane (`compose` 0.5 ms, `convert` 0.6–0.85 ms), and a title child window would make those scale with the strip too. But the largest single component is now gone, and the remaining case for Phase 4 is considerably weaker than it was two hours ago.

**The decomposition earned this.** The observation is available to anyone reading `paintFrame`, and three review documents plus my own analysis missed it. What surfaced it was having `transfer` as a separate number and watching it dominate one path and not the other.

### What was tricky to build

Correctness rests on an invariant that is true but nowhere enforced: the client window covers exactly the frame interior. It is established in `manage` (reparent at `(0, TitleH)`, configure to `W-2*BorderW × H-TitleH-BorderW`) and maintained by `relayoutPaint`. If a future change moved the client or left a gap, the uncovered region would show stale pixels, and no test would fail — the screenshots would show it, which is one more reason they are now committed to the ticket.

The two paths also need different repair calls: `XPaintRects` for the fallback, explicit `ClearArea` per rectangle for shm. `ClearAll` would repair the whole window from a background pixmap whose interior was never converted, which is exactly the failure this invariant protects against.

### What warrants a second pair of eyes

- **The covering invariant above.** It deserves an assertion in development builds, or a comment at `manage`'s reparent call pointing at `chromeRects`.
- **Fullscreen and float frames.** Floats reparent at `(BorderW, TitleH)` and are also fully covered, so `chromeRects` applies; a fullscreen frame is handled by `fullscreenState` and skipped by `relayoutPaint`, so it should be unaffected. Neither was exercised by the harness, which only drives divider drags.
- **Builtin and script tiles are deliberately excluded** (`f.client == 0` returns nil) and still pay a full-surface upload. They are also the surfaces that genuinely need one.

### What should be done in the future

1. Extend the harness to cover floats, fullscreen transitions and workspace switches — the paths this change touches but does not exercise.
2. Reconsider Phase 4's priority. Its remaining value is making compose and convert scale with the strip; that is ~1.1–1.3 ms of a ~2 ms paint, no longer the 60%+ it looked like.
3. Sweep the bucket granularity now that transfer no longer depends on it.

### Code review instructions

- **Start at** `WM.chromeRects` in `pkg/wmx11/manage.go` and its two call sites — the shm branch and the fallback branch of `paintFrame`.
- **Check the invariant**, not the arithmetic: confirm the client window truly covers everything `chromeRects` omits.
- **Verify rendering** from `images/final2-shm-*.png` and `images/chrome-noshm-*.png`.
- **Reproduce:**
  ```bash
  S=ttmp/2026/07/21/GGWM-012-GUIDES--*/scripts/ggwm-xephyr-validate.sh
  $S final2-shm ; GO_GO_WM_NO_SHM=1 $S final2-noshm
  ```

### Technical details

```
                     baseline    after Step 12
MIT-SHM
  compose_ms_total       450          256
  surface_ms_total      1578          325
  convert_ms_total       759          303
  transfer (derived)      15           23
  paint_ms_total        2802          905
  relayout_ms_total     2852          944

PutImage fallback
  compose_ms_total       386          235
  surface_ms_total       912          237
  convert_ms_total       483          413
  transfer (derived)    1680          222
  paint_ms_total        3462         1108
  relayout_ms_total     3518         1150
```

## Step 13: Validating the covering invariant, and filling only the chrome

Step 12's chrome-only upload rests on an invariant that nothing enforces: a reparented client covers exactly the frame interior. The drag harness only exercises tiled divider resizes, so every other path that changes what covers a frame was untested — and a violation would not fail a test, it would show as stale pixels. This step builds a scenario sweep to look at those paths, then applies the same insight to composition.

### Prompt Context

**User prompt (verbatim):** (see Step 10 — "do as much work as possible…")

### What I did

- Wrote `scripts/ggwm-xephyr-scenarios.sh`, which drives six scenario families in a nested server and screenshots each: two tiled clients, focus changes, fullscreen enter/exit, float lift/sink, workspace switch away and back, and a theme swap in both directions.
- Inspected the resulting images.
- Applied `chromeRects` to the fill as well as the upload: for a client frame only the chrome needs a background, since the interior is covered.

### What worked

**The invariant holds on every path exercised.** Specifically:

- **Builtin tiles are correctly excluded.** The launcher tile renders its full contents — every command row, the query field, the hint line — because `chromeRects` returns `nil` when `f.client == 0`. This is the case that would break most visibly if the exclusion were wrong, and it is intact.
- **Floats keep their chrome.** After `float`, the frame shows the float title style (close button only, no split/dock buttons) with borders intact and no stale interior.
- **Fullscreen enter and exit** leave no residue.
- **A theme swap repaints correctly**, including the builtin tile's full surface in the new palette. This was a specific concern: a theme swap changes the pane colour, and for a client frame the interior keeps the old colour — but it is covered, so it is never seen, and the screenshot confirms it.

Filling only the chrome reduced composition further:

| | compose ms/paint before | after |
|---|---:|---:|
| MIT-SHM | 0.52 | **0.39** |
| fallback | 0.48 | **0.27** |

Total paint is unchanged within run-to-run noise (1.85 ms shm, 2.30 ms fallback), which is expected: composition was already the smallest component after Step 12.

### What didn't work

Two scenarios did not exercise what they were meant to, and the harness reported it honestly rather than silently passing:

- **Focus scenario produced empty leaf ids** (`-- scenario 2: focus  then`). The tree-walking helper assumes a JSON shape for the serialized desktop that does not match what `{"q":"tree"}` returns, so it extracted nothing and the focus calls were no-ops.
- **Workspace switch-back did not happen.** The screenshot after `switch-workspace ws-1` still shows `ws-2` highlighted, so either the workspace id is not `ws-1` or the op needs a different field.

Both are defects in the harness, not in the window manager, but they mean two of the six scenario families are currently unvalidated. The screenshots are still useful — they show the workspace-2 state rendering correctly — but the transitions themselves were not driven.

### What I learned

**A screenshot sweep is a different instrument from a counter sweep, and this change needed both.** The counters proved the optimization worked; only the images could prove it did not break rendering, because every failure mode here is visual and silent. Committing the images into the ticket means a future reviewer can check the claim rather than trust it.

**Excluding builtin tiles was the load-bearing detail.** They have no client covering them, so their whole surface is visible; had `chromeRects` not returned `nil` for them, the launcher, trace, listener and inspector tiles would have rendered as a title strip over garbage. That is the kind of case that a drag-only harness would never surface, since the drag test uses two xterms.

### What was tricky to build

Driving the window manager from outside required going through the IPC control plane rather than keybindings, and the IPC request shapes are not uniform: `focus` takes a `target`, `fullscreen` and `float` take nothing and act on the current focus, and workspace changes go through `op` with a `wmcore.Op` payload. Getting a leaf id at all requires walking the serialized desktop, and that is where the harness failed — the walker was written against an assumed schema instead of an observed one.

The correct approach would have been to dump one `{"q":"tree"}` response and read it before writing the walker. That is the same error as the "five hand-picked strings" check in Step 8: assuming a shape rather than observing it.

### What warrants a second pair of eyes

- **The two unvalidated scenarios.** Focus changes repaint two frames' chrome, and workspace switches change frame visibility — both interact directly with the covering invariant and with the `mapped` mirror added in Step 7. They should be driven properly before this is considered fully validated.
- **The gap between frame resize and client resize.** `relayoutPaint` configures the frame and then the client. Between those two requests the frame is larger than its client, so a strip of uncovered frame exists briefly. With chrome-only fill that strip now contains stale pixels rather than the pane colour. No screenshot caught it, but the harness samples at rest rather than during that window.

### What should be done in the future

1. Fix the harness's tree walker against the real serialization, then re-run the focus and workspace scenarios.
2. Consider whether the frame/client configure gap needs the interior filled after all — it may be cheaper to keep filling the pane background than to reason about that race.
3. Sweep bucket granularity now that transfer no longer depends on it.

### Code review instructions

- **Run the sweep:**
  ```bash
  ttmp/2026/07/21/GGWM-012-GUIDES--*/scripts/ggwm-xephyr-scenarios.sh scen
  ```
- **Look at** `images/scen-09-workspace-1-back.png` (builtin tile, full render), `scen-06-float-on.png` (float chrome), and `scen-10-theme-dark.png` (theme swap).
- The fill change is one branch at the top of `paintFrame`, guarded by the same `chromeRects` call as the upload.

### Step 13b: harness defect fixed, remaining scenarios validated

The two scenario families Step 13 reported as undriven are now driven. The cause was not the schema after all: `ggwm-xephyr-scenarios.sh`'s `ipc` helper truncated every response to 200 characters (`[:200]`), so `leaf_of` received a clipped JSON document, failed to parse, and returned an empty string. The tree schema I had assumed — `workspaces[].root`, `node.kind` as the string `"leaf"`, children under `a`/`b` — was correct all along, as `pkg/wmcore/desktop.go:10-18` and `tree.go:45-55` confirm.

Truncation was added for readable logging and silently broke the only consumer that parsed the output. Moved to the call sites that echo, leaving the helper's output intact.

Also replaced the assumed workspace identifiers with ones read from the running system: they are `ws1` and `ws2`, not `ws-1` and `ws-2`. The display names in the bar are `ws-1`/`ws-2`, which is where the wrong guess came from.

**Both families now validate clean:**

- **Focus** (`{"ok":true,"data":"n2"}`) drives and the chrome repaints correctly on both frames.
- **Workspace switch away and back** restores both clients with correct chrome and no stale pixels (`scen2-09-workspace-1-back.png`). This exercises the `frame.mapped` mirror added in Step 7 across exactly the transition flagged there as needing review — workspace switches intentionally unmap frames, and a missed transition would leave windows invisible. It does not.

That closes the review item from Step 7 and the two open families from Step 13.

## Step 14: Bucket granularity sweep — and why bigger is now free

The 64-pixel bucket in Step 11 was a guess chosen to keep surplus memory under about 10%. With the counters in place and the harness taking ninety seconds per run, guessing is unnecessary. This step sweeps it, and the result is more one-sided than expected because Step 12 changed what a bucket costs.

### Prompt Context

**User prompt (verbatim):** (see Step 10 — "do as much work as possible…")

### The sweep

`sizeBucket` made overridable via `GO_GO_WM_SIZE_BUCKET`, then one harness run per value on the MIT-SHM path:

| bucket | shm creates | ms/paint | relayout ms |
|---:|---:|---:|---:|
| 16 | 375 | 4.38 | 2362 |
| 32 | 237 | 2.94 | 1496 |
| 64 | 124 | 1.85 | 969 |
| **128** | **64** | **1.08** | **578** |
| 256 | 28 | 0.74 | 414 |

Monotonic, and steeply so: 16 to 256 is a 5.9x difference in per-paint cost.

### Why it is monotonic

When bucketing was introduced in Step 11 it was a genuine trade — a larger backing store meant a larger `PutImage`, which is why it had to be disabled on the fallback path. Step 12 removed that coupling. Composition fills only the chrome rectangles, conversion covers only the chrome rows, and the upload transfers only the chrome. **None of the three scales with the backing store any more.** The only remaining per-tick cost that bucketing removes is resource recreation, and the only cost it adds is surplus memory.

So the sweep has no interior optimum in time. It is bounded by memory alone, and the choice becomes a straightforward statement about how much surplus is acceptable.

Surplus for a 636×664 pane:

| bucket | backing store | surplus |
|---:|---|---:|
| 64 | 640×704 | +6.5% |
| 128 | 640×768 | +16% |
| 256 | 768×768 | +40% |

At larger pane sizes the relative surplus shrinks — a 956×1024 pane rounds to 1024×1024 at both 128 and 256, about +7%.

**128 is the new default.** It captures most of the win while keeping surplus under ~16% for typical panes; 256 is faster still but wastes up to 255 pixels per axis, which is poor behaviour for small panes and for the many-frames case. The sweep table is recorded at the declaration so the choice can be revisited with data rather than reargued.

### Cumulative result

Against the Step 9 baseline, one scripted drag of three sweeps:

| | baseline | now | |
|---|---:|---:|---:|
| **MIT-SHM** ms/paint | 5.31 | **1.11** | **4.8x** |
| **MIT-SHM** relayout total | 2852 ms | **595 ms** | **4.8x** |
| **fallback** ms/paint | 6.63 | **1.89** | **3.5x** |
| **fallback** relayout total | 3518 ms | **963 ms** | **3.7x** |
| shm surface creations per drag | 528 | **64** | 8.3x |

Rendering verified by screenshot on both paths after the change.

### What I learned

**An optimization can change another optimization's cost structure, and the second one then needs re-deciding.** Bucketing was tuned conservatively, and correctly so, when its downside was transfer volume. Once chrome-only upload removed that downside, the conservative default was leaving a factor of 1.7 on the table for no reason. Nothing about the bucketing code changed — only the surrounding costs did.

This is an argument for keeping tuning constants cheap to sweep rather than reasoning about them once and freezing the conclusion. `GO_GO_WM_SIZE_BUCKET` costs eight lines and turns a debate into a table.

### What warrants a second pair of eyes

- **Memory ceiling under many frames.** Buffers round up and are freed only by `dropBuffers` on workspace hide or unmanage. Nine workspaces of tiles each holding a bucket-rounded buffer is the case to check; the counters do not currently report resident buffer bytes, and they should.
- **Whether 256 is actually the better default** for single-monitor use where frame counts are low. The data says it is 1.46x faster than 128; the argument against is worst-case waste, not measured cost.

### What should be done in the future

- Add a `surface_resource_bytes` counter so the memory side of this trade is measured rather than estimated.
- Re-run the sweep after any change to what scales with the backing store.

### Code review instructions

- The constant and its sweep table are at `sizeBucket` in `pkg/wmx11/manage.go`.
- **Reproduce the sweep:**
  ```bash
  S=ttmp/2026/07/21/GGWM-012-GUIDES--*/scripts/ggwm-xephyr-validate.sh
  for b in 16 32 64 128 256; do GO_GO_WM_SIZE_BUCKET=$b $S "bucket$b"; done
  ```
