#!/usr/bin/env bash
# Tests for rebuild-gt/run.sh, in particular that the build phase and the
# daemon-restart phase are independent of each other (gt-if5q): a daemon can
# be running a stale commit even when the on-disk binary is already fresh
# (e.g. this run needed no build), and a build happening this run must not
# be a precondition for checking/restarting a stale daemon process.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="$ROOT_DIR/plugins/rebuild-gt/run.sh"
ORIGINAL_PATH="$PATH"
PASS=0
FAIL=0
CLEANUP_DIRS=()

cleanup() {
  for dir in "${CLEANUP_DIRS[@]}"; do
    rm -rf "$dir"
  done
}
trap cleanup EXIT

record_pass() {
  PASS=$((PASS + 1))
  printf 'PASS: %s\n' "$1"
}

record_fail() {
  FAIL=$((FAIL + 1))
  printf 'FAIL: %s\n' "$1"
}

assert_file_empty() {
  local file="$1" label="$2"
  if [ ! -s "$file" ]; then
    record_pass "$label"
  else
    record_fail "$label"
    printf '  unexpected contents of %s:\n' "$file"
    sed 's/^/    /' "$file"
  fi
}

assert_file_contains() {
  local file="$1" needle="$2" label="$3"
  if grep -Fq -- "$needle" "$file" 2>/dev/null; then
    record_pass "$label"
  else
    record_fail "$label"
    printf '  expected %q in %s\n' "$needle" "$file"
    sed 's/^/    /' "$file" 2>/dev/null || true
  fi
}

assert_line_count() {
  local file="$1" expected="$2" label="$3"
  local actual=0
  if [ -f "$file" ]; then
    actual=$(wc -l < "$file" | tr -d ' ')
  fi
  if [ "$actual" = "$expected" ]; then
    record_pass "$label"
  else
    record_fail "$label"
    printf '  expected %s lines in %s, got %s\n' "$expected" "$file" "$actual"
    sed 's/^/    /' "$file" 2>/dev/null || true
  fi
}

assert_exit_code() {
  local expected="$1" label="$2"
  local actual
  actual=$(cat "$TEST_STATE/exit_code" 2>/dev/null || echo "?")
  if [ "$actual" = "$expected" ]; then
    record_pass "$label"
  else
    record_fail "$label"
    printf '  expected exit %s, got %s\n' "$expected" "$actual"
  fi
}

write_fake_commands() {
  local bin_dir="$1"

  cat > "$bin_dir/gt" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

case "${1:-}" in
  town)
    [ "${2:-}" = "root" ] && { printf '%s\n' "$GT_TOWN_ROOT"; exit 0; }
    ;;
  stale)
    if [ -f "$TEST_STATE/stale.json" ]; then
      cat "$TEST_STATE/stale.json"
    else
      printf '{"stale": false, "safe_to_rebuild": false}\n'
    fi
    exit 0
    ;;
  version)
    n=0
    [ -f "$TEST_STATE/version_calls" ] && n=$(cat "$TEST_STATE/version_calls")
    n=$((n + 1))
    echo "$n" > "$TEST_STATE/version_calls"
    if [ "$n" -eq 1 ]; then printf 'old-ver\n'; else printf 'new-ver\n'; fi
    exit 0
    ;;
  daemon)
    case "${2:-}" in
      status)
        if [ -f "$TEST_STATE/daemon_status_queue" ] && [ -s "$TEST_STATE/daemon_status_queue" ]; then
          sed -n '1p' "$TEST_STATE/daemon_status_queue"
          if [ "$(wc -l < "$TEST_STATE/daemon_status_queue" | tr -d ' ')" -gt 1 ]; then
            sed '1d' "$TEST_STATE/daemon_status_queue" > "$TEST_STATE/daemon_status_queue.tmp"
            mv "$TEST_STATE/daemon_status_queue.tmp" "$TEST_STATE/daemon_status_queue"
          fi
        else
          printf '{"running": false, "stale": false}\n'
        fi
        exit 0
        ;;
      stop)
        printf 'stop\n' >> "$TEST_STATE/daemon_calls.log"
        [ -f "$TEST_STATE/daemon_stop_fail" ] && exit 1
        exit 0
        ;;
      start)
        printf 'start\n' >> "$TEST_STATE/daemon_calls.log"
        [ -f "$TEST_STATE/daemon_start_fail" ] && exit 1
        exit 0
        ;;
    esac
    ;;
  plugin)
    if [ "${2:-}" = "record-run" ]; then
      printf '%s\n' "$*" >> "$TEST_STATE/record.log"
      exit 0
    fi
    ;;
  escalate)
    printf '%s\n' "$*" >> "$TEST_STATE/escalate.log"
    exit 0
    ;;
