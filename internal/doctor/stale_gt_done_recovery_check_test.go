package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStaleGtDoneRecoveryCheck_NoRecords(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := &CheckContext{TownRoot: tmpDir}

	check := NewStaleGtDoneRecoveryCheck()
	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK for no records, got %s", result.Status)
	}
	if result.Message != "No gt-done-recovery records found" {
		t.Errorf("unexpected message: %s", result.Message)
	}
}

func TestStaleGtDoneRecoveryCheck_MissingDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := &CheckContext{TownRoot: tmpDir}

	check := NewStaleGtDoneRecoveryCheck()
	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK for missing directory, got %s", result.Status)
	}
}

func TestStaleGtDoneRecoveryCheck_NewRecords(t *testing.T) {
	tmpDir := t.TempDir()
	recoveryDir := filepath.Join(tmpDir, ".runtime", "gt-done-recovery")
	if err := os.MkdirAll(recoveryDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create a recent recovery record
	recordData := map[string]interface{}{
		"branch":      "polecat/test/gt-abc+def",
		"reason":      "detached HEAD",
		"total_paths": 42,
		"timestamp":   time.Now().Format(time.RFC3339),
	}
	recordJSON, _ := json.Marshal(recordData)
	recordPath := filepath.Join(recoveryDir, "recovery-2026-08-19-153000.json")
	if err := os.WriteFile(recordPath, recordJSON, 0644); err != nil {
		t.Fatalf("write record: %v", err)
	}

	ctx := &CheckContext{TownRoot: tmpDir}
	check := NewStaleGtDoneRecoveryCheck()
	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK for recent records, got %s", result.Status)
	}
	if result.Message != "No stale gt-done-recovery records found" {
		t.Errorf("unexpected message: %s", result.Message)
	}
}

func TestStaleGtDoneRecoveryCheck_OldRecords(t *testing.T) {
	tmpDir := t.TempDir()
	recoveryDir := filepath.Join(tmpDir, ".runtime", "gt-done-recovery")
	if err := os.MkdirAll(recoveryDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create an old recovery record (10 days old)
	oldTime := time.Now().Add(-10 * 24 * time.Hour)
	recordData := map[string]interface{}{
		"branch":      "polecat/test/gt-abc+def",
		"reason":      "protected branch",
		"total_paths": 100,
	}
	recordJSON, _ := json.Marshal(recordData)
	recordPath := filepath.Join(recoveryDir, "recovery-old.json")
	if err := os.WriteFile(recordPath, recordJSON, 0644); err != nil {
		t.Fatalf("write record: %v", err)
	}
	// Set old modification time
	if err := os.Chtimes(recordPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	ctx := &CheckContext{TownRoot: tmpDir}
	check := NewStaleGtDoneRecoveryCheck()
	result := check.Run(ctx)

	if result.Status != StatusWarning {
		t.Errorf("expected StatusWarning for old records, got %s", result.Status)
	}
	if len(check.staleRecords) != 1 {
		t.Errorf("expected 1 stale record, got %d", len(check.staleRecords))
	}
}

func TestStaleGtDoneRecoveryCheck_Fix(t *testing.T) {
	tmpDir := t.TempDir()
	recoveryDir := filepath.Join(tmpDir, ".runtime", "gt-done-recovery")
	if err := os.MkdirAll(recoveryDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create an old recovery record
	oldTime := time.Now().Add(-10 * 24 * time.Hour)
	recordData := map[string]interface{}{"branch": "test"}
	recordJSON, _ := json.Marshal(recordData)
	recordPath := filepath.Join(recoveryDir, "recovery-old.json")
	if err := os.WriteFile(recordPath, recordJSON, 0644); err != nil {
		t.Fatalf("write record: %v", err)
	}
	if err := os.Chtimes(recordPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	ctx := &CheckContext{TownRoot: tmpDir}
	check := NewStaleGtDoneRecoveryCheck()
	check.Run(ctx) // Populate staleRecords

	// Fix should remove the old record
	if err := check.Fix(ctx); err != nil {
		t.Errorf("Fix failed: %v", err)
	}

	// Verify file was removed
	if _, err := os.Stat(recordPath); !os.IsNotExist(err) {
		t.Errorf("expected old record to be removed, but it still exists")
	}
}

func TestStaleGtDoneRecoveryCheck_MixedRecords(t *testing.T) {
	tmpDir := t.TempDir()
	recoveryDir := filepath.Join(tmpDir, ".runtime", "gt-done-recovery")
	if err := os.MkdirAll(recoveryDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create a recent record
	recentPath := filepath.Join(recoveryDir, "recent.json")
	if err := os.WriteFile(recentPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("write recent: %v", err)
	}

	// Create an old record
	oldTime := time.Now().Add(-10 * 24 * time.Hour)
	oldPath := filepath.Join(recoveryDir, "old.json")
	if err := os.WriteFile(oldPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("write old: %v", err)
	}
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	ctx := &CheckContext{TownRoot: tmpDir}
	check := NewStaleGtDoneRecoveryCheck()
	result := check.Run(ctx)

	if result.Status != StatusWarning {
		t.Errorf("expected StatusWarning, got %s", result.Status)
	}
	if len(check.staleRecords) != 1 {
		t.Errorf("expected 1 stale record, got %d", len(check.staleRecords))
	}

	// Fix should remove only the old record
	if err := check.Fix(ctx); err != nil {
		t.Errorf("Fix failed: %v", err)
	}

	// Verify recent record still exists
	if _, err := os.Stat(recentPath); os.IsNotExist(err) {
		t.Errorf("recent record was incorrectly removed")
	}

	// Verify old record was removed
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old record was not removed")
	}
}
