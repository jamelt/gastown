package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// WorktreeGitdirCheck validates that worktree .git files reference existing
// gitdir paths. When a worktree's .git file contains "gitdir: /path/to/.repo.git/worktrees/X"
// but the referenced path doesn't exist, all git operations in that worktree fail.
//
// This detects two scenarios:
//   - gt-fmnml: a rig's .repo.git was missing, causing worktrees to break
//   - hq-c6u: rsync/move between machines changes the path prefix (e.g.
//     /Users/bob -> /home/bob), breaking all absolute gitdir references
//
// For the relocation case, the check infers the correct .repo.git path from
// the current town root and stores it so Fix() can recreate the worktree.
type WorktreeGitdirCheck struct {
	FixableCheck
	townRoot        string
	brokenWorktrees []brokenWorktree
}

type brokenWorktree struct {
	worktreePath      string // e.g., /home/bob/gt/wyvern/refinery/rig
	gitdirTarget      string // e.g., /Users/bob/gt/wyvern/.repo.git/worktrees/rig (stale)
	rigPath           string // e.g., /home/bob/gt/wyvern
	bareRepoPath      string // e.g., /Users/bob/gt/wyvern/.repo.git (from gitdir, may be stale)
	correctedBareRepo string // e.g., /home/bob/gt/wyvern/.repo.git (inferred from town root)
	reason            string // what's broken
}

// NewWorktreeGitdirCheck creates a new worktree gitdir validity check.
func NewWorktreeGitdirCheck() *WorktreeGitdirCheck {
	return &WorktreeGitdirCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "worktree-gitdir-valid",
				CheckDescription: "Verify worktree .git files reference existing gitdir paths",
				CheckCategory:    CategoryRig,
			},
		},
	}
}

// Run scans all rigs and deacon dogs for worktrees with broken gitdir references.
func (c *WorktreeGitdirCheck) Run(ctx *CheckContext) *CheckResult {
	c.brokenWorktrees = nil
	c.townRoot = ctx.TownRoot

	entries, err := os.ReadDir(ctx.TownRoot)
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: fmt.Sprintf("Cannot read town root: %v", err),
		}
	}

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		rigPath := filepath.Join(ctx.TownRoot, entry.Name())

		// Skip non-rig directories
		if !isRigDir(rigPath) {
			continue
		}

		// If --rig is specified, only check that rig
		if ctx.RigName != "" && entry.Name() != ctx.RigName {
			continue
		}

		c.checkRigWorktrees(rigPath, entry.Name())
	}

	// Scan deacon/dogs for cross-rig worktrees (not covered by rig scan
	// because deacon/ doesn't have config.json or standard rig subdirs).
	c.checkDeaconDogs(ctx.TownRoot)

	if len(c.brokenWorktrees) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "All worktree gitdir references are valid",
		}
	}

	var details []string
	for _, bw := range c.brokenWorktrees {
		relPath, _ := filepath.Rel(ctx.TownRoot, bw.worktreePath)
		if relPath == "" {
			relPath = bw.worktreePath
		}
		details = append(details, fmt.Sprintf("%s: %s", relPath, bw.reason))
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusError,
		Message: fmt.Sprintf("%d worktree(s) with broken gitdir references", len(c.brokenWorktrees)),
		Details: details,
		FixHint: "Run 'gt doctor --fix' to safely repair broken worktree links; worktrees with missing Git metadata require manual recovery",
	}
}

// checkRigWorktrees checks all worktrees within a single rig: refinery,
// polecats, witness, mayor, and crew.
func (c *WorktreeGitdirCheck) checkRigWorktrees(rigPath, rigName string) {
	// Check refinery/rig
	refineryRig := filepath.Join(rigPath, "refinery", "rig")
	c.checkWorktree(refineryRig, rigPath)

	// Check polecats (both structures: polecats/<name>/<rigname>/ and polecats/<name>/)
	// A missing polecats dir must not short-circuit the witness/mayor/crew
	// checks below.
	polecatsDir := filepath.Join(rigPath, "polecats")
	if polecatEntries, err := os.ReadDir(polecatsDir); err == nil {
		for _, entry := range polecatEntries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}

			// Try new structure first: polecats/<name>/<rigname>/
			newPath := filepath.Join(polecatsDir, entry.Name(), rigName)
			if c.hasGitFile(newPath) {
				c.checkWorktree(newPath, rigPath)
				continue
			}

			// Fall back to old structure: polecats/<name>/
			oldPath := filepath.Join(polecatsDir, entry.Name())
			if c.hasGitFile(oldPath) {
				c.checkWorktree(oldPath, rigPath)
			}
		}
	}

	// Check witness/rig
	witnessRig := filepath.Join(rigPath, "witness", "rig")
	if c.hasGitFile(witnessRig) {
		c.checkWorktree(witnessRig, rigPath)
	}

	// Check mayor/rig
	mayorRig := filepath.Join(rigPath, "mayor", "rig")
	if c.hasGitFile(mayorRig) {
		c.checkWorktree(mayorRig, rigPath)
	}

	// Check crew/<name> (each crew member is a single-level worktree, unlike
	// the two-level polecats/<name>/<rigname> layout).
	crewDir := filepath.Join(rigPath, "crew")
	if crewEntries, err := os.ReadDir(crewDir); err == nil {
		for _, entry := range crewEntries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			crewPath := filepath.Join(crewDir, entry.Name())
			if c.hasGitFile(crewPath) {
				c.checkWorktree(crewPath, rigPath)
			}
		}
	}
}

