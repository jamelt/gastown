package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// StaleGtDoneRecoveryCheck detects and removes old recovery records from the
// gt-done-recovery directory. These records inventory refused auto-saves when
// gt done encounters protection issues (detached HEAD, protected branch, or
// unresolvable source issue).
//
// Recovery records are temporary — they document why a specific gt done
// invocation failed but don't need to be retained indefinitely. This check
// removes records older than a retention threshold (default: 7 days).
type StaleGtDoneRecoveryCheck struct {
	FixableCheck
	staleRecords []string
}

// NewStaleGtDoneRecoveryCheck creates a new stale gt-done-recovery check.
func NewStaleGtDoneRecoveryCheck() *StaleGtDoneRecoveryCheck {
	return &StaleGtDoneRecoveryCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "stale-gt-done-recovery",
				CheckDescription: "Detect and remove old gt-done-recovery records",
				CheckCategory:    CategoryCleanup,
			},
		},
	}
}

// Run checks for stale gt-done-recovery records.
func (c *StaleGtDoneRecoveryCheck) Run(ctx *CheckContext) *CheckResult {
	c.staleRecords = nil

	recoveryDir := filepath.Join(ctx.TownRoot, ".runtime", "gt-done-recovery")

	// If directory doesn't exist, no old records to clean
	entries, err := os.ReadDir(recoveryDir)
	if err != nil {
		if os.IsNotExist(err) {
			return &CheckResult{
				Name:    c.Name(),
				Status:  StatusOK,
				Message: "No gt-done-recovery records found",
			}
		}
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: "Could not read gt-done-recovery directory",
			Details: []string{err.Error()},
		}
	}

	// Retention threshold: 7 days
	retentionDays := 7
	cutoffTime := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)

	var details []string

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filePath := filepath.Join(recoveryDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue // Skip files we can't stat
		}

		// Check if file is older than retention threshold
		if info.ModTime().Before(cutoffTime) {
			c.staleRecords = append(c.staleRecords, filePath)
			details = append(details, fmt.Sprintf("Old recovery record (%s): %s",
				info.ModTime().Format("2006-01-02"), entry.Name()))
		}
	}

	if len(c.staleRecords) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No stale gt-done-recovery records found",
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: fmt.Sprintf("%d stale gt-done-recovery record(s) older than %d days", len(c.staleRecords), retentionDays),
		Details: details,
		FixHint: "Run 'gt doctor --fix' to remove stale recovery records",
	}
}

// Fix removes stale gt-done-recovery records.
func (c *StaleGtDoneRecoveryCheck) Fix(ctx *CheckContext) error {
	for _, path := range c.staleRecords {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("could not remove %s: %w", path, err)
		}
	}
	return nil
}
