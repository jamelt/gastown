package daemon

import (
	"errors"
	"testing"

	"github.com/steveyegge/gastown/internal/version"
)

func TestStaleBinaryNeedsRestart(t *testing.T) {
	// A genuinely stale-and-adoptable staleness verdict: the running commit is a
	// forward step behind the build branch.
	staleForward := &version.StaleBinaryInfo{
		IsStale:      true,
		IsForward:    true,
		BinaryCommit: "aaaaaaaaaaaa",
		RepoCommit:   "bbbbbbbbbbbb",
		CompareRef:   "origin/main",
	}

	tests := []struct {
		name        string
		fileChanged bool
		info        *version.StaleBinaryInfo
		want        bool
	}{
		{
			// The load-bearing churn guard: origin/main advancing (via the
			// daemon's own git fetch) does not touch the binary file, so even a
			// stale-forward verdict must not restart while the file is unchanged.
			name:        "unchanged file never restarts even when stale",
			fileChanged: false,
			info:        staleForward,
			want:        false,
		},
		{
			name:        "transient error never restarts",
			fileChanged: true,
			info:        &version.StaleBinaryInfo{Error: errors.New("git failed")},
			want:        false,
		},
		{
			name:        "skipped never restarts",
			fileChanged: true,
			info:        &version.StaleBinaryInfo{Skipped: true, SkipReason: "no build ref"},
			want:        false,
		},
		{
			name:        "not stale never restarts",
			fileChanged: true,
			info:        &version.StaleBinaryInfo{IsStale: false},
			want:        false,
		},
		{
			// Never restart toward a diverged/older head — this is the crash-loop
			// hazard IsForward guards against.
			name:        "stale but not forward never restarts",
			fileChanged: true,
			info:        &version.StaleBinaryInfo{IsStale: true, IsForward: false, RepoCommit: "bbbbbbbbbbbb"},
			want:        false,
		},
		{
			name:        "changed file and stale-forward restarts",
			fileChanged: true,
			info:        staleForward,
			want:        true,
		},
		{
			name:        "nil info never restarts",
			fileChanged: true,
			info:        nil,
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := staleBinaryNeedsRestart(tt.fileChanged, tt.info); got != tt.want {
				t.Errorf("staleBinaryNeedsRestart(%v, %+v) = %v, want %v", tt.fileChanged, tt.info, got, tt.want)
			}
		})
	}
}
