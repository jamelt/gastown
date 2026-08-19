package cmd

import (
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/mail"
)

func TestIsDrainableMessage(t *testing.T) {
	tests := []struct {
		subject   string
		drainable bool
	}{
		// Drainable protocol messages
		{"CRASHED_POLECAT: furiosa", false},
		{"POLECAT_DONE furiosa", true},
		{"POLECAT_STARTED: furiosa", true},
		{"LIFECYCLE:Shutdown furiosa", true},
		{"LIFECYCLE:Restart furiosa", true},
		{"MERGED furiosa", true},
		{"MERGE_READY furiosa", true},
		{"MERGE_FAILED furiosa", true},
		{"SWARM_START", true},
		{"MERGEDFOO", false},
		{"MERGED: customer escalation", false},
		{"POLECAT_DONE_WHATEVER", false},

		// Non-drainable messages (need attention)
		{"HELP: stuck on implementation", false},
		{"🤝 HANDOFF", false},
		{"Status check", false},
		{"Question about deployment", false},
		{"ALERT: something", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.subject, func(t *testing.T) {
			got := isDrainableMessage(tc.subject)
			if got != tc.drainable {
				t.Errorf("isDrainableMessage(%q) = %v, want %v", tc.subject, got, tc.drainable)
			}
		})
	}
}

func TestSelectMailDrainCandidates(t *testing.T) {
	cutoff := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
	old := cutoff.Add(-time.Minute)
	recent := cutoff.Add(time.Nanosecond)
	messages := []*mail.Message{
		{ID: "merged", Subject: "MERGED vault", Timestamp: old},
		{ID: "ready-wisp", Subject: "MERGE_READY vault", Timestamp: old, Wisp: true, Read: true},
		{ID: "recent", Subject: "POLECAT_DONE vault", Timestamp: recent},
		{ID: "help", Subject: "HELP: active", Timestamp: old, Wisp: true, Read: true},
		{ID: "handoff", Subject: "🤝 HANDOFF", Timestamp: old, Wisp: true, Read: true},
		{ID: "fix", Subject: "FIX_NEEDED vault", Timestamp: old, Wisp: true, Read: true},
		{ID: "recovery", Subject: "RECOVERY_NEEDED vault", Timestamp: old, Wisp: true, Read: true},
		{ID: "patrol", Subject: "PATROL BLOCKERS", Timestamp: old, Wisp: true, Read: true},
		{ID: "health", Subject: "Health: witness", Timestamp: old, Wisp: true, Read: true},
		{ID: "rca", Subject: "RCA: drain", Timestamp: old, Wisp: true, Read: true},
		{ID: "lookalike", Subject: "MERGEDFOO", Timestamp: old},
	}

	for _, tt := range []struct {
		name string
		all  bool
		want []string
	}{
		{name: "stale protocol only", want: []string{"merged:protocol", "ready-wisp:wisp+protocol"}},
		{name: "all protocol ages only", all: true, want: []string{"merged:protocol", "ready-wisp:wisp+protocol", "recent:protocol"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			candidates := selectMailDrainCandidates(messages, cutoff, tt.all)
			got := make([]string, 0, len(candidates))
			for _, candidate := range candidates {
				got = append(got, candidate.Message.ID+":"+candidate.Reason)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("candidate count = %d (%v), want %d (%v)", len(got), got, len(tt.want), tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("candidate[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
