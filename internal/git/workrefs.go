package git

import "strings"

// WorkRefs is the resolved set of git refs/remotes governing a single unit
// of polecat work: where to base a fresh branch, where to publish it, and
// where a PR/MR ultimately merges. It generalizes the base_branch-only model
// so callers stop independently assuming origin/main.
type WorkRefs struct {
	// BaseRef is the ref to checkout/rebase/diff against, e.g. "origin/main"
	// or "upstream/main" on a fork rig.
	BaseRef string
	// PublishRemote is the remote branches are pushed to, e.g. "origin".
	PublishRemote string
	// PublishRef is the branch name pushed to PublishRemote. Empty means the
	// caller's own local branch name (this field only matters when a work
	// item must publish under a different remote branch name).
	PublishRef string
	// PRTargetRef is "<remote>/<branch>" identifying what a PR/MR merges
	// into, e.g. "upstream/main" on a fork rig.
	PRTargetRef string
}

// ResolveWorkRefs fills empty WorkRefs fields with rig-derived defaults,
// built on the existing CleanBaseRef/ForkBackedRemote fork-detection
// primitives, applying any explicit override. Non-empty override fields
// pass through unchanged — explicit sling/bead/convoy metadata always wins
// over the rig default.
//
// defaultBranch is the rig's configured default branch (e.g. "main").
// override.BaseRef may be empty, a bare branch name, or an already
// remote-qualified ref ("origin/<branch>" / "upstream/<branch>") — CleanBaseRef
// normalizes all three.
func (g *Git) ResolveWorkRefs(defaultBranch string, override WorkRefs) WorkRefs {
	resolved := override
	resolved.BaseRef = g.CleanBaseRef("origin", defaultBranch, override.BaseRef)
	if resolved.PublishRemote == "" {
		resolved.PublishRemote = "origin"
	}
	if resolved.PRTargetRef == "" {
		resolved.PRTargetRef = g.CleanDefaultBranchBaseRef("origin", defaultBranch)
	}
	return resolved
}

// PRTargetRepo returns the "owner/repo" GitHub slug a PR should target: the
// upstream remote's repo when configured, otherwise origin's.
func (g *Git) PRTargetRepo() (string, error) {
	return g.pullRequestTargetRepo("")
}

// PRHeadRef returns the --head value for `gh pr create`: "<owner>:<branch>"
// when the head repo (origin's push destination) differs from the PR target
// repo, otherwise the bare branch name.
func (g *Git) PRHeadRef(branch string) (string, error) {
	targetRepo, err := g.PRTargetRepo()
	if err != nil {
		return "", err
	}
	pushURL, err := g.GetPushURL("origin")
	if err != nil {
		return "", err
	}
	headRepo, err := githubRepoFromRemoteURL(pushURL)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(headRepo, targetRepo) {
		return branch, nil
	}
	headOwner, _, ok := strings.Cut(headRepo, "/")
	if !ok || headOwner == "" {
		return branch, nil
	}
	return headOwner + ":" + branch, nil
}
