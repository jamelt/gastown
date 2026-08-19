package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewWorktreeGitdirCheck(t *testing.T) {
	check := NewWorktreeGitdirCheck()

	if check.Name() != "worktree-gitdir-valid" {
		t.Errorf("expected name 'worktree-gitdir-valid', got %q", check.Name())
	}

	if !check.CanFix() {
		t.Error("expected CanFix to return true")
	}
}

func TestWorktreeGitdirCheck_NoRigs(t *testing.T) {
	tmpDir := t.TempDir()

	check := NewWorktreeGitdirCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK when no rigs exist, got %v", result.Status)
	}
}

func TestWorktreeGitdirCheck_ValidWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"

	// Create rig structure with config.json
	rigDir := filepath.Join(tmpDir, rigName)
	if err := os.MkdirAll(filepath.Join(rigDir, "refinery", "rig"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigDir, "config.json"), []byte(`{"repo":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a fake .repo.git/worktrees/rig directory
	worktreeEntry := filepath.Join(rigDir, ".repo.git", "worktrees", "rig")
	if err := os.MkdirAll(worktreeEntry, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a .git file in refinery/rig that points to the worktree entry
	gitFile := filepath.Join(rigDir, "refinery", "rig", ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: "+worktreeEntry+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewWorktreeGitdirCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK for valid worktree, got %v: %s", result.Status, result.Message)
	}
}

func TestWorktreeGitdirCheck_BrokenGitdir_MissingBareRepo(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"

	// Create rig structure
	rigDir := filepath.Join(tmpDir, rigName)
	if err := os.MkdirAll(filepath.Join(rigDir, "refinery", "rig"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigDir, "config.json"), []byte(`{"repo":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create .git file pointing to non-existent .repo.git
	gitFile := filepath.Join(rigDir, "refinery", "rig", ".git")
	brokenPath := filepath.Join(rigDir, ".repo.git", "worktrees", "rig")
	if err := os.WriteFile(gitFile, []byte("gitdir: "+brokenPath+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewWorktreeGitdirCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for broken gitdir, got %v", result.Status)
	}
	if !strings.Contains(result.Message, "broken gitdir") {
		t.Errorf("expected message about broken gitdir, got %q", result.Message)
	}
	if len(result.Details) == 0 {
		t.Error("expected details about the broken worktree")
	}
	if !strings.Contains(result.Details[0], ".repo.git missing") {
		t.Errorf("expected detail about missing .repo.git, got %q", result.Details[0])
	}
}

func TestWorktreeGitdirCheck_BrokenGitdir_MissingWorktreeEntry(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"

	// Create rig structure
	rigDir := filepath.Join(tmpDir, rigName)
	if err := os.MkdirAll(filepath.Join(rigDir, "refinery", "rig"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigDir, "config.json"), []byte(`{"repo":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create .repo.git but WITHOUT the worktree entry
	if err := os.MkdirAll(filepath.Join(rigDir, ".repo.git", "worktrees"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create .git file pointing to missing worktree entry
	gitFile := filepath.Join(rigDir, "refinery", "rig", ".git")
	brokenPath := filepath.Join(rigDir, ".repo.git", "worktrees", "rig")
	if err := os.WriteFile(gitFile, []byte("gitdir: "+brokenPath+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewWorktreeGitdirCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for missing worktree entry, got %v", result.Status)
	}
	if len(result.Details) == 0 {
		t.Error("expected details about the broken worktree")
	}
	if !strings.Contains(result.Details[0], "worktree entry missing") {
		t.Errorf("expected detail about missing worktree entry, got %q", result.Details[0])
	}
}

func TestWorktreeGitdirCheck_CloneNotWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"

	// Create rig with refinery/rig as a regular clone (directory .git, not file)
	rigDir := filepath.Join(tmpDir, rigName)
	if err := os.MkdirAll(filepath.Join(rigDir, "refinery", "rig", ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigDir, "config.json"), []byte(`{"repo":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewWorktreeGitdirCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	// Should pass - regular clones (directory .git) are not checked
	if result.Status != StatusOK {
		t.Errorf("expected StatusOK for regular clone, got %v: %s", result.Status, result.Message)
	}
}

func TestWorktreeGitdirCheck_MalformedGitFile(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"

	rigDir := filepath.Join(tmpDir, rigName)
	if err := os.MkdirAll(filepath.Join(rigDir, "refinery", "rig"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigDir, "config.json"), []byte(`{"repo":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a malformed .git file
	gitFile := filepath.Join(rigDir, "refinery", "rig", ".git")
	if err := os.WriteFile(gitFile, []byte("not a valid gitdir reference\n"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewWorktreeGitdirCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for malformed .git file, got %v", result.Status)
	}
	if len(result.Details) == 0 {
		t.Error("expected details about malformed .git file")
	}
	if !strings.Contains(result.Details[0], "malformed") {
		t.Errorf("expected detail about malformed file, got %q", result.Details[0])
	}
}

func TestWorktreeGitdirCheck_PolecatWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"

	// Create rig structure with a polecat worktree (new structure)
	rigDir := filepath.Join(tmpDir, rigName)
	polecatDir := filepath.Join(rigDir, "polecats", "alpha", rigName)
	if err := os.MkdirAll(polecatDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigDir, "config.json"), []byte(`{"repo":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create broken .git file for polecat
	gitFile := filepath.Join(polecatDir, ".git")
	brokenPath := filepath.Join(rigDir, ".repo.git", "worktrees", "alpha")
	if err := os.WriteFile(gitFile, []byte("gitdir: "+brokenPath+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewWorktreeGitdirCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for broken polecat worktree, got %v", result.Status)
	}
}

func TestWorktreeGitdirCheck_MayorWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"

	rigDir := filepath.Join(tmpDir, rigName)
	if err := os.MkdirAll(filepath.Join(rigDir, "mayor", "rig"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigDir, "config.json"), []byte(`{"repo":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create broken .git file for mayor/rig
	gitFile := filepath.Join(rigDir, "mayor", "rig", ".git")
	brokenPath := filepath.Join(rigDir, ".repo.git", "worktrees", "mayor")
	if err := os.WriteFile(gitFile, []byte("gitdir: "+brokenPath+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewWorktreeGitdirCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for broken mayor worktree, got %v", result.Status)
	}
	if len(result.Details) == 0 || !strings.Contains(result.Details[0], "mayor") {
		t.Errorf("expected detail mentioning mayor/rig, got %v", result.Details)
	}
}

func TestWorktreeGitdirCheck_CrewWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	rigName := "testrig"

	rigDir := filepath.Join(tmpDir, rigName)
	crewPath := filepath.Join(rigDir, "crew", "alice")
	if err := os.MkdirAll(crewPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigDir, "config.json"), []byte(`{"repo":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}
	// A README.md at crew/ root (created by AddRig) must not be mistaken for a
	// crew member worktree.
	if err := os.WriteFile(filepath.Join(rigDir, "crew", "README.md"), []byte("crew workspaces\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create broken .git file for crew/alice (single-level worktree, unlike polecats)
	gitFile := filepath.Join(crewPath, ".git")
	brokenPath := filepath.Join(rigDir, ".repo.git", "worktrees", "alice")
	if err := os.WriteFile(gitFile, []byte("gitdir: "+brokenPath+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewWorktreeGitdirCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for broken crew worktree, got %v", result.Status)
	}
	if len(result.Details) == 0 || !strings.Contains(result.Details[0], filepath.Join("crew", "alice")) {
		t.Errorf("expected detail mentioning crew/alice, got %v", result.Details)
	}
}

func TestWorktreeGitdirCheck_RigFilter(t *testing.T) {
	tmpDir := t.TempDir()

	// Create two rigs, one with broken worktree
	for _, rigName := range []string{"goodrig", "badrig"} {
		rigDir := filepath.Join(tmpDir, rigName)
		if err := os.MkdirAll(filepath.Join(rigDir, "refinery", "rig"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(rigDir, "config.json"), []byte(`{"repo":"test"}`), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Create broken .git file only in badrig
	gitFile := filepath.Join(tmpDir, "badrig", "refinery", "rig", ".git")
	brokenPath := filepath.Join(tmpDir, "badrig", ".repo.git", "worktrees", "rig")
	if err := os.WriteFile(gitFile, []byte("gitdir: "+brokenPath+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewWorktreeGitdirCheck()

	// When checking only goodrig, should pass
	ctx := &CheckContext{TownRoot: tmpDir, RigName: "goodrig"}
	result := check.Run(ctx)
	if result.Status != StatusOK {
		t.Errorf("expected StatusOK when filtering to goodrig, got %v", result.Status)
	}

	// When checking badrig, should fail
	check2 := NewWorktreeGitdirCheck()
	ctx2 := &CheckContext{TownRoot: tmpDir, RigName: "badrig"}
	result2 := check2.Run(ctx2)
	if result2.Status != StatusError {
		t.Errorf("expected StatusError when filtering to badrig, got %v", result2.Status)
	}
}

// ── New tests for hq-c6u: relocation and deacon dogs ──────────────────── //

func TestWorktreeGitdirCheck_RelocatedWorktree(t *testing.T) {
	// Simulate rsync from /old/prefix/gt to tmpDir (new town root).
	// The .git file contains an absolute path with the old prefix,
	// but .repo.git exists at the new location.
	tmpDir := t.TempDir()
	rigName := "myrig"

	// Create rig with .repo.git at the new (correct) location
	rigDir := filepath.Join(tmpDir, rigName)
	if err := os.MkdirAll(filepath.Join(rigDir, "refinery", "rig"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigDir, "config.json"), []byte(`{"repo":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(rigDir, ".repo.git", "worktrees"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create .git file with OLD prefix path (simulating rsync from another machine)
	gitFile := filepath.Join(rigDir, "refinery", "rig", ".git")
	oldPath := "/Users/olduser/gt/" + rigName + "/.repo.git/worktrees/rig"
	if err := os.WriteFile(gitFile, []byte("gitdir: "+oldPath+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewWorktreeGitdirCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for relocated worktree, got %v", result.Status)
	}
	if len(result.Details) == 0 {
		t.Fatal("expected details about relocated worktree")
	}
	// Should say "relocated" not ".repo.git missing"
	if !strings.Contains(result.Details[0], "relocated") {
		t.Errorf("expected 'relocated' in detail, got %q", result.Details[0])
	}

	// Verify the corrected bare repo was found
	if len(check.brokenWorktrees) != 1 {
		t.Fatalf("expected 1 broken worktree, got %d", len(check.brokenWorktrees))
	}
	bw := check.brokenWorktrees[0]
	expectedCorrected := filepath.Join(rigDir, ".repo.git")
	if bw.correctedBareRepo != expectedCorrected {
		t.Errorf("expected correctedBareRepo=%q, got %q", expectedCorrected, bw.correctedBareRepo)
	}
}

func TestWorktreeGitdirCheck_DeaconDogs(t *testing.T) {
	// Simulate deacon/dogs/<dogname>/<rigname>/.git pointing to stale paths.
	tmpDir := t.TempDir()
	rigName := "myrig"

	// Create the rig with .repo.git
	rigDir := filepath.Join(tmpDir, rigName)
	if err := os.MkdirAll(filepath.Join(rigDir, ".repo.git", "worktrees"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigDir, "config.json"), []byte(`{"repo":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create deacon/dogs/alpha/myrig/ with a broken .git file
	dogWtDir := filepath.Join(tmpDir, "deacon", "dogs", "alpha", rigName)
	if err := os.MkdirAll(dogWtDir, 0755); err != nil {
		t.Fatal(err)
	}

	gitFile := filepath.Join(dogWtDir, ".git")
	oldPath := "/old/prefix/gt/" + rigName + "/.repo.git/worktrees/myrig1"
	if err := os.WriteFile(gitFile, []byte("gitdir: "+oldPath+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewWorktreeGitdirCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError for broken deacon dog worktree, got %v", result.Status)
	}
	if len(result.Details) == 0 {
		t.Fatal("expected details about broken deacon dog worktree")
	}
	// Should mention deacon/dogs path (normalize separators for Windows compatibility)
	normalizedDetail := filepath.ToSlash(result.Details[0])
	if !strings.Contains(normalizedDetail, "deacon/dogs/alpha") {
		t.Errorf("expected deacon/dogs/alpha in detail, got %q", result.Details[0])
	}
	// Should identify as relocated (since .repo.git exists at correct location)
	if !strings.Contains(result.Details[0], "relocated") {
		t.Errorf("expected 'relocated' in detail, got %q", result.Details[0])
	}
}

func TestWorktreeGitdirCheck_DeaconDogs_MultipleDogs(t *testing.T) {
	// Multiple dogs with broken worktrees for the same rig.
	tmpDir := t.TempDir()
	rigName := "testrig"

	// Create rig
	rigDir := filepath.Join(tmpDir, rigName)
	if err := os.MkdirAll(filepath.Join(rigDir, ".repo.git", "worktrees"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigDir, "config.json"), []byte(`{"repo":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create 3 dogs with broken worktrees
	for _, dog := range []string{"alpha", "bravo", "charlie"} {
		dogWtDir := filepath.Join(tmpDir, "deacon", "dogs", dog, rigName)
		if err := os.MkdirAll(dogWtDir, 0755); err != nil {
			t.Fatal(err)
		}
		gitFile := filepath.Join(dogWtDir, ".git")
		oldPath := "/old/path/" + rigName + "/.repo.git/worktrees/" + rigName + "_" + dog
		if err := os.WriteFile(gitFile, []byte("gitdir: "+oldPath+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	check := NewWorktreeGitdirCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusError {
		t.Errorf("expected StatusError, got %v", result.Status)
	}
	if !strings.Contains(result.Message, "3 worktree") {
		t.Errorf("expected 3 broken worktrees, got %q", result.Message)
	}
}

func TestWorktreeGitdirCheck_FixRepairsLinkWithoutReplacingWorktree(t *testing.T) {
	tmpDir, rigDir, bareRepo, worktree := setupRepairableWorktree(t)
	branch := "polecat/alpha/gt-yuwe+test"

	if err := os.WriteFile(filepath.Join(worktree, "README.md"), []byte("local edit\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "sentinel"), []byte("keep me"), 0644); err != nil {
		t.Fatal(err)
	}

	// The existing admin entry and index must survive; only the link is stale.
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: /old/town/testrig/.repo.git/worktrees/alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewWorktreeGitdirCheck()
	ctx := &CheckContext{TownRoot: tmpDir}
	if result := check.Run(ctx); result.Status != StatusError {
		t.Fatalf("expected broken worktree, got %v: %s", result.Status, result.Message)
	}
	if err := check.Fix(ctx); err != nil {
		t.Fatal(err)
	}

	if got := doctorGit(t, worktree, "branch", "--show-current"); got != branch {
		t.Errorf("branch = %q, want %q", got, branch)
	}
	if got := doctorGit(t, worktree, "status", "--porcelain"); got != "M README.md\n?? sentinel" {
		t.Errorf("status = %q, want preserved tracked and untracked changes", got)
	}
	if got := doctorGit(t, worktree, "diff", "--", "README.md"); !strings.Contains(got, "+local edit") {
		t.Errorf("tracked edit was not preserved: %s", got)
	}
	if got := doctorGit(t, worktree, "ls-files", "--stage"); got == "" {
		t.Error("repaired worktree has an empty index")
	}
	for _, path := range []string{".beads/config.yaml", ".beads/metadata.json"} {
		if got := doctorGit(t, worktree, "ls-files", "-v", "--", path); !strings.HasPrefix(got, "S "+path) {
			t.Errorf("skip-worktree bit for %s = %q", path, got)
		}
	}
	if got := doctorGit(t, bareRepo, "worktree", "list", "--porcelain"); !strings.Contains(got, "worktree "+worktree) {
		t.Errorf("worktree not registered after repair: %s", got)
	}
	if result := check.Run(ctx); result.Status != StatusOK {
		t.Errorf("expected repaired worktree to validate, got %v: %s", result.Status, result.Message)
	}
	_ = rigDir
}

func TestWorktreeGitdirCheck_FixRefusesMissingMetadataWithoutMutation(t *testing.T) {
	tmpDir, _, bareRepo, worktree := setupRepairableWorktree(t)
	brokenGitFile := []byte("gitdir: /old/town/testrig/.repo.git/worktrees/alpha\n")
	if err := os.WriteFile(filepath.Join(worktree, ".git"), brokenGitFile, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(bareRepo, "worktrees", "testrig")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "sentinel"), []byte("keep me"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewWorktreeGitdirCheck()
	ctx := &CheckContext{TownRoot: tmpDir}
	check.Run(ctx)
	if err := check.Fix(ctx); err == nil {
		t.Fatal("expected missing metadata repair to fail")
	}
	if got, err := os.ReadFile(filepath.Join(worktree, ".git")); err != nil || string(got) != string(brokenGitFile) {
		t.Errorf(".git mutated after failed repair: %q, %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(worktree, "sentinel")); err != nil || string(got) != "keep me" {
		t.Errorf("worktree content mutated after failed repair: %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(bareRepo, "worktrees", "testrig")); !os.IsNotExist(err) {
		t.Errorf("failed repair recreated worktree metadata: %v", err)
	}
}

func setupRepairableWorktree(t *testing.T) (tmpDir, rigDir, bareRepo, worktree string) {
	t.Helper()
	tmpDir = t.TempDir()
	rigDir = filepath.Join(tmpDir, "testrig")
	bareRepo = filepath.Join(rigDir, ".repo.git")
	seed := filepath.Join(tmpDir, "seed")
	worktree = filepath.Join(rigDir, "polecats", "alpha", "testrig")
	if err := os.MkdirAll(rigDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigDir, "config.json"), []byte(`{"repo":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}
	doctorGit(t, "", "init", "--bare", "--initial-branch=main", bareRepo)
	doctorGit(t, "", "init", "--initial-branch=main", seed)
	doctorGit(t, seed, "config", "user.email", "test@example.com")
	doctorGit(t, seed, "config", "user.name", "Test User")
	for path, content := range map[string]string{"README.md": "initial\n", ".beads/config.yaml": "config\n", ".beads/metadata.json": "metadata\n"} {
		fullPath := filepath.Join(seed, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	doctorGit(t, seed, "add", ".")
	doctorGit(t, seed, "commit", "-m", "initial")
	doctorGit(t, seed, "remote", "add", "origin", bareRepo)
	doctorGit(t, seed, "push", "origin", "main")
	doctorGit(t, bareRepo, "worktree", "add", "-b", "polecat/alpha/gt-yuwe+test", worktree, "main")
	doctorGit(t, worktree, "update-index", "--skip-worktree", "--", ".beads/config.yaml", ".beads/metadata.json")
	return tmpDir, rigDir, bareRepo, worktree
}

func doctorGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{}, args...)
	if dir != "" {
		cmdArgs = append([]string{"-C", dir}, cmdArgs...)
	}
	out, err := exec.Command("git", cmdArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(cmdArgs, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestWorktreeGitdirCheck_FixRepairsAllRolesAfterTownMove simulates retiring
// an old town path: refinery, mayor, and crew worktrees are all rsync'd to a
// new town root, leaving every .git file (and the bare repo's reciprocal
// worktrees/<name>/gitdir back-reference) pointing at the old, now-gone
// prefix. Doctor must find and repair all three roles, and each worktree
// must be independently usable (git status, git checkout) afterward.
func TestWorktreeGitdirCheck_FixRepairsAllRolesAfterTownMove(t *testing.T) {
	oldTownRoot := t.TempDir()
	newTownRoot := t.TempDir()
	rigName := "testrig"

	oldRigDir := filepath.Join(oldTownRoot, rigName)
	bareRepo := filepath.Join(oldRigDir, ".repo.git")
	seed := filepath.Join(oldTownRoot, "seed")
	if err := os.MkdirAll(oldRigDir, 0755); err != nil {
		t.Fatal(err)
	}
	doctorGit(t, "", "init", "--bare", "--initial-branch=main", bareRepo)
	doctorGit(t, "", "init", "--initial-branch=main", seed)
	doctorGit(t, seed, "config", "user.email", "test@example.com")
	doctorGit(t, seed, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}
	doctorGit(t, seed, "add", ".")
	doctorGit(t, seed, "commit", "-m", "initial")
	doctorGit(t, seed, "remote", "add", "origin", bareRepo)
	doctorGit(t, seed, "push", "origin", "main")

	roles := map[string]string{
		"refinery": filepath.Join(oldRigDir, "refinery", "rig"),
		"mayor":    filepath.Join(oldRigDir, "mayor", "rig"),
		"crew":     filepath.Join(oldRigDir, "crew", "alice"),
	}
	for name, worktree := range roles {
		doctorGit(t, bareRepo, "worktree", "add", "-b", "role-"+name, worktree, "main")
	}

	// Simulate an rsync move: copy the whole old town root to the new one,
	// then remove the old path entirely so nothing can fall back to it.
	if err := copyDir(t, oldTownRoot, newTownRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(oldTownRoot); err != nil {
		t.Fatal(err)
	}

	newRigDir := filepath.Join(newTownRoot, rigName)
	if err := os.WriteFile(filepath.Join(newRigDir, "config.json"), []byte(`{"repo":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewWorktreeGitdirCheck()
	ctx := &CheckContext{TownRoot: newTownRoot}
	result := check.Run(ctx)
	if result.Status != StatusError {
		t.Fatalf("expected StatusError before repair, got %v: %s", result.Status, result.Message)
	}
	if len(check.brokenWorktrees) != len(roles) {
		t.Fatalf("expected %d broken worktrees, got %d: %+v", len(roles), len(check.brokenWorktrees), result.Details)
	}

	if err := check.Fix(ctx); err != nil {
		t.Fatalf("Fix failed: %v", err)
	}

	// Every role must be independently usable after repair: git status must
	// work, and checkout back to main must succeed (proving the branch and
	// index survived, not just the link).
	for name, oldPath := range roles {
		worktree := filepath.Join(newRigDir, strings.TrimPrefix(oldPath, oldRigDir+string(filepath.Separator)))
		if got := doctorGit(t, worktree, "status", "--porcelain"); got != "" {
			t.Errorf("%s: expected clean status after repair, got %q", name, got)
		}
		if got := doctorGit(t, worktree, "branch", "--show-current"); got != "role-"+name {
			t.Errorf("%s: branch = %q, want %q (repair must not force HEAD to default branch)", name, got, "role-"+name)
		}
		doctorGit(t, worktree, "checkout", "main")
		doctorGit(t, worktree, "checkout", "role-"+name)
	}

	if result := check.Run(ctx); result.Status != StatusOK {
		t.Errorf("expected all worktrees to validate after repair, got %v: %s", result.Status, result.Message)
	}
}

func copyDir(t *testing.T, src, dst string) error {
	t.Helper()
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(linkTarget, target)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

func TestWorktreeGitdirCheck_NoDeaconDogs(t *testing.T) {
	// Town with no deacon/dogs should still pass.
	tmpDir := t.TempDir()

	check := NewWorktreeGitdirCheck()
	ctx := &CheckContext{TownRoot: tmpDir}

	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK for town with no deacon/dogs, got %v", result.Status)
	}
}
