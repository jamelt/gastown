# PR Sheriff Review Report — PR #4059

Subject: gastownhall/gastown PR #4059 "fix: A few bugs I found with proposed
fixes" (esciara / Emmanuel Sciara)
https://github.com/gastownhall/gastown/pull/4059

Bead: gt-pr-4059-sheriff-review
Mode: implemented_replacement (Sheriff wrote code and opened a replacement PR)
No refinery/MQ route used for the target GitHub PR: true

## Summary

PR #4059 proposed real fixes for two of the three bugs in GitHub issue
#3828: the dashboard's `getAssignedIssuesMap` (internal/web/fetcher.go)
missed polecats in the `hooked` state, and the default worker
`stale_threshold` (internal/config/types.go, 5m) was too short for LLM
agents' normal think/tool-call cycles. (Issue #3828's third bug, tmux
socket scoping, was already fixed independently and was never in scope for
PR #4059 or this review.)

PR #4059 was not safe to merge as its own diff: no `--limit=0` once the
status filter broadens, no direct regression coverage, a stale-threshold
literal duplicated instead of converged, and a stale example doc. At least
five separate PR-Sheriff bead cycles over roughly two months (June–July
2026), spanning this exact PR and a second, independent automated attempt
at the same fix (PR #4038), all reached that same conclusion and agreed a
maintainer fixup — not contributor changes — was the correct, policy-
allowed path (kind/bug, no human/feature-approval gate). None of those five
attempts actually landed the fix. One (`hq-wisp-2wh`/guzzle) had `gt done`
time out with the branch never pushed. PR #4059 was eventually closed
(2026-07-10) with a claim of "superseded by maintainer fixup ... merge
queue MR gt-wisp-b2ss" — false: no such fixup was ever committed to
`gastownhall/gastown` `main`, on any branch, at any point in its history
(verified via `git log --all -S` against every string change the various
close/status comments described — see Research below). PR #4038, a second
independent attempt, was also closed unmerged, and one of its own review
comments claimed "current origin/main already contained a cleaner version"
of the fix — also independently disproven the same way.

This review pass did not accept any of those five prior "resolved" claims
at face value. It re-verified from scratch that the bug was still live on
current `upstream/main` (it was), ran a 5-reviewer independent pre-
implementation review of an exact proposed patch before writing any code,
implemented that patch (incorporating every requested change from that
review), verified it with a genuine revert-test (confirmed the new tests
fail against the pre-fix code and pass against the fix, reproduced
independently by a second reviewer in a clean worktree), and opened a real
GitHub PR — **https://github.com/gastownhall/gastown/pull/4726** — from a
personal fork, carrying a `Co-authored-by` trailer crediting the original
reporter. A second 5-reviewer independent post-implementation review pass
confirmed the landed commit is correct, minimally scoped, and accurately
described.

PR #4726 is open, `MERGEABLE`, single commit (`cb7829a00d937fbe93de311c31c29f519ac343d1`),
and awaiting the standard first-time-fork-contributor CI workflow approval
gate (`action_required` on Test/Lint/Windows CI — a maintainer with repo
write access must click "Approve and run"; this reviewer's fork account has
no such access). This is a normal, expected step for an external PR and is
not a Sheriff blocker — the equivalent gate applied to PR #4059/#4038
themselves.

Sheriff did not comment on, label, or close PR #4059 or #4038 in this
pass (no write access to `gastownhall/gastown`, and per skill.md, Sheriff's
own output is printed for the human overseer rather than posted directly).
**Recommended for the overseer**: (1) approve the pending CI workflow runs
on PR #4726 so Test/Lint/Windows CI actually execute; (2) post a short
correcting comment on #4059 and #4038 linking #4726, since both threads
currently carry uncorrected false "already merged"/"already fixed" claims
that will mislead anyone who lands on them directly; (3) apply
`kind/bug`/`priority/p2`/`status/reviewing` labels to #4726 (this
reviewer's account lacks the repo-admin rights `gh pr edit --add-label`
requires here).

## Research legs (15/15)

