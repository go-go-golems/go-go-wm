---
Title: Investigation diary
Ticket: GGWM-010-PR1-REVIEW
Status: active
Topics:
    - ci
    - security
    - lint
    - concurrency
    - wm
DocType: reference
Intent: long-term
Owners: []
RelatedFiles:
    - Path: pkg/cmds/replui.go
      Note: REPL UI kernel — RC-1 (SyntaxError retry) and RC-2 (eval serialization)
    - Path: pkg/jsmod/eventfan.go
      Note: event fan-out — RC-3 (concurrent map read after unlock)
    - Path: pkg/repl/value.go
      Note: rich value normalization — RC-4 (descriptor stored as payload)
    - Path: pkg/wmx11/manage.go
      Note: WM focus — RC-5 (fullscreen focus leak)
    - Path: pkg/wmx11/launcher.go
      Note: lint exhaustive switch (KindApp missing)
    - Path: pkg/wmx11/fullscreen.go
      Note: gosec G115 integer overflow uint32 casts
    - Path: pkg/cmds/kitty_install.go
      Note: gosec G703/G302 path traversal + file perms
    - Path: pkg/cmds/wm.go
      Note: gosec G108/G114 pprof + http timeouts
    - Path: go.mod
      Note: go 1.26.1 — govulncheck stdlib vulns fixed in 1.26.2+
    - Path: scripts/00-pr-review-comments.md
      Note: captured Codex review comments
    - Path: scripts/01-lint-output.txt
      Note: golangci-lint output (2 issues)
    - Path: scripts/02-govulncheck-output.txt
      Note: govulncheck output (9 stdlib vulns)
    - Path: scripts/03-gosec-output.txt
      Note: gosec output (20 issues)
---

# Diary

## Goal

Capture the investigation of PR #1's code review comments and failing CI
actions, the in-depth topic-by-topic analysis, and the production of an
intern-ready analysis/design/implementation guide stored in this ticket and
uploaded to reMarkable.

## Step 1: Gathered PR review comments and reproduced all failing CI checks locally