esac

printf 'unexpected gt call: %s\n' "$*" >&2
exit 1
SH
  chmod +x "$bin_dir/gt"

  cat > "$bin_dir/git" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

if [ "${1:-}" = "-C" ]; then
  shift 2
  case "${1:-}" in
    status)
      [ -f "$TEST_STATE/dirty" ] && printf ' M somefile\n'
      exit 0
      ;;
    branch)
      if [ -f "$TEST_STATE/branch" ]; then cat "$TEST_STATE/branch"; else printf 'main\n'; fi
      exit 0
      ;;
  esac
fi

printf 'unexpected git call: %s\n' "$*" >&2
exit 1
SH
  chmod +x "$bin_dir/git"

  cat > "$bin_dir/make" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

case "${1:-}" in
  build)
    [ -f "$TEST_STATE/make_build_fail" ] && exit 1
    exit 0
    ;;
  safe-install)
    [ -f "$TEST_STATE/make_install_fail" ] && exit 1
    exit 0
    ;;
esac

printf 'unexpected make call: %s\n' "$*" >&2
exit 1
SH
  chmod +x "$bin_dir/make"
}

setup_case() {
  TEST_TMP=$(mktemp -d)
  CLEANUP_DIRS+=("$TEST_TMP")
  export TEST_STATE="$TEST_TMP/state"
  export GT_TOWN_ROOT="$TEST_TMP/town"
  local bin_dir="$TEST_TMP/bin"

  mkdir -p "$TEST_STATE" "$GT_TOWN_ROOT/gastown/mayor/rig" "$bin_dir"
  : > "$TEST_STATE/record.log"
  : > "$TEST_STATE/escalate.log"
  : > "$TEST_STATE/daemon_calls.log"

  write_fake_commands "$bin_dir"
  export PATH="$bin_dir:$ORIGINAL_PATH"
}

run_script() {
  set +e
  bash "$SCRIPT" > "$TEST_STATE/output.log" 2>&1
  echo $? > "$TEST_STATE/exit_code"
  set -e
}

test_fresh_binary_daemon_not_running() {
  setup_case
  printf '{"stale": false, "safe_to_rebuild": false}\n' > "$TEST_STATE/stale.json"
  run_script

  assert_exit_code 0 "fresh binary, no daemon: exit 0"
  assert_line_count "$TEST_STATE/record.log" 1 "fresh binary, no daemon: one record"
  assert_file_contains "$TEST_STATE/record.log" "binary is fresh" "fresh binary, no daemon: fresh wisp recorded"
  assert_file_empty "$TEST_STATE/daemon_calls.log" "fresh binary, no daemon: no daemon calls"
  assert_file_empty "$TEST_STATE/escalate.log" "fresh binary, no daemon: no escalation"
}

test_build_success_daemon_not_running() {
  setup_case
  printf '{"stale": true, "safe_to_rebuild": true}\n' > "$TEST_STATE/stale.json"
  run_script

  assert_exit_code 0 "build success, no daemon: exit 0"
  assert_line_count "$TEST_STATE/record.log" 1 "build success, no daemon: one record"
  assert_file_contains "$TEST_STATE/record.log" "rebuild-gt: old-ver -> new-ver" "build success, no daemon: build recorded"
  assert_file_empty "$TEST_STATE/daemon_calls.log" "build success, no daemon: no daemon calls"
  assert_file_empty "$TEST_STATE/escalate.log" "build success, no daemon: no escalation"
}

test_build_and_daemon_restart_success() {
  setup_case
  printf '{"stale": true, "safe_to_rebuild": true}\n' > "$TEST_STATE/stale.json"
  printf '{"running": true, "stale": true, "binary_commit": "old123"}\n{"running": true, "stale": false, "binary_commit": "new456"}\n' \
    > "$TEST_STATE/daemon_status_queue"
  run_script

  assert_exit_code 0 "build + restart: exit 0"
  assert_line_count "$TEST_STATE/record.log" 2 "build + restart: two records"
  assert_file_contains "$TEST_STATE/record.log" "rebuild-gt: old-ver -> new-ver" "build + restart: build recorded"
  assert_file_contains "$TEST_STATE/record.log" "daemon restarted" "build + restart: daemon restart recorded"
  assert_line_count "$TEST_STATE/daemon_calls.log" 2 "build + restart: stop+start called"
  assert_file_empty "$TEST_STATE/escalate.log" "build + restart: no escalation"
}

