package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type donePolecatWorktreeInfo struct {
	townRoot   string
	cwd        string
	rigName    string
	polecatName string
	actor      string
}

// resolveDonePolecatWorktree validates that gt done is being run from a polecat's own worktree.
// It returns worktree information if valid, or an error if not.
func resolveDonePolecatWorktree() (*donePolecatWorktreeInfo, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("cannot get current working directory: %w", err)
	}
	return resolveDonePolecatWorktreeAt(cwd)
}

// resolveDonePolecatWorktreeAt validates that the given path is a polecat's own worktree.
// This is a security gate: gt done must run from within a polecat's isolated git worktree,
// not from shared directories (town root, rig root, other polecats' worktrees, etc).
func resolveDonePolecatWorktreeAt(cwd string) (*donePolecatWorktreeInfo, error) {
	// Get the actor and role from environment variables
	actor := os.Getenv("BD_ACTOR")
	if actor == "" {
		return nil, fmt.Errorf("BD_ACTOR not set")
	}

	// Parse the actor to extract rig and polecat names
	parts := strings.Split(actor, "/")
	if len(parts) != 3 || parts[1] != "polecats" {
		return nil, fmt.Errorf("invalid actor format: %s (expected rig/polecats/name)", actor)
	}
	rigName, polecatName := parts[0], parts[2]

	// Verify role matches actor
	gtRole := os.Getenv("GT_ROLE")
	if gtRole != actor {
		return nil, fmt.Errorf("GT_ROLE mismatch: %s != %s", gtRole, actor)
	}

	// Verify rig name matches
	gtRig := os.Getenv("GT_RIG")
	if gtRig != rigName {
		return nil, fmt.Errorf("GT_RIG mismatch: %s != %s", gtRig, rigName)
	}

	// Verify polecat name matches
	gtPolecat := os.Getenv("GT_POLECAT")
	if gtPolecat != polecatName {
		return nil, fmt.Errorf("GT_POLECAT mismatch: %s != %s", gtPolecat, polecatName)
	}

	// Find the town root
	townRoot := os.Getenv("GT_TOWN_ROOT")
	if townRoot == "" {
		townRoot = os.Getenv("GT_ROOT")
	}
	if townRoot == "" {
		return nil, fmt.Errorf("GT_TOWN_ROOT or GT_ROOT not set")
	}

	// Verify git environment overrides are not set (security check)
	if os.Getenv("GIT_DIR") != "" {
		return nil, fmt.Errorf("unset GIT_DIR before running gt done")
	}
	if os.Getenv("GIT_WORK_TREE") != "" {
		return nil, fmt.Errorf("unset GIT_WORK_TREE before running gt done")
	}

	// Canonicalize the path
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve absolute path: %w", err)
	}

	// Ensure cwd is within the polecat's own worktree
	polecatWorktreeRoot := filepath.Join(townRoot, rigName, "polecats", polecatName, rigName)
	if _, err := os.Stat(polecatWorktreeRoot); err != nil {
		return nil, fmt.Errorf("polecat worktree not found: %s: %w", polecatWorktreeRoot, err)
	}

	// Check if cwd is within the polecat's worktree
	if !strings.HasPrefix(absCwd, polecatWorktreeRoot) {
		return nil, fmt.Errorf("not running from polecat worktree: %s is outside %s", absCwd, polecatWorktreeRoot)
	}

	return &donePolecatWorktreeInfo{
		townRoot:    townRoot,
		cwd:         absCwd,
		rigName:     rigName,
		polecatName: polecatName,
		actor:       actor,
	}, nil
}
