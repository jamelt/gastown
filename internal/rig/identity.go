package rig

import (
	"errors"
	"fmt"

	"github.com/steveyegge/gastown/internal/beads"
)

// EnsureRigIdentities creates and verifies the three durable identities that
// make a rig operational: the rig itself, its Witness, and its Refinery. All
// operations are pinned to the rig database; agent creation must not use the
// normal town-level agent routing path.
func EnsureRigIdentities(rigPath, rigName, prefix, repo string) error {
	if rigPath == "" || rigName == "" || prefix == "" {
		return fmt.Errorf("rig path, name, and beads prefix are required")
	}

	rigBeadsDir := beads.ResolveBeadsDir(rigPath)
	bd := beads.NewWithBeadsDir(rigPath, rigBeadsDir).ForLocalBeads()

	rigID := beads.RigBeadIDWithPrefix(prefix, rigName)
	rigIssue, err := bd.EnsureRigBead(rigName, &beads.RigFields{
		Repo:   repo,
		Prefix: prefix,
		State:  beads.RigStateActive,
	})
	if err != nil {
		return fmt.Errorf("ensuring rig identity %s: %w", rigID, err)
	}
	if rigIssue.Status == "closed" {
		open := "open"
		if err := bd.Update(rigID, beads.UpdateOptions{Status: &open}); err != nil {
			return fmt.Errorf("reopening rig identity %s: %w", rigID, err)
		}
	}
	if !beads.HasLabel(rigIssue, "gt:rig") {
		if err := bd.Update(rigID, beads.UpdateOptions{AddLabels: []string{"gt:rig"}}); err != nil {
			return fmt.Errorf("labeling rig identity %s: %w", rigID, err)
		}
	}

	type singleton struct {
		id    string
		role  string
		title string
	}
	singletons := []singleton{
		{
			id:    beads.WitnessBeadIDWithPrefix(prefix, rigName),
			role:  "witness",
			title: fmt.Sprintf("Witness for %s - monitors polecat health and progress.", rigName),
		},
		{
			id:    beads.RefineryBeadIDWithPrefix(prefix, rigName),
			role:  "refinery",
			title: fmt.Sprintf("Refinery for %s - processes merge queue.", rigName),
		},
	}

	for _, agent := range singletons {
		if _, err := ensureRigSingletonAgent(bd, agent.id, agent.title, agent.role, rigName); err != nil {
			return fmt.Errorf("ensuring %s identity %s: %w", agent.role, agent.id, err)
		}
	}

	// Read the records back through the same pinned database before callers are
	// allowed to register or start managers. This catches half-server/half-direct
	// metadata and silent wrong-database writes immediately.
	var verifyErrs []error
	for _, expected := range []struct {
		id    string
		label string
		role  string
	}{
		{id: rigID, label: "gt:rig"},
		{id: singletons[0].id, label: "gt:agent", role: singletons[0].role},
		{id: singletons[1].id, label: "gt:agent", role: singletons[1].role},
	} {
		issue, err := bd.Show(expected.id)
		if err != nil {
			verifyErrs = append(verifyErrs, fmt.Errorf("reading %s from rig database: %w", expected.id, err))
			continue
		}
		if issue.Status == "closed" {
			verifyErrs = append(verifyErrs, fmt.Errorf("identity %s is closed", expected.id))
		}
		if !beads.HasLabel(issue, expected.label) {
			verifyErrs = append(verifyErrs, fmt.Errorf("identity %s is missing %s label", expected.id, expected.label))
		}
		if expected.role != "" {
			fields := beads.ParseAgentFields(issue.Description)
			if fields.RoleType != expected.role || fields.Rig != rigName {
				verifyErrs = append(verifyErrs, fmt.Errorf("identity %s has role/rig %q/%q, want %q/%q", expected.id, fields.RoleType, fields.Rig, expected.role, rigName))
			}
		}
	}
	return errors.Join(verifyErrs...)
}

func ensureRigSingletonAgent(bd *beads.Beads, id, title, role, rigName string) (*beads.Issue, error) {
	issue, showErr := bd.Show(id)
	if showErr != nil || issue == nil || issue.ID != id {
		created, createErr := bd.CreateAgentBead(id, title, &beads.AgentFields{
			RoleType:   role,
			Rig:        rigName,
			AgentState: "idle",
		})
		if createErr == nil {
			return created, nil
		}

		// A concurrent repair may have won the create race. Accept it only after
		// an exact-ID readback; otherwise surface the original write failure.
		issue, showErr = bd.Show(id)
		if showErr != nil || issue == nil || issue.ID != id {
			return nil, createErr
		}
	}

	var opts beads.UpdateOptions
	needsUpdate := false
	if issue.Status == "closed" {
		open := "open"
		opts.Status = &open
		needsUpdate = true
	}
	if !beads.HasLabel(issue, "gt:agent") {
		opts.AddLabels = []string{"gt:agent"}
		needsUpdate = true
	}
	fields := beads.ParseAgentFields(issue.Description)
	if fields.RoleType != role || fields.Rig != rigName || fields.AgentState == "" {
		fields.RoleType = role
		fields.Rig = rigName
		if fields.AgentState == "" {
			fields.AgentState = "idle"
		}
		description := beads.FormatAgentDescription(title, fields)
		opts.Description = &description
		needsUpdate = true
	}
	if needsUpdate {
		if err := bd.Update(id, opts); err != nil {
			return nil, err
		}
		return bd.Show(id)
	}
	return issue, nil
}

// EnsureIdentities verifies the rig database and singleton identities before
// a Witness or Refinery manager starts.
func (r *Rig) EnsureIdentities() error {
	if r == nil {
		return fmt.Errorf("rig is nil")
	}
	prefix := ""
	if r.Config != nil {
		prefix = r.Config.Prefix
	}
	repo := r.GitURL
	if cfg, err := LoadRigConfig(r.Path); err == nil {
		if prefix == "" && cfg.Beads != nil {
			prefix = cfg.Beads.Prefix
		}
		if repo == "" {
			repo = cfg.GitURL
		}
	}
	return EnsureRigIdentities(r.Path, r.Name, prefix, repo)
}
