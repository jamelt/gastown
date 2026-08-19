package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initGitRepoWithRemote creates a minimal git repo at dir with an "origin"
// remote pointing at remoteURL, without requiring network access.
func initGitRepoWithRemote(t *testing.T, dir, remoteURL string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test User")
	run("remote", "add", "origin", remoteURL)
}

// installFakeGHForPRView puts a fake `gh` on PATH that answers
// `gh pr view <prNumber> ... --repo <repo>` with headRefName=branch, and
// fails loudly on any other invocation -- catching a wrong --repo (which
// would mean the wrong rig's remote was resolved) or a cwd-dependent
// invocation that omits --repo entirely.
func installFakeGHForPRView(t *testing.T, prNumber int, repo, branch string) {
	t.Helper()
	dir := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "pr" ] && [ "$2" = "view" ] && [ "$3" = "%d" ]; then
  case "$*" in
    *"--repo %s"*)
      printf '{"headRefName":"%s"}\n'
      exit 0
      ;;
  esac
fi
printf 'unexpected gh invocation: %%s\n' "$*" >&2
exit 1
`, prNumber, repo, branch)
	path := filepath.Join(dir, "gh")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestResolvePRBranchUsesTargetRigNotCWD proves gt-vln3: --pr resolution must
// use the target rig's own repo, not whatever repo (if any) the caller's cwd
// happens to be in. Simulates the reported failure mode by running from town
// root, which has no git remotes of its own.
func TestResolvePRBranchUsesTargetRigNotCWD(t *testing.T) {
	townRoot := t.TempDir()
	rigName := "gastown"
	rigRepoDir := filepath.Join(townRoot, rigName, "mayor", "rig")
	initGitRepoWithRemote(t, rigRepoDir, "https://github.com/acme/gastown.git")
	installFakeGHForPRView(t, 4377, "acme/gastown", "gt-pr4377-refresh")

	// town root itself is not a git repo -- this is exactly the caller cwd
	// that broke resolvePRBranch before gt-vln3 (gh pr view: no git remotes found).
	t.Chdir(townRoot)

	branch, err := resolvePRBranch(townRoot, rigName, 4377)
	if err != nil {
		t.Fatalf("resolvePRBranch: %v", err)
	}
	if branch != "gt-pr4377-refresh" {
		t.Fatalf("branch = %q, want gt-pr4377-refresh", branch)
	}
}

// TestResolvePRBranchNoRigRepo verifies a clear error when the target rig has
// no repo (neither .repo.git nor mayor/rig) to resolve PRs against.
func TestResolvePRBranchNoRigRepo(t *testing.T) {
	townRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(townRoot, "gastown"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	_, err := resolvePRBranch(townRoot, "gastown", 4377)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "gastown") {
		t.Fatalf("error %q should name the rig", err.Error())
	}
}

// TestResolvePRBranchNoGitHubRemote verifies a clear error when the target
// rig's repo exists but has no GitHub-resolvable remote.
func TestResolvePRBranchNoGitHubRemote(t *testing.T) {
	townRoot := t.TempDir()
	rigRepoDir := filepath.Join(townRoot, "gastown", "mayor", "rig")
	if err := os.MkdirAll(rigRepoDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cmd := exec.Command("git", "init")
	cmd.Dir = rigRepoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	// No "origin" remote configured.

	_, err := resolvePRBranch(townRoot, "gastown", 4377)
	if err == nil {
		t.Fatal("expected error for missing GitHub remote, got nil")
	}
}
