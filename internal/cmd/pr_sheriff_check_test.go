package cmd

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// initBranchHygieneTestRepo creates a bare "upstream" repo plus a local repo
// with origin pointing at it, on branch "main" with one initial commit.
// Shared by pr_sheriff_check_test.go and tap_guard_branch_hygiene_test.go.
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

func newPRSheriffCheckTestCommand(t *testing.T) *cobra.Command {
	t.Helper()
	oldBase, oldGate, oldJSON := prSheriffCheckBase, prSheriffCheckMergeGate, prSheriffCheckJSON
	prSheriffCheckBase, prSheriffCheckMergeGate, prSheriffCheckJSON = "", false, false
	t.Cleanup(func() {
		prSheriffCheckBase, prSheriffCheckMergeGate, prSheriffCheckJSON = oldBase, oldGate, oldJSON
	})

	cmd := &cobra.Command{Use: "pr-sheriff-check", RunE: runPRSheriffCheck}
	cmd.Flags().StringVar(&prSheriffCheckBase, "base", "", "")
	cmd.Flags().BoolVar(&prSheriffCheckMergeGate, "merge-gate", false, "")
	cmd.Flags().BoolVar(&prSheriffCheckJSON, "json", false, "")
	return cmd
}

func runPRSheriffCheckCommandTest(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd.SetArgs(args)
	err := cmd.Execute()

	w.Close()
	os.Stdout = oldStdout
	out, _ := io.ReadAll(r)
	return string(out), err
}

func TestPRSheriffCheck_CleanBranch_Passes(t *testing.T) {
	dir := initBranchHygieneTestRepo(t)
	chdirForTest(t, dir)

	cmd := newPRSheriffCheckTestCommand(t)
	out, err := runPRSheriffCheckCommandTest(t, cmd, "--base", "origin/main")
	if err != nil {
		t.Fatalf("runPRSheriffCheck: %v", err)
	}
	if !containsAll(out, "PASS", "Ahead: 0", "Behind: 0") {
		t.Errorf("unexpected report output: %q", out)
	}
}

func TestPRSheriffCheck_AutoDetectedBase_MatchesExplicitOriginMain(t *testing.T) {
	dir := initBranchHygieneTestRepo(t)
	chdirForTest(t, dir)

	cmd := newPRSheriffCheckTestCommand(t)
	out, err := runPRSheriffCheckCommandTest(t, cmd) // no --base: exercise auto-detection
	if err != nil {
		t.Fatalf("runPRSheriffCheck: %v", err)
	}
	// This fixture has no upstream remote, so CleanBaseRef falls back to origin/main.
	if !containsAll(out, "Base: origin/main", "PASS") {
		t.Errorf("expected auto-detected base to resolve to origin/main and pass, got: %q", out)
	}
}

func TestPRSheriffCheck_FetchFailure_FailsOpenNotFatal(t *testing.T) {
	dir := initBranchHygieneTestRepo(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// Point origin at a path that no longer exists so Fetch fails; the command
	// must still complete using local refs rather than hard-failing on the fetch.
	run("remote", "set-url", "origin", filepath.Join(t.TempDir(), "does-not-exist"))
	chdirForTest(t, dir)

	cmd := newPRSheriffCheckTestCommand(t)
	out, err := runPRSheriffCheckCommandTest(t, cmd, "--base", "origin/main")
	if err != nil {
		t.Fatalf("expected fetch failure to be non-fatal, got error: %v", err)
	}
	if !containsAll(out, "PASS") {
		t.Errorf("expected report to still complete using local refs, got: %q", out)
	}
}

func TestPRSheriffCheck_MergeGate_BlocksOnUnrelatedAhead(t *testing.T) {
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

	cmd := newPRSheriffCheckTestCommand(t)
	out, err := runPRSheriffCheckCommandTest(t, cmd, "--base", "origin/main", "--merge-gate")
	if err == nil {
		t.Fatalf("expected --merge-gate to return an error when blocked, got nil (output: %q)", out)
	}
	if !containsAll(out, "BLOCK") {
		t.Errorf("expected report to show BLOCK, got: %q", out)
	}
}

func TestPRSheriffCheck_WithoutMergeGate_NeverFailsOnBlock(t *testing.T) {
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

	cmd := newPRSheriffCheckTestCommand(t)
	out, err := runPRSheriffCheckCommandTest(t, cmd, "--base", "origin/main")
	if err != nil {
		t.Fatalf("expected nil error without --merge-gate even when blocked, got: %v", err)
	}
	if !containsAll(out, "BLOCK") {
		t.Errorf("expected report to still show BLOCK, got: %q", out)
	}
}

func TestPRSheriffCheck_JSONOutput_Shape(t *testing.T) {
	dir := initBranchHygieneTestRepo(t)
	chdirForTest(t, dir)

	cmd := newPRSheriffCheckTestCommand(t)
	out, err := runPRSheriffCheckCommandTest(t, cmd, "--base", "origin/main", "--json")
	if err != nil {
		t.Fatalf("runPRSheriffCheck: %v", err)
	}

	var result prSheriffCheckResult
	if jsonErr := json.Unmarshal([]byte(out), &result); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", jsonErr, out)
	}
	if result.Base != "origin/main" {
		t.Errorf("Base = %q, want origin/main", result.Base)
	}
	if result.Severity != "clean" {
		t.Errorf("Severity = %q, want clean", result.Severity)
	}
	if !result.MergePathAllowed {
		t.Errorf("MergePathAllowed = false, want true for a clean branch")
	}
}

func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
