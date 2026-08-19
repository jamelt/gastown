package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/git"
)

// initTownConfigTestRepo creates a throwaway git repo seeded with the three
// town-level config files (mayor/rigs.json, mayor/daemon.json,
// .beads/routes.jsonl) already committed, mirroring a real town root.
func initTownConfigTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test User")

	for _, f := range townConfigFiles {
		full := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatalf("mkdir for %s: %v", f, err)
		}
		if err := os.WriteFile(full, []byte("{}\n"), 0644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}
	run("add", ".")
	run("commit", "-m", "initial town config")

	return dir
}

func commitLog(t *testing.T, dir string) []string {
	t.Helper()
	cmd := exec.Command("git", "log", "--format=%s")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// TestCommitPreExistingTownConfigDirt_CommitsSeparately reproduces gt-2xrj:
// an unrelated edit already sitting on daemon.json before a rig operation
// begins must land in its own honestly-labeled commit, not get silently
// folded into the later "register rig" commit.
func TestCommitPreExistingTownConfigDirt_CommitsSeparately(t *testing.T) {
	dir := initTownConfigTestRepo(t)
	g := git.NewGit(dir)

	// Simulate an out-of-band edit (e.g. a manual patrol toggle) landing on
	// daemon.json before any rig-add/adopt operation has touched anything.
	daemonPath := filepath.Join(dir, "mayor", "daemon.json")
	if err := os.WriteFile(daemonPath, []byte(`{"patrols":{"wisp_reaper":{"enabled":false}}}`+"\n"), 0644); err != nil {
		t.Fatalf("write daemon.json: %v", err)
	}

	commitPreExistingTownConfigDirt(g, "gastown_src")

	log := commitLog(t, dir)
	if len(log) != 2 {
		t.Fatalf("expected 2 commits (initial + pre-existing-dirt), got %d: %v", len(log), log)
	}
	latest := log[0]
	if !strings.Contains(latest, "chore: commit pending") {
		t.Errorf("latest commit message = %q, want it to start with %q", latest, "chore: commit pending")
	}
	if strings.Contains(latest, "register rig") {
		t.Errorf("pre-existing dirt must not be attributed to rig registration, got message %q", latest)
	}
	if !strings.Contains(latest, filepath.Join("mayor", "daemon.json")) {
		t.Errorf("commit message %q should name the dirty file", latest)
	}

	status, err := g.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Clean {
		t.Errorf("expected clean tree after committing pre-existing dirt, got %+v", status)
	}

	// Now simulate the operation's own change plus its real commit, and
	// confirm it is NOT polluted by the already-committed pre-existing edit.
	if err := os.WriteFile(filepath.Join(dir, "mayor", "rigs.json"), []byte(`{"rigs":{"gastown_src":{}}}`+"\n"), 0644); err != nil {
		t.Fatalf("write rigs.json: %v", err)
	}
	commitTownConfigChanges(dir, "gastown_src")

	log = commitLog(t, dir)
	if len(log) != 3 {
		t.Fatalf("expected 3 commits after registration commit, got %d: %v", len(log), log)
	}
	if !strings.Contains(log[0], "register rig gastown_src") {
		t.Errorf("final commit message = %q, want it to reference registering the rig", log[0])
	}
}

// TestCommitPreExistingTownConfigDirt_NoOpWhenClean verifies the normal
// (healthy) path is unchanged: no pre-existing dirt means no extra commit,
// so a rig add/adopt still produces exactly one "register rig" commit.
func TestCommitPreExistingTownConfigDirt_NoOpWhenClean(t *testing.T) {
	dir := initTownConfigTestRepo(t)
	g := git.NewGit(dir)

	commitPreExistingTownConfigDirt(g, "testrig")

	log := commitLog(t, dir)
	if len(log) != 1 {
		t.Fatalf("expected no new commit on a clean tree, got %d commits: %v", len(log), log)
	}
}

// TestCommitPreExistingTownConfigDirt_IgnoresUnrelatedFiles verifies the
// helper stays scoped to townConfigFiles and never sweeps up unrelated dirty
// files elsewhere in the town repo.
func TestCommitPreExistingTownConfigDirt_IgnoresUnrelatedFiles(t *testing.T) {
	dir := initTownConfigTestRepo(t)
	g := git.NewGit(dir)

	if err := os.WriteFile(filepath.Join(dir, "unrelated.txt"), []byte("someone's WIP\n"), 0644); err != nil {
		t.Fatalf("write unrelated.txt: %v", err)
	}

	commitPreExistingTownConfigDirt(g, "testrig")

	log := commitLog(t, dir)
	if len(log) != 1 {
		t.Fatalf("expected no commit for a file outside townConfigFiles, got %d commits: %v", len(log), log)
	}

	status, err := g.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	found := false
	for _, f := range status.Untracked {
		if f == "unrelated.txt" {
			found = true
		}
	}
	if !found {
		t.Errorf("unrelated.txt should remain untracked/uncommitted, status: %+v", status)
	}
}
