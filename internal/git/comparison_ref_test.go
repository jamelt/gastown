package git

import (
	"reflect"
	"testing"
)

// gt-mv6f: a bare target ref must resolve against the REQUESTED remote first.
// Preferring upstream/ meant that in a fork-based town — where work merges to the
// fork and the parent never receives it — merged commits compared as unpreserved
// forever, pinning polecats at NEEDS_MQ_SUBMIT / NEEDS_RECOVERY.
func TestComparisonRefCandidatesPrefersRequestedRemote(t *testing.T) {
	tests := []struct {
		name   string
		ref    string
		remote string
		want   []string
	}{
		{
			name: "bare ref prefers requested remote, keeps upstream as fallback",
			ref:  "main", remote: "origin",
			want: []string{"origin/main", "upstream/main", "main"},
		},
		{
			name: "explicitly qualified upstream ref is respected",
			ref:  "upstream/main", remote: "origin",
			want: []string{"upstream/main"},
		},
		{
			name: "ref already qualified with the requested remote is left alone",
			ref:  "origin/main", remote: "origin",
			want: []string{"origin/main"},
		},
		{
			name: "fully qualified refs pass through",
			ref:  "refs/heads/main", remote: "origin",
			want: []string{"refs/heads/main"},
		},
		{
			name: "when upstream IS the requested remote there is no duplicate prefix pass",
			ref:  "main", remote: "upstream",
			want: []string{"upstream/main", "main"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := comparisonRefCandidates(tt.ref, tt.remote)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("comparisonRefCandidates(%q, %q) = %v, want %v", tt.ref, tt.remote, got, tt.want)
			}
		})
	}
}

// The invariant that matters: whatever the ordering, the requested remote must be
// consulted before any other remote. This fails if someone reorders the slice.
func TestRequestedRemotePrecedesUpstream(t *testing.T) {
	got := comparisonRefCandidates("main", "origin")
	originAt, upstreamAt := -1, -1
	for i, c := range got {
		if c == "origin/main" && originAt < 0 {
			originAt = i
		}
		if c == "upstream/main" && upstreamAt < 0 {
			upstreamAt = i
		}
	}
	if originAt < 0 {
		t.Fatalf("requested remote ref absent from candidates: %v", got)
	}
	if upstreamAt >= 0 && upstreamAt < originAt {
		t.Fatalf("upstream/main (index %d) precedes origin/main (index %d): %v", upstreamAt, originAt, got)
	}
}
