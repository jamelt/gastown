---
name: pr-sheriff
description: >
  PR Sheriff workflow: triage PRs into easy-wins and crew assignments.
  Prints recommendations inline - does NOT post to GitHub.
allowed-tools: "Bash(gh pr *), Bash(git *), Bash(gt *), Bash(bd *), Bash(cat *)"
version: "2.1.0"
author: "Gas Town"
---

# PR Sheriff - Triage and Review Workflow

This skill is prompt-driven: there is no `mol-pr-sheriff-patrol` formula and
no `pr-sheriff-config.json` config file. Everything an agent needs to run a
patrol — the step order, the crew-dispatch mechanism, the triage tree, and
the policy tiers — is defined directly below. Follow this document as the
single source of truth; do not look for a formula or config to "load" first.

## Repo Scope

This rig (gastown/crew/max) is responsible for **steveyegge/gastown only**.
The beads repo (steveyegge/beads) is handled by beads/crew/emma.
Do NOT discover or triage PRs from repos outside your scope.

## Usage

```
/pr-sheriff [repo]
```

- `repo` - Optional. If provided, overrides the default scope.
  If omitted, scan only `steveyegge/gastown` (this rig's scope).

## How to Execute

Work through these steps in order:

| Step | What to do |
|------|-------------|
| discover-prs | `gh pr list` for the in-scope repo(s); find open PRs needing review |
| triage-batch | Categorize all PRs in one pass using the Category Decision Tree below (preserves cross-PR context) |
| merge-easy-wins | Merge approved EASY-WIN PRs via `gh pr merge` |
| dispatch-crew-reviews | `gt crew list` to find crew, then nudge them for NEEDS-CREW PRs |
| dispatch-deep-reviews | Nudge crew for NEEDS-HUMAN PRs, pointing them at the Deep Review Evaluation Framework below |
| collect-results | Gather crew review nudge-backs |
| interactive-review | Walk through remaining NEEDS-HUMAN PRs with the overseer |
| summarize | Print the patrol summary (see Summary Output below) |

## Contributor Policy Tiers

Applied directly from the table below — there is no external config file:

| Tier | Handling |
|------|----------|
| bot-trusted | Auto-merge if CI passes (e.g., dependabot) |
| community | Normal triage — easy-win / crew / deep review |
| firewalled | Always NEEDS-HUMAN, never auto-merge, deep review required |

## Category Decision Tree

```
Draft? → SKIP
Contributor firewalled? → NEEDS-HUMAN (deep review)
Dependabot patch bump + CI green? → EASY-WIN
<50 lines, obvious bug/doc/test fix? → EASY-WIN
Security/architecture/API change? → NEEDS-HUMAN
Multi-concern PR? → NEEDS-HUMAN
100+ lines new feature? → NEEDS-CREW or NEEDS-HUMAN
Everything else → NEEDS-CREW
```

## Deep Review Evaluation Framework (NEEDS-HUMAN)

Apply six lenses when evaluating a NEEDS-HUMAN PR:

1. **Plugin/integration fit** — core vs plugin/formula/integration?
2. **Tech-debt weight** — complexity justified by user breadth?
3. **Contributor track record** — first-time, repeat, or firewalled?
4. **ZFC compliance** — structural checks, not string heuristics?
5. **Problem validity + solution fit** — real problem, right solution?
6. **Splitability** — can good parts be cherry-picked from bad?

Final verdicts: MERGE | CHERRY-PICK | REWORK | REIMPLEMENT | CLOSE

## Branch Hygiene Gate (required before any replacement PR)

Before opening or recommending a maintainer **replacement PR** (a
fix-merge/clean-redo branch that carries forward a contributor's original
PR), run:

```bash
gt pr-sheriff-check --merge-gate
```

This computes how far the current branch has diverged from its base
(commits behind, commits ahead) and fails loudly if the branch is stale
or carries commits unrelated to the intended fix — the exact failure mode
that let PR #4238 (~553 behind / ~86 ahead) and PR #4257 (~553 behind /
~98 ahead) get created and merge-recommended as contaminated replacements.

The check also raises the bar structurally: `gt tap guard branch-hygiene`
is wired to `Bash(gh pr create*)` in Gas Town's default hook set, so an
agent running a literal `gh pr create` Bash command from a contaminated
branch is blocked before the PR is even opened. That match is a
command-string prefix match, not a universal interception point — it does
not see a PR opened via the GitHub API, a different tool, or shell
indirection, so `gt pr-sheriff-check --merge-gate` above is still the
step of record; treat the hook as defense in depth, not the whole guard.
Existing rigs pick the hook up via the normal `gt hooks sync` /
`gt doctor --fix hooks-sync` propagation path (also run by the standing
deacon patrol), not automatically the moment this code ships.

**If a rig's fork is known to lag its upstream** (see the fork-rig-setup
guide), pass `--base` explicitly (e.g. `--base upstream/main` or
`--base origin/main`) rather than relying on auto-detection — the
auto-detected base can otherwise report normal fork lag as "unrelated
ahead" contamination.

Record the check's output (or its `--json` form) as the branch-hygiene
evidence artifact in the PR-Sheriff evidence record for the replacement.

## Output Format

For each PR, print a recommendation block:

```
### PR #<num>: <title>
Author: <login> | +<additions>/-<deletions> | <changedFiles> files

**Category**: EASY-WIN | NEEDS-CREW | NEEDS-HUMAN | SKIP

**Analysis**:
<1-3 sentences explaining the change and why it fits this category>

**Recommendation**:
<specific action>
```

## Summary Output

```
## PR Sheriff Patrol Summary — <date>

**Easy-wins merged**: N
**Crew-reviewed and merged**: N
**Sent back for rework**: N
**Closed**: N
**Still pending**: N
```

## Dispatching Work: Use Ephemeral Beads

When creating beads to track fix-merge work for polecats or crew, **use
ephemeral beads (wisps)** rather than persistent beads. PR review/fix-merge
tasks are orchestration scaffolding — they exist to give a polecat something
to hook and track, not to create a permanent record.

Ephemeral beads are the right trade-off: they give polecats and crew trackable
work items without polluting Dolt's permanent ledger with one-off orchestration
noise. If/when beads are exported to permanent ledgers, review-task wisps won't
clutter the history.

```bash
# Ephemeral bead for fix-merge dispatch
bd new -t task "Fix-merge PR #1234: description" -p 2 -l pr-review \
  --wisp-type patrol

# vs persistent (avoid for orchestration work)
bd new -t task "Fix-merge PR #1234: description" -p 2 -l pr-review
```

The `--wisp-type patrol` flag marks it as ephemeral orchestration work that
the reaper will eventually clean up. The polecat/crew member can still hook it,
work it, and close it normally.

## CRITICAL Rules

- All output is printed inline. Do NOT post comments to GitHub.
- The overseer decides what gets posted externally.
- Contributor-friendly: help contributors get to the finish line.
- Use `Co-authored-by` trailer when fixing up contributor work.
