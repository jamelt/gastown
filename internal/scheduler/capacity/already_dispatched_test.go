package capacity

import (
	"errors"
	"testing"
)

// gt-l8p0: a bead already held by a polecat must NOT be re-dispatched, and must
// NOT be recorded as a dispatch failure. The distinction matters: three
// interruptions would otherwise circuit-break the bead and stop it dispatching
// entirely, which is the failure mode observed in the live fleet.
func TestAlreadyDispatchedIsNotAFailure(t *testing.T) {
	var executed, succeeded int
	var failures []error

	cycle := &DispatchCycle{
		AvailableCapacity: func() (int, error) { return 10, nil },
		QueryPending: func() ([]PendingBead, error) {
			return []PendingBead{
				{ID: "sc-1", WorkBeadID: "gt-already", TargetRig: "gastown"},
				{ID: "sc-2", WorkBeadID: "gt-fresh", TargetRig: "gastown"},
			}, nil
		},
		Validate: func(b PendingBead) error {
			if b.WorkBeadID == "gt-already" {
				return ErrAlreadyDispatched
			}
			return nil
		},
		Execute:   func(PendingBead) error { executed++; return nil },
		OnSuccess: func(PendingBead) error { succeeded++; return nil },
		OnFailure: func(_ PendingBead, err error) { failures = append(failures, err) },
		BatchSize: 10,
	}

	report, err := cycle.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if executed != 1 {
		t.Fatalf("Execute called %d times, want 1 — the already-held bead must not be dispatched again", executed)
	}
	if succeeded != 1 {
		t.Fatalf("OnSuccess called %d times, want 1", succeeded)
	}
	if len(failures) != 1 || !errors.Is(failures[0], ErrAlreadyDispatched) {
		t.Fatalf("OnFailure got %v, want exactly one ErrAlreadyDispatched", failures)
	}
	if report.Dispatched != 1 {
		t.Fatalf("Dispatched = %d, want 1", report.Dispatched)
	}
}

// The sentinel must survive wrapping, since callers match it with errors.Is
// through the Validate boundary.
func TestAlreadyDispatchedUnwraps(t *testing.T) {
	wrapped := errors.Join(errors.New("context"), ErrAlreadyDispatched)
	if !errors.Is(wrapped, ErrAlreadyDispatched) {
		t.Fatal("ErrAlreadyDispatched must be detectable through wrapping")
	}
	if errors.Is(ErrCrossRigPrefix, ErrAlreadyDispatched) {
		t.Fatal("ErrAlreadyDispatched must not alias ErrCrossRigPrefix")
	}
}
