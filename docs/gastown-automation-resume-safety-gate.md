# Gastown Automation Resume Safety Gate

Go/no-go checklist for resuming broad Gastown scheduler/refinery/witness
automation after the 2026-06 stabilization core (bead
`gt-resume-automation-safety-gate`, epic
`gt-gastown-stabilization-restart-epic`).

Verified 2026-08-19 from a live polecat session (`gastown/polecats/atom`)
dispatched through the normal hook/sling/prime path.

## Checklist

| Criterion | Result | Evidence |
|---|---|---|
| Stabilization epic dependencies closed | ✅ PASS | `gt-gastown-stabilization-restart-epic`: 52/53 children closed; the only open child is this gate bead. All 16 `DEPENDS ON` beads on this gate show closed (✓). |
| hook/sling/prime smoke passes | ✅ PASS | This session was dispatched via `gt sling` → hooked via `gt hook` → primed via `gt prime --hook`, all of which resolved this bead's molecule/formula correctly end-to-end. |
| `mq list`/`status` passes | ✅ PASS | `gt mq list gastown` → empty queue, no error. `gt refinery status` → `queue_length: 0`, `running: true`, no error. |
| Fork PR flow constraints respected | ✅ PASS | `git remote -v` shows `origin` = `jamelt/gastown` (fork, push-enabled) and `upstream` = `gastownhall/gastown` with push explicitly `DISABLED`. Refinery merges land on fork `main` only (see recent merge commits `8bb595f`, `77e1d6f`, `788eb91`); it cannot push to upstream `gastownhall/gastown` main even if it tried. Guard fixes landed via `gt-fork-pr-flow-convergence-gastown-20260629` and `gt-clean-port-fork-pr-flow-upstream-20260629` (both closed). |
| Witness/refinery only started when safe | ✅ PASS | Both are already running (`gt witness status gastown`, `gt refinery status gastown`) with a healthy, empty queue and normal polecat throughput (50 commits in the last 24h, 18 by refinery). The gate bead no longer carries the `safety_stop:gt-resume-automation-safety-gate` label that Mayor applied on 2026-06-29 to hold refinery back; `gt-refinery-safety-stop-verify-4345` (which cleared it) and `gt-patrol-auto-boots-refinery-despite-safety-stop` (which fixed the auto-boot-during-safety-stop bug) are both closed. |
| Every remaining automation blocker has a specific bead | ✅ PASS | No open beads under the stabilization epic other than this gate. `bd ready` / `bd list --status=open` show no untracked automation blockers at gate-check time. |

## Go/No-Go

**GO.** All checklist items pass. Automation (scheduler, refinery, witness)
is already running safely on the fork with an empty, healthy merge queue,
and no undocumented blockers remain. This gate bead can close.

If a new automation blocker is discovered after this gate closes, file it
against the rig that owns the fix and let it stand on its own — do not
reopen this gate bead as a catch-all.
