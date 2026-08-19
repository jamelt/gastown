#!/usr/bin/env bash
# rebuild-gt/run.sh — Rebuild gt binary from gastown source if stale, then
# restart the daemon if the process it's running is itself stale.
#
# SAFETY: Only rebuilds forward (binary is ancestor of HEAD) and only
# from main branch. A bad rebuild caused a crash loop (every session's
# startup hook failed, witness respawned, loop repeated every 1-2 min).
#
# TWO INDEPENDENT PHASES: build and daemon-restart run unconditionally of
# each other's outcome. A daemon can be running a stale commit even when the
# on-disk binary is already fresh (e.g. a prior run rebuilt it but its own
# restart attempt failed, or was skipped because the daemon wasn't running
# yet) — so restart-checking must not be gated on "did a build happen this
# run" (gt-if5q).

set -euo pipefail

TOWN_ROOT="${GT_TOWN_ROOT:-$(gt town root 2>/dev/null)}"
RIG_ROOT="${TOWN_ROOT}/gastown/mayor/rig"

log() { echo "[rebuild-gt] $*"; }

json_field() {
  # json_field <json> <key> <default>
  echo "$1" | python3 -c "import json,sys; print(json.load(sys.stdin).get('$2', $3))" 2>/dev/null || echo "$3"
}

BUILD_FAILED=0
DAEMON_FAILED=0

# --- Phase 1: rebuild the on-disk binary if stale ----------------------------

log "Checking binary staleness..."
STALE_JSON=$(gt stale --json 2>/dev/null) || STALE_JSON=""

if [ -z "$STALE_JSON" ]; then
  log "gt stale --json failed, skipping build phase"
elif [ "$(json_field "$STALE_JSON" stale False)" != "True" ]; then
  log "Binary is fresh. Nothing to build."
  gt plugin record-run --plugin rebuild-gt --result success --rig gastown \
    --title "rebuild-gt: binary is fresh" >/dev/null 2>&1 || true
elif [ "$(json_field "$STALE_JSON" safe_to_rebuild False)" != "True" ]; then
  log "Not safe to rebuild (not on main or would be a downgrade). Skipping."
  gt plugin record-run --plugin rebuild-gt --result skipped --rig gastown \
    --title "Plugin: rebuild-gt [skipped]" \
    --description "Skipped: not safe to rebuild" >/dev/null 2>&1 || true
elif [ ! -d "$RIG_ROOT" ]; then
  log "Rig root $RIG_ROOT does not exist. Skipping."
else
  DIRTY=$(git -C "$RIG_ROOT" status --porcelain 2>/dev/null)
  BRANCH=$(git -C "$RIG_ROOT" branch --show-current 2>/dev/null)

  if [ -n "$DIRTY" ]; then
    log "Repo is dirty, skipping rebuild."
    gt plugin record-run --plugin rebuild-gt --result skipped --rig gastown \
      --title "Plugin: rebuild-gt [skipped]" \
      --description "Skipped: repo has uncommitted changes" >/dev/null 2>&1 || true
  elif [ "$BRANCH" != "main" ]; then
    log "Not on main branch (on $BRANCH), skipping rebuild."
    gt plugin record-run --plugin rebuild-gt --result skipped --rig gastown \
      --title "Plugin: rebuild-gt [skipped]" \
      --description "Skipped: not on main branch (on $BRANCH)" >/dev/null 2>&1 || true
  else
    OLD_VER=$(gt version 2>/dev/null | head -1 || echo "unknown")
    log "Rebuilding gt from $RIG_ROOT..."

    if (cd "$RIG_ROOT" && make build && make safe-install) 2>&1; then
      NEW_VER=$(gt version 2>/dev/null | head -1 || echo "unknown")
      log "Rebuilt: $OLD_VER -> $NEW_VER"
      gt plugin record-run --plugin rebuild-gt --result success --rig gastown \
        --title "rebuild-gt: $OLD_VER -> $NEW_VER" >/dev/null 2>&1 || true
    else
      ERROR="make build/safe-install failed"
      log "FAILED: $ERROR"
      gt plugin record-run --plugin rebuild-gt --result failure --rig gastown \
        --title "Plugin: rebuild-gt [failure]" \
        --description "Build failed: $ERROR" >/dev/null 2>&1 || true
      gt escalate "Plugin FAILED: rebuild-gt" -s medium 2>/dev/null || true
      BUILD_FAILED=1
    fi
  fi
fi

# --- Phase 2: restart the daemon if the running process is itself stale ------
#
# safe-install deliberately does NOT restart the daemon (see comment above)
# to protect active sessions. But that means a fix to the daemon itself
# (e.g. gt-yycw) sits unused in the freshly-installed binary until something
# restarts the long-lived 'gt daemon run' process. hq-wic90 grants standing
# authorization to do that restart proactively rather than waiting for a
# human to notice (gt-if5q). Restarting the daemon does not affect polecat/
# agent tmux sessions — the daemon only schedules them, it doesn't embed them.

DAEMON_JSON=$(gt daemon status --json 2>/dev/null) || DAEMON_JSON=""

if [ "$(json_field "$DAEMON_JSON" running False)" != "True" ]; then
  log "Daemon is not running, nothing to restart."
elif [ "$(json_field "$DAEMON_JSON" stale False)" != "True" ]; then
  log "Running daemon process is already fresh. Nothing to restart."
else
  log "Running daemon process is stale. Restarting..."
  if gt daemon stop 2>&1 && gt daemon start 2>&1; then
    # Verify rather than assume: confirm the restarted daemon actually
    # picked up a fresher commit before declaring success.
    POST_JSON=$(gt daemon status --json 2>/dev/null) || POST_JSON=""
    POST_COMMIT=$(json_field "$POST_JSON" binary_commit "''")

    if [ "$(json_field "$POST_JSON" stale True)" = "True" ]; then
      log "FAILED: daemon restarted but is still reporting stale (commit: $POST_COMMIT)"
      gt plugin record-run --plugin rebuild-gt --result failure --rig gastown \
        --title "Plugin: rebuild-gt [daemon restart verify failed]" \
        --description "Daemon restarted but still stale after restart (commit: $POST_COMMIT)" >/dev/null 2>&1 || true
      gt escalate "Plugin FAILED: rebuild-gt daemon restart did not take effect" -s medium 2>/dev/null || true
      DAEMON_FAILED=1
    else
      log "Daemon restarted and verified fresh (commit: $POST_COMMIT)"
      gt plugin record-run --plugin rebuild-gt --result success --rig gastown \
        --title "rebuild-gt: daemon restarted" \
        --description "Daemon process restarted and verified fresh at commit $POST_COMMIT" >/dev/null 2>&1 || true
    fi
  else
    ERROR="gt daemon stop/start failed"
    log "FAILED: $ERROR"
    gt plugin record-run --plugin rebuild-gt --result failure --rig gastown \
      --title "Plugin: rebuild-gt [daemon restart failure]" \
      --description "$ERROR" >/dev/null 2>&1 || true
    gt escalate "Plugin FAILED: rebuild-gt daemon restart" -s medium 2>/dev/null || true
    DAEMON_FAILED=1
  fi
fi

if [ "$BUILD_FAILED" -eq 1 ] || [ "$DAEMON_FAILED" -eq 1 ]; then
  exit 1
fi
exit 0
