package daemon

import (
	"testing"
	"time"
)

// TestDoltSQLTimeout_Unbounded verifies the periodic-patrol call path (zero
// deadline) is unaffected by the shutdown-budget change: every dolt sql call
// still gets the full doltPushTimeout.
func TestDoltSQLTimeout_Unbounded(t *testing.T) {
	got := doltSQLTimeout(time.Time{})
	if got != doltPushTimeout {
		t.Errorf("doltSQLTimeout(zero) = %v, want %v", got, doltPushTimeout)
	}
}

// TestDoltSQLTimeout_CappedByDeadline is the core fix for gt-if5q's
// shutdown-safety gap: a single dolt sql call must not be able to consume a
// full fresh doltPushTimeout when an overall shutdown deadline is closer than
// that -- otherwise one slow/unreachable remote can blow the entire
// gracefulShutdownTimeout budget on its own.
func TestDoltSQLTimeout_CappedByDeadline(t *testing.T) {
	deadline := time.Now().Add(3 * time.Second)
	got := doltSQLTimeout(deadline)
	if got > 3*time.Second || got <= 0 {
		t.Errorf("doltSQLTimeout(deadline in 3s) = %v, want a small positive duration <= 3s", got)
	}
}

// TestDoltSQLTimeout_DoesNotExceedDefaultWhenDeadlineFar verifies a distant
// deadline doesn't inflate the per-call timeout beyond doltPushTimeout.
func TestDoltSQLTimeout_DoesNotExceedDefaultWhenDeadlineFar(t *testing.T) {
	deadline := time.Now().Add(time.Hour)
	got := doltSQLTimeout(deadline)
	if got != doltPushTimeout {
		t.Errorf("doltSQLTimeout(far deadline) = %v, want %v (capped at doltPushTimeout)", got, doltPushTimeout)
	}
}

// TestDoltSQLTimeout_PastDeadlineIsNegative documents that an already-passed
// deadline yields a non-positive duration. context.WithTimeout treats that as
// an already-expired context, so the subprocess never starts (the caller's
// loop should also check the deadline before starting a new database, but
// this is the last line of defense for a deadline that ticks over mid-call).
func TestDoltSQLTimeout_PastDeadlineIsNegative(t *testing.T) {
	deadline := time.Now().Add(-1 * time.Second)
	got := doltSQLTimeout(deadline)
	if got > 0 {
		t.Errorf("doltSQLTimeout(past deadline) = %v, want <= 0", got)
	}
}
