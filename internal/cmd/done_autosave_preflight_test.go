package cmd

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression coverage for gt-xz2: gt done's uncommitted-work auto-save
// safety net (gt-pvx) must never touch the index or HEAD until a
// non-protected/attached branch and a resolvable source issue are
// established. See incident hq-wisp-a4a1, where a cold-started polecat with
// an empty hook and a stray checkout of main had 12,378 pre-existing/shared
// files auto-added and committed to main.

// installNoAssignmentsBDStub makes any bd invocation succeed instantly with
// "no results", so findAssignedBeadsForAgent's hook lookup resolves quickly
// and deterministically instead of depending on a real bd/Dolt round-trip
// (bd CLI has known flakiness in this test environment — see the skip note
// on TestFindHookedBeadForAgent).
func installNoAssignmentsBDStub(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--allow-stale\" ]; then shift; fi\n" +
		"if [ \"$1\" = \"version\" ]; then echo 'bd stub'; exit 0; fi\n" +
		"echo '[]'\n" +
		"exit 0\n"
	path := filepath.Join(binDir, "bd")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write bd stub: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// writeMixedAutoSaveFixture writes a large, mixed set of tracked
// modifications, untracked source files, and untracked runtime-artifact
// files into repoRoot, mirroring the shape of the incident's 12,378 mixed
// paths. Returns the total number of dirty (non-runtime + runtime) paths
// created.
func writeMixedAutoSaveFixture(t *testing.T, repoRoot string, n int) int {
	t.Helper()
	total := 0
	for i := 0; i < n; i++ {
		p := filepath.Join(repoRoot, "src", fmt.Sprintf("file_%d.go", i))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte(fmt.Sprintf("package src\n// %d\n", i)), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
		total++
	}
	for i := 0; i < n/4; i++ {
		p := filepath.Join(repoRoot, "node_modules", "pkg", fmt.Sprintf("index_%d.js", i))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte("module.exports = {}\n"), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
		total++
	}
	return total
}

func TestRunDoneRefusesAutoSaveOnProtectedBranchWithEmptyHook(t *testing.T) {
	for _, tt := range []struct {
		name       string
		exitStatus string
	}{
		{name: "DEFERRED retry after failed COMPLETED", exitStatus: ExitDeferred},
		{name: "COMPLETED", exitStatus: ExitCompleted},
		{name: "ESCALATED", exitStatus: ExitEscalated},
	} {
		t.Run(tt.name, func(t *testing.T) {
			installNoAssignmentsBDStub(t)
			townRoot, repoRoot := setupDoneGuardWorktree(t, "nested", "shiny")
			// The worktree's git checkout is a stray "main" — the exact
			// cold-start defect from hq-wisp-a4a1: a valid polecat worktree
			// whose HEAD is on the protected branch instead of a feature
			// branch, with no hook.
			doneGuardGitOutput(t, repoRoot, "config", "user.email", "test@test.com")
			doneGuardGitOutput(t, repoRoot, "config", "user.name", "Test")
			if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("# repo\n"), 0644); err != nil {
				t.Fatalf("write README: %v", err)
			}
			doneGuardGitOutput(t, repoRoot, "add", "README.md")
			doneGuardGitOutput(t, repoRoot, "commit", "-m", "initial")
			doneGuardGitOutput(t, repoRoot, "branch", "-M", "main")

			wantPaths := writeMixedAutoSaveFixture(t, repoRoot, 2000)

			beforeStatus := doneGuardGitOutput(t, repoRoot, "status", "--porcelain")
			beforeHead := doneGuardGitOutput(t, repoRoot, "rev-parse", "HEAD")

			resetDoneFlagsForTest(t)
			doneStatus = tt.exitStatus
			setDoneGuardEnv(t, "gastown", "shiny", "gastown/polecats/shiny")

			origDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(repoRoot); err != nil {
				t.Fatalf("chdir repo: %v", err)
			}
			t.Cleanup(func() { _ = os.Chdir(origDir) })

			runErr := runDone(nil, nil)
			if runErr == nil {
				t.Fatal("runDone succeeded, want protected-branch refusal")
			}
			if !strings.Contains(runErr.Error(), "protected branch") {
				t.Fatalf("runDone error = %v, want protected branch refusal", runErr)
			}

			afterStatus := doneGuardGitOutput(t, repoRoot, "status", "--porcelain")
			afterHead := doneGuardGitOutput(t, repoRoot, "rev-parse", "HEAD")
			if afterHead != beforeHead {
				t.Fatalf("HEAD moved from %q to %q; auto-save must not mutate history on refusal", beforeHead, afterHead)
			}
			if afterStatus != beforeStatus {
				t.Fatalf("git status changed after refused auto-save:\nbefore:\n%s\nafter:\n%s", beforeStatus, afterStatus)
			}
			if gotPaths := countFilesUnder(t, repoRoot, "src", "node_modules"); gotPaths != wantPaths {
				t.Fatalf("expected all %d mixed dirty paths to survive untouched on disk, found %d", wantPaths, gotPaths)
			}

			assertAutoSaveRecoveryRecord(t, townRoot, "main", "protected branch", wantPaths)
		})
	}
}

