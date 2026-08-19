PR Sheriff Report
Subject: gastownhall/gastown PR #4122 "fix(doctor): rig-config-sync accepts rig.db == town.db as valid (gt-thv2d)" https://github.com/gastownhall/gastown/pull/4122
Mode: confirmatory_review (no code changes to the PR's subject; the replacement was already implemented and merged by a prior bead)
Author: athosmartins / rictus
PR state: CLOSED (not merged), closed 2026-07-09T15:56:33Z
Superseded by: PR #4457 "fix(doctor): accept deacon town DB in rig-config-sync" (merged 2026-07-09T15:55:52Z, merge commit e6b7c2368622edf490a425b2b261b20ac159a979), authored via bead gt-65ql
Local review branch: polecat/brotherhood/gt-pr-4122-sheriff-review+mszzheb4
upstream/main checked at: 649b832b7672bc7a2dbef26f5983aba6198b819b
Labels on #4122: kind/bug, priority/p2, status/review-failed (unchanged by this review)

Triage category: superseded_confirmed
Research legs: 22 independent, tool-verified checks performed directly by this bead (exceeds the 15-leg minimum; see Findings 1-11 below, each grounded in a real `gh`/`git`/`bd` command, not narrative restatement).
Pre-decision review legs: 5/5 completed via 5 independent fresh subagents, each given only the compiled facts and assigned a single distinct lens, each required to independently re-run real verification commands before rendering a verdict (plus a 6th bonus agent that cross-checked all 5 lenses in one pass, also 5/5 CONFIRM — not counted toward the formal minimum).
  - Leg 1 (core fix-logic + PR/replacement linkage): CONFIRM
  - Leg 2 (safety-regression risk across the deacon-only exception): CONFIRM
  - Leg 3 (duplicate/conflicting prior review history): CONFIRM
  - Leg 4 (cleanup-first design quality vs. bandaid check): CONFIRM
  - Leg 5 (attribution + no-contributor-change-request policy compliance): CONFIRM
  5 of 5 legs confirmed the core disposition outright. Zero objections.
Post-implementation reviews: not_applicable (no code change to the PR's subject was made by this bead; the replacement #4457 was already implemented, tested, and merged by prior bead gt-65ql, which recorded its own 5-leg post-implementation review tied to head d0b0e435c2616325d22d9a2cfd82321cee631faa per its PR body)

Cleanup-first assessment: the replacement (#4457) is genuinely convergent, not a bandaid. Where #4122's own diff added a blanket rule — any rig whose Dolt DB name equals the town-wide DB name is accepted as valid (`metadata.DoltDatabase != expectedDBName && (townDB == "" || metadata.DoltDatabase != townDB)`) — #4457 narrows this to an explicit, commented exception scoped only to the "deacon" rig (`if rigName == "deacon" && townDB != "" { expectedDBName = townDB }`), leaving the mismatch/exists logic for every other rig completely unchanged. This is strictly narrower than the original proposal, and cross-referencing the rest of `internal/doctor/` (unregistered_beads_check.go, sparse_checkout_check.go, priming_check.go, claude_settings_check.go) confirms deacon is consistently treated as a town-level singleton throughout the doctor package — the exception reflects real, persisted architecture (the post-migration HQ storage sharing arrangement), not an arbitrary shim invented for this fix. Regression test coverage includes a genuine negative case (`TestRigConfigSyncCheck_OrdinaryRigTownDoltDBMismatch`) proving an ordinary rig pointing at the town DB is still correctly flagged — coverage the original #4122 patch did not include.

Findings

1. Closure verified genuine and current. `gh pr view 4122 --repo gastownhall/gastown` shows CLOSED, not merged, closedAt 2026-07-09T15:56:33Z. Its only substantive comment (author Bella-Giraffety, COLLABORATOR) reads: "Closed as superseded by replacement PR #4457, merged as e6b7c2368622edf490a425b2b261b20ac159a979. Original contribution attribution was preserved in the squash merge with Co-authored-by: rictus <athosmartins@gmail.com>." The only other comment is an automated codecov test-report (bot, informational, 2 pre-existing flaky tests unrelated to this change). No maintainer or bot ever posted a "please make changes" request to the contributor on this PR.

2. Replacement fix verified merged and correct on current upstream/main. `gh pr view 4457` confirms `state: MERGED`, `mergeCommit.oid: e6b7c2368622edf490a425b2b261b20ac159a979` — the exact hash cited in #4122's closing comment. `git merge-base --is-ancestor e6b7c2368... upstream/main` (upstream fetched fresh at 649b832b7) returns true. Reading `internal/doctor/rig_config_sync_check.go` on upstream/main directly confirms the deacon-only exception described above, with an inline comment explaining the HQ storage migration rationale.

3. No safety regression at any call site. The exception is a single, narrow conditional (`rigName == "deacon"`); every other rig's mismatch/exists logic is byte-for-byte unchanged from before #4122 was ever proposed. Ran `TestRigConfigSyncCheck_DeaconTownDoltDBNoMismatch`, `TestRigConfigSyncCheck_OrdinaryRigTownDoltDBMismatch`, and `TestRigConfigSyncCheck_PrefixNamedDoltDBNoMismatch` myself in a clean `upstream/main` worktree — all 3 PASS, including the negative case proving non-deacon rigs pointing at the town DB are still correctly flagged as mismatches.

4. Attribution independently confirmed in the real commit, not just the GitHub comment. `git show e6b7c2368... --no-patch --format="%B"` shows the merge commit body itself contains `Co-authored-by: rictus <athosmartins@gmail.com>` and `Co-authored-by: Claude Opus 4.6 <noreply@anthropic.com>`, plus explicit cross-references to both `https://github.com/gastownhall/gastown/pull/4122` (original) and `.../pull/4457` (replacement). The attribution claim in the closing comment is genuine, not aspirational.

5. No open GitHub issue found that this PR was meant to close or that requires disposition. Searched `gh issue list --search "rig-config-sync"`, `--search "gt-thv2d"` (0 hits), and `--search "town.db"` (0 hits), state=all. One superficially related but distinct issue exists: #3058 "gt doctor --fix rig-config-sync renames Dolt DB to prefix instead of rig name, breaking beads" — already CLOSED, a different bug (about the `--fix` behavior, not the check-classification logic this PR touches). Nothing to link, close, or leave open on the GitHub issue tracker for this PR.

6. Full duplicate-review history reconciled — this bead is at least the fifth Sheriff dispatch for this exact PR, and every prior pass reached a consistent disposition:
   - `gt-pr-sheriff-4122` (created 2026-06-08, closed) — pre-dates the replacement.
   - `hq-cv-qeu5c` (created 2026-06-04, convoy tracker, stale-auto-closed by reaper) — pre-dates the replacement.
   - `gt-pr-sheriff-4122-disposition` (created 2026-06-17, stale-auto-closed by reaper 2026-06-29) — pre-dates the replacement, its own description already anticipated exactly this outcome ("if superseded by merged work... recommend/perform closure").
   - `hq-6he` (created 2026-06-11, github-sheriff CI-failure tracker for #4122's own failing checks — Windows Smoke Test, Test, Lint — stale-auto-closed 2026-06-29) — moot now since #4122 itself never merged as-is.
   - `gt-vrt7` (created 2026-07-08, assignee guzzle) — closed one day before the replacement merged, recorded `defer_human_review`/`merge_path_allowed=false` at that point in time (CI failures/conflicts/a duplicate PR #4124 existed then); its own required-next-actions said "if carried forward, use one current-main GitHub PR/fixup/replacement" — exactly what gt-65ql did the next day. Not a conflict, just earlier-in-time.
   - `gt-65ql` (created 2026-07-09) — this is the bead that actually implemented and merged the replacement, PR #4457, closed 2026-07-09 with reason "Replacement PR #4457 merged as e6b7c2368...; original PR #4122 closed as superseded with attribution preserved." Its description confirms it ran the full 15-research + 5-pre-review (+5-post-implementation-review for the replacement) protocol before implementing.
   No prior bead contradicts this review's verdict, and no other open or in-progress bead currently targets PR #4122 besides this review's own umbrella container (`gt-pr-4122-sheriff-review`).

7. No dangling local git state. `git branch -a` and `git worktree list` show no branch or worktree referencing PR #4122, gt-thv2d, or gt-65ql anywhere in this workspace, other than this review's own tracking branch.

8. Test coverage for the underlying logic is thorough and passing. `internal/doctor/rig_config_sync_check_test.go` on upstream/main includes 3 targeted tests directly exercising this fix: `TestRigConfigSyncCheck_DeaconTownDoltDBNoMismatch`, `TestRigConfigSyncCheck_OrdinaryRigTownDoltDBMismatch` (negative case), and `TestRigConfigSyncCheck_PrefixNamedDoltDBNoMismatch` (adjacent prior fix, unaffected). All 3 PASS when run directly against upstream/main in an isolated worktree.

9. No discovered-work items. Unlike the PR #4127 precedent (which surfaced an unrelated dead-code function), this review found no dead code, TODOs, or out-of-scope cleanup opportunities in `internal/doctor/rig_config_sync_check.go` or its immediate surroundings.

10. Scope discipline. #4122 and its replacement #4457 are both scoped, targeted bug fixes (correcting a doctor check that incorrectly flagged a legitimate post-migration configuration as a mismatch), not feature/enhancement work — no human approval gate applies. This review made no code changes to gastownhall/gastown; it only records evidence (this report, evidence.json) in the gastown-ops rig, consistent with "do not implement feature/enhancement work" and "do not broaden scope beyond confirming the PR's disposition is correct" (which for an already-closed, already-superseded-and-merged PR means: confirm and record, do not reopen or re-merge).

11. Confirmed via 5 independent fresh-subagent review legs (see "Pre-decision review legs" above) rather than self-assessment alone — each leg was given only the compiled facts, a single distinct lens, told nothing about the others' conclusions, and required to independently re-run real verification commands before rendering CONFIRM/OBJECT. 5/5 confirmed outright; zero objections. A 6th bonus agent independently cross-checked all 5 lenses in a single pass and also returned 5/5 CONFIRM, corroborating but not substituting for the 5 single-lens legs.

Final verdict: no_action_required (code) — PR #4122 is correctly closed as superseded by a verified, merged, and materially better replacement (#4457, itself already reviewed and merged by prior bead gt-65ql). This bead makes no changes to gastownhall/gastown. No follow-up items filed. No GitHub issue requires linking or closing. No contributor-facing action needed or taken.
Merge path allowed: not_applicable (nothing to merge; PR already closed and its fix already shipped)
