package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/constants"
)

var (
	agentsResolveRole  string
	agentsResolveRig   string
	agentsResolveJSON  bool
	agentsResolveQuiet bool
)

var agentsResolveCmd = &cobra.Command{
	Use:   "resolve",
	Short: "Resolve the active agent bead for a role",
	Long: `Resolve the active agent bead for a role.

The resolver searches the current rig database and the town database across
both durable issues and ephemeral wisps. It prefers the current rig's wisp
record, then rig issue, town wisp, and town issue. Closed beads are ignored.`,
	RunE: runAgentsResolve,
}

func init() {
	agentsResolveCmd.Flags().StringVar(&agentsResolveRole, "role", "", "Agent role to resolve (witness, refinery, crew, polecat, mayor, deacon)")
	agentsResolveCmd.Flags().StringVar(&agentsResolveRig, "rig", "", "Rig name for rig-scoped roles")
	agentsResolveCmd.Flags().BoolVar(&agentsResolveJSON, "json", false, "Output match provenance as JSON")
	agentsResolveCmd.Flags().BoolVar(&agentsResolveQuiet, "quiet", false, "Suppress no-match diagnostics")
	agentsCmd.AddCommand(agentsResolveCmd)
}

type agentBeadSource string

const (
	agentSourceRigWisps   agentBeadSource = "rig-wisps"
	agentSourceRigIssues  agentBeadSource = "rig-issues"
	agentSourceTownWisps  agentBeadSource = "town-wisps"
	agentSourceTownIssues agentBeadSource = "town-issues"
)

type agentBeadCandidate struct {
	ID       string
	Source   agentBeadSource
	BeadsDir string
	Status   string
	Issue    *beads.Issue
}

type agentsResolveResult struct {
	ID       string `json:"id"`
	Source   string `json:"source"`
	BeadsDir string `json:"beads_dir"`
	Status   string `json:"status"`
}

func runAgentsResolve(cmd *cobra.Command, _ []string) error {
	role := strings.TrimSpace(agentsResolveRole)
	rig := strings.TrimSpace(agentsResolveRig)
	if role == "" {
		return fmt.Errorf("--role is required")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}
	currentBeadsDir, err := resolveAgentTrackingBeadsDir()
	if err != nil {
		return err
	}
	// An explicit rig selects that rig's database regardless of CWD. Without
	// this, running from town root searches town state twice and cannot prove
	// the singleton identity is the authoritative rig-local record.
	currentBeadsDir, err = resolveAgentsPrimaryBeadsDir(cwd, currentBeadsDir, rig)
	if err != nil {
		return err
	}

	candidates, err := findAgentBeadCandidates(cwd, currentBeadsDir)
	if err != nil {
		return err
	}

	var matches []agentBeadCandidate
	for _, candidate := range candidates {
		if agentBeadMatches(candidate.Issue, role, rig) {
			matches = append(matches, candidate)
		}
	}

	// A rig-local Witness/Refinery record can be closed for a mundane
	// reason (e.g. auto-closed by the reaper for heartbeat staleness) while
	// a same-ID town-sourced bead stays open. Reopen it in place — mirroring
	// the reopen-don't-recreate pattern already used by EnsureRigIdentities
	// and gt doctor --fix — before ranking, so pickBestAgentBead sees it as
	// open and picks the authoritative rig record over the town duplicate.
	// This MUST run before pickBestAgentBead: that function's in-place
	// closed-candidate filter reuses matches's backing array, so anything
	// read from matches afterward may already be overwritten (gt-n99t).
	healRigSingletons(matches, role)

	match := pickBestAgentBead(matches)
	if match == nil {
		message := fmt.Sprintf("no agent bead found for role %q", role)
		if rig != "" {
			message += fmt.Sprintf(" in rig %q", rig)
		}
		if agentsResolveJSON {
			_ = json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]string{"error": message})
			return NewSilentExit(1)
		}
		if agentsResolveQuiet {
			return NewSilentExit(1)
		}
		return fmt.Errorf("%s", message)
	}
	if rig != "" && agentBeadSourceIsTown(match.Source) && !agentsResolveJSON {
		return fmt.Errorf("agent bead %s was found only in %s; patrol await/state commands require a rig-local agent bead", match.ID, match.Source)
	}

	if agentsResolveJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(agentsResolveResult{
			ID:       match.ID,
			Source:   string(match.Source),
			BeadsDir: match.BeadsDir,
			Status:   match.Status,
		})
	}

	fmt.Fprintln(cmd.OutOrStdout(), match.ID)
	return nil
}

