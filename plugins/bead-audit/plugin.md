+++
name = "bead-audit"
description = "Recurring semantic sweep for misplaced, misrouted, undispatchable, and stale-environment beads"
version = 1

[gate]
type = "cooldown"
duration = "24h"

[tracking]
labels = ["plugin:bead-audit", "category:beads-hygiene"]
digest = true

[execution]
timeout = "20m"
notify_on_failure = true
severity = "low"
+++

# Bead Audit

A recurring semantic sweep for misfiled and unworkable beads across every
database in the town. This is the "deacon dog" half of the misfiled-bead
problem (see gt-d2qp for the mechanical doctor check, gt-xmd3 for the
subject-matter routing table doc — both may still be open; this plugin does
not depend on either landing first).

**This plugin is REPORT ONLY.** It never moves, closes, or edits a bead.
Every finding below is a candidate for mayor/human review, not an action
taken. Today's cleanup (2026-08-18) required per-bead judgment: one bead was
closed on its merits, one was refiled rather than closed, one was kept open
because it was real, and two agents correctly refused to close beads they
did not own. An automated mover would have gotten several of those wrong —
so this plugin only ever reports.

**Re-escalation is load-bearing.** Use `gt escalate` (not a one-shot
`gt mail send`) for every non-empty category, with a stable `--fingerprint`
per category so the escalation system's own stale-threshold re-escalation
(see `gt escalate --help`) keeps surfacing the finding while it persists.
A one-shot report is exactly the failure mode that let NEEDS_MQ_SUBMIT sit
unaddressed for two days (gt-sl5c) — do not repeat it here.

## Step 1: Prefix/database mismatch (mechanical)

For each database, list beads whose ID prefix does not match that
database's configured prefix. This overlaps gt-d2qp's proposed doctor
check — report, do not move.

```bash
GT_ROOT="${GT_ROOT:-$HOME/Projects/gastown-ops}"
ROUTES="$GT_ROOT/.beads/routes.jsonl"

# Build the set of distinct db paths -> allowed prefixes.
# routes.jsonl rows look like: {"prefix":"hq-","path":"."}
#                               {"prefix":"gt-","path":"gastown/mayor/rig"}
# Multiple prefixes can share one path (hq- and hq-cv- both map to ".").
jq -r '[.prefix, .path] | @tsv' "$ROUTES" | sort -u -t$'\t' -k2,2 | \
while IFS=$'\t' read -r _ RPATH; do
  DBPATH="$GT_ROOT/${RPATH#./}"
  [ "$RPATH" = "." ] && DBPATH="$GT_ROOT"
  DBPATH="$DBPATH/.beads"
  [ -d "$DBPATH" ] || continue

  # Space-joined for the case/glob loop below, comma-joined for the
  # single-line report so a multi-prefix path (e.g. "." -> hq-, hq-cv-)
  # never breaks a line-oriented reading of the output.
  ALLOWED=$(jq -r --arg p "$RPATH" 'select(.path==$p) | .prefix' "$ROUTES")
  ALLOWED_DISPLAY=$(echo "$ALLOWED" | tr '\n' ',' | sed 's/,$//')

  RAW=$(bd list --all --db "$DBPATH" --json 2>&1)
  if [ $? -ne 0 ]; then
    echo "ERROR: bd list failed for $DBPATH (excluded from counts, not zero): $RAW"
    continue
  fi
  IDS=$(echo "$RAW" | jq -r '.[].id')
  BAD=$(echo "$IDS" | while read -r id; do
    ok=0
    for pfx in $ALLOWED; do
      case "$id" in "$pfx"*) ok=1 ;; esac
    done
    [ "$ok" = 0 ] && echo "$id"
  done)

  if [ -n "$BAD" ]; then
    COUNT=$(echo "$BAD" | grep -c .)
    echo "MISMATCH: $DBPATH expects [$ALLOWED_DISPLAY] — $COUNT offending, sample: $(echo "$BAD" | head -5 | tr '\n' ' ')"
  fi
done
```

Record the full list of `(db, count, sample-ids)` mismatches for the report
in Step 5. Do not move any of them. If any database errored instead of
returning zero, say so explicitly in the report — an error is not a clean
result and must never be folded into a "0 found" count (see Step 5's
no-silent-caps note).

## Step 2: Semantic misrouting (judgment)

Sample recently-updated open beads and check subject matter against the
routing table.

**Routing table** (from gt-xmd3; use CLAUDE.md's documented table instead
if gt-xmd3 has landed and it exists there — check first):

| Subject matter | Rig | Prefix |
|---|---|---|
| `gt` CLI / Gas Town tooling behavior | gastown | `gt-` |
| Gas Town infrastructure / deployment | gastown_infra | `gti-` |
| Trader product / trading domain code | trader | `trader-` |
| Town operations, cross-rig coordination | town (hq) | `hq-` |
| `bd` (beads) CLI itself | *(no local rig — upstream steveyegge/beads; cannot be dispatched from this town; say so explicitly)* | n/a |

