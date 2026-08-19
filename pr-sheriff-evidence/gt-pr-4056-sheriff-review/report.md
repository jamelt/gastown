# PR Sheriff Review Report — PR #4056

Subject: gastownhall/gastown PR #4056 "fix(mail): route deacon/dogs/<name> to
dog pane, not Deacon (gt-osx)"
https://github.com/gastownhall/gastown/pull/4056

Bead: gt-pr-4056-sheriff-review
Mode: confirmed_prior_disposition
No refinery/MQ route used: true

## Summary

PR #4056 (author adensur / Maksim, branch `fix/router-dog-prefix-match-gt-osx`)
identified a real bug: `AddressToSessionIDs` used `strings.HasPrefix(address,
"deacon")`, which matched both `"deacon"` and `"deacon/dogs/alpha"` — so
plugin-dispatch mail nudges addressed to a named dog landed in the Deacon's
own `hq-deacon` tmux pane instead. Each misrouted nudge sent an `Escape`
keystroke via `NudgeSession`, interrupting fresh Deacon sessions mid-thinking
and freezing them at `[Request interrupted by user]`. The proposed fix —
match `deacon/dogs/<name>` first and resolve through `session.DogSessionName`,
falling through to an exact-equal check for the Deacon top-level address —
was sound, but the branch itself was not safe to merge: it was a 30-file,
+1706/-285 stacked branch with only one commit (`6010f264`) actually relevant
to the stated fix.

Two PR Sheriff review rounds independently confirmed the branch was
stale/conflicting/broad and chose reimplementation over merging or
cherry-picking it as-is (`gt-pr-sheriff-4056`/nuka, 2026-06-09 and
2026-06-16 — two successive reimplementation attempts on fork-local branches
that were not what ultimately shipped). Two further review-only passes
(`gt-p1rn`/ghoul, 2026-07-06, and `gt-5vgr`, 2026-07-07) both again deferred
rather than merging, with `gt-5vgr` explicitly calling for a focused
current-main replacement before superseded closure. That replacement
(`gt-yzhl`, pipboy, 2026-07-07) built a clean, independently-scoped fix on
current `main`, carried a complete 15+5+5 PR Sheriff evidence trail, and
opened PR #4417 ("fix(mail): route dog mail to dog panes"), which merged the
same day with a fully green CI bar (`44717e5b8a9cd4c16229f6310e5b3808ef17b46c`).
A maintainer (Bella-Giraffety) then closed PR #4056 as superseded, preserving
attribution to adensur/Maksim and original commit `6010f264`.

This review pass did not take the close comment or the prior beads' own
records on faith. It independently re-verified every load-bearing fact: the
merge commit `44717e5b8` is confirmed a real ancestor of current
`upstream/main` (git-level, not just an API-reported field); a dedicated
`internal/mail/dog_address.go` helper (`DogAddress`/`DogAddressName`/
`isSafeDogName`/`isReservedTownSubpath`) is confirmed present and correctly
wired ahead of the exact-equality Deacon/Mayor checks in `AddressToSessionIDs`
— the old broad `strings.HasPrefix` match is gone entirely; the helper
canonicalizes BOTH the legacy `gt-dog-` and current `hq-dog-` bead-ID
prefixes, satisfying a mandatory-tightening requirement from `gt-yzhl`'s own
pre-implementation review; regression tests covering both the address
resolver (`TestAddressToSessionIDs`, including malformed-path guards for
`deacon/dogs/..`, `deacon/foo`, `deaconer`) and the nudge-dispatch layer
(`TestNudgeDogTargetRoutesToDogSession`, directly covering the PR's stated
symptom) are confirmed present AND were run directly (all pass) against a
freshly checked-out `upstream/main` worktree, not the review branch's own
diverged tree; `go build ./cmd/gt` succeeds on the same checkout; attribution
to adensur is preserved via the close-comment trail; and PR #4417's own CI
(Test, Lint, Integration Tests, Windows Smoke Test, and housekeeping gates)
was fully green at merge, versus PR #4056's own only real CI run predating
the two review rounds that found its branch stale.

This pass also searched for related open GitHub issues and found none
directly on point. Two broad searches (`deacon/dogs`, `dog pane`) surfaced
only topically-adjacent-but-distinct-subsystem issues (dog health-check
timing, worktree cleanup, wisp reaper, session freezes). One CLOSED issue,
**#3223** ("dogs can't read their mail"), is topically adjacent but a
different root cause — a dog's own session missing an injected identity when
reading ITS OWN inbox, not mail addressed TO a dog being misrouted by the
sender's resolver — and requires no reopening. The PR body's `gt-osx` and
`gt-097` references were searched as GitHub issues and beads and resolve to
neither; they are the external contributor's own local bead-tracker IDs.

Sheriff wrote no code, opened no PR, and took no new GitHub-visible action in
this pass (no comments, labels, or merges) — the prior close was already
correct and within a maintainer's authority.

## Research legs (15/15)

