// Package testguard provides a fail-closed safety check that stops Go test
// binaries from mutating the live Gas Town control plane.
//
// Per-package TestMain isolation (temp GT_ROOT/BEADS_DIR/tmux socket) only
// takes effect in worktrees built after that isolation code was added — a
// stale polecat worktree's compiled test binary lacks it entirely, and
// without a guard here it silently falls through to whatever GT_ROOT,
// BEADS_DIR, or tmux socket its process inherited. That is normally the live
// production town: a polecat's tmux pane is launched with the live GT_ROOT
// explicitly exported into its environment, and the live tmux socket is
// inherited ambiently, so any `go test` run in that pane sees production
// state by default. See gt-8ik.
package testguard

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// tempRoots returns the directories a legitimate isolated test sandbox may
// live under. os.TempDir() covers the common case; /tmp is included because
// tmux deliberately ignores $TMPDIR on macOS (see internal/tmux.SocketDir),
// so isolated sandboxes built for tmux compatibility can live directly under
// /tmp even when os.TempDir() resolves elsewhere (e.g. macOS /var/folders).
func tempRoots() []string {
	seen := map[string]struct{}{}
	var roots []string
	for _, r := range []string{os.TempDir(), "/tmp"} {
		if r == "" {
			continue
		}
		resolved := r
		if real, err := filepath.EvalSymlinks(r); err == nil {
			resolved = real
		}
		if _, ok := seen[resolved]; ok {
			continue
		}
		seen[resolved] = struct{}{}
		roots = append(roots, resolved)
	}
	return roots
}

// IsIsolatedSandbox reports whether path resolves to somewhere under a
// recognized temp root — the pattern every isolated TestMain/t.TempDir()
// sandbox in this repo already uses.
func IsIsolatedSandbox(path string) bool {
	if path == "" {
		return false
	}
	resolved := path
	if real, err := filepath.EvalSymlinks(path); err == nil {
		resolved = real
	} else if abs, aerr := filepath.Abs(path); aerr == nil {
		resolved = abs
	}
	for _, root := range tempRoots() {
		if resolved == root {
			return true
		}
		if rel, err := filepath.Rel(root, resolved); err == nil &&
			rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// sanitizeTownNameRe mirrors internal/session.sanitizeTownName's character
// class. Duplicated (rather than imported) because internal/session already
// imports internal/tmux, and internal/tmux needs this from testguard: a
// three-way import cycle would result if testguard imported session.
var sanitizeTownNameRe = regexp.MustCompile(`[^a-z0-9-]+`)

// LiveSocketName mirrors internal/session.townSocketName's derivation, so
// this leaf package can recognize the live town's default tmux socket name
// without importing internal/session. Returns "" if townRoot is empty.
func LiveSocketName(townRoot string) string {
	if townRoot == "" {
		return ""
	}
	base := strings.ToLower(filepath.Base(townRoot))
	base = sanitizeTownNameRe.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-")
	if base == "" {
		base = "default"
	}

	canonical, err := filepath.EvalSymlinks(townRoot)
	if err != nil {
		canonical, err = filepath.Abs(townRoot)
		if err != nil {
			canonical = townRoot
		}
	}
	h := sha256.Sum256([]byte(canonical))
	return base + "-" + hex.EncodeToString(h[:3])
}

// LiveTownRoot resolves GT_ROOT/GT_TOWN_ROOT the same way this repo's
// production code does (see internal/workspace.FindFromCwdOrError's env
// fallback), returning "" if neither is set.
func LiveTownRoot() string {
	if root := os.Getenv("GT_ROOT"); root != "" {
		return root
	}
	return os.Getenv("GT_TOWN_ROOT")
}

// RequireIsolated fails closed when called from a test binary against a
// path that is not a recognized isolated sandbox. An empty path is treated
// as "nothing meaningful to check" and always passes; any non-empty path
// that fails to resolve to an isolated sandbox blocks.
func RequireIsolated(op, path string) error {
	if !testing.Testing() {
		return nil
	}
	if path == "" || IsIsolatedSandbox(path) {
		return nil
	}
	return fmt.Errorf("gt-8ik guard: refusing %s from a test binary — %q is not an isolated test sandbox (expected a path under a temp dir; see gt-8ik and this repo's TestMain pattern)", op, path)
}

// RequireIsolatedSocket fails closed when called from a test binary and
// socket exactly matches the tmux socket the live (non-isolated) GT_ROOT
// would derive as its default. This repo's existing tests use many
// different unique-per-run socket names (not one fixed naming convention),
// so isolation is recognized by "does this operation target the live
// socket", not by pattern-matching the name itself. If GT_ROOT/GT_TOWN_ROOT
// isn't set, or is itself an isolated sandbox, there is nothing live to
// compare against and the call passes.
func RequireIsolatedSocket(op, socket string) error {
	if !testing.Testing() {
		return nil
	}
	townRoot := LiveTownRoot()
	if townRoot == "" || IsIsolatedSandbox(townRoot) {
		return nil
	}
	// An empty socket means tmux falls back to ambient/default socket
	// resolution (no -L flag) rather than a named one. That is exactly what
	// a stale/unprotected test binary produces when its own TestMain never
	// called SetDefaultSocket — the realistic "isolation code never ran"
	// case this guard exists for. With a live, non-isolated GT_ROOT in
	// play there is no positive signal this is isolated, so fail closed
	// rather than let an unnamed socket slip through unchecked.
	if socket == "" {
		return fmt.Errorf("gt-8ik guard: refusing %s from a test binary — no isolated tmux socket is configured while GT_ROOT %q resolves to a live town (expected GT_TMUX_SOCKET or tmux.SetDefaultSocket to be set by an isolated TestMain); see gt-8ik", op, townRoot)
	}
	live := LiveSocketName(townRoot)
	if live == "" || socket != live {
		return nil
	}
	return fmt.Errorf("gt-8ik guard: refusing %s from a test binary — socket %q is the live town's derived default socket for GT_ROOT %q; see gt-8ik", op, socket, townRoot)
}

// Block rewires cmd so cmd.Run()/cmd.Start()/cmd.Output() fails immediately
// with reason, without executing anything. Used by call sites that build an
// *exec.Cmd through a void-returning configurator and so cannot change their
// own signature to return an error without touching every caller.
func Block(cmd *exec.Cmd, reason error) {
	fmt.Fprintln(os.Stderr, reason)
	cmd.Path = filepath.Join(os.TempDir(), "gt-8ik-isolation-guard-blocked")
	cmd.Args = []string{cmd.Path}
	cmd.Env = nil
	cmd.Dir = ""
}
