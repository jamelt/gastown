+++
name = "rebuild-gt"
description = "Rebuild stale gt binary from gastown source"
version = 2

[gate]
type = "cooldown"
duration = "1h"

[tracking]
labels = ["plugin:rebuild-gt", "rig:gastown", "category:maintenance"]
digest = true

[execution]
timeout = "5m"
notify_on_failure = true
severity = "medium"
+++

# Rebuild gt Binary

Checks if the gt binary is stale (built from older commit than HEAD) and rebuilds.
Then, independently, checks if the *running daemon process* is behind the
on-disk binary and restarts it if so.

**SAFETY**: This plugin MUST only rebuild forward (binary ancestor of HEAD) and
only from the main branch. Rebuilding to an older or diverged commit caused a
crash loop where every new session's startup hook failed, the witness respawned
it, and the loop repeated every 1-2 minutes.

**TWO INDEPENDENT PHASES**: build and daemon-restart run regardless of each
other's outcome. A daemon can be running a stale commit even when the on-disk
binary is already fresh — e.g. a prior run rebuilt it but the restart failed
or was skipped — so the daemon-restart check is never gated on "did a build
happen this run" (gt-if5q).

## Gate Check

The Deacon evaluates this before dispatch. If gate closed, skip.

## Detection

Check binary staleness:

```bash
gt stale --json
```

Parse the JSON output and check these fields:
- If `"stale": false` → record success wisp and exit early (binary is fresh)
- If `"safe_to_rebuild": false` → **DO NOT REBUILD**. Record a skip wisp and exit.
  This means the repo is on a non-main branch or HEAD is not a descendant of the
  binary commit (would be a downgrade).
- If `"safe_to_rebuild": true` → proceed to build

If `safe_to_rebuild` is false, record a skip wisp:
```bash
gt plugin record-run --plugin rebuild-gt --result skipped --rig gastown \
  --title "Plugin: rebuild-gt [skipped]" \
  --description "Skipped: not safe to rebuild (forward=$FORWARD, main=$ON_MAIN)" >/dev/null 2>&1 || true
```

## Pre-flight Checks

Before building, verify the source repo is clean and on main:

```bash
cd ~/gt/gastown/mayor/rig
git status --porcelain  # Must be clean, ignoring .beads/config.yaml and .beads.gate.lock
git branch --show-current  # Must be "main"
```

`.beads/config.yaml` and `.beads.gate.lock` are machine-local runtime
artifacts, not source changes, and are always present in the canonical
checkout — porcelain lines for only those two paths do not count as dirty
(gt-svqh).

If either check fails, skip the rebuild and record a wisp.

## Action

Rebuild from source (the mayor/rig directory is the canonical source):

```bash
cd ~/gt/gastown/mayor/rig && make build && make safe-install
```

**IMPORTANT**: Use `make safe-install` (not `make install`) to avoid restarting
the daemon while sessions are active. safe-install replaces the binary but does
NOT restart the daemon — sessions will pick up the new binary on their next cycle.

## Record Result (build phase)

On success:
```bash
gt plugin record-run --plugin rebuild-gt --result success --rig gastown \
  --title "Plugin: rebuild-gt [success]" \
  --description "Rebuilt gt: $OLD → $NEW ($N commits)" >/dev/null 2>&1 || true
```

On failure:
```bash
gt plugin record-run --plugin rebuild-gt --result failure --rig gastown \
  --title "Plugin: rebuild-gt [failure]" \
  --description "Build failed: $ERROR" >/dev/null 2>&1 || true

gt escalate --severity=medium \
  --subject="Plugin FAILED: rebuild-gt" \
  --body="$ERROR" \
  --source="plugin:rebuild-gt"
```

## Daemon Restart (trigger)

`make safe-install` deliberately does not restart the daemon, to protect
active sessions. That means a fix to the daemon itself has no effect until
the long-lived `gt daemon run` process is restarted separately. hq-wic90
grants standing authorization for this restart; restarting the daemon does
not affect polecat/agent tmux sessions — it only schedules them, it doesn't
embed them.

Check whether the *running* daemon process (not just the on-disk binary) is
stale:

```bash
gt daemon status --json
```

- `"running": false` → nothing to restart, done.
- `"running": true, "stale": false` → daemon is already fresh, done.
- `"running": true, "stale": true` → restart:

```bash
gt daemon stop && gt daemon start
```

Then **verify rather than assume** — re-check `gt daemon status --json` and
confirm `"stale": false` before recording success. If it's still stale, record
a failure and escalate (the on-disk binary likely needs rebuilding first).

```bash
gt plugin record-run --plugin rebuild-gt --result success --rig gastown \
  --title "rebuild-gt: daemon restarted" \
  --description "Daemon process restarted and verified fresh at commit $COMMIT" >/dev/null 2>&1 || true
```