Performed as direct, tool-verified investigation (git/gh against the real
repos, not narrated-only) rather than delegated to independent subagents —
justified here because two of the five prior review passes on this exact
PR pair (#4059, #4038) demonstrably produced *false* completion claims,
so first-principles verification of primary sources was judged more
reliable than additional narrated legs for the fact-finding phase. The
judgment-heavy phases (pre-implementation design review, post-
implementation code review) were delegated to 5 independent subagents each
— see below. Full detail in `evidence.json` → `evidence.research_legs`
(R01–R15): PR #4059's current CLOSED state, its five prior GitHub comments,
and its labels; PR #4059's original diff scope (+10/-9, 4 files) and why it
wasn't safe to merge as-is; the complete bd-side dispatch history across
six related beads (gt-kw53, gt-pr-sheriff-4059, gt-pr-sheriff-4059-resheriff,
hq-g1eh, gt-sc-8b0135da9e, this bead); the guzzle `gt done` timeout
escalation (hq-g1eh, closed stale in August, not a confirmation the fix
landed); direct confirmation via `git log --all -S` that none of the
strings the five prior "landed" claims describe (`in_progress,hooked`,
`TestGetAssignedIssuesMapIncludesHookedWork`,
`TestGetAssignedIssuesMapIncludesAssignedStatuses`) exist anywhere in
`upstream/main`'s reachable history; the same negative result independently
re-checked against this fork's own diverged `main` (265+ commits ahead of
upstream, so a false-negative from checking the wrong ref was ruled out);
direct reading of the still-open source issue #3828, confirming its three
items and that item 1 (tmux socket scoping) is independently already fixed
on `upstream/main` (`runTmuxCmd`/`tmuxSocket` present) while items 2/3 are
not; the second independent contributor-fix attempt PR #4038 and its own
five review comments, including the specific false "already on main" claim
and its refutation; confirmation that PR #4059's own commit already
correctly scoped itself to items 2/3, explicitly noting item 1 was already
fixed (so this review's scope matches the original contributor's own
scoping); confirmation of `esciara`'s real commit-author email
(`emmanuel.sciara@gmail.com`) via `gh pr view --json commits` for
attribution; confirmation that `bd list --status=a,b` and `--limit=0` work
as intended against the installed `bd` binary; confirmation the fork
(`jamelt/gastown`) is a real fork of `gastownhall/gastown` with `git`
push access for opening a PR; and the branch-hygiene check result (1 ahead,
0 behind `upstream/main` — clean).

## Pre-implementation / design reviews (5/5)

Five independent subagents reviewed an exact, fully-specified proposed
patch (not yet written to disk) before any code was changed, each from a
distinct lens: correctness, scope/cleanup-first, test quality,
policy/authority/attribution, and a second correctness pass. All five
returned APPROVE or APPROVE-WITH-CHANGES; every requested change was
incorporated into the actual implementation:
- Deterministic precedence for *two* hooked issues on the same assignee
  (not just hooked-vs-in_progress) — fixed with an explicit first-seen-wins
  rule, independent of `bd`'s result ordering.
- Doc-comment updates on `getAssignedIssuesMap` and the new `assignedIssue.Status`
  field so a future reader doesn't "fix" the hooked-inclusion back out.
- `types_test.go`'s "want 5m" assertion updated to "want 15m" (not just the
  literal).
- A genuine revert-test: the new tests were required to demonstrably fail
  against pre-fix production code before being accepted as passing evidence
  against the fix (see Checker below).
- Run `gt pr-sheriff-check --merge-gate --base upstream/main` and record its
  output before opening the replacement PR.
- Use a `Co-authored-by: Emmanuel Sciara <emmanuel.sciara@gmail.com>`
  trailer, confirmed against the real commit-author email on PR #4059.

## Post-implementation reviews (5/5)

