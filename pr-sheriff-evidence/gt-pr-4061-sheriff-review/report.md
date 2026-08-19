# PR Sheriff Review Report — PR #4061

Subject: gastownhall/gastown PR #4061 "fix: gt polecat prune --remote misses
squash/rebase merges (uses IsAncestor instead of patch-equivalence)"
https://github.com/gastownhall/gastown/pull/4061

Bead: gt-pr-4061-sheriff-review
Mode: confirmed_prior_disposition
No refinery/MQ route used for the #4061 disposition itself: true

## Summary

PR #4061 (author esciara / Emmanuel Sciara, branch
`issue-hunter/3779-gt-polecat-prune-remote-misses-squash-re`) proposed a real
fix: `gt polecat prune --remote` classified a branch as merged using
`IsAncestor` against `origin/main`, which returns false for branches merged
via squash or rebase because the original commit SHAs never land on main.
Stale polecat branches then accumulate forever on the remote. The proposed
fix — swap in a `git cherry` patch-id comparison — was directionally correct
and its own CI was fully green, but an inline PR-sheriff workflow comment
(2026-06-09) and an independent review bead (`gt-qdc9`, 2026-07-06) both
identified the same real gap: the PR's `git cherry` counting only proves
equivalence for a single-commit branch, and it resolved branches by name
rather than by exact push-remote hash, leaving it unsafe to merge as-is for
multi-commit squash merges and vulnerable to a stale-local-ref race during
prune.

A maintainer fixup pass (`gt-oae2`, brahmin, 2026-07-08/09) built a clean
replacement on current `main` that converges classification through a
three-tier helper — ancestor check, then a whole-tree merge-tree no-op proof
(the multi-commit-squash-safe check the review demanded), then `git cherry`
patch-equivalence as the last resort — resolves branches by exact push-remote
hash fetched fresh at prune time (failing closed if the hash changed under
it), and deletes only via `git push --force-with-lease` pinned to that exact
hash. That replacement, PR #4447 ("fix: prune patch-equivalent remote polecat
branches"), merged the same day with a fully green CI bar
(`2f6f14071afb49b71c0a41e03545b14158e7012c`). A maintainer (Bella-Giraffety)
then closed PR #4061 as superseded, preserving attribution to Emmanuel
Sciara via a `Co-authored-by` trailer on the merge commit.

This review pass did not take the close comment or the prior beads' own
records on faith. It independently re-verified every load-bearing fact: the
merge commit `2f6f14071` is confirmed a real ancestor of current
`upstream/main` (git-level, not just an API-reported field) and carries the
`Co-authored-by: Emmanuel Sciara` trailer; the three-tier classification
helper (`PushRemoteRefTargetStatus` → `preservationOfRefAgainstRef`:
ancestor → merge-tree-noop → cherry) is confirmed present in
`internal/git/git.go` and correctly wired into the `--remote` prune path
(`pruneRemotePolecatBranches` in `internal/cmd/polecat.go`), which resolves
branches via exact push-remote hashes (`ListPushRemoteRefsWithHashes`) rather
than stale local tracking refs, and deletes only via a hash-pinned
`force-with-lease` push; the six new regression tests — including a
dedicated multi-commit-squash test, the exact gap the prior review flagged —
are confirmed present AND were run directly (all pass) against a freshly
checked-out `upstream/main` worktree, not the review branch's own diverged
tree; `go build ./cmd/gt` succeeds on the same checkout; and PR #4447's own
CI (Test, Lint, Integration Tests, Windows Smoke Test, plus housekeeping
gates) was fully green at merge.

