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

// addStaleBehindCommits pushes n additional commits directly to the bare
// "origin" remote of a repo created by initBranchHygieneTestRepo, without
// touching the local checkout -- simulating a base branch that has moved on
// while dir's local branch stayed put. Once ResolveContaminationCheck fetches
// origin, this shows up as a genuine "Behind" reading from dir's perspective.
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

func TestPRSheriffCheck_MergeGate_BlocksOnStaleBehind(t *testing.T) {
	dir := initBranchHygieneTestRepo(t)
	addStaleBehindCommits(t, dir, 210) // > ContaminationBlockBehind (200)
	chdirForTest(t, dir)

	cmd := newPRSheriffCheckTestCommand(t)
	out, err := runPRSheriffCheckCommandTest(t, cmd, "--base", "origin/main", "--merge-gate")
	if err == nil {
		t.Fatalf("expected --merge-gate to block a stale-behind branch, got nil error (output: %q)", out)
	}
	if !containsAll(out, "BLOCK", "Behind: 210") {
		t.Errorf("expected report to show BLOCK with Behind: 210, got: %q", out)
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

func TestPRSheriffCheck_JSONOutput_Shape_CleanBranch(t *testing.T) {
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
	if result.Ahead != 0 || result.Behind != 0 {
		t.Errorf("Ahead/Behind = %d/%d, want 0/0", result.Ahead, result.Behind)
	}
	if result.Severity != "clean" {
		t.Errorf("Severity = %q, want clean", result.Severity)
	}
	if len(result.Reasons) != 0 {
		t.Errorf("Reasons = %v, want empty", result.Reasons)
	}
	if result.MergeGate {
		t.Errorf("MergeGate = true, want false (flag not passed)")
	}
	if !result.MergePathAllowed {
		t.Errorf("MergePathAllowed = false, want true for a clean branch")
	}
	// The JSON encoding of Reasons must be an array, not null (nil slices
	// marshal to null unless explicitly initialized).
	if !strings.Contains(out, `"reasons":[]`) {
		t.Errorf(`expected "reasons":[] in JSON output, got: %q`, out)
	}
}

func TestPRSheriffCheck_JSONOutput_Shape_WarnLevel(t *testing.T) {
	dir := initBranchHygieneTestRepo(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// 25 is between ContaminationWarnAhead (20) and ContaminationBlockAhead
	// (50): warn, not block.
	for i := 0; i < 25; i++ {
		fname := filepath.Join(dir, "unrelated_"+strconv.Itoa(i)+".txt")
		if err := os.WriteFile(fname, []byte("unrelated"), 0644); err != nil {
			t.Fatal(err)
		}
		run("add", ".")
		run("commit", "-m", "unrelated commit")
	}
	chdirForTest(t, dir)

	cmd := newPRSheriffCheckTestCommand(t)
	out, err := runPRSheriffCheckCommandTest(t, cmd, "--base", "origin/main", "--merge-gate", "--json")
	if err != nil {
		t.Fatalf("expected warn-level (not block) to not error even with --merge-gate, got: %v", err)
	}

	var result prSheriffCheckResult
	if jsonErr := json.Unmarshal([]byte(out), &result); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", jsonErr, out)
	}
	if result.Ahead != 25 {
		t.Errorf("Ahead = %d, want 25", result.Ahead)
	}
	if result.Behind != 0 {
		t.Errorf("Behind = %d, want 0", result.Behind)
	}
	if result.Severity != "warn" {
		t.Errorf("Severity = %q, want warn", result.Severity)
	}
	if len(result.Reasons) != 1 {
		t.Errorf("Reasons = %v, want exactly 1 reason", result.Reasons)
	}
	if !result.MergeGate {
		t.Errorf("MergeGate = false, want true (flag was passed)")
	}
	if !result.MergePathAllowed {
		t.Errorf("MergePathAllowed = false, want true (warn severity does not block)")
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