```bash
# Include the town root ("." -> hq-*) alongside every registered rig — the
# routing table above explicitly covers hq- beads, so the sample must too.
for rig_name in $(gt rig list --json | jq -r '.[].name') .; do
  DBPATH="$GT_ROOT/$rig_name/mayor/rig/.beads"
  [ "$rig_name" = "." ] && DBPATH="$GT_ROOT/.beads"
  [ -d "$DBPATH" ] || continue
  RAW=$(bd list --all --status=open --db "$DBPATH" --json 2>&1)
  if [ $? -ne 0 ]; then
    echo "ERROR: bd list failed for $DBPATH (excluded, not zero): $RAW"
    continue
  fi
  echo "$RAW" | jq -c 'sort_by(.updated_at) | reverse | .[:30] | .[]'
done
```

For each sampled bead, read title + description. Flag a candidate only when
the described work clearly lives in a different rig's codebase (e.g. a
`gt`-CLI bug filed under `trader-*`, or trading-domain code filed under
`gt-*`). Where a bead is ABOUT one rig's code but happens to be STORED in
another database, note both facts — they are different problems (storage
location vs. subject matter) and conflating them is the exact mistake
gt-xmd3 documents.

Do not flag ambiguous or borderline cases — only clear mismatches worth a
human's time.

## Step 3: Undispatchable beads

Flag any open bead that `gt sling` cannot resolve from any context — the
bead looks healthy in every listing but `gt sling <id>` errors "bead not
found" (or similar) because it is invisible to prefix-based routing from
where it lives. hq-26y was in this state.

Coverage: run against every bead flagged as a prefix/database mismatch in
Step 1 (highest risk), plus a random sample of ~15 other open beads. This
is not exhaustive — note the sample size and total open-bead count in the
report so the reader knows the coverage, per "no silent caps."

```bash
# Dogs can sling; polecats cannot. This step must run as a dog.
# CANDIDATES = every Step-1 mismatch id, plus ~15 ids drawn at random from
# the Step-2 open-bead listing (or a fresh `bd list --all --status=open
# --db <path> --json` per db if Step 2's sample is exhausted).
SAMPLED=0
UNDISPATCHABLE=""
for bead_id in $CANDIDATES; do
  SAMPLED=$((SAMPLED + 1))
  OUT=$(gt sling "$bead_id" --dry-run 2>&1)
  if [ $? -ne 0 ] || echo "$OUT" | grep -qi "not found\|no issue\|no matching"; then
    UNDISPATCHABLE="$UNDISPATCHABLE $bead_id"
  fi
done
# Record $SAMPLED and $UNDISPATCHABLE for the report; also record the total
# open-bead count across all dbs so Step 5 can state <sampled>/<open-total>.
```

A "bead not found" / resolution error despite the bead showing up in
`bd show <bead-id>` is the undispatchable signature.

## Step 4: Stale-environment beads

Flag open beads referencing paths or diagnostics that no longer exist on
this host (e.g. a defunct town root like `/home/coder/gt`). 95 such beads
were closed on 2026-08-18 (hq-t9wlm — read it for the exact methodology);
the class regrows over time.

```bash
for rig_name in $(gt rig list --json | jq -r '.[].name') .; do
  DBPATH="$GT_ROOT/$rig_name/mayor/rig/.beads"
  [ "$rig_name" = "." ] && DBPATH="$GT_ROOT/.beads"
  [ -d "$DBPATH" ] || continue
  RAW=$(bd list --all --status=open --db "$DBPATH" --json 2>&1)
  if [ $? -ne 0 ]; then
    echo "ERROR: bd list failed for $DBPATH (excluded, not zero): $RAW"
    continue
  fi
  # Check title AND description; absolute-path roots aren't limited to
  # /home (e.g. /Users/..., /root/..., /data/...).
  echo "$RAW" | jq -r '.[] | select(((.title // "") + " " + (.description // "")) | test("/(home|Users|root|data)/[a-zA-Z0-9_./-]+")) | .id'
done
```

For each candidate, extract the absolute path(s) mentioned and check
`test -e "$path"` on this host. A missing path is only a real hit if it
looks like a town/rig root or a diagnostic artifact (not, e.g., a path in
someone's shell history quoted for context). Use judgment; false positives
here waste a human's time more than a missed one does.

## Step 5: Report — never act

Compose one report covering all four categories, even when some are empty
(say so explicitly rather than omitting the category).

For each non-empty category, escalate with a stable fingerprint so the
escalation system re-escalates while the condition persists:

```bash
gt escalate "Bead audit: <category> (<count> found)" \
  --severity low \
  --source "plugin:bead-audit" \
  --fingerprint "bead-audit:<category>" \
  --reason "<summary with counts and sample IDs — see plugin-run receipt for full list>"
```

Categories: `prefix-mismatch`, `semantic-misrouting`, `undispatchable`,
`stale-environment`. Only escalate categories with findings this run — an
empty category needs no escalation (the existing escalation, if any, will
naturally stop being fed and can be acked/closed by whoever is handling it).

Record the run regardless of whether anything was found:

```bash
gt plugin record-run --plugin bead-audit --result success \
  --title "bead-audit: <total> candidate(s) across 4 categories" \
  --description "prefix-mismatch=<n> semantic-misrouting=<n> undispatchable=<n> stale-environment=<n>. Coverage: <sampled>/<open-total> open beads sampled for semantic/undispatchable checks."
```

On unexpected failure (script error, Dolt unreachable, etc.):

```bash
gt plugin record-run --plugin bead-audit --result failure \
  --title "bead-audit: FAILED" --description "<error>"

gt escalate "Plugin FAILED: bead-audit" --severity low --reason "<error>"
```
