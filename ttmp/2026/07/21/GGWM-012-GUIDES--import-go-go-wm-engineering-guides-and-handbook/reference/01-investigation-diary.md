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
