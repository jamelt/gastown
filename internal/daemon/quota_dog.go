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
			if stderrStr != "" {
				d.logger.Printf("quota_dog: %s failed (non-fatal): %v: %s", action, err, stderrStr)
			} else {
				d.logger.Printf("quota_dog: %s failed (non-fatal): %v", action, err)
			}
			continue
		}

		outStr := stdout.String()
		if outStr != "" && outStr != "[]\n" && outStr != "[]" {
			d.logger.Printf("quota_dog: %s result: %s", action, outStr)
		} else {
			d.logger.Printf("quota_dog: %s found no actionable sessions", action)
		}
	}
}

func quotaDogActions() []string {
	return []string{"rotate", "failover"}
}
