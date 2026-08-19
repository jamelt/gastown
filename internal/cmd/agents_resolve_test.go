package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

func TestResolveAgentsPrimaryBeadsDirUsesExplicitRigFromTownAndRigContexts(t *testing.T) {
	townRoot := t.TempDir()
	for _, dir := range []string{
		filepath.Join(townRoot, "mayor"),
		filepath.Join(townRoot, ".beads"),
		filepath.Join(townRoot, "gastown", ".beads"),
		filepath.Join(townRoot, "gastown", "mayor", "rig"),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(townRoot, "mayor", "town.json"), []byte(`{"name":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(townRoot, ".beads", "routes.jsonl"), []byte(`{"prefix":"gt-","path":"gastown"}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rigBeads := filepath.Join(townRoot, "gastown", ".beads")
	for _, cwd := range []string{townRoot, filepath.Join(townRoot, "gastown", "mayor", "rig")} {
		got, err := resolveAgentsPrimaryBeadsDir(cwd, filepath.Join(townRoot, ".beads"), "gastown")
		if err != nil {
			t.Fatalf("cwd %s: %v", cwd, err)
		}
		if filepath.Clean(got) != filepath.Clean(rigBeads) {
			t.Fatalf("cwd %s resolved %s, want %s", cwd, got, rigBeads)
		}
	}
}

func TestAgentBeadMatchesDescriptionAndIDFallback(t *testing.T) {
	tests := []struct {
		name  string
		issue *beads.Issue
		role  string
		rig   string
		want  bool
	}{
		{
			name: "description matches legacy random wisp ID",
			issue: &beads.Issue{
				ID:          "au-wisp-0ti",
				Description: "Agent\n\nrole_type: refinery\nrig: alleago_ui",
			},
			role: "refinery",
			rig:  "alleago_ui",
			want: true,
		},
		{
			name: "canonical ID fallback matches sparse wisp metadata",
			issue: &beads.Issue{
				ID: "gt-gastown-witness",
			},
			role: "witness",
			rig:  "gastown",
			want: true,
		},
		{
			name: "collapsed prefix-rig ID fallback matches sparse metadata",
			issue: &beads.Issue{
				ID: "cp-refinery",
			},
			role: "refinery",
			rig:  "cp",
			want: true,
		},
		{
			name: "role mismatch",
			issue: &beads.Issue{
				ID:          "gt-gastown-witness",
				Description: "Agent\n\nrole_type: witness\nrig: gastown",
			},
			role: "refinery",
			rig:  "gastown",
			want: false,
		},
		{
			name: "rig mismatch",
			issue: &beads.Issue{
				ID:          "gt-gastown-refinery",
				Description: "Agent\n\nrole_type: refinery\nrig: gastown",
			},
			role: "refinery",
			rig:  "other",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := agentBeadMatches(tt.issue, tt.role, tt.rig)
			if got != tt.want {
				t.Fatalf("agentBeadMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPickBestAgentBead(t *testing.T) {
	candidates := []agentBeadCandidate{
		candidate("town-issue", agentSourceTownIssues, "open"),
		candidate("rig-issue", agentSourceRigIssues, "open"),
		candidate("town-wisp", agentSourceTownWisps, "open"),
		candidate("rig-wisp", agentSourceRigWisps, "open"),
	}

	got := pickBestAgentBead(candidates)
	if got == nil || got.ID != "rig-wisp" {
		t.Fatalf("pickBestAgentBead picked %v, want rig-wisp", got)
	}
}

func TestPickBestAgentBeadSkipsClosed(t *testing.T) {
	candidates := []agentBeadCandidate{
		candidate("closed-rig-wisp", agentSourceRigWisps, "closed"),
		candidate("open-rig-issue", agentSourceRigIssues, "open"),
	}

	got := pickBestAgentBead(candidates)
	if got == nil || got.ID != "open-rig-issue" {
		t.Fatalf("pickBestAgentBead picked %v, want open-rig-issue", got)
	}
}

// TestPickBestAgentBeadCanonicalizesLegacyDuplicate reproduces the trader
// rig incident (gt-xjd): a legacy bead named "<rig>-<role>" from before the
// prefix-rig-role convention, and the canonical "<prefix>-<rig>-<role>" bead,
// both survive registration and match the same role/rig at the same source
// rank. Resolution must deterministically prefer the canonical-form ID
// instead of blocking await-signal with an ambiguity error.
func TestPickBestAgentBeadCanonicalizesLegacyDuplicate(t *testing.T) {
	candidates := []agentBeadCandidate{
		candidate("trader-refinery", agentSourceRigIssues, "open"),    // legacy, pre-prefix
		candidate("tr-trader-refinery", agentSourceRigIssues, "open"), // canonical
	}

	got := pickBestAgentBead(candidates)
	if got == nil || got.ID != "tr-trader-refinery" {
		t.Fatalf("pickBestAgentBead picked %v, want canonical tr-trader-refinery", got)
	}

	// Order independence: the canonicalization must not depend on input order.
	reversed := []agentBeadCandidate{candidates[1], candidates[0]}
	got = pickBestAgentBead(reversed)
	if got == nil || got.ID != "tr-trader-refinery" {
		t.Fatalf("pickBestAgentBead (reversed input) picked %v, want canonical tr-trader-refinery", got)
	}
}

// TestPickBestAgentBeadDeterministicWhenBothCanonical covers same-rank
// duplicates that both conform to the ID convention (e.g. a genuine
// double-registration race). Resolution still must not error — it falls
// back to lexicographic order so repeated calls agree.
func TestPickBestAgentBeadDeterministicWhenBothCanonical(t *testing.T) {
	candidates := []agentBeadCandidate{
		candidate("rig-wisp-b", agentSourceRigWisps, "open"),
		candidate("rig-wisp-a", agentSourceRigWisps, "open"),
	}

	got := pickBestAgentBead(candidates)
	if got == nil || got.ID != "rig-wisp-a" {
		t.Fatalf("pickBestAgentBead picked %v, want lexicographically first rig-wisp-a", got)
	}
}

func candidate(id string, source agentBeadSource, status string) agentBeadCandidate {
	return agentBeadCandidate{
		ID:     id,
		Source: source,
		Status: status,
		Issue:  &beads.Issue{ID: id, Status: status},
	}
}
