package convoy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/lock"
	"github.com/steveyegge/gastown/internal/util"
)

// completionClaimLockTimeout bounds how long ClaimCompletionNotification
// waits to acquire the per-convoy claim lock before falling back to an
// unlocked best-effort claim. This must never block indefinitely:
// ClaimCompletionNotification is called from the refinery's merge hot path,
// and a stuck lock holder must not stall the merge queue.
const completionClaimLockTimeout = 5 * time.Second

// ClaimCompletionNotification is the single authoritative idempotency gate
// for convoy-complete notification. It freshly reads the convoy's current
// CompletionNotifiedAt state and, if unset, claims it by persisting a
// timestamp — serialized per convoy ID via a local flock so concurrent
// callers (CLI `gt convoy check`, deacon's periodic sweep, the refinery's
// post-merge check) cannot both observe "not yet notified" and both send.
//
// Every caller that wants to send a convoy-complete notification MUST call
// this first and only notify if claimed is true. This replaces two
// previously-independent, non-atomic claim implementations in
// internal/cmd/convoy.go and internal/refinery/engineer.go.
//
// If the per-convoy lock cannot be acquired within completionClaimLockTimeout
// (e.g. a stuck holder on the same host), this falls back to an unlocked
// read-check-write rather than blocking the caller indefinitely — that
// degrades to the pre-fix race window rather than stalling the merge queue
// or silently losing the notification forever.
func ClaimCompletionNotification(townRoot, convoyID string) (claimed bool, fields *beads.ConvoyFields, err error) {
	lockPath := filepath.Join(townRoot, ".runtime", "convoy-notify-locks", convoyID+".lock")
	if mkErr := os.MkdirAll(filepath.Dir(lockPath), 0755); mkErr == nil {
		if unlock, acquired, lockErr := lock.FlockTryAcquireWithTimeout(lockPath, completionClaimLockTimeout); lockErr == nil && acquired {
			defer unlock()
		}
		// On timeout or lock error, fall through to the unlocked best-effort
		// claim below instead of failing the notification outright.
	}
	return claimUnlocked(townRoot, convoyID)
}

// claimUnlocked performs the fresh read-check-write of CompletionNotifiedAt.
// Called with the per-convoy flock held (the common case) or without it as a
// bounded fallback when the lock could not be acquired in time.
func claimUnlocked(townRoot, convoyID string) (bool, *beads.ConvoyFields, error) {
	beadsDir := filepath.Join(townRoot, ".beads")

	showCmd := beads.Command(beadsDir, beadsDir, beads.ReadOnlyPinned, "show", convoyID, "--json")
	var showOut bytes.Buffer
	showCmd.Stdout = &showOut
	if err := showCmd.Run(); err != nil {
		return false, nil, fmt.Errorf("reading convoy %s: %w", convoyID, err)
	}

	var convoys []struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal(showOut.Bytes(), &convoys); err != nil || len(convoys) == 0 {
		return false, nil, fmt.Errorf("parsing convoy %s: %w", convoyID, err)
	}

	fields := beads.ParseConvoyFields(&beads.Issue{Description: convoys[0].Description})
	if fields == nil {
		fields = &beads.ConvoyFields{}
	}
	if fields.CompletionNotifiedAt != "" {
		return false, fields, nil
	}

	fields.CompletionNotifiedAt = time.Now().UTC().Format(time.RFC3339)
	newDesc := beads.SetConvoyFields(&beads.Issue{Description: convoys[0].Description}, fields)

	updateCmd := beads.Command(beadsDir, beadsDir, beads.MutationPinned, "update", convoyID, "--description="+newDesc)
	if err := updateCmd.Run(); err != nil {
		return false, nil, fmt.Errorf("persisting convoy completion claim for %s: %w", convoyID, err)
	}

	// Best-effort JSONL export mirror so the claim survives a later Dolt
	// rebuild from issues.jsonl. The update above already committed to Dolt,
	// which is authoritative — export failure here does not fail the claim.
	exportCmd := beads.Command(beadsDir, beadsDir, beads.MutationPinned, "export", "-o", filepath.Join(beadsDir, "issues.jsonl"))
	_ = exportCmd.Run()

	return true, fields, nil
}

