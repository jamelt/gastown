package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/lock"
	"github.com/steveyegge/gastown/internal/scheduler/capacity"
)

func installFakeBD(t *testing.T, script string) {
	t.Helper()
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir fake bd bin: %v", err)
	}
	fakeBD := filepath.Join(binDir, "bd")
	if err := os.WriteFile(fakeBD, []byte(script), 0755); err != nil {
		t.Fatalf("write fake bd: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func setupSchedulerScanFailureTown(t *testing.T) string {
	t.Helper()
	townRoot := t.TempDir()
	for _, dir := range []string{
		filepath.Join(townRoot, "mayor"),
		filepath.Join(townRoot, ".beads"),
		filepath.Join(townRoot, "rig", ".beads"),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	installFakeBD(t, `#!/bin/sh
case "$BEADS_DIR" in
  */rig/.beads) echo "scan failed" >&2; exit 7 ;;
  *) printf '[]\n'; exit 0 ;;
esac
`)
	return townRoot
}

// setupSchedulerEmptyTown is like setupSchedulerScanFailureTown but its fake
// bd always succeeds with an empty result, so dispatchScheduledWork can run
// past context-assessment into the (default direct-dispatch) early-return
// path instead of failing on a scan error.
func setupSchedulerEmptyTown(t *testing.T) string {
	t.Helper()
	townRoot := t.TempDir()
	for _, dir := range []string{
		filepath.Join(townRoot, "mayor"),
		filepath.Join(townRoot, ".beads"),
		filepath.Join(townRoot, "rig", ".beads"),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	installFakeBD(t, "#!/bin/sh\nprintf '[]\\n'\nexit 0\n")
	return townRoot
}

// startForeignAliveProcess spawns a real, currently-running subprocess and
// returns its PID for tests that need an alive-but-not-us PID to fake a
// lock held by another process. Using our own PID would instead hit the
// lock package's same-PID "we already hold it, refresh" path and silently
// succeed rather than modeling contention.
func startForeignAliveProcess(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("could not start helper subprocess: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd.Process.Pid
}

func TestDispatchScheduledWorkReportsHeldLock(t *testing.T) {
	townRoot := t.TempDir()
	runtimeDir := filepath.Join(townRoot, ".runtime")
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		t.Fatalf("mkdir runtime: %v", err)
	}
	lockFile := filepath.Join(runtimeDir, "scheduler-dispatch.lock")

	held := lock.LockInfo{PID: startForeignAliveProcess(t), AcquiredAt: time.Now(), SessionID: "other-actor"}
	data, err := json.Marshal(held)
	if err != nil {
		t.Fatalf("marshal lock info: %v", err)
	}
	if err := os.WriteFile(lockFile, data, 0644); err != nil {
		t.Fatalf("write lock file: %v", err)
	}

	_, err = dispatchScheduledWork(townRoot, "test", 1, false)
	if err == nil {
		t.Fatal("dispatchScheduledWork succeeded with held scheduler lock")
	}
	if !strings.Contains(err.Error(), "scheduler dispatch already in progress") || !strings.Contains(err.Error(), lockFile) {
		t.Fatalf("error = %q, want explicit held lock reason with path", err.Error())
	}
}

// TestDispatchScheduledWorkReclaimsStaleLock is the direct regression test
// for gt-jpib: a dispatch killed mid-run leaves scheduler-dispatch.lock
// behind with a now-dead owning PID. The next dispatch attempt must reclaim
// it automatically instead of failing with "already in progress" forever.
func TestDispatchScheduledWorkReclaimsStaleLock(t *testing.T) {
	townRoot := setupSchedulerEmptyTown(t)
	runtimeDir := filepath.Join(townRoot, ".runtime")
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		t.Fatalf("mkdir runtime: %v", err)
	}
	lockFile := filepath.Join(runtimeDir, "scheduler-dispatch.lock")

	orphaned := lock.LockInfo{PID: 999999999, AcquiredAt: time.Now().Add(-time.Hour), SessionID: "dead-actor"}
	data, err := json.Marshal(orphaned)
	if err != nil {
		t.Fatalf("marshal lock info: %v", err)
	}
	if err := os.WriteFile(lockFile, data, 0644); err != nil {
		t.Fatalf("write lock file: %v", err)
	}

	if _, err := dispatchScheduledWork(townRoot, "test", 1, false); err != nil {
		t.Fatalf("dispatchScheduledWork with orphaned (dead-PID) lock = %v, want nil (reclaimed)", err)
	}
}

// TestDispatchScheduledWorkDaemonBypassesHeldLock verifies the daemon
// dispatch path (isDaemonDispatch) skips silently on lock contention rather
// than surfacing an error — daemon heartbeats fire regardless of whether a
// manual dispatch happens to be running.
func TestDispatchScheduledWorkDaemonBypassesHeldLock(t *testing.T) {
	townRoot := t.TempDir()
	runtimeDir := filepath.Join(townRoot, ".runtime")
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		t.Fatalf("mkdir runtime: %v", err)
	}
	lockFile := filepath.Join(runtimeDir, "scheduler-dispatch.lock")
	held := lock.LockInfo{PID: startForeignAliveProcess(t), AcquiredAt: time.Now(), SessionID: "other-actor"}
	data, err := json.Marshal(held)
	if err != nil {
		t.Fatalf("marshal lock info: %v", err)
	}
	if err := os.WriteFile(lockFile, data, 0644); err != nil {
		t.Fatalf("write lock file: %v", err)
	}

	t.Setenv("GT_DAEMON", "1")

	n, err := dispatchScheduledWork(townRoot, "test", 1, false)
	if err != nil {
		t.Fatalf("daemon dispatch on held lock = %v, want nil (silent skip)", err)
	}
	if n != 0 {
		t.Fatalf("daemon dispatch on held lock dispatched %d, want 0", n)
	}
}

// TestLoadSchedulerDispatchLockStatus covers the `gt scheduler status`
// lock-reporting helper directly: unlocked, corrupt, and held-and-fresh.
func TestLoadSchedulerDispatchLockStatus(t *testing.T) {
	townRoot := t.TempDir()

	if got := loadSchedulerDispatchLockStatus(townRoot); got != nil {
		t.Fatalf("no lock file: got %+v, want nil", got)
	}

	runtimeDir := filepath.Join(townRoot, ".runtime")
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		t.Fatalf("mkdir runtime: %v", err)
	}
	lockFile := filepath.Join(runtimeDir, "scheduler-dispatch.lock")

	if err := os.WriteFile(lockFile, []byte("not json"), 0644); err != nil {
		t.Fatalf("write corrupt lock file: %v", err)
	}
	if got := loadSchedulerDispatchLockStatus(townRoot); got == nil || got.Error == "" {
		t.Fatalf("corrupt lock file: got %+v, want non-nil with Error set", got)
	}

	held := lock.LockInfo{PID: os.Getpid(), AcquiredAt: time.Now().Add(-90 * time.Second), Hostname: "host-a"}
	data, err := json.Marshal(held)
	if err != nil {
		t.Fatalf("marshal lock info: %v", err)
	}
	if err := os.WriteFile(lockFile, data, 0644); err != nil {
		t.Fatalf("write lock file: %v", err)
	}
	got := loadSchedulerDispatchLockStatus(townRoot)
	if got == nil {
		t.Fatal("held lock: got nil, want non-nil status")
	}
	if got.Error != "" {
		t.Fatalf("held lock: got Error=%q, want empty", got.Error)
	}
	if got.PID != os.Getpid() || got.Hostname != "host-a" {
		t.Fatalf("held lock: got PID=%d Hostname=%q, want PID=%d Hostname=host-a", got.PID, got.Hostname, os.Getpid())
	}
	if got.Stale {
		t.Fatal("held lock: got Stale=true, want false (fresh, alive PID)")
	}
	if got.AgeSeconds < 90 {
		t.Fatalf("held lock: got AgeSeconds=%d, want >= 90", got.AgeSeconds)
	}
}

