package daemon

import (
	"bytes"
	"context"
	"os/exec"
	"time"
)

const (
	defaultFeederDogInterval = 5 * time.Minute
	// feederDogTimeout is the maximum time allowed for a single feed cycle.
	feederDogTimeout = 2 * time.Minute
)

// FeederDogConfig holds configuration for the feeder_dog patrol.
type FeederDogConfig struct {
	// Enabled controls whether the feeder dog runs.
	Enabled bool `json:"enabled"`

	// IntervalStr is how often to run, as a string (e.g., "5m").
	IntervalStr string `json:"interval,omitempty"`
}

// feederDogInterval returns the configured interval, or the default (5m).
func feederDogInterval(config *DaemonPatrolConfig) time.Duration {
	if config != nil && config.Patrols != nil && config.Patrols.FeederDog != nil {
		if config.Patrols.FeederDog.IntervalStr != "" {
			if d, err := time.ParseDuration(config.Patrols.FeederDog.IntervalStr); err == nil && d > 0 {
				return d
			}
		}
	}
	return defaultFeederDogInterval
}

// runFeederDog executes one feed cycle by shelling out to `gt scheduler feed`.
// The daemon is a thin ticker — `gt scheduler feed` handles surveying ready
// beads across rigs, filtering out ineligible ones, and scheduling eligible
// work up to free capacity.
//
// This follows the daemon's "dumb scheduler" principle: the daemon schedules,
// existing commands do the work. Disabled by default (gt-j3xq: start
// conservative) — enable via patrols.feeder_dog.enabled once the town's
// scheduler is in deferred-dispatch mode.
func (d *Daemon) runFeederDog() {
	if !d.isPatrolActive("feeder_dog") {
		return
	}

	d.logger.Printf("feeder_dog: starting feed cycle")

	ctx, cancel := context.WithTimeout(d.ctx, feederDogTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, d.gtPath, "scheduler", "feed", "--json") //nolint:gosec // G204: gtPath resolved at daemon init
	cmd.Dir = d.config.TownRoot

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Non-fatal: a failed feed cycle shouldn't crash the daemon. The next
		// tick tries again.
		stderrStr := stderr.String()
		if stderrStr != "" {
			d.logger.Printf("feeder_dog: feed cycle failed (non-fatal): %v: %s", err, stderrStr)
		} else {
			d.logger.Printf("feeder_dog: feed cycle failed (non-fatal): %v", err)
		}
		return
	}

	d.logger.Printf("feeder_dog: feed cycle result: %s", stdout.String())
}
