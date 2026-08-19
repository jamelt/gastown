# PR Sheriff Review Report — PR #4124

Subject: gastownhall/gastown PR #4124 "fix(doctor): rig-config-sync accepts
rig.db == town.db as valid (gt-dky8e)"
https://github.com/gastownhall/gastown/pull/4124

Bead: gt-pr-4124-sheriff-review
Mode: confirmed_prior_disposition
No refinery/MQ route used: true

## Summary

PR #4124 (author athosmartins / polecat rictus, refinery temp-merge branch
for bead gt-dky8e) is already fully resolved. It failed CI at push time
(Test, Lint, and Windows Smoke Test all FAILURE), duplicated another
refinery temp-merge branch carrying the identical fix under a different
bead (#4122, bead gt-thv2d), and was correctly closed by a maintainer
(Bella-Giraffety) on 2026-07-10 as superseded by replacement PR #4457
("fix(doctor): accept deacon town DB in rig-config-sync"), which had
independently merged to `main` the day before (2026-07-09, commit
`e6b7c2368622edf490a425b2b261b20ac159a979`).

A third PR, #4123, carried the same underlying fix (hand-authored by
athosmartins directly, cherry-picked from an abandoned 16k-line "kitchen
sink" PR in the contributor's personal fork) but was closed by the author
himself the same day for an unrelated cross-repo-push-policy reason — a
genuinely distinct dead end, never part of the #4457 replacement chain.

Four prior beads dispatched to review PR #4124 (`gt-pr-sheriff-4124`,
`gt-pr-sheriff-4124-disposition`, `hq-9wb`, `hq-cv-apnxy`) were all
auto-closed stale by the reaper with no recorded findings — this is the
first pass to actually verify and record the disposition.

This review pass did not take the prior close comment on faith. It
independently re-verified every load-bearing fact: the merge commit
`e6b7c2368` is confirmed a real ancestor of current `upstream/main` (not
just an API-reported field); the described fix — accepting the town-wide
Dolt DB name for the `deacon` rig after the HQ shared-storage migration —
is confirmed still present in `rig_config_sync_check.go`; both regression
tests described in #4457's body (`...DeaconTownDoltDBNoMismatch` and
`...OrdinaryRigTownDoltDBMismatch`) are confirmed present in the test
file; attribution to athosmartins is preserved via matching
`Co-authored-by` trailers across the merge commit, #4457's body, and
#4122's independent close comment; and #4457's CI was fully green at
merge versus #4124's own CI failures.

Sheriff wrote no code, opened no PR, and took no new GitHub-visible action
in this pass (no comments, labels, or merges) — the prior close was
already correct and within a maintainer's authority.

## Research legs (17/15 — exceeds minimum)

See `evidence.json` → `evidence.research_legs` (R01–R17) for the full
record: #4124's current state and close-comment trail; its original
scope/intent as a refinery temp-merge branch; the two duplicate PRs
(#4122's identical-fix/identical-disposition twin, #4123's distinct
policy-closed dead end); the fork-origin "kitchen sink" PR context;
#4457's merged state and self-reported PR-Sheriff evidence; independent
git-level ancestry confirmation; independent content-level fix-presence
and regression-test-presence confirmation; attribution-trailer
verification across three sources; #4124's own failing CI (justifying the
replacement path); #4457's green CI; label-state review for #4124, #4122,
and the stale #4123; a same-morning sibling-PR citation confirmed as
policy-pattern-only (no code linkage); a competing/undiscovered-PR and
related-issue search; the prior stale-reaped review-dispatch history for
this exact PR; and the (inapplicable) branch-hygiene gate's scope.

## Pre-implementation / merge-decision reviews (5/5)

See `evidence.json` → `evidence.pre_implementation_reviews` (P01–P05):
cleanup-first/policy, correctness, scope/authority, duplicate/sibling
disposition, evidence-completeness / schema fit. All `approve` or
`approve-with-changes`; the one `approve-with-changes` correction (add
independent git-level and content-level checks rather than trusting the
GitHub API report or #4457's own body claims alone; set schema fields to
`confirmed_prior_disposition`/`closed`) is already applied in
`evidence.json`.

## Post-implementation reviews

Not applicable — this pass made no code change, replacement, or
cherry-pick (`implementation.code_changed_by_sheriff: false`). The 5
post-implementation review tier is conditional on Sheriff-authored code
changes per this bead's own acceptance criteria and does not apply to a
`confirmed_prior_disposition` verdict.

## Checker output

See `checker.txt` in this directory. It records: the independent
merge-ancestry check (`git merge-base --is-ancestor`), the independent
fix-content-presence check, the independent regression-test-presence
check, the (inapplicable, recorded for completeness only) branch-hygiene
gate output, and the raw `gh`/`bd` command output backing R01–R17.

## Related GitHub issues — disposition

- No open GitHub issue references PR #4124, bead `gt-dky8e`, or bead
  `gt-thv2d`. The one topically-adjacent issue, **#3058** ("`gt doctor
  --fix rig-config-sync` renames Dolt DB to prefix instead of rig name"),
  is already **CLOSED** and describes a different code path (the `Fix`/
  rename behavior) from #4124's acceptance-predicate change — neither
  #4124 nor #4457 claims to address it.
- **PR #4155** is cited in #4123's close comment purely as a same-morning
  "sibling pattern" (same cross-repo-push-policy closure reason); it is
  independently confirmed unrelated in code and already merged — no
  action tied to it.

## Duplicate-PR disposition

- **#4122** (refinery temp-merge, bead gt-thv2d): correctly closed as
  superseded by #4457, same as #4124.
- **#4123** (athosmartins' own hand-authored cherry-pick): correctly
  closed by the author for an unrelated cross-repo-push-policy reason,
  never merged, never part of the #4457 chain. Still carries a stale
  `status/needs-triage` label — flagged as a residual, non-blocking gap
  outside this review-only bead's authority.

## Final verdict

**confirmed_resolved_no_action.** PR #4124 is fully and correctly
resolved: closed as superseded by an independently-authored, CI-green,
merged replacement (#4457), with attribution preserved and both the fix
and its regression tests verified live on current `main`. This review
pass found nothing to correct in the prior disposition and took no
GitHub-visible action. No refinery/MQ route was used.

Required actions: none for PR #4124. Optional/low-priority: a maintainer
could correct PR #4123's stale `status/needs-triage` label (out of this
review-only bead's authority).
