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
