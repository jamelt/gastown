package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/doltserver"
)

var inspectDoltLineageFn = doltserver.InspectLineageSQL

// DoltLineageCheck detects independent local and remote Beads histories.
// It is intentionally non-fixable: choosing authoritative history and
// preserving unique records requires explicit operator approval.
type DoltLineageCheck struct{ BaseCheck }

func NewDoltLineageCheck() *DoltLineageCheck {
	return &DoltLineageCheck{BaseCheck: BaseCheck{
		CheckName:        "dolt-lineage",
		CheckDescription: "Verify rig Beads local and remote histories share an ancestor",
		CheckCategory:    CategoryInfrastructure,
	}}
}

func (c *DoltLineageCheck) Run(ctx *CheckContext) *CheckResult {
	if ctx.RigName != "" {
		return c.runRig(ctx, ctx.RigName)
	}
	rigs, err := config.LoadRigsConfig(filepath.Join(ctx.TownRoot, "mayor", "rigs.json"))
	if err != nil {
		return &CheckResult{Name: c.Name(), Status: StatusWarning, Message: "Could not load rigs for lineage inspection", Details: []string{err.Error()}}
	}
	var names []string
	for name := range rigs.Rigs {
		names = append(names, name)
	}
	sort.Strings(names)
	worst := StatusOK
	checked := 0
	var details []string
	for _, name := range names {
		result := c.runRig(ctx, name)
		if result.Message == "No Beads remote configured" {
			continue
		}
		checked++
		if result.Status > worst {
			worst = result.Status
		}
		details = append(details, fmt.Sprintf("%s: %s", name, result.Message))
		for _, detail := range result.Details {
			details = append(details, fmt.Sprintf("%s: %s", name, detail))
		}
	}
	if checked == 0 {
		return &CheckResult{Name: c.Name(), Status: StatusOK, Message: "No rig Beads remotes configured"}
	}
	message := fmt.Sprintf("Verified lineage for %d rig database(s)", checked)
	fixHint := ""
	if worst == StatusError {
		message = "One or more rig Beads histories have no common ancestor"
		fixHint = "Run 'gt dolt reconcile --db <database>' for each divergent rig; do not force-push"
	} else if worst == StatusWarning {
		message = "One or more rig Beads histories could not be verified"
		fixHint = "Restore Dolt connectivity, or bootstrap/register the configured remote before dispatch"
	}
	return &CheckResult{Name: c.Name(), Status: worst, Message: message, Details: details, FixHint: fixHint}
}

func (c *DoltLineageCheck) runRig(ctx *CheckContext, rigName string) *CheckResult {
	beadsDir := doltserver.FindRigBeadsDir(ctx.TownRoot, rigName)
	config, err := os.ReadFile(filepath.Join(beadsDir, "config.yaml"))
	if err != nil {
		if os.IsNotExist(err) {
			return &CheckResult{Name: c.Name(), Status: StatusOK, Message: "No Beads remote configured"}
		}
		return &CheckResult{Name: c.Name(), Status: StatusWarning, Message: "Could not read Beads config", Details: []string{err.Error()}}
	}
	if !doctorConfigHasSyncRemote(string(config)) {
		return &CheckResult{Name: c.Name(), Status: StatusOK, Message: "No Beads remote configured"}
	}
	dbName := beads.DatabaseNameFromMetadata(beadsDir)
	if dbName == "" {
		return &CheckResult{
			Name: c.Name(), Status: StatusError,
			Message: "Remote sync configured without Dolt database metadata",
			FixHint: "Repair metadata before dispatching work",
		}
	}
	report, err := inspectDoltLineageFn(ctx.TownRoot, dbName)
	if err != nil {
		return &CheckResult{
			Name: c.Name(), Status: StatusWarning,
			Message: "Could not verify Dolt lineage",
			Details: []string{err.Error()},
			FixHint: "Restore Dolt connectivity and rerun gt doctor --rig " + rigName,
		}
	}
	if report.State == doltserver.LineageDiverged {
		return &CheckResult{
			Name: c.Name(), Status: StatusError,
			Message: "Local and remote Beads histories have no common ancestor",
			Details: []string{report.Diagnostic()},
			FixHint: fmt.Sprintf("Run 'gt dolt reconcile --db %s' to inspect; reconciliation requires an explicit authority and preservation bundle", dbName),
		}
	}
	if report.State == doltserver.LineageNoRemote {
		return &CheckResult{
			Name: c.Name(), Status: StatusWarning,
			Message: "Beads config declares sync.remote but the Dolt database has no registered remote",
			Details: []string{report.Diagnostic()},
			FixHint: fmt.Sprintf("Register the Dolt remote for %s and bootstrap it, or remove the sync.remote line if remote sync isn't intended", dbName),
		}
	}
	if !report.Shared() {
		return &CheckResult{
			Name: c.Name(), Status: StatusWarning,
			Message: "Remote Beads lineage is not yet verified",
			Details: []string{report.Diagnostic()},
			FixHint: "Bootstrap from the configured remote before dispatching work",
		}
	}
	return &CheckResult{Name: c.Name(), Status: StatusOK, Message: "Local and remote Beads histories share lineage"}
}

func (c *DoltLineageCheck) Fix(_ *CheckContext) error { return nil }
func (c *DoltLineageCheck) CanFix() bool              { return false }

func doctorConfigHasSyncRemote(config string) bool {
	for _, line := range strings.Split(config, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.HasPrefix(line, "sync.remote:") {
			continue
		}
		return strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "sync.remote:")), `"'`) != ""
	}
	return false
}