See `evidence.json` → `evidence.research_legs` (R01–R15) for the full record:
#4056's current state and four-comment trail (an automated coverage comment
plus two PR Sheriff review rounds and the close comment); its original
scope/intent as a narrow bug with a broad/stale carrier branch; why it was not
safe to merge as-is (30 files, +1706/-285, only one commit relevant); the
complete prior dispatch/review history across four beads
(`gt-pr-sheriff-4056`, `gt-p1rn`, `gt-5vgr`, `gt-yzhl`); the two immediate
deferral decisions (`gt-p1rn`, `gt-5vgr`) that preceded and set up the final
fix; the replacement's own durable 15+5+5 evidence record persisted in
`gt-yzhl`'s comments, including a mandatory-tightening requirement to
canonicalize both bead-ID prefixes; #4417's merged state and body; independent
git-level ancestry confirmation of the merge commit; independent content-level
confirmation that the dog-address helper is present and correctly wired ahead
of the exact-equality checks, with the mandatory dual-prefix tightening
applied; independent regression-test presence-and-pass confirmation at BOTH
the address-resolver and nudge-dispatch layers, run against a fresh
`upstream/main` worktree; #4417's green CI at merge; attribution-preservation
verification; a related-PR search confirming no competing replacement; a
related-open-issue search finding nothing directly on point (with one
topically-adjacent-but-distinct CLOSED issue, #3223, confirmed unrelated); and
a check of the PR body's non-GitHub `gt-osx`/`gt-097` references.

## Pre-implementation / merge-decision reviews (5/5)

See `evidence.json` → `evidence.pre_implementation_reviews` (P01–P05):
cleanup-first/policy, correctness, scope/authority, technical-defect
disposition (why the sound fix idea still needed a clean reimplementation
rather than merging the stale carrier branch), and evidence-completeness /
schema fit. All `approve` or `approve-with-changes`; the one
`approve-with-changes` correction — add independent git-level ancestry,
content-level presence/wiring checks, live `go test` runs at both the
resolver and nudge-dispatch layers against a fresh `upstream/main` worktree,
and a related-issue/PR-body-reference search, rather than trusting the close
comment or the prior beads' own records alone — is already applied in
`evidence.json`.

## Post-implementation reviews

Not applicable — this pass made no code change, replacement, or cherry-pick
(`implementation.code_changed_by_sheriff: false`). The 5 post-implementation
review tier is conditional on Sheriff-authored code changes per this bead's
own acceptance criteria and does not apply to a `confirmed_prior_disposition`
verdict.

## Checker output

See `checker.txt` in this directory. It records: the independent
merge-ancestry check (`git merge-base --is-ancestor`), independent
fix-content-presence and wiring checks across `dog_address.go`/`router.go`, an
independent `go test`/`go build` run against a fresh detached `upstream/main`
worktree covering both the address resolver and nudge-dispatch regression
suites, the related-PR and related-issue searches, and the raw `gh`/`bd`
command output backing R01–R15.

## Related GitHub issues — disposition

- No open GitHub issue was found directly on point for this bug. Searches for
  `deacon/dogs` and `dog pane` (38 combined results) surfaced only
  topically-adjacent-but-distinct-subsystem issues (dog health-check timing,
  worktree registration cleanup, wisp reaper preconditions, patrol formula
  audits, session input freezes, zombie detection) — none describe the
  broad-prefix mail-routing bug this PR fixed.
- **#3223** ("dogs can't read their mail") — CLOSED (2026-05-13), before this
  PR was opened. Topically adjacent (dog mail) but a distinct root cause: a
  dog's own session missing an injected identity path when reading ITS OWN
  inbox, versus this PR's bug of mail addressed TO a dog being misrouted by
  the SENDER'S `AddressToSessionIDs` resolver. No linkage or reopening owed.
- The PR body's `gt-osx` / `gt-097` references do not resolve to any GitHub
  issue in this repo (`gt-osx` search only surfaces unrelated #128; `gt-097`
  surfaces nothing) or to any Gas Town bead — they are the external
  contributor's own local/personal bead-tracker IDs. Nothing to disposition.

## Prior-attempt disposition

- **`gt-pr-sheriff-4056`** (nuka's two review/reimplementation rounds,
  2026-06-09 and 2026-06-16): correctly identified the branch as
  stale/conflicting/broad across both passes and reimplemented the fix twice
  on fork-local branches (MRs `gt-wisp-cul`, `gt-wisp-tki6`); neither attempt
  is the code that ultimately shipped, but both correctly avoided merging the
  dirty branch as-is.
- **`gt-p1rn`** (ghoul's review-only pass, 2026-07-06): `defer_human_review`,
  `merge_path_allowed=false`, no code changes, filed a follow-up for
  replacement work.
- **`gt-5vgr`** (2026-07-07): `defer_human_review`, `merge_path_allowed=false`,
  explicitly required a focused current-main replacement/fixup before
  superseded closure — directly setting up the final fix.
- **`gt-yzhl`** (pipboy's replacement, 2026-07-07): produced PR #4417, which
  fixes the routing bug via a dedicated, reused dog-address helper applied
  consistently across router/resolver/nudge/DND paths, merged with full green
  CI, and carries its own complete 15+5+5 evidence trail (persisted in bead
  comments, including a mandatory-tightening requirement independently
  confirmed applied in this pass).

## Final verdict

**confirmed_resolved_no_action.** PR #4056 is fully and correctly resolved:
closed as superseded by an independently-authored, CI-green, merged
replacement (#4417), with the broad-prefix mail-routing defect that made the
original unsafe to merge as-is actually fixed via a dedicated, reused helper
(not a bandaid), the mandatory dual-bead-ID-prefix tightening from the
replacement's own pre-implementation review confirmed applied, attribution
preserved, and both the fix and its regression tests — at the address-resolver
layer AND the nudge-dispatch layer that produced the original symptom —
verified live and passing on current `upstream/main`. This review pass found
nothing to correct in the prior disposition, found no related open GitHub
issue requiring linkage or closure, and took no GitHub-visible action. No
refinery/MQ route was used.

Required actions:
- None for PR #4056 — fully resolved.
