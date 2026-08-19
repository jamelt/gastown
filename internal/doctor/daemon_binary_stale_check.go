package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/steveyegge/gastown/internal/daemon"
	"github.com/steveyegge/gastown/internal/util"
	"github.com/steveyegge/gastown/internal/version"
)

// DaemonBinaryStaleCheck verifies the *running* daemon process's own build
// commit (recorded at its startup, which can drift from the on-disk binary
// once a rebuild replaces the file underneath an already-running process)
// is not behind the build branch.
//
// This is distinct from StaleBinaryCheck, which only checks the commit of
// the CLI invocation currently running gt doctor. A merged fix to the daemon
// itself has no effect until the long-lived daemon process is restarted, and
// that gap is invisible to StaleBinaryCheck (gt-if5q).
type DaemonBinaryStaleCheck struct {
	FixableCheck
}

// NewDaemonBinaryStaleCheck creates a new daemon-process staleness check.
func NewDaemonBinaryStaleCheck() *DaemonBinaryStaleCheck {
	return &DaemonBinaryStaleCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "daemon-binary-stale",
				CheckDescription: "Check if the running daemon process is behind a rebuilt binary",
				CheckCategory:    CategoryInfrastructure,
			},
		},
	}
}

// Run checks if the running daemon process's recorded commit is stale.
func (c *DaemonBinaryStaleCheck) Run(ctx *CheckContext) *CheckResult {
	running, _, err := daemon.IsRunning(ctx.TownRoot)
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "Failed to check daemon status",
			Details: []string{err.Error()},
		}
	}
	if !running {
		return &CheckResult{Name: c.Name(), Status: StatusOK, Message: "Daemon is not running"}
	}

	state, err := daemon.LoadState(ctx.TownRoot)
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "Cannot load daemon state",
			Details: []string{err.Error()},
		}
	}
	if state.BinaryCommit == "" {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "Daemon predates binary-commit tracking (restart to enable staleness checks)",
		}
	}

	repoRoot, err := version.GetRepoRoot()
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "Cannot locate gt source repo (not a development environment)",
			Details: []string{err.Error()},
		}
	}

	return daemonStaleResult(c.Name(), version.CheckStaleBinaryForCommit(repoRoot, state.BinaryCommit))
}

// daemonStaleResult maps a completed staleness check to a doctor CheckResult.
// Pure (no git/env/process access) so it's unit-testable directly.
func daemonStaleResult(name string, info *version.StaleBinaryInfo) *CheckResult {
	if info.Error != nil {
		return &CheckResult{
			Name:    name,
			Status:  StatusOK,
			Message: "Cannot determine daemon process commit",
			Details: []string{info.Error.Error()},
		}
	}
	if info.Skipped {
		return &CheckResult{
			Name:    name,
			Status:  StatusOK,
			Message: "Daemon staleness check skipped",
			Details: []string{info.SkipReason},
		}
	}
	if info.IsStale {
		return &CheckResult{
			Name:    name,
			Status:  StatusWarning,
			Message: info.Describe("Daemon process"),
			FixHint: "Run 'gt daemon stop && gt daemon start', or 'gt doctor --fix'",
		}
	}
	return &CheckResult{
		Name:    name,
		Status:  StatusOK,
		Message: fmt.Sprintf("Daemon process is up to date (%s)", version.ShortCommit(info.BinaryCommit)),
	}
}

// Fix restarts the daemon so it picks up an already-installed newer binary.
// It does NOT rebuild anything — StaleBinaryCheck / the rebuild-gt plugin own
// that. It only restarts a process that is demonstrably running an old
// commit while a newer one may already be on disk.
//
// hq-wic90 grants standing authorization for this class of restart.
// StopDaemon's graceful-shutdown wait (internal/daemon/daemon.go) is what
// keeps this from corrupting in-flight Dolt state; restarting the daemon
// process does not touch polecat/agent tmux sessions, which the daemon only
// schedules rather than embeds.
func (c *DaemonBinaryStaleCheck) Fix(ctx *CheckContext) error {
	if ctx.NoStart {
		return ErrSkippedNoStart
	}

	running, oldPID, err := daemon.IsRunning(ctx.TownRoot)
	if err != nil {
		return fmt.Errorf("checking daemon status: %w", err)
	}
	if !running {
		// Something else (another doctor --fix, a manual gt daemon stop)
		// already stopped it between Run() and Fix() -- nothing left to do,
		// not a failure.
		return nil
	}

	if err := daemon.StopDaemon(ctx.TownRoot); err != nil {
		return fmt.Errorf("stopping stale daemon: %w", err)
	}

	restartBegan := time.Now()
	gtPath, err := os.Executable()
	if err != nil {
		return err
	}
	restartCmd := exec.Command(gtPath, "daemon", "run")
	restartCmd.Dir = ctx.TownRoot
	restartCmd.Stdin = nil
	restartCmd.Stdout = nil
	restartCmd.Stderr = nil
	util.SetDetachedProcessGroup(restartCmd)
	if err := restartCmd.Start(); err != nil {
		return fmt.Errorf("restarting daemon: %w", err)
	}

	// Poll for the new process to come up, mirroring runDaemonStart's own
	// startup budget (up to 3s). Primarily distinguished by a different PID;
	// StopDaemon already blocks until the old process is confirmed dead
	// before we get here, so this is a narrow window, but a StartedAt-after
	// check is a cheap fallback in the unlikely event the OS recycles oldPID
	// for the new process (e.g. a low pid_max container).
	var newPID int
	var up bool
	for range 30 {
		time.Sleep(100 * time.Millisecond)
		running, pid, err := daemon.IsRunning(ctx.TownRoot)
		if err != nil || !running {
			continue
		}
		if pid != oldPID {
			newPID = pid
			up = true
			break
		}
		if state, err := daemon.LoadState(ctx.TownRoot); err == nil && state.StartedAt.After(restartBegan) {
			newPID = pid
			up = true
			break
		}
	}
	if !up {
		return fmt.Errorf("daemon did not come back up after restart (check 'gt daemon logs')")
	}

	// Verify the restarted process actually picked up a fresher binary
	// rather than assuming the restart fixed anything — gt-if5q's
	// acceptance criterion is "verified rather than assumed".
	state, err := daemon.LoadState(ctx.TownRoot)
	if err != nil {
		return fmt.Errorf("daemon restarted (PID %d) but state unreadable: %w", newPID, err)
	}
	repoRoot, err := version.GetRepoRoot()
	if err != nil {
		// Can't verify outside a dev environment; the restart itself succeeded.
		return nil
	}
	if info := version.CheckStaleBinaryForCommit(repoRoot, state.BinaryCommit); info.Error == nil && !info.Skipped && info.IsStale {
		return fmt.Errorf("daemon restarted (PID %d) but is still running a stale commit (%s) — the on-disk binary needs rebuilding first ('gt install')",
			newPID, version.ShortCommit(state.BinaryCommit))
	}

	return nil
}
