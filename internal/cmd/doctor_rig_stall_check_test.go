package cmd

import (
	"errors"
	"testing"
)

func TestEvaluateRigStalls_ZeroUsableCapacityIsStalled(t *testing.T) {
	readyByRig := map[string]int{"trader": 4}
	capacityFn := func(rigName string) (int, int, []string, error) {
		if rigName != "trader" {
			t.Fatalf("unexpected rig %q", rigName)
		}
		return 0, 23, []string{"NEEDS_MQ_SUBMIT=4", "NEEDS_RECOVERY=19"}, nil
	}

	stalled := evaluateRigStalls(readyByRig, capacityFn)
	if len(stalled) != 1 {
		t.Fatalf("expected 1 stalled rig, got %d: %v", len(stalled), stalled)
	}
	got := stalled[0]
	want := "trader: 4 ready bead(s) queued, 0 of 23 polecat(s) usable (NEEDS_MQ_SUBMIT=4, NEEDS_RECOVERY=19)"
	if got != want {
		t.Fatalf("detail mismatch:\n got:  %s\n want: %s", got, want)
	}
}

func TestEvaluateRigStalls_UsableCapacityIsHealthy(t *testing.T) {
	readyByRig := map[string]int{"gastown": 3}
	capacityFn := func(rigName string) (int, int, []string, error) {
		return 2, 10, nil, nil
	}

	stalled := evaluateRigStalls(readyByRig, capacityFn)
	if len(stalled) != 0 {
		t.Fatalf("expected no stalled rigs, got %v", stalled)
	}
}

func TestEvaluateRigStalls_NoPolecatsDeployedIsSkipped(t *testing.T) {
	// Zero polecats deployed is a different failure mode (owned by
	// polecat-clones-valid), not a capacity stall.
	readyByRig := map[string]int{"newrig": 1}
	capacityFn := func(rigName string) (int, int, []string, error) {
		return 0, 0, nil, nil
	}

	stalled := evaluateRigStalls(readyByRig, capacityFn)
	if len(stalled) != 0 {
		t.Fatalf("expected no stalled rigs for a rig with zero polecats, got %v", stalled)
	}
}

func TestEvaluateRigStalls_CapacityLookupErrorIsReported(t *testing.T) {
	readyByRig := map[string]int{"broken": 1}
	capacityFn := func(rigName string) (int, int, []string, error) {
		return 0, 0, nil, errors.New("boom")
	}

	stalled := evaluateRigStalls(readyByRig, capacityFn)
	if len(stalled) != 1 {
		t.Fatalf("expected 1 stalled entry for capacity lookup error, got %v", stalled)
	}
	if want := "broken: could not assess polecat capacity: boom"; stalled[0] != want {
		t.Fatalf("detail mismatch:\n got:  %s\n want: %s", stalled[0], want)
	}
}

func TestEvaluateRigStalls_MultipleRigsSortedByName(t *testing.T) {
	readyByRig := map[string]int{"zeta": 1, "alpha": 1}
	capacityFn := func(rigName string) (int, int, []string, error) {
		return 0, 5, nil, nil
	}

	stalled := evaluateRigStalls(readyByRig, capacityFn)
	if len(stalled) != 2 {
		t.Fatalf("expected 2 stalled rigs, got %d: %v", len(stalled), stalled)
	}
	if stalled[0][:5] != "alpha" || stalled[1][:4] != "zeta" {
		t.Fatalf("expected alpha before zeta, got %v", stalled)
	}
}
