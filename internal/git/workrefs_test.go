package git

import "testing"

func TestResolveWorkRefs_SingleRemoteRig(t *testing.T) {
	localDir, _, mainBranch := initTestRepoWithRemote(t)
	g := NewGit(localDir)

	got := g.ResolveWorkRefs(mainBranch, WorkRefs{})
	if got.BaseRef != "origin/"+mainBranch {
		t.Errorf("BaseRef = %q, want origin/%s", got.BaseRef, mainBranch)
	}
	if got.PublishRemote != "origin" {
		t.Errorf("PublishRemote = %q, want origin", got.PublishRemote)
	}
	if got.PRTargetRef != "origin/"+mainBranch {
		t.Errorf("PRTargetRef = %q, want origin/%s", got.PRTargetRef, mainBranch)
	}
}

func TestResolveWorkRefs_DivergentForkRig(t *testing.T) {
	localDir, upstream, fork, mainBranch := initTestRepoWithSplitRemote(t)
	g := NewGit(localDir)
	// Move to the distinct-upstream-remote topology (origin = fork entirely,
	// upstream = canonical, matching this repo's own production setup).
	if err := g.ClearPushURL("origin"); err != nil {
		t.Fatalf("ClearPushURL: %v", err)
	}
	if _, err := g.SetRemoteURL("origin", fork); err != nil {
		t.Fatalf("SetRemoteURL origin fork: %v", err)
	}
	if err := g.AddUpstreamRemote(upstream); err != nil {
		t.Fatalf("AddUpstreamRemote: %v", err)
	}

	got := g.ResolveWorkRefs(mainBranch, WorkRefs{})
	if got.BaseRef != "upstream/"+mainBranch {
		t.Errorf("BaseRef = %q, want upstream/%s", got.BaseRef, mainBranch)
	}
	if got.PublishRemote != "origin" {
		t.Errorf("PublishRemote = %q, want origin (publish always goes through origin's configured push URL)", got.PublishRemote)
	}
	if got.PRTargetRef != "upstream/"+mainBranch {
		t.Errorf("PRTargetRef = %q, want upstream/%s", got.PRTargetRef, mainBranch)
	}
}

func TestResolveWorkRefs_ExplicitOverridesWin(t *testing.T) {
	localDir, _, mainBranch := initTestRepoWithRemote(t)
	g := NewGit(localDir)

	override := WorkRefs{
		BaseRef:       "integration/gt-epic",
		PublishRemote: "custom-remote",
		PublishRef:    "custom-branch",
		PRTargetRef:   "origin/integration/gt-epic",
	}
	got := g.ResolveWorkRefs(mainBranch, override)
	if got.BaseRef != "origin/integration/gt-epic" {
		t.Errorf("BaseRef = %q, want origin-qualified override", got.BaseRef)
	}
	if got.PublishRemote != "custom-remote" {
		t.Errorf("PublishRemote = %q, want override to pass through unchanged", got.PublishRemote)
	}
	if got.PublishRef != "custom-branch" {
		t.Errorf("PublishRef = %q, want override to pass through unchanged", got.PublishRef)
	}
	if got.PRTargetRef != "origin/integration/gt-epic" {
		t.Errorf("PRTargetRef = %q, want override to pass through unchanged", got.PRTargetRef)
	}
}

func TestResolveWorkRefs_BareBaseBranchOverrideIsQualified(t *testing.T) {
	// A bare override target (the documented --base-branch convention, e.g.
	// "develop") must be qualified against the resolved remote, not passed
	// through as an unqualified local branch name.
	localDir, _, mainBranch := initTestRepoWithRemote(t)
	g := NewGit(localDir)

	got := g.ResolveWorkRefs(mainBranch, WorkRefs{BaseRef: "develop"})
	if got.BaseRef != "origin/develop" {
		t.Errorf("BaseRef = %q, want origin/develop", got.BaseRef)
	}
}

func TestPRTargetRepo(t *testing.T) {
	dir := initTestRepo(t)
	g := NewGit(dir)
	addGitHubRemotes(t, g)

	repo, err := g.PRTargetRepo()
	if err != nil {
		t.Fatalf("PRTargetRepo: %v", err)
	}
	if repo != "upstream/repo" {
		t.Errorf("PRTargetRepo = %q, want upstream/repo", repo)
	}
}

func TestPRTargetRepo_NoUpstreamFallsBackToOrigin(t *testing.T) {
	dir := initTestRepo(t)
	g := NewGit(dir)
	if _, err := g.AddRemote("origin", "https://github.com/solo/repo.git"); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}

	repo, err := g.PRTargetRepo()
	if err != nil {
		t.Fatalf("PRTargetRepo: %v", err)
	}
	if repo != "solo/repo" {
		t.Errorf("PRTargetRepo = %q, want solo/repo", repo)
	}
}

func TestPRHeadRef_CrossRepoQualifiesOwner(t *testing.T) {
	dir := initTestRepo(t)
	g := NewGit(dir)
	addGitHubRemotes(t, g)

	head, err := g.PRHeadRef("polecat/fix-thing")
	if err != nil {
		t.Fatalf("PRHeadRef: %v", err)
	}
	if head != "fork:polecat/fix-thing" {
		t.Errorf("PRHeadRef = %q, want fork:polecat/fix-thing", head)
	}
}

func TestPRHeadRef_SameRepoIsBareBranch(t *testing.T) {
	dir := initTestRepo(t)
	g := NewGit(dir)
	if _, err := g.AddRemote("origin", "https://github.com/solo/repo.git"); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}

	head, err := g.PRHeadRef("polecat/fix-thing")
	if err != nil {
		t.Fatalf("PRHeadRef: %v", err)
	}
	if head != "polecat/fix-thing" {
		t.Errorf("PRHeadRef = %q, want bare branch name polecat/fix-thing", head)
	}
}
