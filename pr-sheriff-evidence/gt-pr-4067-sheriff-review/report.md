# PR Sheriff Review Report — PR #4067

Subject: gastownhall/gastown PR #4067 "fix(done): accept ancestor commit when
verifying push to shared target branch"
https://github.com/gastownhall/gastown/pull/4067

Bead: gt-pr-4067-sheriff-review
Mode: confirmed_prior_disposition
No refinery/MQ route used: true

## Summary

PR #4067 (author KayoticSully / Ryan Sullivan, branch `fix/done-ancestry-check`)
proposed a real fix: `gt done` was rejecting `origin/main` pushes with
`verified_push_failed` whenever the shared target branch's tip advanced past
the polecat's own commit between push and verify (concurrent agent activity),
even though the commit was a genuine ancestor of the new tip. This caused
polecats to idle and accumulate as zombies. The fix idea — treat "commit is
tip-or-ancestor of the shared target" as success, while keeping exact-tip
verification for branches a caller owns exclusively — was sound, but the
PR's own implementation was not safe to merge as-is: it was CONFLICTING/DIRTY
against current `main`, had no maintainer approval (`status/needs-info`, no
review-approved label), never ran the Test/Lint/Build CI pipeline (only
label-automation checks fired), and — most importantly — its ancestry helper
read the branch tip via the *push* URL but fetched/verified against the
*fetch*-side `origin/<branch>`, which is a genuine correctness gap on
split fetch/push-URL remotes (the common case for polecats working against a
fork with an upstream push guard).

Three beads were dispatched against this PR over time. An initial review
(`gt-od73`, radrat, 2026-07-06) independently confirmed the bug was real and
not yet fixed on `main`, confirmed the PR was not mergeable as-is (merge
conflict in `internal/cmd/done.go`, no approvals), and specifically diagnosed
the split fetch/push-URL correctness gap in the PR's own helper — filing a
maintainer fixup/replacement bead rather than merging or requesting contributor
changes. That fixup (`gt-q88c`, pipboy, 2026-07-07) built a clean replacement
on current `main`, tying the ancestry check to the same push target being
verified (fixing the exact gap `gt-od73` found), carried a complete 15+5+5 PR
Sheriff evidence trail, and opened PR #4420 ("fix(done): verify pushed commits
against target branch ancestry"), which merged the same day with a fully green
CI bar (`4f6b9fe1351741eefd706b95f7420384b8de5609`). A maintainer
(Bella-Giraffety) then closed PR #4067 as superseded, preserving attribution to
KayoticSully, and separately verified the replacement's merge commit in a
follow-up PR comment.

This review pass did not take the close comment or the prior beads' own
records on faith. It independently re-verified every load-bearing fact: the
merge commit `4f6b9fe135` is confirmed a real ancestor of current
`upstream/main` (git-level, not just an API-reported field); the ancestor-aware
verifier (`VerifyPushedCommitReachableFromPushTarget`, correctly tying the
ancestry fetch to the *push*-target remote via `pushTarget()`/`PushRemoteBranchTip`
— the exact split-URL fix `gt-od73` called for) is confirmed present and wired
at all three shared-target-branch call sites in `done.go` (no-MR close, direct
merge, late direct merge), while the original exact-tip `VerifyPushedCommit` is
confirmed still used only for owned-branch/immediate-self-push contexts (the MQ
submit path and refinery's own post-merge writes); the four regression tests
(`TestVerifyPushedCommit`, `TestVerifyPushedCommitReachableFromPushTarget`,
`TestVerifyPushedCommitSplitURL`, `TestVerifyPushedCommitReachableFromPushTargetSplitURL`)
are confirmed present AND were run directly (all pass) against a freshly
checked-out `upstream/main` worktree, not the review branch's own diverged
tree; `go build ./cmd/gt` succeeds on the same checkout; attribution to
KayoticSully is preserved via a `Co-authored-by`-equivalent PR body attribution
section plus the close-comment trail; and PR #4420's own CI (Test, Lint,
Integration Tests, Windows Smoke Test, and both housekeeping gates) was fully
green at merge.

This pass also searched for related open GitHub issues and found one directly
on point: **#4188** ("gt polecat done: push-verify uses tip-equality not
ancestry — merged beads flagged incomplete, triggering re-dispatch churn"),
which describes the same root cause this fix addresses and is still OPEN. Its
most recent comment (2026-08-10) reports the no-MR variant still reproducing —
but on `gt 1.1.0` (commit `ebef3c851`, dated 2026-05-06), which independently
verified git ancestry confirms **predates** the fix commit `4f6b9fe135`
(2026-07-07) by two months. That comment reflects a pre-fix binary still in
field use, not a live gap on current `upstream/main`. See disposition below.

Sheriff wrote no code, opened no PR, and took no new GitHub-visible action in
this pass (no comments, labels, or merges) — the prior close was already
correct and within a maintainer's authority.

## Research legs (15/15)

