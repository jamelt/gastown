package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/constants"
	"github.com/steveyegge/gastown/internal/nudge"
	"github.com/steveyegge/gastown/internal/tmux"
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
//
// Entries already past their ExpiresAt are reported separately from the
// backlog count. An idle session that takes no further turns never calls
// Drain, so expired entries accumulate in the queue directory forever even
// though they were never "undelivered" in any actionable sense — counting
// them as backlog manufactures phantom outages (gt-lp89).
type NudgeQueueBacklogCheck struct {
	FixableCheck
}

// NewNudgeQueueBacklogCheck creates a check for undelivered nudge backlogs.
func NewNudgeQueueBacklogCheck() *NudgeQueueBacklogCheck {
	return &NudgeQueueBacklogCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "nudge-queue-backlog",
				CheckDescription: "Detect nudge queues that are not draining",
				CheckCategory:    CategoryInfrastructure,
			},
		},
	}
}

// Run scans every session's nudge queue for entries older than staleNudgeThreshold,
// excluding entries already past their ExpiresAt (those are cleanup residue,
// not evidence of a stuck queue).
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
	expiredEntries := 0

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
			path := filepath.Join(sessionDir, f.Name())
			if expiresAt, ok := readExpiresAt(path); ok && now.After(expiresAt) {
				expiredEntries++
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

	sort.Strings(details)

	if staleSessions == 0 {
		if expiredEntries == 0 {
			return &CheckResult{
				Name:     c.Name(),
				Status:   StatusOK,
				Message:  "All nudge queues are draining normally",
				Category: c.Category(),
			}
		}
		return &CheckResult{
			Name:     c.Name(),
			Status:   StatusWarning,
			Message:  fmt.Sprintf("No undelivered nudges; %d expired nudge-queue entries awaiting cleanup", expiredEntries),
			FixHint:  "Run 'gt doctor --fix' to reap expired nudge-queue entries",
			Category: c.Category(),
		}
	}

	message := fmt.Sprintf("%d session(s) have undelivered nudges older than %s", staleSessions, staleNudgeThreshold)
	if expiredEntries > 0 {
		message += fmt.Sprintf(" (plus %d expired entries awaiting cleanup)", expiredEntries)
	}

	return &CheckResult{
		Name:     c.Name(),
		Status:   StatusWarning,
		Message:  message,
		Details:  details,
		FixHint:  "Check that the session is alive and its nudge poller is running (gt nudge-poller); undelivered escalations may need manual review before they expire. Run 'gt doctor --fix' to reap expired entries.",
		Category: c.Category(),
	}
}

// Fix reaps expired nudge-queue entries, orphaned claim files, and queue
// directories for sessions that no longer exist. It never removes entries
// that are still within their TTL, so a genuine backlog (poller down, dead
// session with a live queue) survives the fix and stays flagged.
func (c *NudgeQueueBacklogCheck) Fix(ctx *CheckContext) error {
	live := map[string]bool{}
	if sessions, err := tmux.NewTmux().ListSessions(); err == nil {
		for _, s := range sessions {
			live[s] = true
		}
	}
	_, err := nudge.Reap(ctx.TownRoot, live)
	return err
}

// readExpiresAt reads a queued nudge's expires_at field without pulling in
// the full QueuedNudge struct's decode cost for fields we don't need here.
func readExpiresAt(path string) (time.Time, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, false
	}
	var n struct {
		ExpiresAt time.Time `json:"expires_at"`
	}
	if json.Unmarshal(data, &n) != nil || n.ExpiresAt.IsZero() {
		return time.Time{}, false
	}
	return n.ExpiresAt, true
}
