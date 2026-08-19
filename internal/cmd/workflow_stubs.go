package cmd

import (
	"strings"
)

// applyWorkflowStepAgentOverride applies agent-specific overrides to a workflow step.
// This is a stub implementation that may need expansion for full functionality.
func applyWorkflowStepAgentOverride(args []string) {
	if len(args) == 0 {
		return
	}
	// Stub: Apply agent-specific overrides to the arguments
	// This might set fields like agent assignment, priority, etc.
}

// moleculeScaffoldRejectReason returns why a molecule scaffold is not valid for a given bead info.
// Empty string means the scaffold is valid.
func moleculeScaffoldRejectReason(info *beadInfo) string {
	if info == nil {
		return "info-nil"
	}
	if strings.TrimSpace(info.Title) == "" {
		return "info-title-empty"
	}
	// Stub: Add validation logic for molecule scaffolds
	return ""
}

// collectExistingMoleculesForBead collects all existing molecule (wisp) ID strings for a given bead.
// Returns a list of molecule IDs.
func collectExistingMoleculesForBead(info *beadInfo, beadID string, townRoot string) ([]string, error) {
	if info == nil || beadID == "" {
		return []string{}, nil
	}
	// Stub: Query for existing molecules/wisps attached to this bead
	// This would typically use bd.List or similar to find gt-wisp-* issues and extract their IDs
	return []string{}, nil
}

// storeFieldsInBeadFromTownRoot stores result fields in a bead from the town root context.
func storeFieldsInBeadFromTownRoot(townRoot string, beadID string, updates beadFieldUpdates) error {
	if townRoot == "" || beadID == "" {
		return nil
	}
	// Stub: Store fields in the bead using bd.Update or similar
	return nil
}

// collectExistingMoleculeDeps collects dependencies for existing molecules.
func collectExistingMoleculeDeps(beadID string, townRoot string) ([]string, error) {
	if beadID == "" || townRoot == "" {
		return []string{}, nil
	}
	// Stub: Collect dependencies for the given bead's molecules
	return []string{}, nil
}

// appendUniqueMolecules appends unique molecule IDs to a list, avoiding duplicates.
func appendUniqueMolecules(existing []string, new ...string) []string {
	seen := make(map[string]bool)
	for _, id := range existing {
		seen[id] = true
	}
	for _, id := range new {
		if !seen[id] {
			existing = append(existing, id)
			seen[id] = true
		}
	}
	return existing
}

// hookBeadWithRetryFn is a function that hooks a bead with retry logic.
func hookBeadWithRetryFn(townRoot string, beadID string, moleculeID string) error {
	if townRoot == "" || beadID == "" {
		return nil
	}
	// Stub: Hook the bead with retry logic
	return nil
}

// hookBeadWithRetryWithTownRootFn is a function that hooks a bead with retry logic and town root.
func hookBeadWithRetryWithTownRootFn(beadID string, targetAgent string, hookDir string, townRoot string) error {
	if beadID == "" || townRoot == "" {
		return nil
	}
	// Stub: Hook the bead with retry logic using town root
	return nil
}

// resolveTargetAgentID resolves the target agent ID from a target agent string.
func resolveTargetAgentID(targetAgent string) (string, string, error) {
	// Stub: Resolve agent ID and name from the target agent string
	// Returns (agentID, agentName, error)
	if targetAgent != "" {
		return targetAgent, targetAgent, nil
	}
	return "", "", nil
}