func resolveAgentsPrimaryBeadsDir(cwd, currentBeadsDir, rigName string) (string, error) {
	if rigName == "" {
		return currentBeadsDir, nil
	}
	townRoot := beads.FindTownRoot(cwd)
	if townRoot == "" {
		return "", fmt.Errorf("cannot resolve rig %q outside a Gas Town workspace", rigName)
	}
	rigBeadsDir, ok := beads.ResolveRepoAliasBeadsDir(townRoot, rigName)
	if !ok {
		return "", fmt.Errorf("cannot resolve beads database for rig %q", rigName)
	}
	return beads.ResolveBeadsDir(rigBeadsDir), nil
}

func findAgentBeadCandidates(cwd, currentBeadsDir string) ([]agentBeadCandidate, error) {
	var candidates []agentBeadCandidate

	rigCandidates, err := loadAgentBeadsFromDir(currentBeadsDir, agentSourceRigIssues, agentSourceRigWisps)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, rigCandidates...)

	townRoot := beads.FindTownRoot(cwd)
	if townRoot == "" {
		return candidates, nil
	}
	townBeadsDir := beads.ResolveBeadsDir(beads.GetTownBeadsPath(townRoot))
	if townBeadsDir == "" || filepath.Clean(townBeadsDir) == filepath.Clean(currentBeadsDir) {
		return candidates, nil
	}

	townCandidates, err := loadAgentBeadsFromDir(townBeadsDir, agentSourceTownIssues, agentSourceTownWisps)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, townCandidates...)
	return candidates, nil
}

func loadAgentBeadsFromDir(beadsDir string, issueSource, wispSource agentBeadSource) ([]agentBeadCandidate, error) {
	db := beads.NewWithBeadsDir(filepath.Dir(beadsDir), beadsDir)
	var candidates []agentBeadCandidate

	issues, err := listAgentIssues(db)
	if err != nil {
		return nil, fmt.Errorf("listing agent issues in %s: %w", beadsDir, err)
	}
	for _, issue := range issues {
		candidates = append(candidates, agentBeadCandidate{
			ID:       issue.ID,
			Source:   issueSource,
			BeadsDir: beadsDir,
			Status:   issue.Status,
			Issue:    issue,
		})
	}

	if wisps, err := db.List(beads.ListOptions{Ephemeral: true, Label: "gt:agent", Status: "all"}); err == nil {
		for _, wisp := range wisps {
			candidates = append(candidates, agentBeadCandidate{
				ID:       wisp.ID,
				Source:   wispSource,
				BeadsDir: beadsDir,
				Status:   wisp.Status,
				Issue:    wisp,
			})
		}
	}

	return candidates, nil
}

func listAgentIssues(db *beads.Beads) ([]*beads.Issue, error) {
	out, err := db.Run("list", "--label=gt:agent", "--include-infra", "--status=all", "--json", "--flat", "--no-pager", "--limit=0")
	if err != nil {
		return nil, err
	}
	if len(out) == 0 || !json.Valid(out) {
		return nil, nil
	}

	var issues []*beads.Issue
	if err := json.Unmarshal(out, &issues); err != nil {
		return nil, fmt.Errorf("parsing bd list output: %w", err)
	}
	return issues, nil
}

