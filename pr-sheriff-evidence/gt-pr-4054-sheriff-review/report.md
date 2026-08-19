# PR Sheriff Review Report — PR #4054

Subject: gastownhall/gastown PR #4054 "fix: gt is not fork-aware at runtime:
refinery start / crew context / no persistent refinery-off for rigs with
upstream_url"
https://github.com/gastownhall/gastown/pull/4054

Bead: gt-pr-4054-sheriff-review
Mode: confirmed_prior_disposition
No refinery/MQ route used: true

## Summary

PR #4054 (author esciara / Emmanuel Sciara, branch
`issue-hunter/4045-gt-is-not-fork-aware-at-runtime-refinery`) proposed a real
fix for issue #4045: `gt refinery start`/`restart` had no guard against
starting on fork-backed rigs (where `config.json` sets `upstream_url`), the
crew/polecat context `gt prime` injects gave fork rigs the wrong
"push-to-main" guidance, and there was no supported way to keep a fork rig's
witness up while permanently disabling its refinery. The PR's fix idea was
sound but its own diff was not safe to merge as-is: 19 files, hundreds of
commits behind current `main`, bundled with unrelated convoy/fanout/sling
changes, and never carrying maintainer approval.

Three beads have now been dispatched against this PR. The first
(`gt-lbdm`, thunder, 2026-07-06) ran a review-only pass and correctly
deferred with verdict `block`/`defer_human_review` rather than merging a
stale, scope-contaminated branch. The second (`gt-s5nz`, mirelurk,
2026-07-08) built a scope-focused current-main replacement, opened PR #4452
("fix: make gt fork-aware at runtime"), and closed with a complete 15+5+5
PR Sheriff evidence trail (`pr-sheriff-evidence/gt-s5nz-pr4054-replacement/`).
PR #4452 merged the next day (2026-07-09T12:39:13Z, commit
`55576cd6ec07d13b7a58a4fe53dbb88d322ae5d8`) with all 7 CI checks green, and a
maintainer (Bella-Giraffety) closed PR #4054 as superseded, preserving
attribution to esciara / Emmanuel Sciara via a `Co-authored-by` trailer.

This third pass did not take the close comment or the prior beads' own
records on faith. It independently re-verified every load-bearing fact from
first principles: the merge commit `55576cd6e` is confirmed a real ancestor
of current `upstream/main` via `git merge-base --is-ancestor` (git-level, not
just an API-reported field); the fork-guard sentinel and helpers
(`ErrForkRig`, `ForkRigError`, `ForkRigStartError`, `blockForkRigStart`) are
confirmed present in `internal/refinery/manager.go` on current `upstream/main`
and correctly derive the guard from the rig's *already-persisted*
`upstream_url` config field rather than a new flag, with `--force`
(`StartAllowingForkRig`) as the sole explicit override; all 12 targeted
regression tests across `internal/refinery`, `internal/templates`,
`internal/daemon`, and `internal/cmd` pass when run directly against a
freshly-created detached worktree on `upstream/main` (not this review
branch's own checkout); `go build ./cmd/gt` succeeds on the same checkout;
PR #4452's own CI (Test, Lint, Integration Tests, Windows Smoke Test, and both
housekeeping gates) was fully green at merge; and attribution is confirmed
consistent across two independent sources (the merge commit trailer and the
PR #4054 close comment).

This pass also searched for related open GitHub issues and found one
directly on point: **#4045**, the issue PR #4054 itself said it would close.
It is still **OPEN** with zero comments — neither PR #4054 nor PR #4452
carried a closing keyword that survived to merge, so it was never
auto-closed. Mapping #4045's three "Expected" asks against the shipped fix:
(1) `gt refinery start` warn/refuse for fork rigs — fixed directly; (2)
fork-aware crew/polecat context — fixed directly; (3) a supported persistent
per-rig refinery-disable — **not** implemented as a literal new flag. PR
#4452's own body states it deliberately "avoids adding `refinery_disabled` or
another persistent compatibility layer," instead deriving the guard from the
rig's existing, already-persisted `upstream_url` config and applying it
uniformly at every start path (manual start/restart, daemon auto-start, `gt
up`, `gt start`, attach). Because that guard fires on every invocation unless
`--force` is passed for that specific call, there is no code path left where
a fork rig's refinery starts and stays running without an explicit override —
which satisfies the *intent* of ask (3) architecturally, and is the more
converged (cleanup-first) design versus adding a second, parallel persistent
flag alongside the same signal.

Sheriff wrote no code, opened no PR, and took no new GitHub-visible action in
this pass (no comments, labels, or merges) — the prior close was already
correct and within a maintainer's authority.

## Research legs (15/15)

See `evidence.json` → `evidence.research_legs` (R01–R15) for the full record:
PR #4054's current state and close-comment trail; its original scope/intent
(19-file diff bundling the fork-guard fix with unrelated changes); the
complete prior dispatch history across two beads (`gt-lbdm` defer,
`gt-s5nz` replacement build); PR #4452's merged state and CI results;
independent git-level ancestry confirmation of the merge commit; independent
content-level confirmation that the fork-guard sentinel/helpers are present
and correctly derive from persisted `upstream_url` config; independent
regression-test presence-and-pass confirmation (12 tests across 4 packages)
run against a fresh `upstream/main` worktree; an independent build check;
attribution-preservation verification across two sources; a related-issue
search that surfaced #4045 as directly on point (three others confirmed
distinct subsystems); a gap analysis mapping #4045's three asks against what
actually shipped, including the architectural (not literal-flag) resolution
of the third ask; and the (inapplicable, recorded for completeness only)
branch-hygiene merge-gate, already run and passed at replacement-build time.