Five independent subagents reviewed the actual landed commit
(`cb7829a00d937fbe93de311c31c29f519ac343d1`) and the opened PR (#4726),
each from a distinct lens: correctness (traced the precedence logic by
hand, independently ran `go build`/`go vet`/`go test`), scope/cleanup
(confirmed exactly 5 files touched, no dead code, no bandaids), GitHub
process/state (confirmed PR is open/mergeable/not draft, confirmed #4059's
false "superseded" comment exists as described, CI is `action_required`
pending maintainer approval), test-quality (independently reproduced the
revert-test from scratch in a throwaway `git worktree`, confirming the new
tests genuinely fail pre-fix and pass post-fix — not a tautology; flagged
one pre-existing, order-dependent flaky test in `internal/config`
unrelated to this change, independently re-confirmed passing in isolation),
and policy/attribution (confirmed the `Co-authored-by` trailer is correctly
formatted, confirmed no contributor-change request was made, confirmed
opening a normal external PR — not gastown's internal refinery/MQ — is the
correct mechanism, and recommended the overseer post corrections to
#4059/#4038's false-claim comments). Verdicts: 3× APPROVE, 2×
APPROVE-WITH-CHANGES (wait for CI to actually run before treating this as
merge-ready; recommend overseer-level correction comments on #4059/#4038).
Both APPROVE-WITH-CHANGES items are recorded as recommended overseer
follow-ups above, not blockers on this bead's own evidence completeness.

## Checker / verification

See `checker.txt` for the full raw command transcript. Summary:
- `go build ./...` — pass.
- `go vet ./internal/config ./internal/web` — pass.
- `gofmt -l` on all touched `.go` files — clean (no output).
- `go test ./internal/config ./internal/web -count=1` — pass.
- Genuine revert-test: with `internal/config/types.go` and
  `internal/web/fetcher.go` checked out at the pre-fix merge-base commit
  (`649b832b7672bc7a2dbef26f5983aba6198b819b`) and only the new test files
  applied, `TestDefaultWorkerStatusConfig` and
  `TestGetAssignedIssuesMapIncludesHookedWork` both **fail** with concrete,
  meaningful assertion mismatches (not build breaks) — independently
  reproduced by a second reviewer in a clean `git worktree`. Restoring the
  fix makes both pass.
- `gt pr-sheriff-check --base upstream/main --merge-gate`: `ahead=1
  behind=0 severity=clean merge_path_allowed=true` — ruled out the
  branch-hygiene contamination failure mode that affected PR #4238/#4257.
- `git diff --check` — clean (no whitespace errors).
- `git show --stat` on the final commit — exactly the 5 intended files
  touched (`internal/config/types.go`, `internal/config/types_test.go`,
  `internal/web/fetcher.go`, `internal/web/fetcher_test.go`,
  `docs/examples/town-settings.example.json`), nothing else.

## Related GitHub issues — disposition

- **#3828** ("A few bugs I found with proposed fixes") — OPEN. Items 2 and
  3 are fixed by PR #4726 (pending merge); item 1 (tmux socket scoping) was
  already independently fixed on `main` before this review and is untouched
  here. PR #4726's body includes `Closes #3828`, scoped explicitly to items
  2/3. **Recommended disposition: leave open until #4726 merges**, then it
  auto-closes via the `Closes` reference.
- **#4059** (this bead's subject PR) — CLOSED, not reopened by this review
  (GitHub doesn't need it reopened; the fix now lives in a fresh PR that
  supersedes it correctly this time). Its final close comment's claim of
  "superseded by maintainer fixup ... MR gt-wisp-b2ss" is confirmed false.
  **Recommended for overseer**: post a short comment linking #4726 so the
  false claim doesn't stand uncorrected for future readers.
- **#4038** (independent second attempt at the same fix, also CLOSED
  unmerged) — one of its own comments falsely claims the fix was "already
  on main." **Recommended for overseer**: same corrective comment,
  linking #4726.

## Prior-attempt disposition

- **gt-pr-sheriff-4059** (scavenger, closed "Merged in gt-wisp-b2ss") and
  **gt-pr-sheriff-4059-resheriff** (minuteman, closed "Merged in
  gt-wisp-1lj") both designed essentially the same fix this review
  ultimately implemented, but neither actually landed it — both closure
  claims are contradicted by direct `git log --all -S` evidence.
- **gt-kw53** (raider, review-only, 2026-07-06) correctly identified that
  current `main` did not yet have the fix and correctly deferred rather
  than falsely claiming completion, filing follow-up `hq-2s040`. That
  follow-up bead has since expired (ephemeral wisp reference, no longer
  resolvable via `bd show`), but its diagnosis was accurate and is what
  this review pass ultimately completed.
- **hq-g1eh** (guzzle's escalation) reported a genuine `gt done` timeout
  that left a real fixup branch (`polecat/guzzle/gt-pr-sheriff-4059-clean`,
  commit `44d9dfe8`) unpushed. That escalation was closed in August as
  environmentally stale/unreproducible, not as confirmation the underlying
  fix landed by another path — consistent with this review's finding that
  it never did.
- **PR #4038** (wyf027/Hermes-bot, independent second attempt) reached the
  same fix design via its own PR-Sheriff review comments but was also
  closed unmerged, with one comment incorrectly claiming the fix was
  already present on `main`.

## Final verdict

**implemented_replacement.** PR #4059 is not merged and will not be
reopened; its two in-scope bugs (#3828 items 2/3) are fixed by a new,
independently reviewed and tested replacement, **PR #4726**
(https://github.com/gastownhall/gastown/pull/4726), opened from a personal
fork with attribution preserved to the original reporter (Emmanuel
Sciara / esciara) via a `Co-authored-by` trailer. The commit is verified
correct (5/5 independent post-implementation reviews, 2 confirming
`go build`/`go vet`/`go test` pass firsthand), minimally scoped (exactly 5
files, no unrelated changes), and backed by a genuine, independently-
reproduced revert-test (fails pre-fix, passes post-fix). Branch hygiene is
clean (1 ahead / 0 behind `upstream/main`). No refinery/MQ route was used
for the target GitHub PR.

Required actions for the overseer (outside this Sheriff pass's write
access):
- Approve the pending CI workflow runs on PR #4726 (Test, Lint, Windows CI
  are `action_required` — standard first-time-fork-contributor gate).
- Post a short comment on #4059 and #4038 linking #4726, since both
  threads currently carry uncorrected false "already merged"/"already on
  main" claims.
- Apply `kind/bug`, `priority/p2`, `status/reviewing` (or
  `status/review-approved` once CI is green) labels to #4726.
- Merge #4726 once CI is green (this reviewer's fork account cannot merge
  into `gastownhall/gastown`).
