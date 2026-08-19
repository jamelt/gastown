package cmd

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/steveyegge/gastown/internal/doctor"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/polecat"
	"github.com/steveyegge/gastown/internal/rig"
	"github.com/steveyegge/gastown/internal/tmux"
)

// rigCapacityStallCheck flags a rig that has ready-to-dispatch work queued
// but zero polecats actually able to take it — a silent stall invisible to
// town-wide capacity checks when the free capacity lives in a different rig.
//
// This lives in package cmd rather than internal/doctor because it must reuse
// listScheduledBeads (the same ready/blocked assessment behind `gt scheduler
// list`) to avoid drifting from what operators see there. Capacity itself is
// still computed through polecat.Manager.WorkstateDispositionForPolecat, the
// same live classifier `gt polecat check-recovery` uses — not the optimistic
// labels from `gt polecat list`, which can disagree. See gt-yl9q.
type rigCapacityStallCheck struct {
	doctor.BaseCheck
}

func newRigCapacityStallCheck() *rigCapacityStallCheck {
	return &rigCapacityStallCheck{
		BaseCheck: doctor.BaseCheck{
			CheckName:        "rig-capacity-stall",
			CheckDescription: "Flag rigs with queued work and zero usable polecat capacity",
			CheckCategory:    doctor.CategoryPatrol,
		},
	}
}

// Run scans every rig with ready scheduled work and compares it against that
// rig's own usable polecat capacity (see usableCapacityForRig).
func (c *rigCapacityStallCheck) Run(ctx *doctor.CheckContext) *doctor.CheckResult {
	scheduled, err := listScheduledBeads(ctx.TownRoot)
	if err != nil {
		return &doctor.CheckResult{
			Name:    c.Name(),
			Status:  doctor.StatusWarning,
			Message: fmt.Sprintf("Could not list scheduled beads: %v", err),
		}
	}

	readyByRig := make(map[string]int)
	for _, b := range scheduled {
		if !b.Blocked && b.TargetRig != "" {
			readyByRig[b.TargetRig]++
		}
	}
	if len(readyByRig) == 0 {
		return &doctor.CheckResult{
			Name:    c.Name(),
			Status:  doctor.StatusOK,
			Message: "No ready scheduled work waiting on dispatch",
		}
	}

	tmuxClient := tmux.NewTmux()
	stalled := evaluateRigStalls(readyByRig, func(rigName string) (int, int, []string, error) {
		return usableCapacityForRig(ctx.TownRoot, rigName, tmuxClient)
	})

	if len(stalled) == 0 {
		return &doctor.CheckResult{
			Name:    c.Name(),
			Status:  doctor.StatusOK,
			Message: fmt.Sprintf("%d rig(s) have ready work, all have usable polecat capacity", len(readyByRig)),
		}
	}

	return &doctor.CheckResult{
		Name:    c.Name(),
		Status:  doctor.StatusWarning,
		Message: fmt.Sprintf("%d rig(s) stalled: ready work queued with zero usable polecat capacity", len(stalled)),
		Details: stalled,
		FixHint: "Recover blocked polecats (gt polecat check-recovery <rig>/<name>) or free capacity",
	}
}

// evaluateRigStalls compares each rig's ready-bead count against its usable
// polecat capacity (via capacityFn) and returns one detail line per stalled
// rig, sorted by rig name. Extracted from Run so the decision logic can be
// unit tested without real beads/git/tmux state.
func evaluateRigStalls(readyByRig map[string]int, capacityFn func(rigName string) (usable, total int, blockers []string, err error)) []string {
	rigs := make([]string, 0, len(readyByRig))
	for rigName := range readyByRig {
		rigs = append(rigs, rigName)
	}
	sort.Strings(rigs)

	var stalled []string
	for _, rigName := range rigs {
		usable, total, blockers, err := capacityFn(rigName)
		if err != nil {
			stalled = append(stalled, fmt.Sprintf("%s: could not assess polecat capacity: %v", rigName, err))
			continue
		}
		if total == 0 {
			// No polecats deployed for this rig at all — a different, existing
			// check (polecat-clones-valid) owns "no polecats" reporting.
			continue
		}
		if usable == 0 {
			detail := fmt.Sprintf("%s: %d ready bead(s) queued, 0 of %d polecat(s) usable", rigName, readyByRig[rigName], total)
			if len(blockers) > 0 {
				detail += " (" + strings.Join(blockers, ", ") + ")"
			}
			stalled = append(stalled, detail)
		}
	}
	return stalled
}

// usableCapacityForRig returns how many of a rig's polecats are genuinely
// dispatchable (reusable idle) versus the rig's total polecat count, plus a
// summary of why the rest are blocked. It classifies each polecat through the
// same live disposition check-recovery uses, so it cannot report capacity as
// healthy when check-recovery would call it blocked.
func usableCapacityForRig(townRoot, rigName string, tmuxClient *tmux.Tmux) (usable, total int, blockerReasons []string, err error) {
	rigPath := filepath.Join(townRoot, rigName)
	names, err := listPolecatDirectoryNames(rigPath)
	if err != nil {
		return 0, 0, nil, err
	}
	if len(names) == 0 {
		return 0, 0, nil, nil
	}

	mgr := polecat.NewManager(&rig.Rig{Name: rigName, Path: rigPath}, git.NewGit(rigPath), tmuxClient)
	reasons := make(map[string]int)
	for _, name := range names {
		p, getErr := mgr.Get(name)
		if getErr != nil {
			reasons["unresolvable"]++
			continue
		}
		disp := mgr.WorkstateDispositionForPolecat(p.Name, p.State, p.Issue)
		if disp.Reusable {
			usable++
			continue
		}
		reasons[disp.Verdict]++
	}

	for reason, count := range reasons {
		blockerReasons = append(blockerReasons, fmt.Sprintf("%s=%d", reason, count))
	}
	sort.Strings(blockerReasons)
	return usable, len(names), blockerReasons, nil
}
