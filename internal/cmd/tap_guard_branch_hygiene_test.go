package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

func TestTapGuardBranchHygiene_NotAGitRepo_FailsOpen(t *testing.T) {
	dir := t.TempDir() // no git init
	chdirForTest(t, dir)

	cmd := newTapGuardBranchHygieneTestCommand()
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected fail-open (nil error) outside a git repo, got: %v", err)
	}
}