// checkDeaconDogs scans deacon/dogs/<dogname>/<rigname>/ for cross-rig worktrees.
// Each dog directory contains worktrees of various rigs, created by gt sling.
func (c *WorktreeGitdirCheck) checkDeaconDogs(townRoot string) {
	dogsDir := filepath.Join(townRoot, "deacon", "dogs")
	dogEntries, err := os.ReadDir(dogsDir)
	if err != nil {
		return // No deacon/dogs — that's fine
	}

	for _, dogEntry := range dogEntries {
		if !dogEntry.IsDir() || strings.HasPrefix(dogEntry.Name(), ".") {
			continue
		}

		dogPath := filepath.Join(dogsDir, dogEntry.Name())
		rigEntries, err := os.ReadDir(dogPath)
		if err != nil {
			continue
		}

		for _, rigEntry := range rigEntries {
			if !rigEntry.IsDir() || strings.HasPrefix(rigEntry.Name(), ".") {
				continue
			}

			wtPath := filepath.Join(dogPath, rigEntry.Name())
			if c.hasGitFile(wtPath) {
				// For dog worktrees, rigPath is the rig the worktree belongs to
				rigPath := filepath.Join(townRoot, rigEntry.Name())
				c.checkWorktree(wtPath, rigPath)
			}
		}
	}
}

// checkWorktree validates a single worktree's .git file reference.
func (c *WorktreeGitdirCheck) checkWorktree(worktreePath, rigPath string) {
	gitFile := filepath.Join(worktreePath, ".git")

	info, err := os.Stat(gitFile)
	if err != nil {
		return // No .git file, not a worktree
	}

	// Only check .git files (worktrees), not .git directories (clones)
	if info.IsDir() {
		return
	}

	content, err := os.ReadFile(gitFile)
	if err != nil {
		c.brokenWorktrees = append(c.brokenWorktrees, brokenWorktree{
			worktreePath: worktreePath,
			rigPath:      rigPath,
			reason:       fmt.Sprintf("cannot read .git file: %v", err),
		})
		return
	}

	line := strings.TrimSpace(string(content))
	if !strings.HasPrefix(line, "gitdir: ") {
		c.brokenWorktrees = append(c.brokenWorktrees, brokenWorktree{
			worktreePath: worktreePath,
			rigPath:      rigPath,
			reason:       fmt.Sprintf("malformed .git file (no gitdir: prefix): %s", line),
		})
		return
	}

	gitdirTarget := strings.TrimPrefix(line, "gitdir: ")

	// Resolve relative paths
	if !filepath.IsAbs(gitdirTarget) {
		gitdirTarget = filepath.Join(worktreePath, gitdirTarget)
	}

	// Check if the gitdir target exists
	if _, err := os.Stat(gitdirTarget); os.IsNotExist(err) {
		// Extract the bare repo path and worktree name from the stale gitdir target.
		// Format: <prefix>/<rigname>/.repo.git/worktrees/<wtname>
		bareRepoPath := ""
		if strings.Contains(gitdirTarget, ".repo.git") {
			parts := strings.SplitN(gitdirTarget, ".repo.git", 2)
			bareRepoPath = parts[0] + ".repo.git"
		}

		// Try to infer the correct bare repo path from the current town root.
		// The gitdir target has the form: <old_prefix>/<rigname>/.repo.git/worktrees/<wtname>
		// We extract <rigname> and look for <townRoot>/<rigname>/.repo.git
		correctedBareRepo := ""
		if bareRepoPath != "" && c.townRoot != "" {
			correctedBareRepo = c.inferCorrectedBareRepo(bareRepoPath)
		}

		reason := c.buildReason(gitdirTarget, bareRepoPath, correctedBareRepo)

		c.brokenWorktrees = append(c.brokenWorktrees, brokenWorktree{
			worktreePath:      worktreePath,
			gitdirTarget:      gitdirTarget,
			rigPath:           rigPath,
			bareRepoPath:      bareRepoPath,
			correctedBareRepo: correctedBareRepo,
			reason:            reason,
		})
	}
}

