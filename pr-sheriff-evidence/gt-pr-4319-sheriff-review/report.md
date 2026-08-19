# PR Sheriff Review Report — PR #4319

Subject: gastownhall/gastown PR #4319 "fix: cap dolt sql-server Go runtime memory"
https://github.com/gastownhall/gastown/pull/4319

Bead: gt-pr-4319-sheriff-review
Mode: audit_already_merged
No refinery/MQ route used: true

## Summary

PR #4319 was already MERGED into main (merge commit `cb098cb41`, 2026-07-07,
merged by Bella-Giraffety via a standard GitHub PR merge) before this review
bead was hooked and dispatched. It is a maintainer replacement carrying
forward contributor PR #4132 (certivpaul), which had been closed
`status/review-failed`. A prior PR Sheriff fixup bead (`gt-5mv4`) already ran
a full 15-research + 5-pre-implementation + 5-post-implementation evidence
gauntlet on this PR's code before merge, with CI green.

This review pass did **not** find any need for a code fixup, replacement, or
cherry-pick. Its job was to independently re-verify the merged state and
disposition any related GitHub issues, then close out with durable evidence.

## Research legs (15/15)

| Leg | Topic | Finding |
|-----|-------|---------|
| R1 | Fix present on main | Confirmed: `NewSQLServerCommand` centralizes construction, used by both daemon and manual paths, with `GOMEMLIMIT=16GiB`/`GOGC=50` defaults. |
| R2 | Build/vet | PASS, clean, no warnings. |
| R3 | Focused + broad tests | PASS (exit 0) for focused tests, `internal/daemon` (218s), and broad `internal/doltserver` (19.9s, no flaky failures this run). |
| R4 | Relation to issue #4145 (CPU pegging) | Unrelated: issue predates PR by 5+ weeks; GC-frequency tuning cannot explain sustained CPU pegging on a ~40MB dataset. |
| R5 | Related-issue/PR search | Found PR #4295, a competing GOMEMLIMIT fix, already closed/superseded. No open issue references GOMEMLIMIT/GOGC. |
| R6 | Codecov gap (1 line) | Expected/non-testable gap (real subprocess-launch line behind a test-override field), not a risk. |
| R7 | Attribution | Confirmed: both merged commits carry `Co-authored-by: certivpaul <...>` and Based-on/Original-commit trailers. |
| R8 | PR #4132 disposition | Already closed with an explicit "superseded by merged #4319" comment; no action needed. |
| R9 | Label taxonomy (issue #1400) | `kind/bug`/`priority/p2` compliant; `status/merged` is outside #1400's documented status/* list — a documentation gap, non-blocking, no write action taken. |
| R10 | Windows env-matching correctness | Scan logic correctly checks the whole env slice; a pre-existing (not introduced here), untested edge case exists for duplicate-case env keys — noted, not blocking. |
| R11 | Refinery/MQ compliance | Confirmed standard GitHub PR merge (human `mergedBy`, canonical merge-commit format), not Refinery/MQ. |
| R12 | Predecessor bead `gt-5mv4` status | Closed; its own evidence gauntlet covered the code at final head `16e1e2cbb`. |
| R13 | Merge-gate BLOCK → merge timing | ~28h gap between BLOCK finding and merge, same actor — normal remediation, not a bypass. |
| R14 | Cross-reference sweep | No other issues/PRs reference #4319 or #4132. |
| R15 | Post-merge CI on main | Green at the merge commit; no revert in 30+ subsequent commits; one complementary follow-up (`376b8267e`) frames the memory cap as a valid backstop. |

## Pre-implementation / merge-decision reviews (5/5)

| Leg | Lens | Verdict |
|-----|------|---------|
| P01 | Correctness | APPROVE |
| P02 | Policy compliance | APPROVE |
| P03 | Cleanup-first | APPROVE |
| P04 | Evidence completeness | REJECT → addressed (this report + evidence.json + checker.txt written) |
| P05 | Scope / contributor policy | APPROVE |

## Post-implementation reviews

Not applicable — this pass made no code changes, replacement, or
cherry-pick. `gt-5mv4` already ran its own 5 post-implementation reviews
against final head `16e1e2cbb` before merge.

## Checker output

See `checker.txt` in this directory (`gt pr-sheriff-check --merge-gate --json`
run against the review worktree). Result: clean, 0 ahead / 2 behind
origin/main, `merge_path_allowed: true`, exit 0. No branch contamination; no
replacement PR was created or needed since the fix is already merged.

## Related GitHub issues — disposition

- **#4132** (original contributor PR): already closed as superseded by
  merged #4319, attribution preserved. No action.
- **#4145** (Runaway Dolt SQL server CPU pegging): assessed and confirmed
  **unrelated** in root cause (predates this PR, CPU vs. memory mechanism
  mismatch). Not linked as fixed-by or blocking. Left open, undisturbed.
- **#4295** (competing GOMEMLIMIT fix): already closed/superseded by #4319.
  No action.
- No other issue or PR references #4319 or #4132.

## Final verdict

**confirmed_merged_no_action_needed.** PR #4319 is correctly merged, its fix
is verified present and correct on main, tests and CI are green, attribution
is preserved, and no related GitHub issue requires disposition beyond what
is already recorded. No code, label, or GitHub write action was taken by
this review pass. No refinery/MQ route was used.
