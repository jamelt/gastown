# Verified runtime activation

`gt activate` is the supported fast lane from an integrated `jamelt/gastown`
main commit to the active local control plane. It does not bypass review,
Refinery, required tests, or origin/main integration.

Run one command with the exact 40-character SHA that Refinery landed:

```bash
gt activate --repo /path/to/jamelt/gastown 0123456789abcdef0123456789abcdef01234567
```

Passing `main` (or omitting the revision) fetches and resolves the exact current
`origin/main` commit before doing anything. The source clone may contain local
operator files because the build never consumes its checkout: activation creates
a clean detached worktree at the integrated object and refuses it if its HEAD or
cleanliness differs. Revisions found only on upstream, feature branches, or
operational state are rejected because they are not ancestors of the fetched
`origin/main`. Non-forward activations are also rejected.

The command holds `<town>/.runtime/activation/activation.lock` and runs a
five-minute smoke gate over isolation-safe activation, atomic-file, version, and
workspace packages. It then builds `cmd/gt` exactly once with the full revision
stamped into it. Broader lifecycle suites that create real tmux or Dolt resources
remain pre-merge CI gates and are never executed against the active town by this
post-merge lane. Activation verifies the binary metadata is clean, backs up the
prior known-good files, and atomically renames the same built artifact over both:

- the executable path used to invoke `gt`;
- `~/.local/libexec/gt-dashboard`.

It then verifies PATH resolves to the activated CLI and gracefully refreshes
only long-lived processes that retain an executable inode: the daemon and any
dashboard process running from the configured dashboard helper. Agent shells,
hooks, queues, Dolt, worktrees, and the town's tmux server/socket are not stopped
or recreated. A component is successful only when its running executable
checksum matches the installed artifact. Stopped components stay stopped.

Every attempt writes a mode-0600, home-redacted JSON receipt under
`<town>/.runtime/activation/receipts/`. The successful `current.json` records the
old/new revision, binary checksums and paths, smoke result, component restart
results, and final verification. Any failure after installation automatically
restores the prior files and component set, while the failed receipt makes the
partial attempt visible.

Rollback is one command:

```bash
gt activate rollback
```

Rollback first proves the active files still match `current.json`, verifies the
saved prior-binary checksums, atomically restores them, and refreshes the same
currently-running component set. It refuses ambiguous or manually modified
state instead of guessing. Use the same `--town`, `--binary`,
`--dashboard-binary`, or `--state-dir` flags if the activation used non-default
paths.
