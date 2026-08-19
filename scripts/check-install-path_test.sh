#!/usr/bin/env bash
# Tests for Makefile check-install-path shadow warnings and
# warn-if-install-symlink notices.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
MAKE_BIN="$(command -v "${MAKE:-make}")"
SYSTEM_PATH="/usr/bin:/bin"
TMPDIR=""
PASS=0
FAIL=0

cleanup() {
  if [[ -n "$TMPDIR" && -d "$TMPDIR" ]]; then
    rm -rf "$TMPDIR"
  fi
}
trap cleanup EXIT

setup_bins() {
  TMPDIR="$(mktemp -d)"
  INSTALL_DIR="$TMPDIR/home/.local/bin"
  BREW_DIR="$TMPDIR/usr/local/bin"
  OTHER_DIR="$TMPDIR/usr/bin"
  GASTOWN_BIN_DIR="$TMPDIR/home/.local/gastown/bin"
  mkdir -p "$INSTALL_DIR" "$BREW_DIR" "$OTHER_DIR" "$GASTOWN_BIN_DIR"
  printf '#!/usr/bin/env sh\nexit 0\n' > "$INSTALL_DIR/gt"
  printf '#!/usr/bin/env sh\nexit 0\n' > "$BREW_DIR/gt"
  printf '#!/usr/bin/env sh\nexit 0\n' > "$GASTOWN_BIN_DIR/gt"
  chmod +x "$INSTALL_DIR/gt" "$BREW_DIR/gt" "$GASTOWN_BIN_DIR/gt"
}

run_check() {
  local path_value="$1"
  env PATH="$path_value" "$MAKE_BIN" --no-print-directory -C "$REPO_ROOT" \
    check-install-path INSTALL_DIR="$INSTALL_DIR" GASTOWN_BIN_DIR="$GASTOWN_BIN_DIR" 2>&1
}

assert_empty() {
  local test_name="$1"
  local output="$2"
  if [[ -z "$output" ]]; then
    echo "  PASS: $test_name"
    PASS=$((PASS + 1))
  else
    echo "  FAIL: $test_name"
    echo "$output"
    FAIL=$((FAIL + 1))
  fi
}

assert_warns() {
  local test_name="$1"
  local output="$2"
  local expected="$3"
  if [[ "$output" == *"Warning: gt resolves to $expected, not $INSTALL_DIR/gt"* ]] && \
     [[ "$output" == *"export PATH=\"$INSTALL_DIR:\$PATH\""* ]]; then
    echo "  PASS: $test_name"
    PASS=$((PASS + 1))
  else
    echo "  FAIL: $test_name"
    echo "$output"
    FAIL=$((FAIL + 1))
  fi
}

run_symlink_check() {
  "$MAKE_BIN" --no-print-directory -C "$REPO_ROOT" \
    warn-if-install-symlink INSTALL_DIR="$INSTALL_DIR" 2>&1
}

assert_warns_symlink() {
  local test_name="$1"
  local output="$2"
  local target="$3"
  if [[ "$output" == *"Note: $INSTALL_DIR/gt was a symlink to $target"* ]]; then
    echo "  PASS: $test_name"
    PASS=$((PASS + 1))
  else
    echo "  FAIL: $test_name"
    echo "$output"
    FAIL=$((FAIL + 1))
  fi
}

echo "=== check-install-path tests ==="

setup_bins
output="$(run_check "$INSTALL_DIR:$BREW_DIR:$OTHER_DIR:$SYSTEM_PATH")"
assert_empty "no warning when install dir wins PATH" "$output"
cleanup

setup_bins
output="$(run_check "$OTHER_DIR:$SYSTEM_PATH")"
assert_warns "warns when install dir is omitted from PATH" "$output" "nothing in PATH"
cleanup

setup_bins
output="$(run_check "$BREW_DIR:$INSTALL_DIR:$OTHER_DIR:$SYSTEM_PATH")"
assert_warns "warns when earlier Homebrew-style gt shadows install" "$output" "$BREW_DIR/gt"
cleanup

setup_bins
output="$(run_check "$GASTOWN_BIN_DIR:$INSTALL_DIR:$OTHER_DIR:$SYSTEM_PATH")"
assert_empty "no warning when the maintained gastown-bin symlink wins PATH" "$output"
cleanup

echo "=== warn-if-install-symlink tests ==="

setup_bins
output="$(run_symlink_check)"
assert_empty "no note when install target is already a regular file" "$output"
cleanup

setup_bins
rm -f "$INSTALL_DIR/gt"
ln -s "$BREW_DIR/gt" "$INSTALL_DIR/gt"
output="$(run_symlink_check)"
assert_warns_symlink "notes when install target is a symlink" "$output" "$BREW_DIR/gt"
cleanup

setup_bins
rm -f "$INSTALL_DIR/gt"
output="$(run_symlink_check)"
assert_empty "no note when install target does not exist yet" "$output"
cleanup

echo "Results: $PASS passed, $FAIL failed"
[[ "$FAIL" -eq 0 ]] && exit 0 || exit 1