func TestValidateDryRunDispatchPlanMarksAllInvalidAsValidation(t *testing.T) {
	townRoot := t.TempDir()
	writeJSONFile(t, filepath.Join(townRoot, "mayor", "rigs.json"), &config.RigsConfig{
		Version: config.CurrentRigsVersion,
		Rigs: map[string]config.RigEntry{
			"testrig": {BeadsConfig: &config.BeadsConfig{Prefix: "gt"}},
		},
	})

	plan := validateDryRunDispatchPlan(townRoot, capacity.DispatchPlan{
		ToDispatch: []capacity.PendingBead{{ID: "ctx-1", WorkBeadID: "hq-one", TargetRig: "testrig"}},
		Reason:     "ready",
	})

	if len(plan.ToDispatch) != 0 || plan.Skipped != 1 || plan.Reason != "validation" {
		t.Fatalf("validated plan = %+v, want no dispatch, skipped=1, reason=validation", plan)
	}
}

func TestListAllSlingContextRecordsFailsOnPartialScanFailure(t *testing.T) {
	townRoot := setupSchedulerScanFailureTown(t)

	_, err := listAllSlingContextRecords(townRoot)
	if err == nil {
		t.Fatal("partial sling-context scan failure should fail closed")
	}
	if !strings.Contains(err.Error(), "listing sling contexts") || !strings.Contains(err.Error(), filepath.Join("rig", ".beads")) {
		t.Fatalf("error = %q, want explicit context scan failure", err.Error())
	}
}

func TestAreScheduledFailsClosedOnContextScanFailure(t *testing.T) {
	townRoot := setupSchedulerScanFailureTown(t)
	oldCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(townRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldCWD) })

	got := areScheduled([]string{"gt-one", "gt-two"})
	if !got["gt-one"] || !got["gt-two"] {
		t.Fatalf("areScheduled on scan failure = %+v, want all requested IDs marked scheduled", got)
	}
}

func TestRunSchedulerClearFailsOnContextScanFailure(t *testing.T) {
	townRoot := setupSchedulerScanFailureTown(t)
	oldCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(townRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldCWD) })
	oldClearBead := schedulerClearBead
	schedulerClearBead = ""
	t.Cleanup(func() { schedulerClearBead = oldClearBead })

	err = runSchedulerClear(nil, nil)
	if err == nil {
		t.Fatal("scheduler clear succeeded with incomplete context scan")
	}
	if !strings.Contains(err.Error(), "listing sling contexts") {
		t.Fatalf("error = %q, want sling context scan failure", err.Error())
	}
}