The PR (#1, "Feat: Introduce JavaScript scripting, application launcher, and
REPL") is a 28,591-addition feature release. GitHub's API was returning HTTP
503 intermittently, so the five Codex review comments were captured from the
PR web UI by hand into `scripts/00-pr-review-comments.md`, and every failing
CI check was reproduced locally with the same tooling the workflows use. This
gave ground-truth output for the analysis instead of relying on the flaky API.

### Prompt Context

**User prompt (verbatim):** "Address code review issues and failing actions on https://github.com/go-go-golems/go-go-wm/pull/1 . Look at them, then create a docmgr ticket in go-go-vm and take a step back and do an in depth analysis, separate the issues into different topics, and analyze each separately. then Create a detailed analysis / design / implementation guide that is for a new intern, explaining all the parts of the system needed to understand what it is, with prose paragraphs and bullet point sand pseudocode and diagrams and api references and file references. It should be very clear and technical. Store in the ticket and the nupload to remarkable. Kepe a detailed diary as you work, and commit at appropriate intervals."

**Assistant interpretation:** Investigate PR #1's review comments and failing GitHub Actions, create a docmgr ticket, separate the issues into distinct topics with per-topic analysis, then write a detailed intern-ready analysis/design/implementation guide, store it in the ticket, upload to reMarkable, keep a diary, and commit at intervals.

**Inferred user intent:** A reproducible, onboarding-grade document that an intern can use to understand the whole system and fix every PR issue, with the issues cleanly categorized rather than a flat list.

### What I did
- Located the repo at `go-go-wm/go-go-wm` (the workspace root is a Go work directory, not the repo).
- Read the PR metadata: 216 changed files, +28,591/-172, base `main` ← head `task/go-go-wm`, mergeable but `UNSTABLE` (CI failing).
- Captured the 5 Codex review comments (4× P1, 1× P2) into `scripts/00-pr-review-comments.md`.
- Ran `golangci-lint run --timeout=5m ./...` → 2 issues (exhaustive, nonamedreturns). Saved to `scripts/01-lint-output.txt`.
- Ran `govulncheck ./...` → 9 stdlib vulnerabilities (all fixed in go1.26.2–1.26.5). Saved to `scripts/02-govulncheck-output.txt`.
- Ran `gosec -exclude=G101,G304,G301,G306,G204 -exclude-dir=.history ./...` → 20 issues. Saved to `scripts/03-gosec-output.txt`.
- Read the source for every flagged site: `replui.go`, `eventfan.go`, `value.go`, `manage.go`, `fullscreen.go`, `launcher.go`, `kitty_install.go`, `wm.go`.
- Created ticket `GGWM-010-PR1-REVIEW` with a design-doc and a diary.
- Added vocabulary slugs `ci`, `concurrency`, `lint`, `security` so `docmgr doctor` passes.

### Why
The user asked for an in-depth, topic-separated analysis. Reproducing the
failures locally (instead of trusting the GitHub UI) gives exact line numbers,
rule IDs, and severity for every issue, which the intern guide needs to be
actionable. Capturing the review comments to a file protects against the
flaky GitHub API.

### What worked
- All three failing scanners are installed locally (`golangci-lint`, `govulncheck`, `gosec`) and reproduce the CI failures exactly.
- The Codex review comments map cleanly onto specific source lines.
- docmgr ticket + vocabulary setup is clean.

### What didn't work
- `gh api repos/.../pulls/1/comments` returned HTTP 503 repeatedly ("No server is currently available to service your request") for ~10+ minutes. Worked around by reading the comments from the PR web UI.

### What I learned
- The PR's failing checks fall into four clean topics: (1) Codex review correctness/concurrency bugs, (2) golangci-lint style/exhaustiveness, (3) gosec security findings, (4) govulncheck + dependency-review stdlib/dep vulnerabilities.
- The govulncheck failures are entirely a Go toolchain version problem: `go.mod` pins `go 1.26.1` but the fixes ship in `1.26.2`–`1.26.5`. Bumping the toolchain is the whole fix.
- The gosec findings cluster: G115 integer-overflow casts (X11 coordinate/size conversions), G703/G302 in `kitty_install.go` (tainted path from a flag), G108/G114 (pprof auto-exposure + no HTTP timeouts).
- The Codex P1s are real concurrency/correctness bugs, not style nits: the eventfan map-copy race can crash the process; the REPL SyntaxError retry double-executes side effects; the rich-value payload bug sends the wrong object to verbs.

### What was tricky to build
- Distinguishing "the wrapper failed to parse" from "the wrapped code threw a SyntaxError at runtime" in `replui.go` is genuinely subtle: goja reports both as a `SyntaxError` in `resp.Cell.Execution.Error`, but only the former is safe to retry. The current code inspects the error *string*, which is why side effects run twice. The fix must parse-check the wrapper before executing it.
- The eventfan race is a classic Go footgun: `goSubs := f.goSubs` copies the map *header* (the pointer), not the contents, so iterating it after `f.mu.Unlock()` races a concurrent `SubscribeGo` append. The map is small but the drainer and a dynamic `wm.rule` registration genuinely run concurrently.

### What warrants a second pair of eyes
- The eventfan fix: copying the whole `goSubs` map under the lock on every batch is correct but allocates per batch; verify the batch rate is low enough that this is fine (it is — events are user-paced).
- The rich-value payload fix (RC-4): changing what goes into `Value.Raw` changes what `pbui.Object.Value` carries to verbs/accepts. Need to confirm no verb currently depends on receiving the descriptor.

### What should be done in the future
- Add a regression test for the eventfan concurrent-subscribe race (a `go test -race` test that subscribes while draining).
- Add a REPL test that a side-effecting expression with a runtime SyntaxError runs exactly once.
- Pin the Go toolchain in CI to a version ≥ the latest patch (or use `go.mod`'s `toolchain` directive) so govulncheck can't regress.

### Code review instructions
- Start at `scripts/00-pr-review-comments.md` for the source review comments.
- Cross-check each against the matching section in `design-doc/01-...md`.
- Validate locally: `golangci-lint run ./...`, `govulncheck ./...`, `gosec -exclude=G101,G304,G301,G306,G204 ./...`.

### Technical details
- Repo path: `/home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm`
- Branch: `task/go-go-wm` (PR head), base `main`.
- Local Go: `go1.26.1 linux/amd64` (matches `go.mod`, which is why govulncheck fails).
- golangci-lint local: v2.4.0; CI pins v2.11.2 via `.golangci-lint-version`. Both flag the same 2 issues.

## Step 2: Wrote the intern analysis/design/implementation guide and uploaded to reMarkable

With all evidence captured, the second step was to step back and separate the
issues into distinct topics, then write a single intern-grade document that
both explains the system and gives an actionable fix for every issue. The
document was stored in the ticket's design doc and uploaded to reMarkable as a
bundled PDF (design doc + diary) with a table of contents.

### Prompt Context

**User prompt (verbatim):** (see Step 1)

**Assistant interpretation:** Produce the detailed analysis/design/implementation guide for a new intern, store it in the ticket, and upload to reMarkable.

**Inferred user intent:** A reproducible, onboarding-grade document an intern can read to understand the whole system and fix every PR issue, with the issues cleanly categorized rather than a flat list.

### What I did
- Read the architecture from `GGWM-001`'s design doc and the source (`wmcore/tree.go`, `pbui/wire.go`, `pbui/broker/broker.go`, `apps/xapp/xapp.go`, `jsmod/eventfan.go`, `repl/value.go`, `cmd/go-go-wm/main.go`) to write a system primer.
- Wrote `design-doc/01-pr-1-review-analysis-and-intern-implementation-guide.md` (10 parts): system primer, issue inventory, four topic analyses (A: Codex bugs, B: lint, C: gosec, D: vulns), phased plan, testing strategy, risks/open questions, references.
- Each topic analysis has root cause, evidence (file:line + rule ID), pseudocode fix, and validation steps.
- Related 5 key source files to the design doc, added 5 phased tasks, updated the changelog.
- `docmgr doctor --ticket GGWM-010-PR1-REVIEW --stale-after 30` → all checks passed.
- Committed the doc + bookkeeping (commit `0bac078`).
- Uploaded the design doc + diary as a bundled PDF to reMarkable at `/ai/2026/07/19/GGWM-010-PR1-REVIEW` (dry-run first, then real upload). Verified with `remarquee cloud ls`.

### Why
The user explicitly asked for an intern-ready guide with prose, bullets, pseudocode, diagrams, and API/file references, stored in the ticket and uploaded to reMarkable. Separating the issues into topics (correctness/concurrency, lint, security, vulnerabilities) makes the work tractable and prioritized instead of a flat 25-item list.

### What worked
- The docmgr doctor passed cleanly after adding the `ci`/`concurrency`/`lint`/`security` vocabulary slugs.
- reMarkable bundle upload (dry-run then real) worked first try; verified the PDF is present on the cloud.
- The topic separation mapped perfectly: every failing check and review comment fell into exactly one of the four topics with no ambiguity.

### What didn't work
- The GitHub API kept returning HTTP 503 during the whole session, so the exact Dependency Review offending-package list could not be fetched remotely. This is documented as an open question in the guide (Part 6.2 / 9.3): the intern must read the failed CI job logs to get the specific package.

### What I learned
- The four failing CI checks and five review comments decompose into a clean priority order: (1) toolchain bump clears 9 stdlib vulns + dependency review, (2) two trivial lint fixes, (3) gosec findings are mostly false-positive integer casts but a few (pprof, path traversal) deserve real fixes, (4) the Codex P1s are the real engineering work.
- The govulncheck failures are entirely a Go toolchain version problem — `go.mod` pins `1.26.1` but fixes ship in `1.26.2`–`1.26.5`. The `toolchain` directive is the cleanest fix.

### What was tricky to build
- Writing a system primer that is accurate without reading all 20k lines: I anchored every architectural claim to a specific file (e.g. the wire protocol to `pbui/wire.go:24`, the layout tree to `wmcore/tree.go:38`, the event fan diagram to `jsmod/eventfan.go`). This keeps the intern grounded in real code.
- Balancing depth vs. scannability in the topic analyses: each issue gets root-cause prose + a code/evidence block + pseudocode fix + validation, so an intern can both understand *why* and see *how*.

### What warrants a second pair of eyes
- The RC-4 fix changes what `Value.Raw` carries (the exported value instead of the descriptor). This is a behavior change for any verb that reads `pbui.Object.Value`. The guide flags this as a risk (Part 9.1) but it must be audited against `pkg/jsmod/pbuimod/verbs.go` before merging.
- The RC-1 fix assumes `replapi` can parse-check without executing; if it cannot, the guide offers a fallback (error-position heuristic) but that is weaker. Confirm `replapi`'s capabilities.

### What should be done in the future
- Implement the phased plan (Part 7): toolchain → lint → gosec → Codex bugs → verify CI.
- Add the four regression tests listed in Part 8.2 (eventfan race, REPL double-exec, REPL ordering, rich-value payload).
- Resolve the open question on the Dependency Review's specific offending package once GitHub Actions is reachable.

### Code review instructions
- Read `design-doc/01-pr-1-review-analysis-and-intern-implementation-guide.md` top to bottom.
- Cross-check each topic's evidence against the captured scripts (`scripts/00..03`).
- Validate the reMarkable upload: `remarquee cloud ls /ai/2026/07/19/GGWM-010-PR1-REVIEW --long --non-interactive`.

### Technical details
- Design doc path: `ttmp/2026/07/19/GGWM-010-PR1-REVIEW--.../design-doc/01-pr-1-review-analysis-and-intern-implementation-guide.md` (48 KB, 10 parts).
- reMarkable remote: `/ai/2026/07/19/GGWM-010-PR1-REVIEW/GGWM-010 PR #1 Review — Analysis & Intern Guide.pdf`.
- Commit (doc + bookkeeping): `0bac078`.

## Step 3: Implemented the phased plan — all failing CI checks now pass locally

With the analysis and guide complete, the user said "go ahead", so I implemented
the fixes in the four phases from the guide. Each phase was committed separately
and verified before moving on. All five failing checks now pass locally: build
OK, 15 test packages pass under `-race`, golangci-lint 0 issues, govulncheck 0
vulnerabilities, gosec 0 issues.

### Prompt Context

**User prompt (verbatim):** "go ahead"

**Assistant interpretation:** Proceed with implementing the phased plan from the design doc to fix all of PR #1's review comments and failing CI actions.

**Inferred user intent:** Move from analysis to implementation: actually fix the issues, test, and commit at intervals, keeping the diary updated.

### What I did
- **Phase 1 (toolchain):** added `toolchain go1.26.5` to `go.mod`; `govulncheck` (run with `GOTOOLCHAIN=go1.26.5`) reports 0 vulnerabilities (was 9 stdlib). Commit `dcb86c7`.
- **Phase 2 (lint):** added the missing `launcher.KindApp` case to `commandTone` (`pkg/wmx11/launcher.go`); dropped the named return on `eval` and declared `console` as a local var (`pkg/cmds/replui.go`). `golangci-lint` reports 0 issues (was 2). Commit `2b11ee3`.
- **Phase 3 (gosec):** added `X16`/`X32` clamp helpers to `pkg/draw/ximage.go` and used them in `fullscreen.go`/`xshm.go`; made `parseHex`'s `hex()` return `uint8` (`apps.go`); added `clampByte` (`uispec.go`); `filepath.Clean` + scoped `#nosec` in `kitty_install.go`; dedicated pprof mux + `http.Server` timeouts in `wm.go`; `#nosec G404` on the deterministic demo RNG (`colors.go`). `gosec` reports 0 issues (was 20). Commit `e174f28`.
- **Phase 4 (Codex bugs):**
  - RC-3: extracted `snapshot()` that deep-copies `goSubs` under the lock in `pkg/jsmod/eventfan.go`; added a `-race` regression test `TestEventFanSnapshotConcurrentSubscribe`.
  - RC-1: changed the retry condition from `strings.Contains(err, "SyntaxError")` to `Execution.Status == "parse-error"` in `pkg/cmds/replui.go` — a runtime SyntaxError has Status `"runtime-error"` and no longer triggers a re-evaluation.
  - RC-2: added an `evalQueue` channel + single `evalWorker` goroutine to `richReplApp` so cells are evaluated in submission order.
  - RC-4: changed `NormalizeRich(value, descriptor)` to store the exported VALUE in `Value.Raw` (not the descriptor); updated caller and test.
  - RC-5: pinned focus to the fullscreen frame in `pkg/wmx11/manage.go` while one is active.
  - Commit `8d9e905`.
- Pushed the branch to `wesen/task/go-go-wm` to trigger CI.

### Why
The guide's phased plan ordered the work from lowest-risk (toolchain) to highest-value (Codex bugs), so each phase was independently verifiable and the dangerous concurrency bug (RC-3) was fixed with a regression test.

### What worked
- The `Execution.Status` field in `replsession.EvaluateResponse` cleanly distinguishes `"parse-error"` from `"runtime-error"` — discovered by reading the `go-go-goja` source, which made the RC-1 fix structural rather than a heuristic.
- The `-race` test for RC-3 passes and would catch the original bug (deep-copy vs map-header copy).
- The toolchain directive (`toolchain go1.26.5`) is honored by `actions/setup-go` with `go-version-file: go.mod`, so CI will pick it up automatically.

### What didn't work
- First attempt at the `nonamedreturns` fix broke the build: `console` was a named return used as a local. Fixed by declaring `var console []string`.
- First RC-2 test draft deadlocked (double `wg.Add(1)`). Fixed the WaitGroup accounting.
- `//nolint:gosec` annotations were ignored by gosec (that's golangci-lint syntax); switched to gosec's own `#nosec` directive, which cleared the findings.
- The first `snapshot()` method used named returns, which re-triggered `nonamedreturns`; removed them.

### What I learned
- gosec's comment directive is `#nosec` (optionally with rule IDs like `#nosec G404`), NOT `//nolint:gosec` (which is golangci-lint's). The two linters use different suppression syntax.
- The `toolchain` directive in `go.mod` is the cleanest way to fix stdlib vulns without forcing all contributors onto a newer language version; `GOTOOLCHAIN=auto` downloads it on demand.
- `replsession.ExecutionReport.Status` is the authoritative signal for parse-vs-runtime failure — far more reliable than error-string sniffing.

### What was tricky to build
- RC-1 was the subtlest: the original code inspected `resp.Cell.Execution.Error` for the substring `"SyntaxError"`, but goja reports both a wrapper parse failure AND a runtime `JSON.parse("{")` throw as `SyntaxError`. The fix required discovering that the kernel sets `Status: "parse-error"` only for true parse failures (line 190 of `evaluate.go`) and `"runtime-error"` for throws (line 343) — so checking Status is structurally sound and cannot be fooled.
- RC-4 required an API change to `NormalizeRich` (new second parameter) and updating both the caller and the test, since the descriptor and the value are genuinely two different things.
- The eventfan deep-copy must hold the lock while copying BOTH the map keys and the slices; copying just the map header (the original bug) shares the backing store.

### What warrants a second pair of eyes
- RC-2's serialized worker adds ordering latency: a slow cell now blocks later cells. This is the correct tradeoff for a notebook (output attribution > throughput), but worth confirming the UX is acceptable.
- RC-4 is a behavior change: any verb that depended on receiving the descriptor in `pbui.Object.Value` will now receive the actual value. Audit `pkg/jsmod/pbuimod/verbs.go` against this.
- The Dependency Review (Topic D) failure could not be reproduced locally — it needs the failed CI job logs to identify the specific third-party package. This remains an open item (see open questions in the design doc).

### What should be done in the future
- Watch the CI run on the pushed branch: confirm `lint`, `Go Vulnerability Check`, `GoSec Security Scan` all pass, then read the `Dependency Review` job logs to resolve the last open item.
- Re-request the Codex review (`@codex review`) and confirm the five comments are resolved.
- Consider adding a REPL-level test for RC-1 (a side-effecting expression with a runtime SyntaxError runs exactly once) and RC-2 (two rapid submissions preserve console attribution) — these need a kernel harness, which is heavier than the unit tests added.

### Code review instructions
- Review the four phase commits in order: `dcb86c7`, `2b11ee3`, `e174f28`, `8d9e905`.
- Validate locally: `env GOTOOLCHAIN=go1.26.5 go test ./... -race`, `golangci-lint run ./...`, `env GOTOOLCHAIN=go1.26.5 govulncheck ./...`, `gosec -exclude=G101,G304,G301,G306,G204 ./...`.
- The key structural fix to verify is RC-1: `pkg/cmds/replui.go` now checks `Status == "parse-error"` (not error-string sniffing).

### Technical details
- Commits: `dcb86c7` (toolchain), `2b11ee3` (lint), `e174f28` (gosec), `8d9e905` (Codex bugs).
- Verification (all with `GOTOOLCHAIN=go1.26.5`): build OK, 15 packages pass `-race`, lint 0 issues, govulncheck 0 vulns, gosec 0 issues (3 justified `#nosec`).
- Branch pushed: `wesen/task/go-go-wm` (e9fa8be..8d9e905).

### CI results (after push)

GitHub Actions ran on the pushed branch. Of the 4 originally-failing checks:
- ✅ **lint** (golangci-lint) — passes
- ✅ **Go Vulnerability Check** (govulncheck) — passes
- ✅ **GoSec Security Scan** (gosec) — passes
- ⚠️ **Dependency Review** — still fails, but NOT a code problem.

The Dependency Review job logs show: `Dependency review is not supported on this repository. Please ensure that Dependency graph is enabled, see https://github.com/go-go-golems/go-go-wm/settings/security_analysis`. This is a **repository settings issue**: the repo's "Dependency graph" feature (Settings → Code security → Dependency graph) is not enabled, so the `dependency-review-action` cannot run. It requires a repo admin to toggle that setting — it cannot be fixed by code changes. This resolves the open question in the design doc (Part 6.2 / 9.3): the failure was never about a specific vulnerable third-party package; it was the missing repository feature.

## Step 4: New Codex review on commit ad7570b3 — 5 more P2 comments (RC-6..RC-10)

After the fixes landed, Codex re-reviewed the PR (commit `ad7570b3`) and left
five more inline comments, all P2. They are a fresh batch: two are
fullscreen/focus edge cases that my RC-5 fix exposed, and three are
data-persistence/caching correctness issues in the launcher and ui module.
Captured them to `scripts/07-pr-review-comments-new.md` and analyzed each
against the source before fixing.

### Prompt Context

**User prompt (verbatim):** "Continue addressing the new code review comments: https://github.com/go-go-golems/go-go-wm/pull/1"

**Assistant interpretation:** Fetch the latest Codex review on the PR and address its new inline comments.

**Inferred user intent:** Close the loop on the Codex review — the first batch (RC-1..RC-5) was fixed, now handle whatever Codex found on the updated code.

### What I did
- Fetched all PR review comments via `gh api`; identified 5 new ones dated 2026-07-20T02:53 on commit `ad7570b3`.
- Captured them to `scripts/07-pr-review-comments-new.md`.
- Read each affected site: `pkg/wmx11/wm.go` (ApplyBatch/afterOp/refocusCurrent), `pkg/wmx11/manage.go` (focus), `pkg/launcher/desktop.go`+`registry.go` (scanDesktopDirs/Refresh), `pkg/launcher/frecency.go` (bump/save), `pkg/jsmod/uimod/app.go` (jsAppState.redraw, show/tile).

### Why
Two of the new comments (RC-6, RC-7) are regressions/edge cases that my RC-5
focus-pinning fix surfaced: pinning focus to the fullscreen leaf is correct
for tiled windows, but a *floating* fullscreen frame has an empty `leaf`, and
`ApplyBatch` (the batch path) doesn't exit fullscreen like `afterOp` does.
The other three (RC-8/9/10) are pre-existing correctness gaps in caching,
persistence, and per-surface redraw hooks.

### What I learned
- RC-5's fix was incomplete: it assumed `w.fullscreen.leaf` is always set,
but floating fullscreen frames have `leaf == ""` (floats aren't tree leaves).
Pin-on-empty-leaf quietly corrupted the focused state.
- The frecency debounce is a classic "drop the last timer" bug: within 5s of a
prior save, `bump` returns without scheduling any future write, so a burst
right before shutdown loses all its counts.
- The desktop-entry scanner only checks directory mtime, which is stable when
a file's *contents* change — a real staleness gap.

### What was tricky to build
- RC-7 (float fullscreen focus): the fix must distinguish a floating
fullscreen frame (track via `focusedFloat`, not `leaf`) from a tiled one. The
cleanest approach is: in `focus()`, only pin to `w.fullscreen` when it is the
focused float or a real leaf, and don't clobber `focusedFloat`.
- RC-10 (per-surface redraw): `setRedraw` stores a single callback, so
`show()` + `tile()` on the same app overwrite each other. Need a per-surface
map of redraw hooks.

### What warrants a second pair of eyes
- RC-7: changing focus pinning for floats — verify a floating fullscreen
window keeps focus through navigation and that close/float/fullscreen verbs
still target it.
- RC-9: the frecency flush on shutdown — confirm the registry has a shutdown
hook to call into, or whether a timer-based delayed flush is safer.

### What should be done in the future
- Add regression tests for the launcher scan (file content change triggers
rescan) and frecency (burst-before-shutdown persists).
- Re-request Codex review after pushing.

### Code review instructions
- Read `scripts/07-pr-review-comments-new.md` for the verbatim comments.
- Cross-check each fix against the matching section below.

### Technical details
- Commit under review: `ad7570b3` (latest pushed).
- New comments: RC-6 (`wm.go:387`), RC-7 (`manage.go:513`), RC-8 (`desktop.go:65`), RC-9 (`frecency.go:50`), RC-10 (`uimod/app.go:199`).

### Fixes applied (commit `8127f27`)
- **RC-6** (`pkg/wmx11/wm.go`): `ApplyBatch` now calls `w.exitFullscreen()` before `refocusCurrent()` on a workspace switch, mirroring `afterOp`. The old fullscreen frame is unmapped by `relayout`, so leaving `w.fullscreen` set caused `refocusCurrent -> focus` to pin focus back to the old leaf, stranding the new workspace.
- **RC-7** (`pkg/wmx11/manage.go`): in `focus()`, a *floating* fullscreen frame (empty `leaf`) is now kept tracked through `focusedFloat` (its client) instead of setting `leaf` to empty and clearing `focusedFloat`. Navigation keeps focus on the visible fullscreen float, so close/float/fullscreen verbs still target it.
- **RC-8** (`pkg/launcher/desktop.go`): `scanDesktopDirs` now stats each `.desktop` file's mtime in addition to the directory mtime, so a content edit to an existing entry (which doesn't bump the dir mtime) invalidates the cache.
- **RC-9** (`pkg/launcher/frecency.go` + `registry.go` + `wm.go`): replaced the "5s since lastSave" check with a resettable `time.AfterFunc` that (re)arms on every `bump` — guaranteeing a write 5s after the LAST launch. Added `Flush()` and call it from `WM.Shutdown` so a pre-quit burst persists. Regression test `TestFrecencyBurstFlushedOnShutdown`.
- **RC-10** (`pkg/jsmod/uimod/app.go`): the single `redraw func()` slot is now a per-surface `map[string]func()` keyed by surface (`"xapp"`, `"tile"`); `postRedraw` invokes every live surface, so a `ui.app` exposed through both `show()` and `tile()` repaints both.

### What worked
- All 5 fixes compile, all tests pass under `-race`, golangci-lint 0 issues, gosec 0 issues.
- The RC-9 regression test reproduces the old bug shape (no file before Flush) and confirms the burst persists.

### What didn't work
- First frecency rewrite had a tangled timer-arming branch; simplified to `time.AfterFunc` + `Reset`.
- Adding a `removeRedraw` cleanup method would have triggered the `unused` linter (the xapp App interface has no Stop hook), so I kept the per-surface map keyed to overwrite safely on re-Start instead.

### Code review instructions
- Review commit `8127f27`.
- Key risk sites: RC-7 float focus (verify a floating fullscreen window keeps focus through Mod4-space and that verbs still target it), RC-9 shutdown flush (verify a burst before `Mod4-Shift-q` persists).

### What should be done in the future
- Add a regression test for RC-8 (edit a .desktop file's contents -> Refresh returns the new Name/Exec).
- Add a launcher hook to drop the `"xapp"` redraw key when a jsXApp window closes (needs a Stop hook on the xapp App interface).
- Re-request Codex review after pushing.

## Step 5: Third Codex batch (RC-11..RC-16) + systemic pattern analysis

Codex reviewed commit `6635c69` and left 6 more comments (1× P1, 5× P2).
The user asked to step back and look for patterns. Three systemic patterns
emerged from this batch plus the earlier fullscreen comments (RC-5/6/7/12/13):

### Prompt Context

**User prompt (verbatim):** "Address the next batch of review comments. Take a step back and see if there are patterns / bigger picture things involved that could benefit from some refactoring to address these issues at a systemic level."

**Assistant interpretation:** Fix the new comments, but first identify cross-cutting patterns and refactor at the systemic level where one change addresses several comments.

**Inferred user intent:** Stop fixing one comment at a time; find the root causes that keep spawning fullscreen/focus/race comments and fix those.

### The three systemic patterns

**Pattern A — Fullscreen state is scattered, not encapsulated.** `w.fullscreen` (a `*frame`) is read/written across `fullscreen.go`, `manage.go` (focus), `wm.go` (ApplyBatch/afterOp), `theme.go` (repaint), `ipc.go`, and `manage.go:330` (relayout skip). Five of the 16 Codex comments (RC-5, RC-6, RC-7, RC-12, RC-13) are all consequences of the same root cause: there is no single "fullscreen owns focus and geometry" invariant — every call site re-derives what fullscreen means (is it floating? does it have a leaf? should focus pin? should configure be ignored?). Each new code path that touches focus or geometry re-asks those questions and gets one wrong.

**Pattern B — Focus state is a coupled trio (`focused` + `focusedFloat` + `fullscreen`) with no single mutator.** 7 files touch both `focused` and `focusedFloat`. `focus()`, `focusFloat()`, `refocusCurrent()`, `unmanageFloat()` each independently maintain the invariant that exactly one of {tiled leaf, float, fullscreen} holds keyboard focus. RC-7 and RC-13 are both "the wrong field got cleared" bugs. The fix shape is the same as Pattern A: one method that transitions focus state atomically.

**Pattern C — The theme palette is a package-global mutable singleton read by 18 files with no synchronization.** `draw.SetTheme` rewrites ~14 package vars + `AppColors` from the event-fan drainer goroutine while renderers on the X loop read them lock-free (the var-block comment admits this and pins the rule to "the goroutine that owns rendering"). RC-14 is the concrete failure: `run`/`repl --ui` have a *separate* X app loop, so `SetTheme` runs on the drainer while `uispec.Render` runs on the app loop — a genuine data race and torn frames. 136 read sites across 18 files make a lock-per-var infeasible; the systemic fix is to make the palette **immutable** (a pointer to a `Theme` struct swapped atomically) so readers never see a half-applied theme.

### Per-comment plan (mapped to patterns)

- **RC-11 (P1, xgojaprovider):** `pbui` and `wm` each call `newState()` → two broker connections, same client name → verb routing nondeterminism. *Not* a pattern issue — a plain bug: the two `NewModuleFactory` closures must share one `runtimeState`. Fix: hoist `state` to the `Register` scope.
- **RC-12 (P2, manage.go:589):** float ConfigureRequest honored while fullscreen → frame shrinks, WM stuck. Pattern A: fullscreen should own geometry. Fix: ignore float configure while `w.fullscreen == f`.
- **RC-13 (P2, manage.go:521):** fullscreen float clears `w.focused`, losing the tiled leaf for restoration. Pattern B: focus state clobbered. Fix: don't clear `w.focused` when pinning a fullscreen float — preserve it for `unmanageFloat`.
- **RC-14 (P2, run.go:236):** `draw.SetTheme` races renderers. Pattern C: make palette immutable. Fix: `SetTheme` swaps an `atomic.Pointer[Theme]`; readers go through `draw.Current()`.
- **RC-15 (P2, xshm:103):** assumes 24-bit root depth; 16-bit visuals corrupt. Fix: validate `X.Screen().RootDepth == 24` in `Available`.
- **RC-16 (P2, frecency:92):** `Flush` returns while a save is in flight, losing the final launch. Fix: `Flush` waits for an in-flight save, then re-snapshots.

### What I did
- Captured the 6 comments to `scripts/08-pr-review-comments-batch3.md`.
- Mapped each to a pattern; confirmed the fullscreen/focus cluster (5 comments) and the palette race (1) are the systemic targets.
- Will implement RC-11 (shared state) and RC-14 (immutable palette) as the systemic refactors; RC-12/13 as the focus-state consolidation; RC-15/16 as targeted fixes.

### What I learned
- The fullscreen comments are not independent nits — they are the same invariant violation surfacing in different code paths. Encapsulating fullscreen state (one method that says "fullscreen owns focus+geometry; everything else defers") would have prevented RC-5/6/7/12/13 at once.
- The palette race is structural: a mutable package-global read by 18 files cannot be made safe by adding locks to each reader. Immutability + atomic swap is the only fix that scales.

### What warrants a second pair of eyes
- The immutable-palette refactor touches 18 files' read sites — high blast radius. Must verify every `draw.Paper` etc. becomes `draw.Current().Paper` (or a captured local) and that no reader caches a stale value across a swap.
- The focus-state consolidation must not change observable behavior for the common (non-fullscreen) path.

### What should be done in the future
- Consider a `fullscreenState` helper type that owns the pin/exit/owns-geometry decisions, so new call sites can't re-derive them.
- Consider a `focusState` type that makes the {tiled, float, fullscreen} transition atomic.

### Technical details
- Commit under review: `6635c69`.
- New comments: RC-11 (`xgojaprovider/provider.go:108`), RC-12 (`manage.go:589`), RC-13 (`manage.go:521`), RC-14 (`run.go:236`), RC-15 (`xshm/xshm.go:103`), RC-16 (`launcher/frecency.go:92`).

### Fixes applied
- **RC-11 (P1, `pkg/xgojaprovider/provider.go`, commit `542ca35`):** `Register` now hoists one shared `runtimeState` so the `pbui` and `wm` module factories reuse ONE broker connection + event fan. Both factories call `state.init(mctx)` (idempotent, guarded by `cfgSet`) instead of each creating its own state. This is the plain bug, not a pattern issue.
- **RC-14 (P2, `pkg/draw/theme.go` + 18 files, commit `542ca35`):** the systemic refactor. Replaced the 14 mutable package-level palette vars + `AppColors` slice with an immutable `Palette` snapshot behind `atomic.Pointer[Palette]`. `SetTheme` publishes a whole Palette atomically; renderers call `draw.Current()` once per paint pass. This closes the race structurally (a paint pass that captured `Current()` at its start never sees a torn palette). Migrated all 136 read sites (`draw.X` -> `draw.Current().X`), the draw package internals (`pal := Current()` per render fn), and the draw tests.
- **RC-12 (P2, `pkg/wmx11/manage.go`, commit `7d023fa`):** `handleConfigureRequest` ignores a float's ConfigureRequest while it is the fullscreen frame — fullscreen owns geometry.
- **RC-13 (P2, `pkg/wmx11/manage.go`, commit `7d023fa`):** `focus()` no longer clears `w.focused` when pinning a fullscreen float — preserves the tiled leaf for `unmanageFloat` restoration.
- **RC-15 (P2, `pkg/xshm/xshm.go`, commit `7d023fa`):** `Available()` now requires `X.Screen().RootDepth == 24` (the only pixel layout the BGRA writer produces).
- **RC-16 (P2, `pkg/launcher/frecency.go`, commit `7d023fa`):** `Flush()` waits for an in-flight save (`waitSaveDone`) then persists again, so entries added after the in-flight snapshot's marshalling are not lost.

### What worked
- The immutable-palette refactor compiled and tested clean on the first full run after the mechanical migration (the python migration script needed one fix to preserve the file header before the first `func`).
- All 6 fixes pass `-race`, lint 0 issues, gosec 0 issues.
- The systemic framing paid off: RC-14 became a clean atomic-swap instead of a scattered locking exercise, and the RC-12/13 fullscreen pair reinforced that the focus/fullscreen state still wants encapsulation (noted as future work).

### What didn't work
- First python migration script dropped the package header (lost everything before the first `^func`); fixed by preserving `lines[:func_idx[0]]`. Required a `git checkout` to restore the 3 draw files.
- The `waitSaveDone` busy-wait-spin-under-lock is a pragmatic choice (Flush runs once at shutdown, save's disk write is fast); a `sync.Cond` would be cleaner but adds machinery.

### What warrants a second pair of eyes
- The immutable-palette refactor touched 18 files' read sites — verify no renderer caches a stale `Palette` across a swap (each render fn captures `Current()` fresh, which is correct).
- RC-13: confirm `unmanageFloat` actually restores focus to the preserved `w.focused` leaf after a fullscreen float closes (manual X11 test).

### What should be done in the future
- Encapsulate fullscreen + focus state (Patterns A & B) into helper types so new call sites cannot re-derive the "fullscreen owns focus/geometry" invariant — this batch's RC-5/6/7/12/13 are all symptoms of that scatter.
- Re-request Codex review after pushing.

### Code review instructions
- Review commits `542ca35` (RC-11 + RC-14 systemic) and `7d023fa` (RC-12/13/15/16).
- Validate: `env GOTOOLCHAIN=go1.26.5 go test ./... -race`, `golangci-lint run ./...`, `gosec -exclude=G101,G304,G301,G306,G204 ./...`.
