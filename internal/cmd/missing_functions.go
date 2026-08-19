package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/workspace"
)

// agentSourceIssueHint returns the source issue ID hint from agent fields.
// Used for lookups in merge-queue indexes and inventory tracking.
func agentSourceIssueHint(prefix string, fields *beads.AgentFields) string {
	if fields == nil {
		return ""
	}
	// Return the last source issue, or hook bead if no source issue is recorded
	if fields.LastSourceIssue != "" {
		return fields.LastSourceIssue
	}
	if fields.HookBead != "" {
		return fields.HookBead
	}
	return ""
}

// donePolecatWorktreeInfo holds resolved information about a polecat worktree.
type donePolecatWorktreeInfo struct {
	townRoot    string // Town root directory
	cwd         string // Canonical polecat worktree path
	rigName     string // Rig name (e.g., "gastown")
	polecatName string // Polecat name (e.g., "atom")
	actor       string // Actor string (e.g., "gastown/polecats/atom")
}

// resolveDonePolecatWorktree resolves the current polecat worktree from the current directory.
func resolveDonePolecatWorktree() (*donePolecatWorktreeInfo, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getting current directory: %w", err)
	}
	return resolveDonePolecatWorktreeAt(cwd)
}

// resolveDonePolecatWorktreeAt resolves a polecat worktree from a given directory path.
func resolveDonePolecatWorktreeAt(cwd string) (*donePolecatWorktreeInfo, error) {
	// Find town root
	townRoot, err := workspace.FindFromCwd(cwd)
	if err != nil {
		return nil, fmt.Errorf("cannot find town root from %q: %w", cwd, err)
	}

	// Determine rig and polecat from GT_RIG, GT_ROLE, GT_POLECAT env vars
	rigName := os.Getenv("GT_RIG")
	roleName := os.Getenv("GT_ROLE")
	polecatName := os.Getenv("GT_POLECAT")

	if rigName == "" || polecatName == "" {
		return nil, fmt.Errorf("GT_RIG and GT_POLECAT environment variables must be set")
	}

	// Build actor string
	actor := fmt.Sprintf("%s/polecats/%s", rigName, polecatName)
	if roleName != "" && roleName != "polecat" {
		// For legacy mode, use GT_ROLE directly
		actor = fmt.Sprintf("%s/polecats/%s", rigName, polecatName)
	}

	// Compute worktree root path
	worktreeRoot := filepath.Join(townRoot, rigName, "polecats", polecatName, rigName)

	// Verify we're in the worktree (not the town root or elsewhere)
	cleanCwd := filepath.Clean(cwd)
	cleanWorktree := filepath.Clean(worktreeRoot)

	// Check if cwd is in the worktree
	rel, err := filepath.Rel(cleanWorktree, cleanCwd)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil, fmt.Errorf("not in polecat worktree (cwd=%q, worktree=%q)", cleanCwd, cleanWorktree)
	}

	return &donePolecatWorktreeInfo{
		townRoot:    townRoot,
		cwd:         cleanWorktree,
		rigName:     rigName,
		polecatName: polecatName,
		actor:       actor,
	}, nil
}

// moleculeScaffoldRejectReason returns a rejection reason if the issue is a
// molecule scaffold (container or step), empty string means it's acceptable.
func moleculeScaffoldRejectReason(issue *beads.Issue) string {
	if issue == nil {
		return ""
	}

	issueID := strings.ToLower(strings.TrimSpace(issue.ID))

	// Reject formula beads (mol-*) - these are formula definitions
	if strings.HasPrefix(issueID, "mol-") {
		return "formula-id"
	}

	// Reject wisp/ephemeral beads containing "wisp" in the ID
	// (molecules are stored as ephemerals with -wisp- in their IDs)
	if strings.Contains(issueID, "-wisp-") {
		return "wisp-id"
	}

	// Reject beads marked with molecule-related labels or types
	if beads.HasLabel(issue, "gt:wisp") || beads.HasLabel(issue, "gt:molecule") {
		return "molecule-label"
	}

	// Check if the issue type indicates it's a molecule or formula
	issueType := strings.ToLower(strings.TrimSpace(issue.Type))
	if issueType == "wisp" || issueType == "formula" {
		return "molecule-type:" + issueType
	}

	return ""
}

// applyWorkflowStepAgentOverride applies agent overrides to workflow step arguments.
// This handles special cases where workflow steps need to apply agent-specific logic.
func applyWorkflowStepAgentOverride(args interface{}) error {
	// This is a placeholder for workflow step agent override logic.
	// The exact behavior depends on the workflow system's needs.
	// For now, we implement a no-op that allows the code to compile.
	// When more context is available from the workflow system,
	// the implementation can be expanded.
	return nil
}

// collectExistingMoleculesForBead collects existing molecule beads for a given work bead.
// Returns a list of existing molecule IDs to avoid duplicate dispatch.
func collectExistingMoleculesForBead(info *beads.Issue, beadID, townRoot string) ([]string, error) {
	if info == nil {
		return nil, fmt.Errorf("issue info is nil")
	}

	// Search for existing molecules that reference this bead as their parent
	// Molecules are stored as ephemeral beads in the town database
	bd := beads.New(townRoot).ForAgentBead()

	opts := beads.ListOptions{
		Ephemeral: true, // Molecules are stored as wisps/ephemeral beads
		Limit:     100,
	}

	issues, err := bd.List(opts)
	if err != nil {
		// If listing fails (e.g., no wisps table yet), return empty list
		// This is not fatal - molecule dispatch will proceed
		return nil, nil
	}

	var moleculeIDs []string
	for _, issue := range issues {
		// Check if this ephemeral bead is a molecule attached to our work bead
		if issue.Parent == beadID || beads.HasLabel(issue, "gt:molecule") {
			moleculeIDs = append(moleculeIDs, issue.ID)
		}
	}

	return moleculeIDs, nil
}