func TestRunDoneRefusesAutoSaveWithoutResolvableIssue(t *testing.T) {
	installNoAssignmentsBDStub(t)
	townRoot, repoRoot := setupDoneGuardWorktree(t, "nested", "shiny")
	doneGuardGitOutput(t, repoRoot, "config", "user.email", "test@test.com")
	doneGuardGitOutput(t, repoRoot, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("# repo\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	doneGuardGitOutput(t, repoRoot, "add", "README.md")
	doneGuardGitOutput(t, repoRoot, "commit", "-m", "initial")
	doneGuardGitOutput(t, repoRoot, "branch", "-M", "main")
	// A legitimate feature branch (not protected) whose name embeds no
	// bead id (no "polecat/" prefix, no issue-shaped hyphenated token), and
	// no hook resolvable from beads (stubbed empty above).
	doneGuardGitOutput(t, repoRoot, "checkout", "-b", "manualcheckout")

	if err := os.WriteFile(filepath.Join(repoRoot, "handler.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("write handler: %v", err)
	}

	beforeStatus := doneGuardGitOutput(t, repoRoot, "status", "--porcelain")
	beforeHead := doneGuardGitOutput(t, repoRoot, "rev-parse", "HEAD")

	resetDoneFlagsForTest(t)
	doneStatus = ExitDeferred
	setDoneGuardEnv(t, "gastown", "shiny", "gastown/polecats/shiny")

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	runErr := runDone(nil, nil)
	if runErr == nil {
		t.Fatal("runDone succeeded, want empty-hook refusal")
	}
	if !strings.Contains(runErr.Error(), "no assigned source issue") {
		t.Fatalf("runDone error = %v, want empty-hook refusal", runErr)
	}

	afterStatus := doneGuardGitOutput(t, repoRoot, "status", "--porcelain")
	afterHead := doneGuardGitOutput(t, repoRoot, "rev-parse", "HEAD")
	if afterHead != beforeHead || afterStatus != beforeStatus {
		t.Fatalf("refused auto-save mutated the worktree: head %q->%q status %q->%q", beforeHead, afterHead, beforeStatus, afterStatus)
	}

	assertAutoSaveRecoveryRecord(t, townRoot, "manualcheckout", "no assigned source issue", 1)
}

func TestRunDoneAutoSavesOnValidHookedFeatureBranch(t *testing.T) {
	installNoAssignmentsBDStub(t)
	_, repoRoot := setupDoneGuardWorktree(t, "nested", "shiny")
	doneGuardGitOutput(t, repoRoot, "config", "user.email", "test@test.com")
	doneGuardGitOutput(t, repoRoot, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("# repo\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	doneGuardGitOutput(t, repoRoot, "add", "README.md")
	doneGuardGitOutput(t, repoRoot, "commit", "-m", "initial")
	doneGuardGitOutput(t, repoRoot, "branch", "-M", "main")
	// A properly named feature branch for a hooked bead — the branch name
	// alone resolves the source issue without any bd round-trip.
	doneGuardGitOutput(t, repoRoot, "checkout", "-b", "polecat/shiny/gt-xz2+abcdef")

	if err := os.WriteFile(filepath.Join(repoRoot, "handler.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("write handler: %v", err)
	}
	beforeHead := doneGuardGitOutput(t, repoRoot, "rev-parse", "HEAD")

	resetDoneFlagsForTest(t)
	doneStatus = ExitDeferred
	setDoneGuardEnv(t, "gastown", "shiny", "gastown/polecats/shiny")
	t.Setenv("GT_TEST_NUDGE_LOG", filepath.Join(t.TempDir(), "nudge.log"))
	origUpdateAgentStateOnDoneFn := updateAgentStateOnDoneFn
	updateAgentStateOnDoneFn = func(cwd, townRoot, exitType, issueID string) error { return nil }
	t.Cleanup(func() { updateAgentStateOnDoneFn = origUpdateAgentStateOnDoneFn })

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	// The rest of a DEFERRED completion may still fail on unstubbed
	// downstream calls (notify/mail) in this minimal fixture — this test
	// only asserts the preflight let the legitimate auto-save through.
	if runErr := runDone(nil, nil); runErr != nil {
		if strings.Contains(runErr.Error(), "refuses to auto-save") || strings.Contains(runErr.Error(), "assigned polecat worktree") {
			t.Fatalf("runDone blocked a valid hooked feature branch: %v", runErr)
		}
	}

	afterHead := doneGuardGitOutput(t, repoRoot, "rev-parse", "HEAD")
	if afterHead == beforeHead {
		t.Fatal("expected auto-save to create a new commit on a valid hooked feature branch")
	}
	subject := doneGuardGitOutput(t, repoRoot, "log", "-1", "--format=%s")
	for _, want := range []string{"gt-xz2", "gastown/polecats/shiny", "gt-pvx safety net"} {
		if !strings.Contains(subject, want) {
			t.Fatalf("auto-save commit subject = %q, want it to contain %q (source/actor provenance)", subject, want)
		}
	}
	status := doneGuardGitOutput(t, repoRoot, "status", "--porcelain")
	if strings.Contains(status, "handler.go") {
		t.Fatalf("handler.go still dirty after auto-save, status:\n%s", status)
	}
}

// countFilesUnder counts regular files under the given subdirectories of
// root, to verify a refused auto-save left every generated path on disk.
func countFilesUnder(t *testing.T, root string, subdirs ...string) int {
	t.Helper()
	total := 0
	for _, subdir := range subdirs {
		err := filepath.WalkDir(filepath.Join(root, subdir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				total++
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", subdir, err)
		}
	}
	return total
}

// assertAutoSaveRecoveryRecord verifies exactly one durable, non-git
// recovery record was written under townRoot/.runtime/gt-done-recovery,
// inventorying the refused auto-save without ever touching git.
func assertAutoSaveRecoveryRecord(t *testing.T, townRoot, wantBranch, wantReasonSubstr string, wantMinPaths int) {
	t.Helper()
	dir := filepath.Join(townRoot, ".runtime", "gt-done-recovery")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading recovery record dir %s: %v", dir, err)
	}
	if len(entries) != 1 {
		t.Fatalf("recovery record dir %s has %d entries, want 1", dir, len(entries))
	}
	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("reading recovery record: %v", err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("recovery record is not valid JSON: %v\n%s", err, data)
	}
	if record["branch"] != wantBranch {
		t.Fatalf("recovery record branch = %v, want %q", record["branch"], wantBranch)
	}
	if totalPaths, ok := record["total_paths"].(float64); !ok || int(totalPaths) < wantMinPaths {
		t.Fatalf("recovery record total_paths = %v, want at least %d", record["total_paths"], wantMinPaths)
	}
	reason, _ := record["reason"].(string)
	if !strings.Contains(reason, wantReasonSubstr) {
		t.Fatalf("recovery record reason = %q, want substring %q", reason, wantReasonSubstr)
	}
}