// inferCorrectedBareRepo tries to find the correct .repo.git path by extracting
// the rig name from the stale path and looking it up under the current town root.
func (c *WorktreeGitdirCheck) inferCorrectedBareRepo(staleBareRepoPath string) string {
	// staleBareRepoPath looks like: /Users/bob/gt/testAnt/.repo.git
	// We need to extract "testAnt" and check <townRoot>/testAnt/.repo.git

	// Get the parent of .repo.git — that's the rig directory (with old prefix)
	staleRigDir := filepath.Dir(staleBareRepoPath)
	rigName := filepath.Base(staleRigDir)

	candidate := filepath.Join(c.townRoot, rigName, ".repo.git")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}

	return ""
}

// buildReason constructs a human-readable reason string for a broken worktree.
func (c *WorktreeGitdirCheck) buildReason(gitdirTarget, bareRepoPath, correctedBareRepo string) string {
	if bareRepoPath == "" {
		return fmt.Sprintf("gitdir target does not exist: %s", gitdirTarget)
	}

	// Check the stale path first (handles the non-relocated case)
	if _, err := os.Stat(bareRepoPath); err == nil {
		return fmt.Sprintf("worktree entry missing in .repo.git (gitdir: %s)", gitdirTarget)
	}

	// Stale .repo.git path doesn't exist — is this a relocation?
	if correctedBareRepo != "" {
		oldPrefix := filepath.Dir(filepath.Dir(bareRepoPath))      // e.g., /Users/bob/gt
		newPrefix := filepath.Dir(filepath.Dir(correctedBareRepo)) // e.g., /home/bob/gt
		return fmt.Sprintf("relocated (%s -> %s), needs worktree re-creation", oldPrefix, newPrefix)
	}

	return fmt.Sprintf(".repo.git missing (gitdir: %s)", gitdirTarget)
}

// hasGitFile checks if a directory has a .git file (not directory).
func (c *WorktreeGitdirCheck) hasGitFile(path string) bool {
	gitFile := filepath.Join(path, ".git")
	info, err := os.Stat(gitFile)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// Fix attempts to safely repair broken worktree links.
func (c *WorktreeGitdirCheck) Fix(ctx *CheckContext) error {
	var errs []string

	for _, bw := range c.brokenWorktrees {
		// Determine which bare repo path to use: corrected (relocated) or original.
		repoPath := bw.correctedBareRepo
		if repoPath == "" {
			repoPath = bw.bareRepoPath
		}
		if repoPath == "" {
			errs = append(errs, fmt.Sprintf("%s: cannot fix (not a .repo.git worktree)", bw.worktreePath))
			continue
		}

		// Check if .repo.git exists
		if _, err := os.Stat(repoPath); os.IsNotExist(err) {
			errs = append(errs, fmt.Sprintf("%s: cannot fix (.repo.git does not exist at %s, needs re-clone via 'gt rig install')", bw.worktreePath, repoPath))
			continue
		}

		if err := c.fixOneWorktree(bw, repoPath); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// fixOneWorktree repairs a single broken worktree.
func (c *WorktreeGitdirCheck) fixOneWorktree(bw brokenWorktree, repoPath string) error {
	// repair must run before removing the .git file or pruning entries: both are
	// the evidence Git needs to preserve this worktree's branch and index.
	cmd := exec.Command("git", "-C", repoPath, "worktree", "repair", bw.worktreePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		repairOutput := strings.TrimSpace(string(output))
		if strings.Contains(repairOutput, "is not a git command") {
			return fmt.Errorf("%s: unable to repair worktree links safely: git worktree repair requires Git 2.29 or newer; the worktree was left unchanged and requires manual recovery", bw.worktreePath)
		}
		// Git reports the broken link it just repaired as an error on some
		// versions. Accept it only when Git can subsequently use the worktree.
		if verifyErr := exec.Command("git", "-C", bw.worktreePath, "status", "--porcelain").Run(); verifyErr == nil {
			return nil
		}
		return fmt.Errorf("%s: unable to repair worktree links safely; the worktree was left unchanged and requires manual recovery: %s",
			bw.worktreePath, repairOutput)
	}
	return nil
}

// isRigDir checks if a directory looks like a rig (has config.json or known subdirectories).
func isRigDir(path string) bool {
	// Check for config.json (most reliable indicator)
	if _, err := os.Stat(filepath.Join(path, "config.json")); err == nil {
		return true
	}
	// Check for known rig subdirectories
	markers := []string{"refinery", "witness", "polecats", "mayor"}
	for _, marker := range markers {
		if _, err := os.Stat(filepath.Join(path, marker)); err == nil {
			return true
		}
	}
	return false
}
