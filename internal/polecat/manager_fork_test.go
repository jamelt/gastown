package polecat

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/rig"
)

// setupForkAwareManagerTest builds a rig whose repo base (mayor/rig) is a
// genuine fork+upstream topology: origin is a distinct fork remote, upstream
// is a distinct canonical remote, and upstream has diverged with an extra
// commit the fork never received. This lets tests prove worktree creation
// actually resolves base_ref against upstream, not just origin.
//
// Returns the manager, the repo-base ("mayor/rig") path, and the SHA of the
// upstream-only commit that origin's copy does not contain.
func setupForkAwareManagerTest(t *testing.T) (*Manager, string, string) {
	t.Helper()
	installMockBd(t)

	root := t.TempDir()
	mayorRig := filepath.Join(root, "mayor", "rig")
	if err := os.MkdirAll(mayorRig, 0755); err != nil {
		t.Fatalf("mkdir mayor/rig: %v", err)
	}
	rigBeads := filepath.Join(root, ".beads")
	if err := os.MkdirAll(rigBeads, 0755); err != nil {
		t.Fatalf("mkdir rig .beads: %v", err)
	}
	mayorBeads := filepath.Join(mayorRig, ".beads")
	if err := os.MkdirAll(mayorBeads, 0755); err != nil {
		t.Fatalf("mkdir mayor/rig/.beads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rigBeads, "redirect"), []byte("mayor/rig/.beads\n"), 0644); err != nil {
		t.Fatalf("write rig redirect: %v", err)
	}

	scratch := t.TempDir()
	upstreamBare := filepath.Join(scratch, "upstream.git")
	forkBare := filepath.Join(scratch, "fork.git")
	runForkTestGit(t, scratch, "init", "--bare", "--initial-branch", "main", upstreamBare)
	runForkTestGit(t, scratch, "init", "--bare", "--initial-branch", "main", forkBare)

	seed := filepath.Join(scratch, "seed")
	if err := os.MkdirAll(seed, 0755); err != nil {
		t.Fatalf("mkdir seed: %v", err)
	}
	runForkTestGit(t, seed, "init", "-b", "main")
	runForkTestGit(t, seed, "config", "user.email", "test@test.com")
	runForkTestGit(t, seed, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("# Test\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runForkTestGit(t, seed, "add", "README.md")
	runForkTestGit(t, seed, "commit", "-m", "initial")
	runForkTestGit(t, seed, "push", upstreamBare, "main")
	runForkTestGit(t, seed, "push", forkBare, "main")

	// Diverge: push an extra commit to upstream only. The fork never gets it.
	if err := os.WriteFile(filepath.Join(seed, "upstream-only.txt"), []byte("canonical\n"), 0644); err != nil {
		t.Fatalf("write upstream-only file: %v", err)
	}
	runForkTestGit(t, seed, "add", "upstream-only.txt")
	runForkTestGit(t, seed, "commit", "-m", "upstream-only commit")
	runForkTestGit(t, seed, "push", upstreamBare, "main")
	upstreamOnlySHA := runForkTestGitOutput(t, seed, "rev-parse", "HEAD")

	// mayor/rig: origin = fork, upstream = canonical (this rig's own
	// production topology per docs/guides/fork-rig-setup.md).
	runForkTestGit(t, mayorRig, "init", "-b", "main")
	runForkTestGit(t, mayorRig, "config", "user.email", "test@test.com")
	runForkTestGit(t, mayorRig, "config", "user.name", "Test User")
	runForkTestGit(t, mayorRig, "remote", "add", "origin", forkBare)
	runForkTestGit(t, mayorRig, "remote", "add", "upstream", upstreamBare)
	runForkTestGit(t, mayorRig, "fetch", "origin")
	runForkTestGit(t, mayorRig, "fetch", "upstream")
	// mayor/rig needs local content too (repoBase() clones worktrees off this
	// dir when no shared bare repo is configured) — check out origin's (stale)
	// main so the local branch itself is behind upstream, same as production.
	runForkTestGit(t, mayorRig, "checkout", "-b", "main", "origin/main")

	r := &rig.Rig{Name: "rig", Path: root}
	return NewManager(r, git.NewGit(root), nil), mayorRig, upstreamOnlySHA
}

func runForkTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v (dir=%s): %v\n%s", args, dir, err, out)
	}
}

func runForkTestGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v (dir=%s): %v", args, dir, err)
	}
	return trimNewline(string(out))
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

// TestAddWithOptions_ForkRigUsesUpstreamStartPoint proves the fix in
// resolveWorktreeStartPoint end-to-end through the real AddWithOptions call
// path: a fresh polecat worktree on a fork+upstream rig must land on
// upstream's tip, not the fork's stale copy, even though repoBase()'s own
// local "main" branch (checked out from origin) never saw the upstream-only
// commit directly.
func TestAddWithOptions_ForkRigUsesUpstreamStartPoint(t *testing.T) {
	mgr, _, upstreamOnlySHA := setupForkAwareManagerTest(t)

	polecat, err := mgr.AddWithOptions("toast", AddOptions{})
	if err != nil {
		t.Fatalf("AddWithOptions: %v", err)
	}

	worktreeGit := git.NewGit(polecat.ClonePath)
	isAncestor, err := worktreeGit.IsAncestor(upstreamOnlySHA, polecat.Branch)
	if err != nil {
		t.Fatalf("check upstream ancestry: %v", err)
	}
	if !isAncestor {
		t.Fatalf("new polecat branch %q should descend from upstream-only commit %s (base_ref must resolve to upstream/main on a fork rig, not origin/main)", polecat.Branch, upstreamOnlySHA)
	}
}
