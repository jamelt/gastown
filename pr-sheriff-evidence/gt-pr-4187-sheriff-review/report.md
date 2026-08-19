# PR Sheriff Review Report — PR #4187

Subject: gastownhall/gastown PR #4187 "fix(refinery): surface orphan MRs + route source-issue close to correct DB"
https://github.com/gastownhall/gastown/pull/4187

Bead: gt-pr-4187-sheriff-review
Mode: existing_replacement_identified
No refinery/MQ route used: true

## Summary

PR #4187 (author Cdfghglz) is unmergeable as-is: `mergeable=CONFLICTING`,
775 commits behind upstream/main and 22 ahead (BLOCK per
`gt pr-sheriff-check --merge-gate --base upstream/main`, block threshold
200 behind). Of 70 changed files / +4195/-494, only ~5 files / ~260 lines
relate to the stated fix; the rest is stale drift from a ~3-month-old base.
It is already correctly labeled `status/review-failed`.

A clean, independently-authored replacement already exists: PR #4450
"fix(refinery): route source issue closes and surface malformed MRs"
(author Bella-Giraffety, opened 2026-07-08). 4 files, +194/-1,
`mergeable=MERGEABLE`/`mergeStateStatus=CLEAN`, all CI green and current
(head `143540a6a5`), with real targeted tests. It carries forward the
malformed-MR-surfacing and cross-DB PostMerge-routing fixes from #4187 at a
more convergent layer (a single shared `beads.ForceCloseWithReason` routing
fix instead of #4187's per-call-site diff). It does not carry over #4187's
"log rig-mismatch skips" claim (non-blocking gap), and its sole commit lacks
a `Co-authored-by` trailer for @Cdfghglz (PR body credits them in prose
only).

Sheriff did not write code, open a replacement, or cherry-pick anything —
it identified, verified, and evaluated the pre-existing #4450. This review
pass independently re-confirmed every load-bearing fact in the evidence
record on 2026-08-19: PR #4187/#4450 state, divergence counts (via both
`gt pr-sheriff-check` and raw `git rev-list --left-right --count`), and
#4450's CI check results — all unchanged since the record was generated.

## Research legs (15/15)

See `evidence.json` → `evidence.research_legs` (R01–R15) for the full
record: malformed-MR warning claim, rig-mismatch-skip logging claim,
cross-DB PostMerge routing claim, #4450 test coverage & CI currency,
#4450 branch cleanliness/scope, #4187 contamination characterization,
#4450 attribution compliance, #4450 review blockers, nmi-rt5dt root-cause
disposition, competing-replacement search, label policy compliance, #4450
staleness vs current main, #4187 branch dependency/safety, branch-hygiene
quantification, and policy fit for close-as-superseded.

## Pre-implementation / merge-decision reviews (5/5)

See `evidence.json` → `evidence.pre_implementation_reviews` (P01–P05):
cleanup-first/policy, correctness, scope/authority, evidence-completeness /
schema fit, verification rigor. All `approve` or `approve-with-changes`;
each `approve-with-changes` correction (do not request contributor changes;
explicitly disposition nmi-rt5dt; keep `implementation.modified_paths`
empty since Sheriff wrote nothing; skip a redundant local CI re-run) is
already applied in `evidence.json`.

## Post-implementation reviews

Not applicable — this pass made no code change, replacement, or cherry-pick
(`implementation.code_changed_by_sheriff: false`). The 5 post-implementation
review tier is conditional on Sheriff-authored code changes per this bead's
own acceptance criteria, and does not apply to an
`existing_replacement_identified` disposition.

## Checker output

See `checker.txt` in this directory: `gt pr-sheriff-check --merge-gate
--base upstream/main` against PR #4187's branch (BLOCK, 775 behind/22
ahead, exit 1) and against PR #4450's branch (WARN, 57 behind/1 ahead,
`merge_path_allowed: true`, exit 0). Both independently cross-checked via
`git rev-list --left-right --count` against a fresh clone — exact match.

## Related GitHub issues — disposition

- **nmi-rt5dt**: a cross-rig Beads issue ID (not a GitHub issue), referenced
  only in #4187's branch name/commit message. Not resolvable from this
  machine (different rig, not locally routable) and no GitHub issue
  references the string. Disposition: stays open in its owning rig's Beads
  DB, referencing #4450, until #4450 merges and #4187 is closed as
  superseded.
- No other GitHub issue references #4187 or #4450.

## Final verdict

**defer_to_existing_replacement.** PR #4187 cannot be merged or cleanly
rebased (CONFLICTING, 775 behind block-threshold). PR #4450 is a clean,
CI-green, independently-authored replacement that already carries forward
the accepted bug-fix intent at a more convergent code layer. Per this
town's replacement-PR precedent (`docs/pr-sheriff/pr-4373-replacement-
evidence.json`), closure of a superseded original defers until the
replacement actually merges, not merely upon its existence — so #4187
stays open and un-actioned by Sheriff. No GitHub-visible action (comment,
label, merge) was taken; that exceeds this review-only bead's granted
authority and belongs to a human maintainer. No refinery/MQ route was
used.

Required actions for a human maintainer/overseer:
1. Review and, if satisfied, merge PR #4450, adding a `Co-authored-by`
   trailer for @Cdfghglz at merge time.
2. After #4450 merges: close PR #4187 as superseded, referencing #4450.
3. After that: close nmi-rt5dt in its owning rig's Beads DB, referencing
   #4450.
4. Optional: file a small follow-up for rig-mismatch-skip logging if still
   wanted (not present in #4450).
5. Optional: correct #4450's stale `status/reviewing` label once a human
   actually reviews it.
