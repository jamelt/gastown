# PR Sheriff Review Report — PR #4066

Subject: gastownhall/gastown PR #4066 "fix(formula): resolve extends/compose in
prime + inject issue= sling var" (author flipch)
https://github.com/gastownhall/gastown/pull/4066

Bead: gt-pr-4066-sheriff-review
Mode: replacement_pr
No refinery/MQ route used: true

## Summary

PR #4066 correctly diagnosed two real bugs: (1) `gt prime --hook` never
resolved `extends`/`compose` before rendering formula steps, so any formula
using them (all `wc26-*` formulas) rendered **zero** steps; (2) several sling
code paths didn't store `issue=<id>` in the bead's `attached_vars`/
`formula_vars`, so `{{issue}}` placeholders rendered literally. But the PR's
own diff is ~3 months stale, its `Test` CI check is failing, it was never
reviewed, and it's labeled `status/review-failed` — it cannot merge as-is.

A prior Sheriff pass (bead `gt-pvy7`, polecat nuka, 2026-07-08/09) already ran
its own 15+5+5 protocol and opened replacement PR #4445. Independent
verification in this pass found #4445 is now **stale/CONFLICTING** against
current `main` (6 weeks untouched), and its sole commit is `WIP: checkpoint
(auto)` with **no real `Co-authored-by` trailer** for flipch, despite the PR
body's own claim that "attribution was preserved." The claim was never
backed by the actual git history.

This pass independently re-derived both bugs from first principles against a
fresh `upstream/main` (649b832b7), rather than trusting either #4066's or
#4445's account. Findings: bug 1 (`formula.Resolve()` never called) is
confirmed still present, at a single shared choke point
(`resolveFormulaForRendering`) rather than the two separate call sites #4066
and #4445 had to fix, since the codebase consolidated in the interim. Bug 2
is confirmed **already fixed** on current `main` for the bond-to-base-bead
sling paths (`sling.go`, `sling_dispatch.go`) via an unrelated refactor
(`formulaVarsForBead` now unconditionally prepends `issue=<beadID>`) — but
still present for the standalone-formula-sling path (`sling_formula.go`).

A minimal fix was implemented, restricted to exactly these two remaining
gaps: one line calling the pre-existing `formula.Resolve()` at the shared
rendering choke point (reusing the existing exported function directly, no
new API — smaller and more converged than #4445's approach of adding a new
`formula.ResolveFormula()` wrapper), and one line prepending
`issue=<wispRootID>` in `sling_formula.go`. `sling.go`/`sling_dispatch.go`
were deliberately left untouched.

Two regression tests were added/extended and independently confirmed (via
`git stash`, twice — once by this bead directly, once again by an
independent post-implementation review agent working from a fresh clone) to
**fail** on the pre-fix code with the exact described symptoms, and **pass**
with the fix. `go build`/`go vet`/`go test` all pass locally. `gt
pr-sheriff-check --merge-gate --base upstream/main` reports clean (1 ahead,
0 behind).

A fresh replacement PR, **#4725**, was opened from current `upstream/main`
with a real commit message and a genuine `Co-authored-by: flipch` trailer.
Stale replacement #4445 was commented on recommending closure as
superseded-by-defect (this bot account lacks GitHub permission to close it
directly — confirmed via a real `403`/GraphQL-permission error, not
assumed). Original #4066 was left open and untouched, per policy (Sheriff
does not unilaterally close a contributor's own PR); a maintainer should
close it once #4725 merges.

PR #4725's GitHub CI workflows (Test, Lint, Integration Tests, Windows Smoke
Test) are gated on maintainer approval (`action_required`), which is normal
for a fork-originated PR on this repo and not a code defect — this was
independently substituted for by two separate local verification passes
(fresh clone + direct build/vet/test) against the exact head commit, both
green.

## Research legs (16/15)

See `evidence.json` → `evidence.research_legs` (R01–R16) for the full
record: #4066's current state, CI, and comment trail; its full prior-dispatch
history (6 beads); a direct read of #4066's own diff (not reconstructed
secondhand); the prior Sheriff pass's own record and its replacement #4445's
current staleness/conflict state and commit-quality defect; a direct read of
#4445's diff; independent confirmation that `formula.Resolve()` exists but
has zero callers in `internal/cmd/` on current `upstream/main`; identification
of the current single shared rendering choke point; independent confirmation
that the `sling.go`/`sling_dispatch.go` gap is already closed via an
unrelated refactor while the `sling_formula.go` gap remains; a read-side
trace confirming the correct fix location; a proof that `formula.Resolve()`'s
no-op branch is safe for non-extends formulas (since `Parse()` already calls
`Validate()`); a proof that `loadFormulaByName` already tries embedded
formulas first, making the on-disk-only `formulaSearchPaths` helper
sufficient; and a related-GitHub-issue sweep (issue #3322, confirmed distinct
and already resolved on current main).

