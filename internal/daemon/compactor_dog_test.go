package daemon

import (
	"errors"
	"testing"

	"github.com/steveyegge/gastown/internal/doltserver"
)

// TestCompactorPushSafe pins the force-push safety gate against the defect
// class from gt-lliz: compactor_dog previously fail-opened (treated
// unverified/erroring lineage as "not diverged") due to querying a
// nonexistent column and the wrong remote-tracking ref name. It must instead
// fail closed on every non-shared, non-no-remote state, and on lineage
// inspection errors.
func TestCompactorPushSafe(t *testing.T) {
	cases := []struct {
		name    string
		lineage doltserver.LineageReport
		err     error
		want    bool
	}{
		{"no remote configured", doltserver.LineageReport{State: doltserver.LineageNoRemote}, nil, true},
		{"shared history", doltserver.LineageReport{State: doltserver.LineageShared}, nil, true},
		{"diverged history", doltserver.LineageReport{State: doltserver.LineageDiverged}, nil, false},
		{"unverified remote (the fail-open bug)", doltserver.LineageReport{State: doltserver.LineageRemoteUnverified}, nil, false},
		{"lineage inspection error", doltserver.LineageReport{}, errors.New("boom"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			safe, reason := compactorPushSafe(tc.lineage, tc.err)
			if safe != tc.want {
				t.Fatalf("compactorPushSafe(%+v, %v) = (%v, %q), want safe=%v", tc.lineage, tc.err, safe, reason, tc.want)
			}
			if !safe && reason == "" {
				t.Fatal("expected a non-empty reason when unsafe")
			}
		})
	}
}

// TestCompactorPushSafe_MirrorsSafeToPush pins that the compactor uses the
// existing doltserver.LineageReport.SafeToPush() predicate verbatim — the
// same gate internal/doltserver/sync.go uses before pushing — rather than a
// second, parallel lineage check that could drift out of sync with it.
func TestCompactorPushSafe_MirrorsSafeToPush(t *testing.T) {
	for _, state := range []doltserver.LineageState{
		doltserver.LineageNoRemote,
		doltserver.LineageRemoteUnverified,
		doltserver.LineageShared,
		doltserver.LineageDiverged,
	} {
		lineage := doltserver.LineageReport{State: state}
		safe, _ := compactorPushSafe(lineage, nil)
		if safe != lineage.SafeToPush() {
			t.Fatalf("state %s: compactorPushSafe=%v, SafeToPush=%v", state, safe, lineage.SafeToPush())
		}
	}
}