func agentBeadMatches(issue *beads.Issue, role, rig string) bool {
	if issue == nil {
		return false
	}

	fields := beads.ParseAgentFields(issue.Description)
	if fields.RoleType == role {
		if rig == "" || fields.Rig == rig {
			return true
		}
	}

	idRig, idRole, _, ok := beads.ParseAgentBeadID(issue.ID)
	if !ok || idRole != role {
		return false
	}
	if rig == "" {
		return idRig == ""
	}
	return idRig == rig
}

// healRigSingletons reopens every closed rig-local Witness/Refinery
// singleton candidate in matches, in place. Only these two roles get this
// treatment — they're the sole rig-local singleton exception
// (beads.Beads.ForLocalBeads); other roles (crew, polecat, ...) can be
// legitimately closed forever, so auto-reopening them would resurrect
// deliberately-retired identities.
//
// Must run before pickBestAgentBead, for two reasons: pickBestAgentBead's
// in-place closed-candidate filter reuses matches's backing array, so a
// closed candidate read from matches afterward may already have been
// overwritten (gt-n99t); and mutating in place — rather than returning a
// single winner — lets pickBestAgentBead's existing canonical-ID tie-break
// (agentBeadIDRank) decide when a legacy and a canonical closed duplicate
// both need reopening, instead of this function guessing which one to heal.
func healRigSingletons(matches []agentBeadCandidate, role string) {
	if role != constants.RoleWitness && role != constants.RoleRefinery {
		return
	}
	for i := range matches {
		c := &matches[i]
		if c.Source != agentSourceRigIssues && c.Source != agentSourceRigWisps {
			continue
		}
		if !strings.EqualFold(c.Status, "closed") {
			continue
		}
		bd := beads.NewRigLocal(c.BeadsDir)
		open := "open"
		if err := bd.Update(c.ID, beads.UpdateOptions{Status: &open}); err != nil {
			continue
		}
		c.Status = "open"
	}
}

// pickBestAgentBead picks the single authoritative candidate. When several
// candidates tie for the best source rank — e.g. a legacy bead ID that
// predates the prefix-rig-role convention coexists with the canonical one
// registered under the current convention — it canonicalizes deterministically
// rather than blocking resolution. Losing candidates are left untouched in
// beads storage; this only decides which one resolve() reports.
func pickBestAgentBead(candidates []agentBeadCandidate) *agentBeadCandidate {
	open := candidates[:0]
	for _, candidate := range candidates {
		if strings.EqualFold(candidate.Status, "closed") {
			continue
		}
		open = append(open, candidate)
	}
	if len(open) == 0 {
		return nil
	}

	sort.Slice(open, func(i, j int) bool {
		leftRank := agentBeadSourceRank(open[i].Source)
		rightRank := agentBeadSourceRank(open[j].Source)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if leftRank2, rightRank2 := agentBeadIDRank(open[i].ID), agentBeadIDRank(open[j].ID); leftRank2 != rightRank2 {
			return leftRank2 < rightRank2
		}
		return open[i].ID < open[j].ID
	})

	return &open[0]
}

// agentBeadIDRank ranks bead IDs by conformance to the structured
// prefix-rig-role[-name] agent ID convention: 0 for IDs that parse
// successfully, 1 for legacy IDs (e.g. a bare "rig-role" predating the
// convention) that only matched via description fields.
func agentBeadIDRank(id string) int {
	if _, _, _, ok := beads.ParseAgentBeadID(id); ok {
		return 0
	}
	return 1
}

func agentBeadSourceRank(source agentBeadSource) int {
	switch source {
	case agentSourceRigWisps:
		return 0
	case agentSourceRigIssues:
		return 1
	case agentSourceTownWisps:
		return 2
	case agentSourceTownIssues:
		return 3
	default:
		return 99
	}
}

func agentBeadSourceIsTown(source agentBeadSource) bool {
	return source == agentSourceTownWisps || source == agentSourceTownIssues
}
