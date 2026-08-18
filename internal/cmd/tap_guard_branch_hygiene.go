package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/git"
)

var tapGuardBranchHygieneCmd = &cobra.Command{
	Use:   "branch-hygiene",
	Short: "Block gh pr create from a contaminated branch (PreToolUse hook)",
	Long: `Block PR creation in Gas Town when the current branch has diverged too
far from its base -- either stale-behind or carrying unrelated-ahead
commits.

This closes the gap that let maintainer replacement PRs #4238 (~553
behind / ~86 ahead) and #4257 (~553 behind / ~98 ahead) get created and
merge-recommended: nothing computed or gated on divergence before
gh pr create ran.

Exit codes:
  0 - Operation allowed (clean/warn, or the contamination check itself
      could not be resolved)
  2 - Operation BLOCKED (branch contamination hit the block threshold)

Failures resolving git state (not a repo, no base ref, fetch failure) fail
OPEN -- this guard only blocks on a confirmed positive contamination
reading, consistent with the existing pr-workflow guard's error handling.

Example hook configuration:
  {
    "PreToolUse": [{
      "matcher": "Bash(gh pr create*)",
      "hooks": [{"command": "gt tap guard branch-hygiene"}]
    }]
  }`,
	RunE: runTapGuardBranchHygiene,
}

func init() {
	tapGuardCmd.AddCommand(tapGuardBranchHygieneCmd)
}

func runTapGuardBranchHygiene(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return nil // fail open: can't resolve cwd
	}
	g := git.NewGit(cwd)

	base := g.CleanBaseRef("origin", g.RemoteDefaultBranch(), "")
	remote := git.RemoteForRef(base)
	if remote == "" {
		remote = "origin"
	}
	// Best-effort fetch: a fetch failure should not block on stale info alone.
	_ = g.Fetch(remote)

	contam, err := g.CheckBranchContamination(base)
	if err != nil {
		return nil // fail open: can't determine contamination
	}

	severity, reasons := contam.Evaluate()
	if severity != git.SeverityBlock {
		return nil
	}

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "╔══════════════════════════════════════════════════════════════════╗")
	fmt.Fprintln(os.Stderr, "║  ❌ PR BLOCKED - BRANCH HYGIENE                                  ║")
	fmt.Fprintln(os.Stderr, "║  Contaminated branch: stale base or unrelated ahead commits.    ║")
	fmt.Fprintln(os.Stderr, "║  Rebase onto a clean base, then re-run:                          ║")
	fmt.Fprintln(os.Stderr, "║    gt pr-sheriff-check --merge-gate                               ║")
	fmt.Fprintln(os.Stderr, "╚══════════════════════════════════════════════════════════════════╝")
	fmt.Fprintf(os.Stderr, "Base: %s   Ahead: %d   Behind: %d\n", base, contam.Ahead, contam.Behind)
	for _, reason := range reasons {
		fmt.Fprintf(os.Stderr, "  - %s\n", reason)
	}
	fmt.Fprintln(os.Stderr, "")
	return NewSilentExit(2) // Exit 2 = BLOCK in Claude Code hooks
}
