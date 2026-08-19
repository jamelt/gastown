package daemon

import (
	"os"
	"path/filepath"
)

// RequestSchedulerWake asks the daemon to promptly process scheduled work.
// The marker is coalesced and durable; Beads remains the queue authority.
func RequestSchedulerWake(townRoot string) {
	runtimeDir := filepath.Join(townRoot, ".runtime")
	if os.MkdirAll(runtimeDir, 0o755) == nil {
		_ = os.WriteFile(filepath.Join(runtimeDir, "scheduler-wake"), nil, 0o644)
	}
}

func schedulerWakeRequested(townRoot string) bool {
	_, err := os.Stat(filepath.Join(townRoot, ".runtime", "scheduler-wake"))
	return err == nil
}

func claimSchedulerWake(townRoot string) (string, bool) {
	path := filepath.Join(townRoot, ".runtime", "scheduler-wake")
	claim := path + ".claimed"
	if err := os.Rename(path, claim); err != nil {
		return "", false
	}
	return claim, true
}

func clearSchedulerWake(claim string) { _ = os.Remove(claim) }
