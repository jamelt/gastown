package daemon

import (
	"os"
	"os/exec"

	"github.com/steveyegge/gastown/internal/util"
	"github.com/steveyegge/gastown/internal/version"
)

// staleBinaryNeedsRestart is the pure decision half of the daemon's binary
// self-heal: given whether the on-disk executable changed since startup and the
// staleness of the running commit, decide whether to restart to adopt it.
//
// The on-disk-changed signal is the load-bearing guard against restart loops.
// version.CheckStaleBinaryForCommit compares the running commit against the
// build-branch tip (normally origin/main), which the daemon's own `git fetch`
// advances WITHOUT rebuilding the on-disk binary — so gating on staleness alone
// would bounce the daemon once per merge, futilely. We only restart when the
// binary FILE was actually replaced (an install/rebuild happened).
//
// The staleness check is a proxy: IsStale && IsForward means the running commit
// is a forward step behind the build branch. Installs build from the build
// branch, so "running is behind the build branch" stands in for "the freshly
// installed on-disk binary is ahead of the one we're running" — it does not
// re-read the on-disk binary's own commit. Transient git/version failures
// surface as Error/Skipped (never IsStale), so they can never trigger a restart.
func staleBinaryNeedsRestart(fileChanged bool, info *version.StaleBinaryInfo) bool {
	if !fileChanged {
		return false
	}
	if info == nil || info.Error != nil || info.Skipped {
		return false
	}
	return info.IsStale && info.IsForward
}

// maybeSelfRestartStaleBinary restarts the daemon when a rebuilt gt binary has
// been installed on disk underneath the running process, so a merged fix goes
// live without a human running `gt doctor --fix` (gt-zpfn). It is called at the
// end of every heartbeat, after the estop/shutdown guards at the top of
// heartbeat, and is a no-op unless the on-disk binary actually changed.
func (d *Daemon) maybeSelfRestartStaleBinary(state *State) {
	// binaryPath == "" means we never resolved our own executable; a zero
	// startup modtime means os.Stat failed at startup so we have no baseline to
	// compare against. In either case we cannot reliably detect a replacement,
	// so don't risk a spurious restart.
	if d.restartRequested || d.binaryPath == "" || d.startupBinaryModTime.IsZero() {
		return
	}

	// Cheap gate: has the on-disk binary been replaced since we started running
	// it? If the file is byte-for-byte what we launched, a restart would just
	// re-exec the same commit — futile. This is what prevents the fetch-driven
	// restart churn (origin/main advancing never touches the binary file).
	// mtime+size is the identity signal: every install path (rm+cp, cp+mv,
	// activate's create+rename) writes a fresh mtime, so a same-mtime same-size
	// replacement — the only theoretical miss — does not occur in practice, and
	// such a miss is fail-safe (a delayed adopt, never a spurious restart).
	fi, err := os.Stat(d.binaryPath)
	if err != nil {
		return // binary path unreadable (e.g. removed) — can't safely restart
	}
	fileChanged := !fi.ModTime().Equal(d.startupBinaryModTime) || fi.Size() != d.startupBinarySize
	if !fileChanged {
		return
	}

	if state.BinaryCommit == "" {
		return // dev build / predates binary-commit tracking
	}
	repoRoot, err := version.GetRepoRoot()
	if err != nil {
		return // not a dev environment; mirrors doctor's daemon-binary-stale check
	}
	info := version.CheckStaleBinaryForCommit(repoRoot, state.BinaryCommit)
	if !staleBinaryNeedsRestart(fileChanged, info) {
		return
	}

	// Re-check shutdown here: the guard at the top of heartbeat can't see a
	// `gt down` that arrived mid-tick, and we must not fight an in-progress
	// shutdown by spawning a fresh daemon it is trying to stop.
	if d.isShutdownInProgress() {
		return
	}

	d.restartRequested = true
	d.logger.Printf("daemon binary replaced on disk; running commit %s is behind %s (%s) — restarting to adopt the new binary",
		version.ShortCommit(info.BinaryCommit), info.CompareRef, version.ShortCommit(info.RepoCommit))
	d.metrics.recordRestart(d.ctx, "daemon-self")

	if err := d.spawnDaemonRestart(); err != nil {
		// Spawn failed: clear the latch so a later heartbeat can retry rather
		// than disabling self-heal until a manual restart.
		d.restartRequested = false
		d.logger.Printf("Warning: failed to spawn daemon self-restart: %v", err)
	}
}

// spawnDaemonRestart launches a detached `gt daemon restart` using this daemon's
// own (newly-installed) on-disk binary. The child stops this daemon (SIGTERM →
// the daemon's signal handler runs a graceful shutdown → the process actually
// exits) and then starts a fresh `gt daemon run`.
//
// We deliberately do NOT syscall.Exec in place: a same-PID re-exec is invisible
// to external StopDaemon callers (gt down/stop/doctor/activate), which poll the
// PID for death and would hang, then SIGKILL a re-initialized, Dolt-active
// daemon. A real stop+start via a detached child keeps the restart observable.
func (d *Daemon) spawnDaemonRestart() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "daemon", "restart")
	cmd.Dir = d.config.TownRoot
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	util.SetDetachedProcessGroup(cmd)
	return cmd.Start()
}