This pass also searched independently for related open GitHub issues rather
than relying on #4061's own "Closes #3779" reference, since that keyword
never fired (a PR that is closed rather than merged does not trigger
GitHub's auto-close). It found: **#3779**, the originating issue itself,
still OPEN; and **#4197**, an independently filed report (2026-06-07) of the
identical ancestry-vs-rebase-merge bug, also still OPEN and never linked to
#3779, #4061, or #4447 by number in either direction.

Sheriff wrote no code, opened no PR, and took no new GitHub-visible action on
PR #4061 or any issue in this pass (no comments, labels, or merges) — the
prior close was already correct and within a maintainer's authority. The only
change in this branch is the durable evidence record itself.

## Research legs (15/15)

See `evidence.json` → `evidence.research_legs` (R01–R15) for the full record:
#4061's current closed/superseded state and close-comment trail; its
original scope/intent as a 2-file, narrow fix; confirmation its own CI was
green (unlike the #4067 precedent); the inline PR-sheriff workflow comment
and the `gt-qdc9`/`gt-oae2` bead trail identifying the multi-commit-squash
and stale-local-ref gaps that made the original unsafe to merge/replace
as-is; independent git-level ancestry and attribution-trailer confirmation of
the merge commit; independent content-level confirmation of the three-tier
classification helper and its exact-hash-based wiring into the `--remote`
prune path (plus confirmation the one remaining raw `IsAncestor` call is in
an unrelated local-branch-prune path, out of scope); independent
regression-test presence-and-pass confirmation (including the dedicated
multi-commit-squash and split-push-remote-negative tests) run against a
fresh `upstream/main` worktree; #4447's green CI at merge; and a related-issue
search that surfaced both #3779 and its independent duplicate #4197 as open
and un-linked, plus two topically-adjacent-but-distinct issues (#4621, #3868)
confirmed to be different subsystems/commands.

## Pre-implementation / merge-decision reviews (5/5)

See `evidence.json` → `evidence.pre_implementation_reviews` (P01–P05):
cleanup-first/policy, correctness (don't trust self-reported prior evidence
alone), scope/authority, technical-defect disposition (verify the
multi-commit-squash fix is actually tested, not just claimed), and
evidence-completeness. All `approve` or `approve-with-changes`; the one
`approve-with-changes` correction — search independently for duplicate/
adjacent open issues beyond the PR's own closing-keyword reference, since
that keyword never fired — is already applied in `evidence.json` (found and
dispositioned #4197 in addition to #3779).

## Post-implementation reviews

Not applicable — this pass made no code change, replacement, or cherry-pick
(`implementation.code_changed_by_sheriff: false`). The 5 post-implementation
review tier is conditional on Sheriff-authored code changes per this bead's
own acceptance criteria and does not apply to a `confirmed_prior_disposition`
verdict.

## Checker output

See `checker.txt` in this directory. It records: the independent
merge-ancestry check (`git merge-base --is-ancestor`), independent
fix-content-presence and wiring checks across `git.go`/`polecat.go`, an
independent `go test`/`go build` run against a fresh detached
`upstream/main` worktree, the related-issue search results for #3779/#4197/
#4621/#3868, the (inapplicable, recorded for completeness only)
branch-hygiene gate output, and the raw `gh`/`git`/`bd` command output
backing R01–R15.

## Related GitHub issues — disposition

- **#3779** ("gt polecat prune --remote misses squash/rebase merges...") —
  OPEN. This is the originating issue PR #4061's body says it closes. Because
  #4061 was closed rather than merged, GitHub's auto-close keyword never
  fired, and replacement PR #4447 does not itself reference #3779 by number.
  The root cause it describes is confirmed fixed on current `upstream/main`
  by PR #4447 (see R08–R13 in `evidence.json`). **Recommended disposition:
  close as fixed by #4447**, noting the merge commit
  `2f6f14071afb49b71c0a41e03545b14158e7012c`. Sheriff is not closing this
  issue directly in this pass (no GitHub-visible action taken per skill.md —
  recommendation only, for the overseer to action).
- **#4197** ("Remote branch pruning misses rebased-merged polecat branches
  that are no longer ancestors of main") — OPEN. Filed independently on
  2026-06-07, 5.5 weeks after #3779, describing the identical root cause
  (ancestry-only check missing rebase-merged branches) from a separate
  observation. Never cross-linked to #3779, #4061, or #4447. **Recommended
  disposition: close as fixed by #4447, duplicate of #3779.** Sheriff took
  no direct GitHub action in this pass.
- **#4621** ("gt orphans measures 'commits ahead of main'...") — topically
  adjacent (polecat-branch-lifecycle correctness) but a confirmed distinct
  subsystem/command on reading its body: it concerns `gt orphans`'
  ahead-count *display* being misleading for empty branches, not
  `gt polecat prune --remote`'s merge *classification*. Not fixed by #4447.
  No linkage or action owed.
- **#3868** ("gt mq post-merge: remote branch deletion fails silently...") —
  already CLOSED independently; topically adjacent (remote polecat branch
  cleanup) but a distinct root cause (a silent deletion failure in the MQ
  post-merge path, not a missed-classification bug in the operator-driven
  `--remote` prune command). No linkage or action owed.

## Prior-attempt disposition

- **`gt-qdc9`** (vault's initial review): correctly identified PR #4061 was
  not safe to merge or replace as-is — its single-commit-only `git cherry`
  counting and reliance on stale local tracking-ref names left a
  multi-commit-squash gap and a hash-staleness race — and deferred to a
  human/fixup path (filing follow-up `hq-quuic`) rather than merging or
  requesting contributor changes.
- **`gt-oae2`** (brahmin's replacement): produced PR #4447, which fixes both
  gaps by adding a merge-tree no-op tier ahead of the cherry check and
  resolving branches via exact, freshly-fetched push-remote hashes; merged
  with full green CI, preserved attribution, and closed #4061 as superseded
  per its own recorded `required_followup`.

## Final verdict

**confirmed_resolved_no_action.** PR #4061 is fully and correctly resolved:
closed as superseded by an independently-authored, CI-green, merged
replacement (#4447) that fixes both the original ancestry bug and the
multi-commit-squash/stale-ref gaps a prior review found in the original PR's
own approach, with attribution preserved and both the fix and its dedicated
regression tests (including the multi-commit-squash case) verified live and
passing on current `upstream/main`. This review pass found nothing to
correct in the prior disposition and took no GitHub-visible action on the PR.
Two open GitHub issues — #3779 (the originating issue) and #4197 (an
independent duplicate) — are recommended for closure as fixed by #4447, since
neither was ever auto-closed or cross-linked. No refinery/MQ route was used
for the #4061 disposition.

Required actions:
- None for PR #4061 — fully resolved.
- Recommend the overseer close GitHub issue #3779 as fixed by PR #4447
  (merge commit `2f6f14071afb49b71c0a41e03545b14158e7012c`).
- Recommend the overseer close GitHub issue #4197 as fixed by PR #4447,
  duplicate of #3779.
