package cmd

import (
	"os"
	"reflect"
	"testing"

	"github.com/steveyegge/gastown/internal/git"
)

// initPRArgsTestRepo creates a bare local git repo with no remotes. Remote
// config only (no clone/push) is needed since prCreateArgs just reads
// `git remote` configuration, not object history.
func initPRArgsTestRepo(t *testing.T) (*git.Git, string) {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test User")
	if err := os.WriteFile(dir+"/README.md", []byte("test\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "initial")
	return git.NewGit(dir), dir
}

func TestPRCreateArgs_SingleRemoteRig(t *testing.T) {
	g, dir := initPRArgsTestRepo(t)
	runGit(t, dir, "remote", "add", "origin", "https://github.com/solo/repo.git")

	args := prCreateArgs(g, "main", "polecat/fix-thing", "Fix thing", "body")

	want := []string{"pr", "create", "--base", "main", "--head", "polecat/fix-thing", "--title", "Fix thing", "--body", "body"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("prCreateArgs = %v, want %v (non-fork rig must get byte-identical args to pre-fork-awareness behavior)", args, want)
	}
}

func TestPRCreateArgs_ForkRigAddsRepoAndQualifiedHead(t *testing.T) {
	g, dir := initPRArgsTestRepo(t)
	runGit(t, dir, "remote", "add", "origin", "https://github.com/fork/repo.git")
	if err := g.AddUpstreamRemote("https://github.com/upstream/repo.git"); err != nil {
		t.Fatalf("AddUpstreamRemote: %v", err)
	}

	args := prCreateArgs(g, "main", "polecat/fix-thing", "Fix thing", "body")

	want := []string{"pr", "create", "--base", "main", "--repo", "upstream/repo", "--head", "fork:polecat/fix-thing", "--title", "Fix thing", "--body", "body"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("prCreateArgs = %v, want %v", args, want)
	}
}
