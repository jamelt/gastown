package daemon

import (
	"bytes"
	"context"
	"os/exec"
	"time"
)

const (
	defaultQuotaDogInterval = 5 * time.Minute
	// quotaDogTimeout is the maximum time allowed for a single rotation cycle.
	quotaDogTimeout = 2 * time.Minute
	// quotaDogFailureEscalationThreshold is the number of consecutive failures
	// of a single quota_dog action before its log level escalates from
	// Warning to Error, surfacing a permanently broken action (e.g. no
	// accounts configured) instead of letting it stay silent in daily
	// "(non-fatal)" log noise.
	quotaDogFailureEscalationThreshold = 3
)

// QuotaDogConfig holds configuration for the quota_dog patrol.
type QuotaDogConfig struct {
	// Enabled controls whether the quota dog runs.
	Enabled bool `json:"enabled"`

	// IntervalStr is how often to run, as a string (e.g., "5m").
	IntervalStr string `json:"interval,omitempty"`
}

// quotaDogInterval returns the configured interval, or the default (5m).
func quotaDogInterval(config *DaemonPatrolConfig) time.Duration {
	if config != nil && config.Patrols != nil && config.Patrols.QuotaDog != nil {
		if config.Patrols.QuotaDog.IntervalStr != "" {
			if d, err := time.ParseDuration(config.Patrols.QuotaDog.IntervalStr); err == nil && d > 0 {
				return d
			}
		}
	}
	return defaultQuotaDogInterval
}

// runQuotaDog executes same-provider account rotation first, then cross-provider
// agent failover for any hard-limited sessions that remain. Rotation errors are
// non-fatal because a town may intentionally have no second Claude account;
// provider fallback must still get its chance.
//
// This follows the daemon's "dumb scheduler" principle: the daemon schedules,
// existing commands do the work. No LLM or molecule needed — pure mechanical rotation.
func (d *Daemon) runQuotaDog() {
	if !d.isPatrolActive("quota_dog") {
		return
	}

	d.logger.Printf("quota_dog: starting quota recovery cycle")

	for _, action := range quotaDogActions() {
		ctx, cancel := context.WithTimeout(d.ctx, quotaDogTimeout)
		cmd := exec.CommandContext(ctx, d.gtPath, "quota", action, "--json") //nolint:gosec // G204: gtPath resolved at daemon init
		cmd.Dir = d.config.TownRoot

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		cancel()

		if err != nil {
			stderrStr := stderr.String()
			if stderrStr == "" {
				stderrStr = err.Error()
			}
			d.recordQuotaDogFailure(action)
			failures := d.getQuotaDogFailures(action)
			if failures >= quotaDogFailureEscalationThreshold {
				d.logger.Printf("quota_dog: Error: %s repeatedly failing (%d consecutive failures, non-fatal): %s", action, failures, stderrStr)
			} else {
				d.logger.Printf("quota_dog: Warning: %s failed (%d consecutive failure(s), non-fatal): %s", action, failures, stderrStr)
			}
			continue
		}
		d.resetQuotaDogFailures(action)

		outStr := stdout.String()
		if outStr != "" && outStr != "[]\n" && outStr != "[]" {
			d.logger.Printf("quota_dog: %s result: %s", action, outStr)
		} else {
			d.logger.Printf("quota_dog: %s found no actionable sessions", action)
		}
	}
}

// recordQuotaDogFailure increments the consecutive failure counter for a quota_dog action.
func (d *Daemon) recordQuotaDogFailure(action string) {
	if d.quotaDogFailures == nil {
		d.quotaDogFailures = make(map[string]int)
	}
	d.quotaDogFailures[action]++
}

// getQuotaDogFailures returns the consecutive failure count for a quota_dog action.
func (d *Daemon) getQuotaDogFailures(action string) int {
	if d.quotaDogFailures == nil {
		return 0
	}
	return d.quotaDogFailures[action]
}

// resetQuotaDogFailures clears the failure counter for a quota_dog action after it succeeds.
func (d *Daemon) resetQuotaDogFailures(action string) {
	if d.quotaDogFailures == nil {
		return
	}
	delete(d.quotaDogFailures, action)
}

func quotaDogActions() []string {
	return []string{"rotate", "failover"}
}
