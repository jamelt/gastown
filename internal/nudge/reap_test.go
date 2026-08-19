package nudge

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReap_RemovesExpiredEntries(t *testing.T) {
	townRoot := t.TempDir()
	session := "gt-witness"

	// Enqueue with an already-past expiry — simulates a nudge whose target
	// session went idle and never took another turn to drain it.
	past := time.Now().Add(-time.Hour)
	if err := Enqueue(townRoot, session, QueuedNudge{
		Sender:    "deacon",
		Message:   "old",
		Timestamp: past,
		ExpiresAt: past.Add(30 * time.Minute), // expired 30m ago
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	// A fresh, non-expired entry must survive.
	if err := Enqueue(townRoot, session, QueuedNudge{
		Sender:  "deacon",
		Message: "fresh",
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	result, err := Reap(townRoot, nil)
	if err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if result.ExpiredRemoved != 1 {
		t.Errorf("ExpiredRemoved = %d, want 1", result.ExpiredRemoved)
	}

	pending, err := Pending(townRoot, session)
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if pending != 1 {
		t.Errorf("Pending after reap = %d, want 1 (fresh entry should survive)", pending)
	}
}

func TestReap_RemovesOrphanedClaims(t *testing.T) {
	townRoot := t.TempDir()
	session := "gt-witness"
	dir := filepath.Join(townRoot, ".runtime", "nudge_queue", session)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	claimPath := filepath.Join(dir, "1-abcd.json.claimed.ff00")
	if err := os.WriteFile(claimPath, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-staleClaimThreshold - time.Minute)
	if err := os.Chtimes(claimPath, old, old); err != nil {
		t.Fatal(err)
	}

	result, err := Reap(townRoot, nil)
	if err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if result.OrphanedClaims != 1 {
		t.Errorf("OrphanedClaims = %d, want 1", result.OrphanedClaims)
	}
	if _, err := os.Stat(claimPath); !os.IsNotExist(err) {
		t.Errorf("expected orphaned claim file to be removed")
	}
}

func TestReap_DeadSessionDirRemovedOnlyWhenEmptyOfContent(t *testing.T) {
	townRoot := t.TempDir()

	// Dead session with only expired residue: directory should be removed.
	dead := "hq-overseer"
	past := time.Now().Add(-time.Hour)
	if err := Enqueue(townRoot, dead, QueuedNudge{
		Sender:    "mayor",
		Message:   "gone",
		Timestamp: past,
		ExpiresAt: past.Add(time.Minute),
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Dead session with a still-valid (non-expired) entry: directory must
	// survive even though the session is gone — the entry might still be
	// read by a manual `gt mail inbox` review before it expires.
	deadButPending := "trader-mayor"
	if err := Enqueue(townRoot, deadButPending, QueuedNudge{
		Sender:  "mayor",
		Message: "still fresh",
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	live := map[string]bool{"gt-witness": true}

	result, err := Reap(townRoot, live)
	if err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if result.DeadSessionDirs != 1 {
		t.Errorf("DeadSessionDirs = %d, want 1", result.DeadSessionDirs)
	}

	if _, err := os.Stat(filepath.Join(townRoot, ".runtime", "nudge_queue", dead)); !os.IsNotExist(err) {
		t.Errorf("expected dead session dir %q to be removed", dead)
	}
	if _, err := os.Stat(filepath.Join(townRoot, ".runtime", "nudge_queue", deadButPending)); err != nil {
		t.Errorf("expected dead-but-pending session dir %q to survive: %v", deadButPending, err)
	}
}

func TestReap_NoQueueDir(t *testing.T) {
	townRoot := t.TempDir()
	result, err := Reap(townRoot, nil)
	if err != nil {
		t.Fatalf("Reap on missing dir should not error: %v", err)
	}
	if result != (ReapResult{}) {
		t.Errorf("expected zero result, got %+v", result)
	}
}
