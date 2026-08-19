# Gas Town Contributor Guidance

Gas Town supplies role identity, hooked work, and completion protocol through
`gt prime --hook`. Run it after session start, compaction, or context reset.
Never infer identity or assignment from a directory, file, or unassigned queue.

## Work Authority

- Execute the Bead on the hook or the Bead explicitly assigned by the user.
  Do not replace assigned work with an item from `bd ready`.
- Beads is the only durable tracker. Use `bd show <id>` for scope, link
  discoveries with `discovered-from:<id>`, and use `bd remember` for durable
  knowledge. Do not create Markdown task or memory ledgers.
- Use the current `bd prime` output for Beads commands and policy.
- Follow role-specific completion from `gt prime --hook`. Polecats use
  `gt done`; Refinery owns integration. Do not apply a universal pull, rebase,
  Dolt sync, commit, or push ritual.

## Repository Work

- Preserve unrelated changes and avoid broad staging, stash, destructive reset,
  blanket checkout, and clean operations.
- Keep provider integration generic. Role context belongs in `gt prime` and
  provider hooks, not duplicated repository instruction blocks.
- Gas Town owns agent lifecycle and Beads bootstrap. Every production `bd init`
  call must use `--skip-agents --skip-hooks` so Beads cannot overwrite Gas Town
  or customer-repository instructions.
- Add focused tests for changed behavior; run the smallest relevant package
  first, then broader Go tests when risk warrants it.
- Update documentation only when a public command or operational contract
  changes, not as a blanket completion step.