See `evidence.json` → `evidence.research_legs` (R01–R15) for the full record:
#4067's current state and close-comment trail; its original scope/intent as a
3-file, narrow fix; why it was not safe to merge as-is (merge conflict, no
approvals, CI never ran against its own diff); the split fetch/push-URL
correctness gap `gt-od73` diagnosed in the PR's own helper; the complete prior
dispatch/review history across three beads (`gt-od73`, `gt-q88c`,
`gt-pr-sheriff-4067`); the replacement's own durable 15+5+5 evidence record
persisted in `gt-q88c`'s comments; #4420's merged state and body; independent
git-level ancestry confirmation of the merge commit; independent content-level
confirmation that the fix is a push-target-aware (not just tip-or-ancestor)
helper, correctly wired at all three shared-target-branch call sites and
nowhere else replacing the intentional exact-tip owned-branch/MQ-submit checks;
independent regression-test presence-and-pass confirmation (including the
dedicated split-URL tests) run against a fresh `upstream/main` worktree;
#4420's green CI at merge; attribution-preservation verification; a
related-issue search that surfaced #4188 as directly on point plus three
topically-adjacent-but-distinct open issues (#4699, #4197, #4439) confirmed to
be different subsystems; independent chronological verification that the
#4188 Aug-10 field report predates the fix; and the (inapplicable)
branch-hygiene gate's scope.

## Pre-implementation / merge-decision reviews (5/5)

See `evidence.json` → `evidence.pre_implementation_reviews` (P01–P05):
cleanup-first/policy, correctness, scope/authority, technical-defect
disposition (the split fetch/push-URL gap that made the original PR unsafe to
merge as-is), and evidence-completeness / schema fit. All `approve` or
`approve-with-changes`; the one `approve-with-changes` correction — add
independent git-level ancestry, content-level presence/wiring checks, and a
live `go test` run (including the split-URL regression tests) against a fresh
`upstream/main` worktree, rather than trusting the close comment or the prior
beads' own records alone — is already applied in `evidence.json`.

## Post-implementation reviews

Not applicable — this pass made no code change, replacement, or cherry-pick
(`implementation.code_changed_by_sheriff: false`). The 5 post-implementation
review tier is conditional on Sheriff-authored code changes per this bead's
own acceptance criteria and does not apply to a `confirmed_prior_disposition`
verdict.

## Checker output

See `checker.txt` in this directory. It records: the independent merge-ancestry
check (`git merge-base --is-ancestor`), independent fix-content-presence and
wiring checks across `git.go`/`done.go`, an independent `go test`/`go build`
run against a fresh detached `upstream/main` worktree, the chronology check
placing the #4188 field report before the fix, the (inapplicable, recorded for
completeness only) branch-hygiene gate output, and the raw `gh`/`bd` command
output backing R01–R15.

## Related GitHub issues — disposition

- **#4188** ("gt polecat done: push-verify uses tip-equality not ancestry —
  merged beads flagged incomplete, triggering re-dispatch churn") — OPEN,
  directly on point. The root cause and the no-MR-path symptom it describes are
  confirmed fixed on current `upstream/main` by PR #4420 (see research legs
  R08/R09/R10 in `evidence.json`). Its newest comment (2026-08-10) reports the
  no-MR variant still failing, but that report is against `gt 1.1.0`
  (`ebef3c851`, 2026-05-06), independently confirmed via `git merge-base
  --is-ancestor` to predate the fix commit (`4f6b9fe135`, 2026-07-07) by two
  months — a stale field binary, not a current-main regression.
  **Recommended disposition: close as fixed by #4420**, with a closing note
  that the fix ships in builds after `4f6b9fe135` / v-next-after-1.1.0 and
  that pre-fix binaries will still exhibit the symptom until upgraded. Sheriff
  is not closing this issue directly in this pass (no GitHub-visible action
  taken per skill.md — recommendation only, for the overseer to action).
- **#4699, #4197, #4439** — topically adjacent (all involve `gt done` /
  polecat-branch-lifecycle correctness) but confirmed distinct subsystems on
  reading each issue body: #4699 is about `gt done` closing the source bead on
  submit rather than on merge (a different signal-timing bug); #4197 is about
  `gt polecat prune --remote` failing to recognize rebase-merged branches
  (branch pruning, not push verification); #4439 is about `gt done`
  auto-rebase rewriting a branch a successor polecat still holds (a handoff/
  rebase-clobber bug). None reference PR #4067/#4420 or the ancestry-verify
  helper. No linkage or action owed.

## Prior-attempt disposition

- **`gt-od73`** (radrat's initial review): correctly identified the PR was not
  mergeable as-is (conflict, no approvals, CI never ran) and correctly
  diagnosed the split fetch/push-URL correctness defect in the PR's own
  proposed helper, filing a maintainer fixup bead rather than merging or
  requesting contributor changes.
- **`gt-q88c`** (pipboy's replacement): produced PR #4420, which fixes the
  split-URL gap by tying the ancestry fetch to the push-target remote, merged
  with full green CI, and carries its own complete 15+5+5 evidence trail
  (persisted in bead comments).

## Final verdict

**confirmed_resolved_no_action.** PR #4067 is fully and correctly resolved:
closed as superseded by an independently-authored, CI-green, merged
replacement (#4420), with the split fetch/push-URL defect that made the
original unsafe to merge actually fixed, attribution preserved, and both the
fix and its regression tests (including dedicated split-URL cases) verified
live and passing on current `upstream/main`. This review pass found nothing to
correct in the prior disposition and took no GitHub-visible action. One
directly-on-point open issue (#4188) is recommended for closure as fixed by
#4420 (see disposition above) but was not closed by Sheriff in this pass. No
refinery/MQ route was used.

Required actions:
- None for PR #4067 — fully resolved.
- Recommend the overseer close GitHub issue #4188 as fixed by PR #4420 (see
  disposition above); Sheriff took no direct GitHub action in this pass.
