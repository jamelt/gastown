# PR Sheriff Review Report — PR #4189

Subject: gastownhall/gastown PR #4189 "Fix convoy stranded blocked
dependency decoding"
https://github.com/gastownhall/gastown/pull/4189

Bead: gt-pr-4189-sheriff-review
Mode: confirmed_prior_disposition
No refinery/MQ route used for the #4189 disposition itself: true

## Summary

PR #4189 (author fengning-starsend, branch
`fix/convoy-stranded-blocked-deps`) proposed a real fix: `gt convoy
stranded --json` decoded a dependency's relation type only from
`dependency_type`, so a `bd show --json` edge that instead carried `type`
(e.g. `type:"blocks"`) was silently treated as non-blocking, letting a
still-blocked tracked issue be reported ready and dispatched before its
blocker closed. The proposed fix — decode both fields in
`internal/cmd/convoy.go`'s stranded-readiness path and reuse the existing
blocking predicate — was directionally correct, but scoped narrowly to that
one consumer, and the PR's own CI showed Test/Lint/Integration Tests as
FAILURE (the contributor's own comment attributes this to pre-existing,
unrelated failures elsewhere in the tree).

A sheriff pass under bead `gt-7yhu` built a clean replacement on current
`upstream/main`, moving the fix to the shared JSON-decode boundary instead:
`internal/beads.IssueDep.UnmarshalJSON` now decodes `dependency_type` as
authoritative and falls back to the relation `type` field via
`knownDependencyRelation()`, keeping `issue_type` separate. That replacement,
PR #4424 ("Fix dependency relation type decoding"), merged 2026-07-07 with a
fully green CI bar (`cb71d2d4b14b1b89553da867383dd8335d3f6ca4`). A maintainer
(Bella-Giraffety) then closed PR #4189 as superseded, preserving attribution
to `@fengning-starsend` and the original commit author (Antigravity Agent /
`@shimonenator`) in #4424's body.

This review pass did not take the close comment or #4424's own self-reported
PR-Sheriff evidence on faith. It independently re-verified every
load-bearing fact: the merge commit `cb71d2d4b1` is confirmed a real
ancestor of current `upstream/main` (git-level, not just an API-reported
field); the shared-boundary fallback decode is confirmed present in
`internal/beads/beads.go`; and — the specific risk worth checking for a
"shared boundary fix" claim — the convoy stranded-readiness path
(`internal/cmd/convoy.go`'s `issueToDetails()`) is confirmed to actually read
its dependency type from the already-decoded `beads.Issue.Dependencies`
returned by `client.Show()`, not from a second, independent JSON decode that
would have bypassed the fix. All four test commands #4424's body cites were
run live against current `upstream/main` (28 tests including subtests, 0
failures), `git diff --check` on the merge commit is clean, and
`go build ./cmd/gt` succeeds. A GitHub issue search for this bug's
description and its distinguishing field name both returned zero results,
and PR #4189's own body has no `Closes #NNN`/`Fixes #NNN` linkage — so
unlike the PR #4061 precedent, there is no related open issue to disposition
here.

Sheriff wrote no code, opened no PR, and took no new GitHub-visible action on
PR #4189 in this pass — the prior close was already correct and within a
maintainer's authority. The only change in this branch is the durable
evidence record itself.

## Research legs (15/15)

See `evidence.json` → `evidence.research_legs` (R01–R15) for the full
record: #4189's current closed/superseded state and close-comment trail; its
original scope as a narrow, per-consumer (`convoy.go`-only) fix, and
confirmation its own CI was red on pre-existing/unrelated failures per the
contributor's comment; the replacement PR #4424's scope, merge state, and
its own recorded prior 15/5/5 PR-Sheriff cycle under a separate bead
(`gt-7yhu`); the full `bd search "4189"` trail across 7 related beads,
including two intermediate review/recovery passes (`gt-u6kh`, `gt-qwlt`)
that each correctly deferred rather than merging as-is or requesting
contributor changes; independent git-level ancestry confirmation of the
merge commit; independent content-level confirmation of the shared-boundary
fallback-decode fix in `internal/beads/beads.go`; independent wiring
confirmation that the convoy stranded-readiness consumer path actually reads
through that fixed decoder rather than a separate local decode; a live
28-test regression run against current `upstream/main` (0 failures) plus a
clean `git diff --check` and successful `go build`; and a related-GitHub-
issue search that found nothing to disposition.

## Pre-implementation / merge-decision reviews (5/5)

See `evidence.json` → `evidence.pre_implementation_reviews` (P01–P05):
cleanup-first/policy, correctness (don't trust self-reported prior evidence
or the close comment alone), scope/authority, technical/wiring risk (verify
the consuming code path actually reads through the fixed shared decoder,
not just claims to), and evidence-completeness (issue-linkage search beyond
the PR's own text). All `approve`.

## Post-implementation reviews

Not applicable — this pass made no code change, replacement, or cherry-pick
(`implementation.code_changed_by_sheriff: false`). The 5 post-implementation
review tier is conditional on Sheriff-authored code changes per this bead's
own acceptance criteria and does not apply to a `confirmed_prior_disposition`
verdict.

## Checker output

See `checker.txt` in this directory. It records: the independent
merge-ancestry check (`git merge-base --is-ancestor`), independent
fix-content-presence and consumer-wiring checks across
`internal/beads/beads.go` and `internal/cmd/convoy.go`, an independent
`go test`/`go build`/`git diff --check` run against current `upstream/main`,
the two related-GitHub-issue searches (zero results), and the raw
`gh`/`git`/`bd` command output backing R01–R15.

## Related GitHub issues — disposition

None. A `gh issue list --search` for both this bug's description ("convoy
stranded blocked dependency") and its distinguishing field name
(`dependency_type`) on `gastownhall/gastown` (state=all) returned zero
results, and PR #4189's own body contains no `Closes #NNN`/`Fixes #NNN`
linkage. Unlike PR #4061 (which left an originating issue #3779 open because
the closing keyword never fired), PR #4189's bug had no originating tracked
GitHub issue, so no issue-closure recommendation is owed here.

## Prior-attempt disposition

- **`gt-u6kh`** (thunder, initial review): correctly deferred
  (`merge_path_allowed=false`) rather than merging PR #4189 as-is or
  requesting contributor changes; filed follow-up `hq-gnhbn`.
- **`gt-qwlt`** (chrome, recovery pass after an MQ rejection of a preserved
  branch): correctly deferred again (`merge_path_allowed=false`), left the
  preserved branch intact rather than destroying it, recorded a review-only
  report.
- **`gt-7yhu`** (replacement): produced PR #4424, moving the fix to the
  shared `internal/beads` decode boundary; merged with full green CI;
  preserved attribution to the original contributor; closed #4189 as
  superseded.

## Final verdict

**confirmed_resolved_no_action.** PR #4189 is fully and correctly resolved:
closed as superseded by an independently-authored, CI-green, merged
replacement (#4424) that consolidates the fix at the shared
`internal/beads` JSON-decode boundary — a more convergent fix than the
original PR's per-consumer `convoy.go`-only patch — with attribution
preserved. Both the fix's presence and its actual wiring into the convoy
stranded-readiness consumer path were independently verified by direct
source inspection (not assumed from #4424's own body text), and all cited
regression tests were run live against current `upstream/main` and pass (28
tests, 0 failures), alongside a clean diff and successful build. No related
GitHub issue requires disposition. This review pass found nothing to correct
in the prior disposition and took no GitHub-visible action on the PR. No
refinery/MQ route was used for the #4189 disposition.

Required actions:
- None for PR #4189 — fully resolved.
