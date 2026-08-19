# PR Sheriff Review Report — PR #4130

Subject: gastownhall/gastown PR #4130 "fix: make worktree .beads strictly redirect-only (refs #2682, #4033)"
https://github.com/gastownhall/gastown/pull/4130

Bead: gt-pr-4130-sheriff-review
Mode: confirmed_prior_disposition
No refinery/MQ route used: true

## Summary

PR #4130 (author csauer02-personal-user / Chris Sauer) is already fully
resolved. It failed CI at push time (Test, Lint, Integration Tests, and
Windows Smoke Test all FAILURE), was correctly left open pending a
replacement, and was closed by a prior PR Sheriff pass on 2026-07-08 as
superseded by replacement PR #4425, which merged to `main` in the same
minute (commit `3b9c2bf73939bad7c6744eddf12d0c5882923cc7`). An intermediate
replacement attempt, PR #4320, never merged (its own body cites a GitHub
HTTPS auth push failure) and was itself superseded by #4425.

This review pass did not take that prior disposition on faith. It
independently re-verified every load-bearing fact: the merge commit
`3b9c2bf73` is confirmed a real ancestor of current `upstream/main` (not
just an API-reported field); the described fix — stripping
`metadata.json`/`config.yaml` from a worktree `.beads` so `bd` cannot bind
to a stale/wrong `dolt_database` — is confirmed still present in
`beads_redirect.go` on current `main`, ~6 weeks after merge; #4425's sole
commit carries correct `Co-authored-by` trailers crediting Chris Sauer by
his exact original email; and #4425's CI was fully green at merge time.

Sheriff wrote no code, opened no PR, and took no new GitHub-visible action
in this pass (no comments, labels, or merges) — the prior close was
already correct and within a maintainer/prior-Sheriff flow's authority.

## Research legs (15/15)

See `evidence.json` → `evidence.research_legs` (R01–R15) for the full
record: #4130 current state and close-comment trail, original scope/intent,
intermediate replacement #4320's fate, final replacement #4425's merged
state, independent git-level ancestry confirmation, independent
content-level fix-presence confirmation, attribution-trailer verification,
#4130's own failing CI (justifying the replacement path), #4425's green CI,
label-state review for both #4130 and the stale #4320, related-issue
disposition for #2682 and #4033, a competing/undiscovered-replacement
search, and the (inapplicable) branch-hygiene gate's scope.

## Pre-implementation / merge-decision reviews (5/5)

See `evidence.json` → `evidence.pre_implementation_reviews` (P01–P05):
cleanup-first/policy, correctness, scope/authority, evidence-completeness /
schema fit, verification rigor. All `approve` or `approve-with-changes`;
the two `approve-with-changes` corrections (use `confirmed_prior_disposition`
/ `closed` instead of reusing the #4187 precedent's in-flight-deferral
schema verbatim; add independent git-level checks rather than trusting the
GitHub API report alone) are already applied in `evidence.json`.

## Post-implementation reviews

Not applicable — this pass made no code change, replacement, or cherry-pick
(`implementation.code_changed_by_sheriff: false`). The 5 post-implementation
review tier is conditional on Sheriff-authored code changes per this bead's
own acceptance criteria and does not apply to a `confirmed_prior_disposition`
verdict.

## Checker output

See `checker.txt` in this directory. The branch-hygiene gate
(`gt pr-sheriff-check --merge-gate`) is scoped to gating a *new*
Sheriff-authored replacement PR before it is opened; it does not apply here
since no replacement is being created by this pass (#4425 already exists,
independently authored and merged). Its output is recorded for completeness
only and excluded from the verdict. In its place, `checker.txt` records two
independent verification commands: `git log upstream/main --oneline | grep
3b9c2bf73` (merge-commit ancestry) and `git show upstream/main:internal/
beads/beads_redirect.go` (fix-content presence), both confirming the merge
is real and live on current `main`.

## Related GitHub issues — disposition

- **#2682** (`bd init` split-brain database naming): **remains open**,
  correctly. #4130's own PR body explicitly scopes this broader root-cause
  class out ("Follow-up (not in this PR)"). A separate merged PR (#3951,
  2026-05-13) addressed only the Gastown-managed subset per its own triage
  note; the `bd`-level naming-convention root cause is still unresolved.
  Neither #4130 nor #4425 claims to fix #2682.
- **#4033** (Dolt `ComInitDB` uses rig/role short-code as DB name):
  **remains open**, correctly. Distinct code path (Dolt DSN construction)
  from #4130's worktree-`.beads`-redirect-cleanup fix; untouched by either
  #4130 or #4425.
- No other GitHub issue references #4130, #4320, or #4425.

## Final verdict

**confirmed_resolved_no_action.** PR #4130 is fully and correctly resolved:
closed as superseded by an independently-authored, CI-green, merged
replacement (#4425), with attribution preserved and the fix verified live
on current `main`. This review pass found nothing to correct in the prior
disposition and took no GitHub-visible action. Related issues #2682 and
#4033 are correctly left open as separate, out-of-scope root-cause tracking
issues. No refinery/MQ route was used.

Required actions: none for PR #4130. Optional/low-priority: a maintainer
could correct the stale `status/reviewing` label still sitting on the
never-merged intermediate replacement PR #4320 (label edits are outside
this review-only bead's authority).
