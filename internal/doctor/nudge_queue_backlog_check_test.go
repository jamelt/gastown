package doctor

import (
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