## Pre-implementation / merge-decision reviews (5/5)

See `evidence.json` → `evidence.pre_implementation_reviews` (P01–P05):
correctness, cleanup-first/no-bandaid, safety/regression-risk,
scope/policy/attribution, and evidence-completeness. Two of the five raised
concrete, narrow objections on the first pass — P01 (does the fix miss
embedded-only extends parents?) and P03 (could re-running `Validate()`
newly reject a formula `Parse()` currently accepts?) — both were resolved
with direct evidence before writing any code (R15 and R14 respectively), not
waved through. P04 raised the #4445-disposition gap, which was incorporated
into the plan. P05 flagged that #4066's own diff had never actually been
read; it was fetched and read in full (R04) before finalizing the
implementation plan. All five are resolved/confirmed in the final record.

## Post-implementation reviews (5/5)

See `evidence.json` → `evidence.post_implementation_reviews` (PI01–PI05),
tied to PR #4725 head `3e04c43f43b57a68dab719afab7c8e1112eff353`:
correctness (independently rebuilt the head, confirmed the diff hunks match
the claim, independently reproduced the fail-then-pass regression-test
evidence a second time from scratch), regression/build verification (fresh
clone, independent `go build`/`vet`/`test`, confirmed the formula package's
own `Resolve()` short-circuit is unmodified and safe), cleanup-first/scope
discipline (confirmed the diff is smaller and less additive than #4445's),
policy/attribution/disposition (confirmed no unauthorized closures, real
attribution trailers, no MQ route), and evidence-completeness (confirmed
#4725 is live, re-verified issue #3322 is genuinely a different bug,
ran an independent related-issue sweep). All 5/5 CONFIRM.

## Checker output

See `checker.txt` in this directory: subject-PR state and CI; direct
confirmation that `formula.Resolve` has zero callers pre-fix; direct
confirmation of the already-fixed `sling.go`/`sling_dispatch.go` paths and
the still-broken `sling_formula.go` path; #4445's staleness/conflict and
commit-message/attribution defect; local build/vet/test output on the fix
branch; the `git stash`-based fail-then-pass regression evidence; the
branch-hygiene gate result; the replacement PR's metadata, CI-gating state,
and real commit trailers; the permission error encountered attempting to
close #4445 directly; and the related-issue disposition.

## Related GitHub issues — disposition

- No open GitHub issue references PR #4066 by number in its own body, in
  #4445's body, or in a dedicated search sweep.
- **#3322** ("gt prime showFormulaStepsFull only reads embedded formulas —
  custom formulas invisible to polecats") is topically adjacent (same
  function neighborhood) but a genuinely different bug: a missing
  embedded-vs-disk **fallback tier**, not a missing extends/compose
  **resolution** step. Confirmed via direct inspection of
  `internal/formula/embed.go` that `ResolveFormulaContent` already
  implements the reported-missing rig→town→embedded 3-tier resolution on
  current `upstream/main` — independently of this PR chain. No action
  needed on #3322 from this review; noted as already-resolved/unrelated.

## Prior-attempt disposition

- **`gt-pvy7`** (nuka's prior Sheriff pass): correctly diagnosed both bugs
  and ran its own 15+5+5 protocol, but its replacement PR #4445 has since
  gone stale/conflicting and carries a commit-attribution defect. Commented
  on #4445 recommending maintainer closure as superseded-by-defect; direct
  closure attempted and blocked by a genuine GitHub permissions error (this
  bot's fork identity has no close-PR rights on `gastownhall/gastown`).

## Final verdict

**replacement_pr_opened_pending_maintainer_action.** PR #4066 correctly
identifies two real bugs, one of which (`formula.Resolve()` never called)
remains unfixed on current `main`. A fresh, minimal, cleanup-first
replacement — **PR #4725** — has been opened from current `upstream/main`
with genuine contributor attribution, passing local build/vet/test, and two
regression tests independently verified (twice) to fail pre-fix and pass
post-fix. `merge_path_allowed: false` for this Sheriff pass — not because
the fix is wrong, but because merging requires a human maintainer to approve
CI on a fork PR and review/merge it, which is outside Sheriff's GitHub
permissions. No refinery/MQ route was used.

Required actions (for a maintainer, not this bead): approve/trigger CI on
#4725; review and merge it; close #4066 as superseded; close #4445 as
superseded-by-defect (comment already posted).
