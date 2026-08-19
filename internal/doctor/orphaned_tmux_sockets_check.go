package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/steveyegge/gastown/internal/tmux"
)

// OrphanedTmuxSocketsCheck detects stale tmux socket files from crashed/killed tests.
type OrphanedTmuxSocketsCheck struct {
	FixableCheck
	sockets []string // track stale sockets for Fix()
}

// NewOrphanedTmuxSocketsCheck creates a new orphaned socket check.
func NewOrphanedTmuxSocketsCheck() *OrphanedTmuxSocketsCheck {
	return &OrphanedTmuxSocketsCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "orphaned-tmux-sockets",
				CheckDescription: "Check for stale tmux socket files from crashed or killed tests",
				CheckCategory:    CategoryInfrastructure,
			},
		},
	}
}

// CanFix returns true if orphaned sockets were found.
func (c *OrphanedTmuxSocketsCheck) CanFix() bool {
	return len(c.sockets) > 0
}

// Fix kills all orphaned tmux servers and removes their socket files.
func (c *OrphanedTmuxSocketsCheck) Fix(ctx *CheckContext) error {
	var errs []string

	for _, socket := range c.sockets {
		socketName := filepath.Base(socket)
		// Kill the tmux server on this socket (handles case where server is
		// still running but unresponsive/zombie).
		_ = exec.Command("tmux", "-L", socketName, "kill-server").Run()

		// Remove the socket file itself.
		if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Sprintf("%s: %v", socketName, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("cleanup errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// Run scans for orphaned tmux sockets and reports findings.
func (c *OrphanedTmuxSocketsCheck) Run(ctx *CheckContext) *CheckResult {
	socketDir := tmux.SocketDir()

	// Check if socket directory exists.
	fi, err := os.Stat(socketDir)
	if err != nil {
		if os.IsNotExist(err) {
			return &CheckResult{
				Name:    c.Name(),
				Status:  StatusOK,
				Message: "No tmux socket directory (no tests have run)",
			}
		}
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "Cannot access tmux socket directory",
			Details: []string{socketDir, err.Error()},
		}
	}

	if !fi.IsDir() {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "Tmux socket path is not a directory",
		}
	}

	// Scan for test-socket files (gt-test-* or gt-h9z-* etc).
	// These are created by individual test functions and should be cleaned up.
	entries, err := os.ReadDir(socketDir)
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "Cannot read tmux socket directory",
			Details: []string{err.Error()},
		}
	}

	var orphaned []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()

		// Look for test socket patterns: gt-test-*, gt-h9z-*, gastown-test-*, etc.
		// Skip live/production sockets (gastown-ops-*, other patterns).
		if !isTestSocket(name) {
			continue
		}

		// Check if the socket has a live server.
		// If `tmux -L <socket> list-sessions` succeeds, there's an active server.
		// If it fails with "no server running", the socket is stale.
		cmd := exec.Command("tmux", "-L", name, "list-sessions")
		err := cmd.Run()
		if err != nil {
			// Socket appears to be dead/orphaned.
			orphaned = append(orphaned, filepath.Join(socketDir, name))
		}
	}

	c.sockets = orphaned

	if len(orphaned) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: fmt.Sprintf("No orphaned test sockets in %s", socketDir),
		}
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: fmt.Sprintf("Found %d orphaned test socket(s)", len(orphaned)),
		Details: detailsForSockets(orphaned),
		FixHint: "Run 'gt doctor --fix' to clean up orphaned sockets",
	}
}

// isTestSocket checks if a socket name matches a test pattern.
func isTestSocket(name string) bool {
	testPrefixes := []string{
		"gt-test-",       // Standard test socket pattern
		"gt-h9z-",        // Live/house sockets (also from tests)
		"gastown-test-",  // Package-specific test patterns
		"dog-",           // dog-related test sockets
	}

	for _, prefix := range testPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// detailsForSockets formats socket paths as checkresult details.
func detailsForSockets(sockets []string) []string {
	if len(sockets) > 20 {
		// Limit details output to avoid overwhelming the user.
		details := make([]string, 20)
		copy(details, sockets)
		details = append(details, fmt.Sprintf("... and %d more", len(sockets)-20))
		return details
	}
	return sockets
}
