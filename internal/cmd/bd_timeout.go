package cmd

import (
	"time"

	"github.com/steveyegge/gastown/internal/constants"
)

// resolveBdCmdTimeout returns the timeout duration for bd command execution.
func resolveBdCmdTimeout() time.Duration {
	return constants.BdCommandTimeout
}
