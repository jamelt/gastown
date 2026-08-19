# PR Sheriff Review Report — PR #4103

Subject: gastownhall/gastown PR #4103 "Auto-repair broken idle polecat
worktree with nuke"
https://github.com/gastownhall/gastown/pull/4103

Bead: gt-pr-4103-sheriff-review
Mode: confirmed_prior_disposition
No refinery/MQ route used: true

## Summary

PR #4103 (author Jacob-qd, branch `mayor/sling-worktree-preflight`) is
already fully resolved. It proposed a real fix — auto-nuke a structurally
broken idle polecat worktree and fall through to fresh allocation — but its
original placement was unsafe on current main: nuking after idle-candidate
reuse could race with hook assignment and burn/detach a polecat's live work
molecule. The PR's own diff never reached the Test/Lint/Build CI pipeline
(only housekeeping label-automation checks fired). It was correctly closed
by a maintainer (Bella-Giraffety) on 2026-07-08 as superseded by replacement
PR #4429 ("fix: reclaim broken idle polecat worktrees"), which merged to
`main` the same day (commit `2e7abbed0984814814dc60d3388788c938c8f1cd`).

Six beads have been dispatched against this PR over time. An early review
(`gt-mgt5`, guzzle, 2026-07-06) correctly diagnosed the hook-assignment-
ordering hazard and recommended a maintainer replacement rather than a
merge-as-is. A first replacement attempt (`gt-75z1`, nuka) was rejected by
mayor review before it could merge or open a PR: its branch had diffed
~123 files against current `upstream/main`, far beyond the idle-worktree-
reclaim scope. Only its narrow final commit was kept as optional input. A
second, clean replacement (`gt-2d1i`, radrat) was built fresh from current
`origin/main`, carried a complete 15+5+5 PR Sheriff evidence trail
(persisted in that bead's `design` field), and produced PR #4429, which
merged with a fully green CI bar.

This review pass did not take the close comment, or `gt-2d1i`'s own record,
on faith. It independently re-verified every load-bearing fact: the merge
commit `2e7abbed0` is confirmed a real ancestor of current `upstream/main`
(not just an API-reported field); the described fail-closed reclaim logic
(`reclaim.go`) and structural-integrity detector (`worktree.go`) are
confirmed present and correctly wired **pre-hook** in the spawn/allocation
loop (`polecat_spawn.go`, `manager.go`) — the exact ordering fix the
original PR got wrong; the regression tests (`reclaim_test.go`,
`worktree_test.go`, plus `TestReuseIdlePolecat`) are confirmed present AND
were run directly (all pass) against a freshly checked-out `upstream/main`
worktree, not the review branch's own diverged fork tree; attribution to
Jacob-qd is preserved via matching `Co-authored-by` trailers across the
merge commit and #4429's own body; and #4429's CI was fully green at merge.

Sheriff wrote no code, opened no PR, and took no new GitHub-visible action
in this pass (no comments, labels, or merges) — the prior close was already
correct and within a maintainer's authority.

## Research legs (15/15)

See `evidence.json` → `evidence.research_legs` (R01–R15) for the full
record: #4103's current state and close-comment trail; its original
scope/intent as a single-file, narrow fix; why it was not safe to merge
as-is (hook-assignment-ordering hazard, never ran full CI); the complete
prior dispatch/review history for this exact PR across six beads; the
replacement's own durable 15+5+5 evidence record persisted in `gt-2d1i`;
#4429's merged state and body; independent git-level ancestry confirmation;
independent content-level fix-presence confirmation across all four touched
non-test files; confirmation the reclaim call sits strictly pre-hook in the
spawn loop; independent regression-test presence-and-pass confirmation run
against a fresh `upstream/main` worktree; #4429's green CI at merge;
confirmation the rejected contaminated attempt (`gt-75z1`) never leaked
into the merged diff; attribution-trailer verification across independent
sources; a competing/undiscovered-PR and related-issue search (including a
topically-adjacent open issue, #3998, confirmed to be a different
subsystem/failure mode); and the (inapplicable) branch-hygiene gate's
scope.

## Pre-implementation / merge-decision reviews (5/5)

See `evidence.json` → `evidence.pre_implementation_reviews` (P01–P05):
cleanup-first/policy, correctness, scope/authority, safety-hazard
disposition (the actual reason #4103 wasn't merged as-is), and
evidence-completeness / schema fit. All `approve` or `approve-with-changes`;
the one `approve-with-changes` correction — add independent git-level
ancestry, content-level presence, and a live `go test` run against a fresh
`upstream/main` worktree, rather than trusting the prior bead record or CI
logs alone — is already applied in `evidence.json`.

## Post-implementation reviews

Not applicable — this pass made no code change, replacement, or
cherry-pick (`implementation.code_changed_by_sheriff: false`). The 5
post-implementation review tier is conditional on Sheriff-authored code
changes per this bead's own acceptance criteria and does not apply to a
`confirmed_prior_disposition` verdict.

## Checker output

See `checker.txt` in this directory. It records: the independent
merge-ancestry check (`git merge-base --is-ancestor`), independent
fix-content-presence checks across `reclaim.go`/`worktree.go`/
`manager.go`/`polecat_spawn.go`, an independent `go test`/`go build` run
against a fresh detached `upstream/main` worktree, the (inapplicable,
recorded for completeness only) branch-hygiene gate output, and the raw
`gh`/`bd` command output backing R01–R15.

## Related GitHub issues — disposition

- No open GitHub issue references PR #4103 or PR #4429 by number.
- **#3998** ("Witness patrol scan flags healthy idle polecats ... as
  zombies") is the one topically-adjacent open issue. Confirmed distinct:
  it describes `gt patrol scan` misclassifying **healthy** idle polecats
  with unmerged commits as dead, a monitoring/detection false-positive bug
  in a different subsystem (patrol scan) — not the **structurally broken**
  worktree case PR #4103/#4429 fixes at spawn/allocation time. No linkage
  or action owed.

## Prior-attempt disposition

- **`gt-75z1`** (nuka's replacement attempt): correctly rejected by mayor
  review before merging or opening a PR — branch was contaminated with
  ~123 unrelated files. Only its narrow, relevant final commit was reused
  as input to the clean replacement. Confirmed the merged PR #4429 diff (7
  files, all directly relevant to idle-worktree reclaim) carries no trace
  of the contamination.
- **`gt-2d1i`** (radrat's clean replacement): produced PR #4429, merged
  with full green CI and its own complete 15+5+5 evidence trail.

## Final verdict

**confirmed_resolved_no_action.** PR #4103 is fully and correctly resolved:
closed as superseded by an independently-authored, CI-green, merged
replacement (#4429), with attribution preserved and both the fix and its
regression tests verified live and passing on current `upstream/main`. This
review pass found nothing to correct in the prior disposition and took no
GitHub-visible action. No refinery/MQ route was used.

Required actions: none for PR #4103.
