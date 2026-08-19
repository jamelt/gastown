package cmd

import (
	"encoding/json"
	"path/filepath"

	"github.com/steveyegge/gastown/internal/beads"
)

// beadInfo holds fetched bead information from a beads database.
type beadInfo struct {
	ID           string
	Status       string
	Title        string
	Labels       []string
	Dependencies []beads.IssueDep
}

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
		Labels       []string         `json:"labels"`
		Dependencies []beads.IssueDep `json:"dependencies"`
	}

	if err := json.Unmarshal(out, &items); err != nil {
		return nil, err
	}

	if len(items) == 0 {
		return nil, nil
	}

	item := items[0]
	return &beadInfo{
		ID:           item.ID,
		Status:       item.Status,
		Title:        item.Title,
		Labels:       item.Labels,
		Dependencies: item.Dependencies,
	}, nil
}
