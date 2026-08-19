package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newTapGuardBranchHygieneTestCommand() *cobra.Command {
	return &cobra.Command{Use: "branch-hygiene", RunE: runTapGuardBranchHygiene}
}

func TestTapGuardBranchHygiene_CleanBranch_Allows(t *testing.T) {
	dir := initBranchHygieneTestRepo(t)
	chdirForTest(t, dir)

	cmd := newTapGuardBranchHygieneTestCommand()
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected clean branch to be allowed (nil error), got: %v", err)
	}
}

func TestTapGuardBranchHygiene_ContaminatedBranch_BlocksWithSilentExit2(t *testing.T) {
	dir := initBranchHygieneTestRepo(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	for i := 0; i < 60; i++ {
		fname := filepath.Join(dir, "unrelated_"+strconv.Itoa(i)+".txt")
		if err := os.WriteFile(fname, []byte("unrelated"), 0644); err != nil {
			t.Fatal(err)
		}
		run("add", ".")
		run("commit", "-m", "unrelated commit")
	}
	chdirForTest(t, dir)

	cmd := newTapGuardBranchHygieneTestCommand()
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected contaminated branch to be blocked, got nil error")
	}
	silentErr, ok := err.(*SilentExitError)
	if !ok {
		t.Fatalf("expected *SilentExitError, got %T: %v", err, err)
	}
	if silentErr.Code != 2 {
		t.Errorf("exit code = %d, want 2", silentErr.Code)
	}
}

func TestTapGuardBranchHygiene_StaleBehindBranch_BlocksWithSilentExit2(t *testing.T) {
	dir := initBranchHygieneTestRepo(t)
	addStaleBehindCommits(t, dir, 210) // > ContaminationBlockBehind (200)
	chdirForTest(t, dir)

	cmd := newTapGuardBranchHygieneTestCommand()
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected stale-behind branch to be blocked, got nil error")
	}
	silentErr, ok := err.(*SilentExitError)
	if !ok {
		t.Fatalf("expected *SilentExitError, got %T: %v", err, err)
	}
	if silentErr.Code != 2 {
		t.Errorf("exit code = %d, want 2", silentErr.Code)
	}
}

func TestTapGuardBranchHygiene_NotAGitRepo_FailsOpen(t *testing.T) {
	dir := t.TempDir() // no git init
	chdirForTest(t, dir)

	cmd := newTapGuardBranchHygieneTestCommand()
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected fail-open (nil error) outside a git repo, got: %v", err)
	}
}

// initBranchHygieneTestRepo creates a git worktree with an origin/main remote
// and returns its path. It previously lived in pr_sheriff_check_test.go, which
// was deleted with the PR Sheriff removal (commit 294649465); relocated here
// since the branch-hygiene guard and its test remain. See gt-ej72.
func initBranchHygieneTestRepo(t *testing.T) string {
	t.Helper()
	upstreamDir := t.TempDir()
	if out, err := exec.Command("git", "init", "--bare", "-b", "main", upstreamDir).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}

	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	run("remote", "add", "origin", upstreamDir)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("init"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "initial commit")
	run("push", "origin", "main")

	return dir
}

// chdirForTest changes into dir for the duration of the test, restoring the
// previous working directory on cleanup. Relocated from the deleted
// pr_sheriff_check_test.go (commit 294649465). See gt-ej72.
func chdirForTest(t *testing.T, dir string) {
	t.Helper()
	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldCwd)
	})
}

// addStaleBehindCommits pushes n commits to origin/main so the local branch in
// dir becomes n commits behind. Relocated from the deleted pr_sheriff_check_test.go
// (commit 294649465). See gt-ej72.
func addStaleBehindCommits(t *testing.T, dir string, n int) {
	t.Helper()
	remoteOut, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		t.Fatalf("git remote get-url origin: %v", err)
	}
	upstreamDir := strings.TrimSpace(string(remoteOut))

	scratch := t.TempDir()
	if out, err := exec.Command("git", "clone", upstreamDir, scratch).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = scratch
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	for i := 0; i < n; i++ {
		fname := filepath.Join(scratch, "upstream_"+strconv.Itoa(i)+".txt")
		if err := os.WriteFile(fname, []byte("upstream progress"), 0644); err != nil {
			t.Fatal(err)
		}
		run("add", ".")
		run("commit", "-m", "upstream commit")
	}
	run("push", "origin", "main")
}