// NotifyCompletion sends the full convoy-complete notification set for a
// convoy that has already been claimed via ClaimCompletionNotification: mail
// to the convoy's owner/notify addresses, nudges to nudge-watchers, a
// mayor/ mail if not already covered by the above, and — if the town has
// convoy.notify_on_complete enabled — a push into the active Mayor session.
//
// closedBy, if non-empty, is appended as a "Closed by: <closedBy>" line so
// automated closers (e.g. the refinery) are attributed in the notification.
//
// warnf receives non-fatal per-recipient send failures for the caller to log
// in its own style; pass nil to discard them.
func NotifyCompletion(townRoot, convoyID, title, closedBy string, fields *beads.ConvoyFields, warnf func(format string, args ...interface{})) {
	if warnf == nil {
		warnf = func(string, ...interface{}) {}
	}

	body := fmt.Sprintf("Convoy %s has completed.\n\nAll tracked issues are now closed.", convoyID)
	if closedBy != "" {
		body += fmt.Sprintf("\n\nClosed by: %s", closedBy)
	}

	notifiedAddrs := make(map[string]bool)
	for _, addr := range fields.NotificationAddresses() {
		notifiedAddrs[addr] = true
		sendConvoyMail(townRoot, addr, fmt.Sprintf("🚚 Convoy landed: %s", title), body, convoyID, warnf)
	}

	for _, addr := range fields.NudgeNotificationAddresses() {
		sendConvoyNudge(townRoot, addr,
			fmt.Sprintf("🚚 Convoy landed: %s — Convoy %s has completed. All tracked issues are now closed.", title, convoyID),
			convoyID, warnf)
	}

	if !notifiedAddrs["mayor/"] {
		mayorBody := fmt.Sprintf("Convoy %s has completed. All tracked issues are now closed.", convoyID)
		if closedBy != "" {
			mayorBody += fmt.Sprintf("\n\nClosed by: %s", closedBy)
		}
		sendConvoyMail(townRoot, "mayor/", fmt.Sprintf("Convoy complete: %s", title), mayorBody, convoyID, warnf)
	}

	notifyMayorSession(townRoot, convoyID, title, warnf)
}

func convoyNotifyFrom(convoyID string) string {
	return "convoy/" + convoyID
}

func sendConvoyMail(townRoot, addr, subject, body, convoyID string, warnf func(string, ...interface{})) {
	mailCmd := exec.Command("gt", "mail", "send", addr, "-s", subject, "-m", body, "--from", convoyNotifyFrom(convoyID), "--no-notify")
	util.SetDetachedProcessGroup(mailCmd)
	mailCmd.Dir = townRoot
	if err := mailCmd.Run(); err != nil {
		warnf("could not notify %s: %v", addr, err)
	}
}

func sendConvoyNudge(townRoot, addr, message, convoyID string, warnf func(string, ...interface{})) {
	nudgeCmd := exec.Command("gt", "nudge", addr, "-m", message)
	nudgeCmd.Env = append(beads.StripEnvKey(os.Environ(), "GT_ROLE"), "GT_ROLE="+convoyNotifyFrom(convoyID))
	util.SetDetachedProcessGroup(nudgeCmd)
	nudgeCmd.Dir = townRoot
	if err := nudgeCmd.Run(); err != nil {
		warnf("could not nudge %s: %v", addr, err)
	}
}

// notifyMayorSession pushes a convoy completion notification into the active
// Mayor session via nudge, if convoy.notify_on_complete is enabled.
func notifyMayorSession(townRoot, convoyID, title string, warnf func(string, ...interface{})) {
	settingsPath := config.TownSettingsPath(townRoot)
	settings, err := config.LoadOrCreateTownSettings(settingsPath)
	if err != nil {
		return
	}
	if settings.Convoy == nil || !settings.Convoy.NotifyOnComplete {
		return
	}

	message := fmt.Sprintf("🚚 Convoy landed: %s — Convoy %s has completed. All tracked issues are now closed.", title, convoyID)
	sendConvoyNudge(townRoot, "mayor", message, convoyID, warnf)
}