## Pre-implementation / merge-decision reviews (5/5)

See `evidence.json` → `evidence.pre_implementation_reviews` (P01–P05):
cleanup-first/policy, correctness, scope/authority, issue-disposition and
attribution, and evidence-completeness/schema fit. All `approve` — no
correction needed to the prior disposition.

## Post-implementation reviews

Not applicable — this pass made no code change, replacement, or cherry-pick
(`implementation.code_changed_by_sheriff: false`). The 5 post-implementation
review tier is conditional on Sheriff-authored code changes and does not
apply to a `confirmed_prior_disposition` verdict.

## Checker output

See `checker.txt` in this directory. It records: the independent
merge-ancestry check (`git merge-base --is-ancestor`), independent
fix-content-presence and wiring checks in `internal/refinery/manager.go`, a
live `go test`/`go build` run (12 tests, 4 packages) against a fresh detached
`upstream/main` worktree, PR #4452's CI results at merge, the related-issue
searches backing R12–R14, and the raw `gh`/`git` command output backing
R01–R15.

## Related GitHub issues — disposition

- **#4045** ("gt is not fork-aware at runtime: refinery start / crew context /
  no persistent refinery-off for rigs with upstream_url") — OPEN, directly on
  point (this is the issue PR #4054 itself targeted). Confirmed satisfied by
  merged replacement PR #4452 (commit `55576cd6e`): asks (1) and (2) fixed
  directly; ask (3) satisfied architecturally via the config-derived guard
  rather than a new flag (see R13 for the full gap analysis).
  **Recommended disposition: close as fixed by #4452**, with a closing note
  explaining that the persistent-refinery-off ask was resolved by deriving
  the guard from existing `upstream_url` config (applied on every start path)
  rather than adding a separate `refinery_disabled` field, so no such field
  exists to document. Sheriff is not closing this issue directly in this pass
  (no GitHub-visible action taken per skill.md — recommendation only, for the
  overseer to action).
- **#4540, #3946** — topically distinct (agent/bead resolution and sling
  regressions, not fork-runtime behavior); no linkage.

## Final verdict

`confirmed_resolved_no_action` — see `evidence.json` → `final` for the full
reasoning and required actions (the single recommended action is: overseer
closes #4045 referencing commit `55576cd6e`).
