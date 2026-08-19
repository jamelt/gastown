package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/constants"
)

// staleNudgeThreshold is how old the oldest pending nudge in a session's
// queue must be before NudgeQueueBacklogCheck flags it. Delivery normally
// happens within seconds of the next turn boundary; an entry surviving this
// long means the queue for that session is not draining (gt-hr90).
const staleNudgeThreshold = 10 * time.Minute

// NudgeQueueBacklogCheck detects sessions whose nudge queue
// (.runtime/nudge_queue/<session>/) is not draining. Queued nudges are
// documented as "picked up by the agent's hook at the next turn boundary",
// but that delivery path can silently fail (idle agent never submits a
// prompt, poller not running, etc.) with no other visible symptom — the
// sender believes the nudge was delivered. A CRITICAL escalation sitting
// unread for hours was the motivating case for this check (gt-hr90).
type NudgeQueueBacklogCheck struct {
	BaseCheck
}

// NewNudgeQueueBacklogCheck creates a check for undelivered nudge backlogs.
func NewNudgeQueueBacklogCheck() *NudgeQueueBacklogCheck {
	return &NudgeQueueBacklogCheck{
		BaseCheck: BaseCheck{
			CheckName:        "nudge-queue-backlog",
			CheckDescription: "Detect nudge queues that are not draining",
			CheckCategory:    CategoryInfrastructure,
		},
	}
}

// Run scans every session's nudge queue for entries older than staleNudgeThreshold.
func (c *NudgeQueueBacklogCheck) Run(ctx *CheckContext) *CheckResult {
	queueRoot := filepath.Join(ctx.TownRoot, constants.DirRuntime, "nudge_queue")

	sessions, err := os.ReadDir(queueRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return &CheckResult{
				Name:     c.Name(),
				Status:   StatusOK,
				Message:  "No nudge queues present",
				Category: c.Category(),
			}
		}
		return &CheckResult{
			Name:     c.Name(),
			Status:   StatusWarning,
			Message:  fmt.Sprintf("Failed to read nudge queue directory: %v", err),
			Category: c.Category(),
		}
	}

	now := time.Now()
	var details []string
	staleSessions := 0

	for _, sessionEntry := range sessions {
		if !sessionEntry.IsDir() {
			continue
		}

		sessionDir := filepath.Join(queueRoot, sessionEntry.Name())
		files, err := os.ReadDir(sessionDir)
		if err != nil {
			continue
		}

		var oldest time.Time
		pending := 0
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			pending++
			if oldest.IsZero() || info.ModTime().Before(oldest) {
				oldest = info.ModTime()
			}
		}

		if pending == 0 || oldest.IsZero() {
			continue
		}

		age := now.Sub(oldest)
		if age >= staleNudgeThreshold {
			staleSessions++
			details = append(details, fmt.Sprintf("%s: %d pending, oldest %s old",
				sessionEntry.Name(), pending, age.Round(time.Second)))
		}
	}

	if staleSessions == 0 {
		return &CheckResult{
			Name:     c.Name(),
			Status:   StatusOK,
			Message:  "All nudge queues are draining normally",
			Category: c.Category(),
		}
	}

	sort.Strings(details)

	return &CheckResult{
		Name:     c.Name(),
		Status:   StatusWarning,
		Message:  fmt.Sprintf("%d session(s) have undelivered nudges older than %s", staleSessions, staleNudgeThreshold),
		Details:  details,
		FixHint:  "Check that the session is alive and its nudge poller is running (gt nudge-poller); undelivered escalations may need manual review before they expire",
		Category: c.Category(),
	}
}
