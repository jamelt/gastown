package doctor

import (
	"fmt"

	"github.com/steveyegge/gastown/internal/doltserver"
)

// DoltServerHealthCheck verifies that the Dolt sql-server is running and accessible.
// This is critical for beads operations and all downstream agents.
type DoltServerHealthCheck struct {
	BaseCheck
}

// NewDoltServerHealthCheck creates a new Dolt server health check.
func NewDoltServerHealthCheck() *DoltServerHealthCheck {
	return &DoltServerHealthCheck{
		BaseCheck: BaseCheck{
			CheckName:        "dolt-server-health",
			CheckDescription: "Check that dolt sql-server is running and accessible",
			CheckCategory:    CategoryInfrastructure,
		},
	}
}

// Run checks if the Dolt server is running and responding to connections.
func (c *DoltServerHealthCheck) Run(ctx *CheckContext) *CheckResult {
	running, pid, err := doltserver.IsRunning(ctx.TownRoot)

	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: fmt.Sprintf("Dolt server health check failed: %v", err),
			FixHint: "Check dolt logs: dolt sql-server logs are usually in .dolt-data/.dolt/logs/",
		}
	}

	if !running {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "Dolt sql-server is not running",
			Details: []string{
				"The beads database backend depends on a running Dolt sql-server",
				"All agents (polecats, refinery, witness, crew) are blocked until Dolt restarts",
			},
			FixHint: "Restart Dolt server: gt dolt start",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: fmt.Sprintf("Dolt sql-server running (PID %d)", pid),
	}
}
