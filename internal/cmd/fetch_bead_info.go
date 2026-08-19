package cmd

import (
	"encoding/json"
	"path/filepath"

	"github.com/steveyegge/gastown/internal/beads"
)

// getBeadInfoFromTownRoot fetches a single bead's information from the town root beads database.
func getBeadInfoFromTownRoot(townRoot string, beadID string) (*beadInfo, error) {
	townBeadsDir := filepath.Join(townRoot, ".beads")
	resolvedBeadsDir := beads.ResolveBeadsDirForID(townBeadsDir, beadID)

	b := beads.NewWithBeadsDir(filepath.Dir(resolvedBeadsDir), resolvedBeadsDir)
	out, err := b.Run("show", "--json", beadID)
	if err != nil {
		return nil, err
	}

	var items []struct {
		ID           string           `json:"id"`
		Status       string           `json:"status"`
		Title        string           `json:"title"`
		Assignee     string           `json:"assignee"`
		Description  string           `json:"description"`
		Labels       []string         `json:"labels"`
		Dependencies []beads.IssueDep `json:"dependencies"`
		IssueType    string           `json:"issue_type"`
	}

	if err := json.Unmarshal(out, &items); err != nil {
		return nil, err
	}

	if len(items) == 0 {
		return nil, nil
	}

	item := items[0]
	return &beadInfo{
		Title:        item.Title,
		Status:       item.Status,
		Assignee:     item.Assignee,
		Description:  item.Description,
		Labels:       item.Labels,
		Dependencies: item.Dependencies,
		IssueType:    item.IssueType,
	}, nil
}
