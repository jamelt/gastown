---
description: Run the wisp reaper — scan, reap, purge, and auto-close stale beads across all Dolt databases
allowed-tools: Bash(gt reaper:*), Bash(gt escalate:*), Bash(gt dolt status:*)
argument-hint: [--dry-run]
---

# Wisp Reaper

Reap stale wisps and close stale issues across all production Dolt databases.
Runs the same cycle as `mol-dog-reaper` but directly, without Dog dispatch.

Arguments: $ARGUMENTS
If `--dry-run` is passed, report counts without making changes.

## Configuration Defaults

| Parameter | Default | Description |
|-----------|---------|-------------|
| max_age | 24h | Wisps older than this are reaped (closed) |
| purge_age | 72h | Closed wisps older than this are purged (deleted) |
| stale_issue_age | 168h | Issues stale longer than this are auto-closed |
| mail_delete_age | 72h | Closed mail older than this is purged |
| alert_threshold | 150 | Reap-candidate backlog count, per database, that triggers escalation (NOT open wisp volume) |
| dolt_port | 3307 | Dolt server port |

## Execution Steps

### Step 1: Verify Dolt server health

```bash
gt dolt status
```

If the server is unhealthy or unreachable, STOP and escalate:
```bash
gt escalate "Reaper blocked: Dolt server unhealthy" -s HIGH
```

### Step 2: Discover databases

```bash
gt reaper databases --json
```

This lists all production databases on the Dolt server.
Expected databases: `hq`, `beads`, `gastown` (and any rig-specific DBs).

### Step 3: Scan each database for candidates

For each database returned in Step 2:

```bash
gt reaper scan --db=<name> --port=3307 \
  --max-age=24h --purge-age=72h \
  --mail-age=72h --stale-age=168h \
  --json
```

Inspect the JSON output (per database):
- `reap_candidates`: wisps eligible for closing
- `purge_candidates`: closed wisps eligible for deletion
- `open_wisps`: total open wisp count
- `anomalies`: array of detected problems

Flag any `reap_candidate_spike` anomaly (that database's `reap_candidates`
exceeded alert_threshold — stale wisps are backing up) for escalation.
Do NOT escalate on `open_wisps`/total volume alone — that number scales
with healthy town activity (a busy town legitimately runs thousands of open
wisps) and is not itself a problem; two prior fixes tried a volume-based
threshold (500->800->3000) and each was outgrown, causing a recurring
false-alarm flood. Only flag volume if a single database looks like genuine
unbounded/runaway growth — use judgment, not a hard number.
If no candidates found across all databases, report "nothing to reap" and stop.

### Step 4: Reap stale wisps

For each database with reap candidates:

```bash
gt reaper reap --db=<name> --port=3307 --max-age=24h [--dry-run] --json
```

**IMPORTANT**: Scan/reap count mismatch is NORMAL (witness closes wisps concurrently).
Do NOT escalate scan > reap mismatches. Only escalate actual errors.

### Step 5: Purge old closed wisps and mail

For each database with purge candidates:

```bash
gt reaper purge --db=<name> --port=3307 \
  --purge-age=72h --mail-age=72h [--dry-run] --json
```

Watch for `dolt_commit_failed` anomalies — purged data may not persist.

### Step 6: Auto-close stale issues

For each database with stale candidates:

```bash
gt reaper auto-close --db=<name> --port=3307 \
  --stale-age=168h [--dry-run] --json
```

Auto-close NEVER touches: P0/P1 issues, epics, or issues with active dependencies.

### Step 7: Report

Print a summary in this format:

```
## Reaper Report

**Databases scanned**: N
**Wisps reaped**: N (stale open wisps closed)
**Wisps purged**: N (old closed wisps deleted)
**Mail purged**: N (old closed mail deleted)
**Issues auto-closed**: N (stale issues past 168h)
**Open wisps remaining**: N
**Anomalies**: <list or "none">
```

If anomalies were found, escalate once per (database, anomaly type) with a
stable fingerprint so a recurring condition collapses into one open
escalation instead of flooding a new bead + Mayor mail every run:
```bash
gt escalate "Reaper: <anomaly-type> in <database>" -s MEDIUM -m "<anomaly details>" \
  --fingerprint "reaper:<database>:<anomaly-type>"
```