# Regression coverage for gt-if5q: the daemon-restart phase must run even
# when no build happened this invocation — a prior run may have installed a
# fresh binary but never got the daemon to pick it up.
test_daemon_restart_without_build() {
  setup_case
  printf '{"stale": false, "safe_to_rebuild": false}\n' > "$TEST_STATE/stale.json"
  printf '{"running": true, "stale": true, "binary_commit": "old123"}\n{"running": true, "stale": false, "binary_commit": "new456"}\n' \
    > "$TEST_STATE/daemon_status_queue"
  run_script

  assert_exit_code 0 "restart without build: exit 0"
  assert_line_count "$TEST_STATE/record.log" 2 "restart without build: fresh-binary + daemon-restart both recorded"
  assert_file_contains "$TEST_STATE/record.log" "binary is fresh" "restart without build: fresh wisp recorded"
  assert_file_contains "$TEST_STATE/record.log" "daemon restarted" "restart without build: daemon restart recorded despite no build this run"
  assert_line_count "$TEST_STATE/daemon_calls.log" 2 "restart without build: stop+start called"
}

test_daemon_not_stale_no_restart() {
  setup_case
  printf '{"stale": false, "safe_to_rebuild": false}\n' > "$TEST_STATE/stale.json"
  printf '{"running": true, "stale": false, "binary_commit": "abc"}\n' > "$TEST_STATE/daemon_status_queue"
  run_script

  assert_exit_code 0 "daemon already fresh: exit 0"
  assert_file_empty "$TEST_STATE/daemon_calls.log" "daemon already fresh: no restart"
}

test_daemon_restart_command_fails() {
  setup_case
  printf '{"stale": false, "safe_to_rebuild": false}\n' > "$TEST_STATE/stale.json"
  printf '{"running": true, "stale": true, "binary_commit": "old123"}\n' > "$TEST_STATE/daemon_status_queue"
  touch "$TEST_STATE/daemon_start_fail"
  run_script

  assert_exit_code 1 "restart command fails: exit 1"
  assert_file_contains "$TEST_STATE/record.log" "daemon restart failure" "restart command fails: failure recorded"
  assert_line_count "$TEST_STATE/escalate.log" 1 "restart command fails: one escalation"
}

test_daemon_restart_verify_fails() {
  setup_case
  printf '{"stale": false, "safe_to_rebuild": false}\n' > "$TEST_STATE/stale.json"
  printf '{"running": true, "stale": true, "binary_commit": "old123"}\n{"running": true, "stale": true, "binary_commit": "old123"}\n' \
    > "$TEST_STATE/daemon_status_queue"
  run_script

  assert_exit_code 1 "restart verify fails: exit 1"
  assert_file_contains "$TEST_STATE/record.log" "daemon restart verify failed" "restart verify fails: failure recorded"
  assert_line_count "$TEST_STATE/escalate.log" 1 "restart verify fails: one escalation"
  assert_line_count "$TEST_STATE/daemon_calls.log" 2 "restart verify fails: stop+start still both attempted"
}

# Build failure must not suppress the independent daemon-restart phase, and
# the overall exit code must still reflect the build failure.
test_build_failure_still_runs_daemon_phase() {
  setup_case
  printf '{"stale": true, "safe_to_rebuild": true}\n' > "$TEST_STATE/stale.json"
  touch "$TEST_STATE/make_build_fail"
  run_script

  assert_exit_code 1 "build failure: exit 1"
  assert_file_contains "$TEST_STATE/record.log" "Plugin: rebuild-gt [failure]" "build failure: failure recorded"
  assert_line_count "$TEST_STATE/escalate.log" 1 "build failure: one escalation"
  assert_file_empty "$TEST_STATE/daemon_calls.log" "build failure: daemon phase found nothing to restart"
}

test_not_safe_to_rebuild_skips_build_only() {
  setup_case
  printf '{"stale": true, "safe_to_rebuild": false}\n' > "$TEST_STATE/stale.json"
  run_script

  assert_exit_code 0 "unsafe rebuild: exit 0"
  assert_file_contains "$TEST_STATE/record.log" "Plugin: rebuild-gt [skipped]" "unsafe rebuild: skip recorded"
  assert_file_empty "$TEST_STATE/escalate.log" "unsafe rebuild: no escalation"
}

test_fresh_binary_daemon_not_running
test_build_success_daemon_not_running
test_build_and_daemon_restart_success
test_daemon_restart_without_build
test_daemon_not_stale_no_restart
test_daemon_restart_command_fails
test_daemon_restart_verify_fails
test_build_failure_still_runs_daemon_phase
test_not_safe_to_rebuild_skips_build_only

printf '\n%s passed, %s failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
