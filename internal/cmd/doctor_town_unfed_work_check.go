package cmd

import (
	"fmt"
	"sort"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/doctor"
)

// dispatchableTownTypes are issue types that imply code/engineering work, as
// opposed to coordination records, standing directives, or Gas Town runtime
// machinery (wisps, convoys, agent/role beads, etc). Mirrors the type list
// gt-1g4w proposed for a bd-create-time warning in the town database.
var dispatchableTownTypes = map[string]bool{
	"bug":     true,
	"feature": true,
	"chore":   true,
	"task":    true,
}

// townUnfedWorkCheck flags dispatchable-looking beads sitting open in the
// town database. `gt scheduler feed` only surveys rig databases (see
// runSchedulerFeed in scheduler_feed.go) — the town database has no owning
// rig, so nothing filed there is ever fed to a polecat automatically.
//
// Without a standing check, this accumulates silently: gt-1g4w found 406
// open town beads including 32 P0s, one of which (hq-f2n8c) contained a
// complete root-cause analysis for a defect that then cost a full night of
// incident debugging because nobody read the town database before the
// defect fired. This check exists so that gap is visible in `gt doctor`
// instead of rediscovered by incident.
type townUnfedWorkCheck struct {
	doctor.BaseCheck
}

func newTownUnfedWorkCheck() *townUnfedWorkCheck {
	return &townUnfedWorkCheck{
		BaseCheck: doctor.BaseCheck{
			CheckName:        "town-unfed-work",
			CheckDescription: "Flag dispatchable-looking beads in the town database that gt scheduler feed cannot see",
			CheckCategory:    doctor.CategoryPatrol,
		},
	}
}

func (c *townUnfedWorkCheck) Run(ctx *doctor.CheckContext) *doctor.CheckResult {
	issues, err := beads.New(ctx.TownRoot).List(beads.ListOptions{Status: "open", Priority: -1})
	if err != nil {
		return &doctor.CheckResult{
			Name:    c.Name(),
			Status:  doctor.StatusWarning,
			Message: fmt.Sprintf("Could not list town beads: %v", err),
		}
	}

	details := evaluateTownUnfedWork(issues)
	if len(details) == 0 {
		return &doctor.CheckResult{
			Name:    c.Name(),
			Status:  doctor.StatusOK,
			Message: "No dispatchable-looking beads stuck in the unfed town database",
		}
	}

	return &doctor.CheckResult{
		Name:    c.Name(),
		Status:  doctor.StatusWarning,
		Message: fmt.Sprintf("%d dispatchable-looking bead(s) open in the town database, invisible to gt scheduler feed", len(details)),
		Details: details,
		FixHint: "gt scheduler feed does not survey town-level (hq-*) beads; manually route each to the rig that owns the code with gt bead move <id> <target-prefix> (e.g. gt-), or close it if it's coordination-only",
	}
}

// evaluateTownUnfedWork filters open town-database issues down to the ones
// that look like real engineering work rather than coordination records or
// Gas Town runtime machinery, and formats one report line per bead sorted by
// priority then ID. Extracted from Run so the filtering logic is unit
// testable without a real beads database (mirrors evaluateRigStalls in
// doctor_rig_stall_check.go).
func evaluateTownUnfedWork(issues []*beads.Issue) []string {
	var flagged []*beads.Issue
	for _, issue := range issues {
		if !dispatchableTownTypes[issue.Type] {
			continue
		}
		if isEpicOrConvoyIssue(issue) {
			continue
		}
		if beads.ConcreteWorkIssueRejectReason(issue) != "" {
			continue
		}
		flagged = append(flagged, issue)
	}

	sort.Slice(flagged, func(i, j int) bool {
		if flagged[i].Priority != flagged[j].Priority {
			return flagged[i].Priority < flagged[j].Priority
		}
		return flagged[i].ID < flagged[j].ID
	})

	details := make([]string, len(flagged))
	for i, issue := range flagged {
		details[i] = fmt.Sprintf("%s [P%d %s]: %s", issue.ID, issue.Priority, issue.Type, issue.Title)
	}
	return details
}
