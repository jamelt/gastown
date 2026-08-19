package doctor

import (
	"errors"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/version"
)

// TestDaemonStaleResult covers the pure info -> CheckResult mapping. Run()
// itself is not fully unit-tested here because it depends on a live daemon
// process and version.GetRepoRoot() (env/git driven); the testable logic
// lives in daemonStaleResult.
func TestDaemonStaleResult(t *testing.T) {
	const name = "daemon-binary-stale"

	tests := []struct {
		name        string
		info        *version.StaleBinaryInfo
		wantStatus  CheckStatus
		wantMessage string
		wantDetail  string
		wantFixHint string
	}{
		{
			name:        "error -> OK cannot determine",
			info:        &version.StaleBinaryInfo{Error: errors.New("cannot determine binary commit")},
			wantStatus:  StatusOK,
			wantMessage: "Cannot determine daemon process commit",
			wantDetail:  "cannot determine binary commit",
		},
		{
			name:        "skipped -> OK with skip reason",
			info:        &version.StaleBinaryInfo{Skipped: true, SkipReason: "no build-branch ref found"},
			wantStatus:  StatusOK,
			wantMessage: "Daemon staleness check skipped",
			wantDetail:  "no build-branch ref found",
		},
		{
			// The scenario this whole check exists for: the merged fix IS a
			// daemon fix, so the daemon process itself (not just the CLI
			// invocation of gt doctor) has fallen behind main (gt-if5q).
			name: "stale daemon commit -> Warning + restart fix hint",
			info: &version.StaleBinaryInfo{
				IsStale: true, IsForward: true, OnMainBranch: true,
				CommitsBehind: 1, CompareRef: "main",
				BinaryCommit: "abc1234567890", RepoCommit: "def4567890123",
			},
			wantStatus:  StatusWarning,
			wantMessage: "Daemon process is 1 commits behind main (built from abc123456789, main at def456789012)",
			wantFixHint: "gt daemon stop && gt daemon start",
		},
		{
			name:        "fresh -> OK up to date",
			info:        &version.StaleBinaryInfo{BinaryCommit: "abc1234567890"},
			wantStatus:  StatusOK,
			wantMessage: "Daemon process is up to date (abc123456789)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := daemonStaleResult(name, tt.info)
			if got.Name != name {
				t.Errorf("Name = %q, want %q", got.Name, name)
			}
			if got.Status != tt.wantStatus {
				t.Errorf("Status = %v, want %v", got.Status, tt.wantStatus)
			}
			if got.Message != tt.wantMessage {
				t.Errorf("Message = %q, want %q", got.Message, tt.wantMessage)
			}
			if tt.wantDetail != "" {
				if len(got.Details) == 0 || !strings.Contains(got.Details[0], tt.wantDetail) {
					t.Errorf("Details = %v, want one containing %q", got.Details, tt.wantDetail)
				}
			}
			if tt.wantFixHint != "" && !strings.Contains(got.FixHint, tt.wantFixHint) {
				t.Errorf("FixHint = %q, want substring %q", got.FixHint, tt.wantFixHint)
			}
			if tt.wantFixHint == "" && got.FixHint != "" {
				t.Errorf("unexpected FixHint %q", got.FixHint)
			}
		})
	}
}

// TestDaemonBinaryStaleCheck_Run_DaemonNotRunning verifies Run() reports OK
// without touching git/env when there's no daemon process to check.
func TestDaemonBinaryStaleCheck_Run_DaemonNotRunning(t *testing.T) {
	ctx := &CheckContext{TownRoot: t.TempDir()}
	c := NewDaemonBinaryStaleCheck()

	got := c.Run(ctx)
	if got.Status != StatusOK {
		t.Errorf("Status = %v, want %v", got.Status, StatusOK)
	}
	if got.Message != "Daemon is not running" {
		t.Errorf("Message = %q, want %q", got.Message, "Daemon is not running")
	}
}
