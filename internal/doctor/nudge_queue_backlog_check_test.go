package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeNudgeFile(t *testing.T, dir, name string, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(`{"sender":"test","message":"hi"}`), 0644); err != nil {
		t.Fatal(err)
	}
	modTime := time.Now().Add(-age)
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
}

// writeExpiredNudgeFile writes a queued nudge whose expires_at has already
// passed, as if its target session went idle before it could be drained.
func writeExpiredNudgeFile(t *testing.T, dir, name string, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	expiresAt := time.Now().Add(-time.Minute).Format(time.RFC3339Nano)
	body := fmt.Sprintf(`{"sender":"test","message":"hi","expires_at":%q}`, expiresAt)
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	modTime := time.Now().Add(-age)
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
}

func TestNudgeQueueBacklogCheck_NoQueueDir(t *testing.T) {
	tmpDir := t.TempDir()

	check := NewNudgeQueueBacklogCheck()
	result := check.Run(&CheckContext{TownRoot: tmpDir})

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK when no queue dir exists, got %v: %s", result.Status, result.Message)
	}
}

func TestNudgeQueueBacklogCheck_FreshNudges(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, ".runtime", "nudge_queue", "hq-mayor")
	writeNudgeFile(t, sessionDir, "1-abcd.json", 30*time.Second)

	check := NewNudgeQueueBacklogCheck()
	result := check.Run(&CheckContext{TownRoot: tmpDir})

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK for fresh nudges, got %v: %s", result.Status, result.Message)
	}
}

func TestNudgeQueueBacklogCheck_StaleNudges(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, ".runtime", "nudge_queue", "hq-mayor")
	writeNudgeFile(t, sessionDir, "1-abcd.json", 25*time.Minute)

	check := NewNudgeQueueBacklogCheck()
	result := check.Run(&CheckContext{TownRoot: tmpDir})

	if result.Status != StatusWarning {
		t.Errorf("expected StatusWarning for stale nudges, got %v: %s", result.Status, result.Message)
	}
	if len(result.Details) != 1 {
		t.Errorf("expected 1 detail line, got %d: %v", len(result.Details), result.Details)
	}
}

func TestNudgeQueueBacklogCheck_IgnoresClaimedFiles(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, ".runtime", "nudge_queue", "hq-mayor")
	// A .claimed file (in-flight drain) should not count as pending backlog.
	writeNudgeFile(t, sessionDir, "1-abcd.json.claimed.ff00", 25*time.Minute)

	check := NewNudgeQueueBacklogCheck()
	result := check.Run(&CheckContext{TownRoot: tmpDir})

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK when only claimed files present, got %v: %s", result.Status, result.Message)
	}
}

// TestNudgeQueueBacklogCheck_ExpiredEntriesAreNotBacklog is the regression
// test for gt-lp89: an idle session that never takes another turn never
// calls Drain, so expired entries accumulate in its queue directory forever.
// Those entries were never "undelivered" in any actionable sense — the doctor
// check must not count them toward the backlog warning, or it manufactures a
// phantom outage every time it runs.
func TestNudgeQueueBacklogCheck_ExpiredEntriesAreNotBacklog(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, ".runtime", "nudge_queue", "gt-witness")
	writeExpiredNudgeFile(t, sessionDir, "1-abcd.json", 8*time.Hour)

	check := NewNudgeQueueBacklogCheck()
	result := check.Run(&CheckContext{TownRoot: tmpDir})

	if result.Status != StatusWarning {
		t.Fatalf("expected StatusWarning (cleanup needed) for expired-only residue, got %v: %s", result.Status, result.Message)
	}
	if len(result.Details) != 0 {
		t.Errorf("expired-only residue should not produce backlog detail lines, got %v", result.Details)
	}
}

// TestNudgeQueueBacklogCheck_ExpiredResidueNotedAlongsideRealBacklog checks
// that expired residue is surfaced without being conflated with a genuine
// (non-expired) backlog in the same or another session.
func TestNudgeQueueBacklogCheck_ExpiredResidueNotedAlongsideRealBacklog(t *testing.T) {
	tmpDir := t.TempDir()
	staleDir := filepath.Join(tmpDir, ".runtime", "nudge_queue", "hq-mayor")
	writeNudgeFile(t, staleDir, "1-abcd.json", 25*time.Minute)

	expiredDir := filepath.Join(tmpDir, ".runtime", "nudge_queue", "gt-witness")
	writeExpiredNudgeFile(t, expiredDir, "1-abcd.json", 8*time.Hour)

	check := NewNudgeQueueBacklogCheck()
	result := check.Run(&CheckContext{TownRoot: tmpDir})

	if result.Status != StatusWarning {
		t.Fatalf("expected StatusWarning, got %v: %s", result.Status, result.Message)
	}
	if len(result.Details) != 1 {
		t.Errorf("expected exactly 1 backlog detail (the non-expired session), got %v", result.Details)
	}
}

func TestNudgeQueueBacklogCheck_Fix(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, ".runtime", "nudge_queue", "gt-witness")
	writeExpiredNudgeFile(t, sessionDir, "1-abcd.json", 8*time.Hour)

	check := NewNudgeQueueBacklogCheck()
	if !check.CanFix() {
		t.Fatal("expected NudgeQueueBacklogCheck to be fixable")
	}

	if err := check.Fix(&CheckContext{TownRoot: tmpDir}); err != nil {
		t.Fatalf("Fix: %v", err)
	}

	result := check.Run(&CheckContext{TownRoot: tmpDir})
	if result.Status != StatusOK {
		t.Errorf("expected StatusOK after fix reaps expired entry, got %v: %s", result.Status, result.Message)
	}
}
