package git

import (
	"os/exec"
	"testing"
)

// hq-sz6wa: the fork-aware base (upstream/main) is correct only for a rig that
// actually contributes UP to its parent. When pushes to upstream are disabled the
// work can never integrate there, so upstream is not the trunk and basing on it
// strands polecats at whatever the parent last published — observed live at 157
// commits and 27 days stale.
func TestCleanDefaultBranchBaseRefRespectsDisabledUpstreamPush(t *testing.T) {
	localDir, _, _ := initTestRepoWithRemote(t)
	g := NewGit(localDir)

	// Give the repo a distinct upstream remote — the fork topology.
	run := func(args ...string) {
		t.Helper()
		if err := exec.Command("git", append([]string{"-C", localDir}, args...)...).Run(); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	run("remote", "add", "upstream", "https://example.invalid/parent/repo.git")

	// Upstream is pushable: the existing fork-aware behaviour must be preserved.
	if got := g.CleanDefaultBranchBaseRef("origin", "main"); got != "upstream/main" {
		t.Fatalf("with a pushable upstream, base = %q, want upstream/main — the fork-aware case must not be removed", got)
	}

	// Upstream push disabled: it cannot be the integration target, so it cannot
	// be the base.
	run("config", "remote.upstream.pushurl", disabledPushURL)
	if got := g.CleanDefaultBranchBaseRef("origin", "main"); got != "origin/main" {
		t.Fatalf("with upstream push disabled, base = %q, want origin/main", got)
	}

	// Case-insensitive, and tolerant of stray whitespace.
	run("config", "remote.upstream.pushurl", "disabled")
	if got := g.CleanDefaultBranchBaseRef("origin", "main"); got != "origin/main" {
		t.Fatalf("lowercase 'disabled' not honoured: base = %q, want origin/main", got)
	}
}

// No upstream remote at all: unchanged, always the fetch remote.
func TestCleanDefaultBranchBaseRefWithoutUpstream(t *testing.T) {
	localDir, _, _ := initTestRepoWithRemote(t)
	g := NewGit(localDir)
	if got := g.CleanDefaultBranchBaseRef("origin", "main"); got != "origin/main" {
		t.Fatalf("base = %q, want origin/main", got)
	}
}

// Fails OPEN: an unreadable push URL must preserve the fork-aware behaviour
// rather than silently narrowing it.
func TestUpstreamAcceptsPushesFailsOpen(t *testing.T) {
	localDir, _, _ := initTestRepoWithRemote(t)
	g := NewGit(localDir)
	// No upstream remote configured — GetPushURL errors.
	if !g.upstreamAcceptsPushes() {
		t.Fatal("upstreamAcceptsPushes must fail open when the push URL cannot be read")
	}
}
