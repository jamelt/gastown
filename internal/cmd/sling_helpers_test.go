package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/nudge"
	"github.com/steveyegge/gastown/internal/session"
	"github.com/steveyegge/gastown/internal/tmux"
)

func setupSlingTestRegistry(t *testing.T) {
	t.Helper()
	reg := session.NewPrefixRegistry()
	reg.Register("gt", "gastown")
	reg.Register("bd", "beads")
	reg.Register("mp", "my-project")
	old := session.DefaultRegistry()
	session.SetDefaultRegistry(reg)
	t.Cleanup(func() { session.SetDefaultRegistry(old) })
}

// TestNudgeRefinerySessionName verifies that nudgeRefinery constructs the
// correct tmux session name ({prefix}-refinery) and passes the message.
func TestNudgeRefinerySessionName(t *testing.T) {
	setupSlingTestRegistry(t)
	logPath := filepath.Join(t.TempDir(), "nudge.log")
	t.Setenv("GT_TEST_NUDGE_LOG", logPath)

	tests := []struct {
		name        string
		rigName     string
		message     string
		wantSession string
	}{
		{
			name:        "simple rig name",
			rigName:     "gastown",
			message:     "MERGE_READY received - check inbox for pending work",
			wantSession: "gt-refinery",
		},
		{
			name:        "hyphenated rig name",
			rigName:     "my-project",
			message:     "MERGE_READY received - check inbox for pending work",
			wantSession: "mp-refinery",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Truncate log for each subtest
			if err := os.WriteFile(logPath, nil, 0644); err != nil {
				t.Fatalf("truncate log: %v", err)
			}

			nudgeRefinery(tt.rigName, tt.message)

			logBytes, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatalf("read log: %v", err)
			}
			logContent := string(logBytes)

			// Verify session name
			wantPrefix := "nudge:" + tt.wantSession + ":"
			if !strings.Contains(logContent, wantPrefix) {
				t.Errorf("nudgeRefinery(%q) session = got log %q, want prefix %q",
					tt.rigName, logContent, wantPrefix)
			}

			// Verify message is passed through
			if !strings.Contains(logContent, tt.message) {
				t.Errorf("nudgeRefinery() message not found in log: got %q, want %q",
					logContent, tt.message)
			}
		})
	}
}

func TestNudgeWitnessSessionName(t *testing.T) {
	setupSlingTestRegistry(t)
	logPath := filepath.Join(t.TempDir(), "nudge.log")
	t.Setenv("GT_TEST_NUDGE_LOG", logPath)

	nudgeWitness("gastown", "POLECAT_DONE: test")

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if got, want := string(logBytes), "nudge:gt-witness:POLECAT_DONE: test\n"; got != want {
		t.Fatalf("nudgeWitness() log = %q, want %q", got, want)
	}
}

func TestNudgeWitnessDoesNotEmitEvent(t *testing.T) {
	t.Setenv("GT_TEST_NUDGE_LOG", "")
	townRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(townRoot, "mayor"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(townRoot, "mayor", "town.json"), []byte(`{"name":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(townRoot, "gastown", "polecats", "test")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatal(err)
	}
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	prevEscalate := escalateNudgeFailure
	t.Cleanup(func() { escalateNudgeFailure = prevEscalate })
	escalateNudgeFailure = func(string, string, error) {}

	nudgeWitness("gastown", "POLECAT_DONE: test")

	paths, err := filepath.Glob(filepath.Join(townRoot, "events", "witness", "*.event"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 0 {
		t.Fatalf("witness event files = %v, want none", paths)
	}
}

// TestQueueFallbackNudgeIgnoresOtherErrors verifies that a nudge failure
// unrelated to a stranded composer (e.g. dead session) is returned unchanged
// instead of being queued — queueing only makes sense when the text is known
// to have made it into the composer (gt-ax7a).
func TestQueueFallbackNudgeIgnoresOtherErrors(t *testing.T) {
	townRoot := t.TempDir()
	original := fmt.Errorf("nudge to session %q: session not found", "gt-witness")

	got := queueFallbackNudge(townRoot, "gt-witness", "hello", original)

	if got != original {
		t.Fatalf("queueFallbackNudge() = %v, want unchanged %v", got, original)
	}
	if n, _ := nudge.Pending(townRoot, "gt-witness"); n != 0 {
		t.Fatalf("queueFallbackNudge() queued %d nudges for a non-stranded error, want 0", n)
	}
}

// TestQueueFallbackNudgeWithoutTownRootReturnsOriginal verifies that without
// a resolvable town root (queueing is impossible: nowhere to write the queue
// file), the original stranded-composer error is surfaced rather than
// silently swallowed.
func TestQueueFallbackNudgeWithoutTownRootReturnsOriginal(t *testing.T) {
	original := fmt.Errorf("nudge to session %q: %w", "gt-witness", tmux.ErrSubmitNotVerified)

	got := queueFallbackNudge("", "gt-witness", "hello", original)

	if got != original {
		t.Fatalf("queueFallbackNudge() = %v, want unchanged %v", got, original)
	}
}

// TestQueueFallbackNudgeOnStrandedComposerQueuesMessage verifies the actual
// fix for gt-ax7a: when a nudge fails with ErrSubmitNotVerified (text was
// typed into the composer but a pre-existing draft blocked submission), the
// message is queued for durable delivery instead of being dropped with a
// warning.
func TestQueueFallbackNudgeOnStrandedComposerQueuesMessage(t *testing.T) {
	townRoot := t.TempDir()
	stranded := fmt.Errorf("nudge to session %q: %w", "gt-witness", tmux.ErrSubmitNotVerified)

	got := queueFallbackNudge(townRoot, "gt-witness", "Polecat dispatched - check for work", stranded)

	if got != nil {
		t.Fatalf("queueFallbackNudge() = %v, want nil (queued successfully)", got)
	}
	queued, err := nudge.Drain(townRoot, "gt-witness")
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(queued) != 1 || queued[0].Message != "Polecat dispatched - check for work" {
		t.Fatalf("queued nudges = %+v, want one entry with the stranded message", queued)
	}
}

// TestWakeRigAgentsDoesNotNudgeRefinery verifies that wakeRigAgents only
// nudges the witness, not the refinery. The refinery should only be nudged
// when an MR is actually created (via nudgeRefinery), not at polecat dispatch time.
func TestWakeRigAgentsDoesNotNudgeRefinery(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "nudge.log")
	t.Setenv("GT_TEST_NUDGE_LOG", logPath)

	// wakeRigAgents calls exec.Command("gt", "rig", "boot", ...) and tmux.NudgeSession.
	// The boot command and witness nudge will fail silently (no real rig/tmux).
	// We only care that nudgeRefinery is NOT called (no log entries).
	wakeRigAgents("testrig")

	// Check that no refinery nudge was logged
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		// File doesn't exist = no nudges logged = correct
		return
	}
	if strings.Contains(string(logBytes), "refinery") {
		t.Errorf("wakeRigAgents() should not nudge refinery, but log contains: %s", string(logBytes))
	}
}

// TestNudgeRefineryNoOpWithoutLog verifies that nudgeRefinery doesn't panic
// or error when called without the test log env var and without a real tmux session.
// The tmux NudgeSession call should fail silently.
func TestNudgeRefineryNoOpWithoutLog(t *testing.T) {
	// Ensure test log is NOT set so we exercise the real tmux path
	t.Setenv("GT_TEST_NUDGE_LOG", "")

	// Should not panic even though no tmux session exists
	nudgeRefinery("nonexistent-rig", "test message")
}

func TestIsDeferredBead(t *testing.T) {
	tests := []struct {
		name string
		info *beadInfo
		want bool
	}{
		{"open bead is not deferred", &beadInfo{Status: "open", Description: "some task"}, false},
		{"in_progress bead is not deferred", &beadInfo{Status: "in_progress", Description: "working on it"}, false},
		{"deferred status", &beadInfo{Status: "deferred", Description: "some task"}, true},
		{"description says deferred to post-launch", &beadInfo{Status: "open", Description: "deferred to post-launch"}, true},
		{"description says deferred to post launch", &beadInfo{Status: "open", Description: "deferred to post launch"}, true},
		{"description says status: deferred", &beadInfo{Status: "open", Description: "status: deferred\nsome other notes"}, true},
		{"case insensitive description", &beadInfo{Status: "open", Description: "Deferred to Post-Launch"}, true},
		{"deferred keyword not in deferral phrase", &beadInfo{Status: "open", Description: "the user deferred this action"}, false},
		{"empty description", &beadInfo{Status: "open", Description: ""}, false},
		{"hooked bead not deferred", &beadInfo{Status: "hooked", Description: "some work"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDeferredBead(tt.info); got != tt.want {
				t.Errorf("isDeferredBead(%+v) = %v, want %v", tt.info, got, tt.want)
			}
		})
	}
}

// TestMoleculeScaffoldRejectReason covers the guard scheduleBead and
// executeSling apply to keep formula-molecule scaffolding out of dispatch
// (gt-6va3). getBeadInfo populates IssueType and Dependencies from bd show, so
// the choke points always see the parent's molecule type.
func TestMoleculeScaffoldRejectReason(t *testing.T) {
	tests := []struct {
		name       string
		info       *beadInfo
		wantReject bool
	}{
		{"nil info", nil, false},
		{"ordinary task dispatches", &beadInfo{Status: "open", IssueType: "task"}, false},
		{"molecule container rejected", &beadInfo{Status: "open", IssueType: "molecule"}, true},
		{
			name: "molecule step bead rejected via parent-child dependency",
			info: &beadInfo{
				Status:    "open",
				IssueType: "task",
				Dependencies: []beads.IssueDep{
					{ID: "gt-vf2u", Type: "molecule", DependencyType: "parent-child"},
				},
			},
			wantReject: true,
		},
		{
			name: "task child of an epic dispatches",
			info: &beadInfo{
				Status:    "open",
				IssueType: "task",
				Dependencies: []beads.IssueDep{
					{ID: "gt-epic", Type: "epic", DependencyType: "parent-child"},
				},
			},
			wantReject: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := moleculeScaffoldRejectReason(tt.info)
			if (reason != "") != tt.wantReject {
				t.Errorf("moleculeScaffoldRejectReason(%+v) = %q, wantReject=%v", tt.info, reason, tt.wantReject)
			}
		})
	}
}

func TestCollectExistingMoleculesFiltersClosedMolecules(t *testing.T) {
	tests := []struct {
		name string
		info *beadInfo
		want []string
	}{
		{
			name: "open molecule is collected",
			info: &beadInfo{
				Dependencies: []beads.IssueDep{
					{ID: "bd-wisp-abc", Status: "open"},
				},
			},
			want: []string{"bd-wisp-abc"},
		},
		{
			name: "closed molecule is skipped",
			info: &beadInfo{
				Dependencies: []beads.IssueDep{
					{ID: "bd-wisp-abc", Status: "closed"},
				},
			},
			want: nil,
		},
		{
			name: "tombstone molecule is skipped",
			info: &beadInfo{
				Dependencies: []beads.IssueDep{
					{ID: "bd-wisp-abc", Status: "tombstone"},
				},
			},
			want: nil,
		},
		{
			name: "mixed: open kept, closed skipped",
			info: &beadInfo{
				Dependencies: []beads.IssueDep{
					{ID: "bd-wisp-dead", Status: "closed"},
					{ID: "bd-wisp-live", Status: "in_progress"},
				},
			},
			want: []string{"bd-wisp-live"},
		},
		{
			name: "non-wisp dependency ignored regardless of status",
			info: &beadInfo{
				Dependencies: []beads.IssueDep{
					{ID: "bd-regular-dep", Status: "open"},
				},
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collectExistingMolecules(tt.info)
			if len(got) != len(tt.want) {
				t.Fatalf("collectExistingMolecules() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("collectExistingMolecules()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestCollectExistingMoleculeDepsReadsCanonicalWispEdges(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses Unix shell script bd stub")
	}

	binDir := t.TempDir()
	logPath := filepath.Join(binDir, "bd.log")
	script := `#!/bin/sh
echo "$*" >> "${BD_LOG}"
if [ "$1" = "sql" ]; then
  case "$2" in
    *wisp_dependencies*depends_on_issue_id*depends_on_wisp_id*)
      echo '[{"issue_id":"gt-wisp-live"},{"issue_id":"gt-wisp-live"},{"issue_id":"gt-wisp-other"}]'
      exit 0
      ;;
  esac
  echo 'unexpected query' >&2
  exit 1
fi
exit 1
`
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatalf("write bd stub: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BD_LOG", logPath)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(binDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	got, err := collectExistingMoleculeDeps("gt-work", "")
	if err != nil {
		t.Fatalf("collectExistingMoleculeDeps: %v", err)
	}
	want := []string{"gt-wisp-live", "gt-wisp-other"}
	if len(got) != len(want) {
		t.Fatalf("collectExistingMoleculeDeps() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("collectExistingMoleculeDeps()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestIsSlingConfigError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"not initialized", fmt.Errorf("database not initialized"), true},
		{"no such table", fmt.Errorf("no such table: issues"), true},
		{"table not found", fmt.Errorf("table not found: issues"), true},
		{"issue_prefix missing", fmt.Errorf("issue_prefix not configured"), true},
		{"no database", fmt.Errorf("no database found"), true},
		{"database not found", fmt.Errorf("database not found"), true},
		{"connection refused", fmt.Errorf("connection refused"), true},
		{"circuit breaker", fmt.Errorf("Dolt circuit breaker is open: server appears down"), true},
		{"server appears down", fmt.Errorf("server appears down"), true},
		{"server down", fmt.Errorf("server down"), true},
		{"server not running", fmt.Errorf("Dolt server is not running"), true},
		{"server may not be running", fmt.Errorf("Dolt server may not be running"), true},
		{"transient error", fmt.Errorf("optimistic lock failed"), false},
		{"generic error", fmt.Errorf("something else"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSlingConfigError(tt.err); got != tt.want {
				t.Errorf("isSlingConfigError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestHookBeadWithRetryFailsFastOnBdStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses Unix shell script bd stub")
	}
	beads.ResetBdAllowStaleCacheForTest()
	t.Cleanup(beads.ResetBdAllowStaleCacheForTest)

	binDir := t.TempDir()
	countPath := filepath.Join(binDir, "count")
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--allow-stale" ]; then
  echo "Error: unknown flag: --allow-stale" >&2
  exit 0
fi
count=0
if [ -f %[1]q ]; then count=$(cat %[1]q); fi
count=$((count + 1))
printf '%%s' "$count" > %[1]q
echo "Dolt circuit breaker is open: server appears down" >&2
exit 1
`, countPath)
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatalf("write bd stub: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GT_TEST_SKIP_HOOK_VERIFY", "1")

	err := hookBeadWithRetry("gt-work", "gastown/polecats/rust", t.TempDir())
	if err == nil {
		t.Fatal("hookBeadWithRetry error = nil, want fail-fast error")
	}
	if !strings.Contains(err.Error(), "Dolt circuit breaker is open") {
		t.Fatalf("error missing bd stderr: %v", err)
	}
	if !strings.Contains(err.Error(), "Safe next action") {
		t.Fatalf("error missing reconciliation guidance: %v", err)
	}
	countBytes, readErr := os.ReadFile(countPath)
	if readErr != nil {
		t.Fatalf("read count: %v", readErr)
	}
	if got := strings.TrimSpace(string(countBytes)); got != "1" {
		t.Fatalf("bd update invoked %s times, want 1", got)
	}
}

func TestDetectActorUnresolvedRolePrefersBDActor(t *testing.T) {
	// t.Chdir to a fresh dir outside any Gas Town workspace so GetRole() cannot
	// resolve a role from the cwd — the daemon/neutral-cwd condition gt-kins is
	// about. detectActor() must then attribute work to the explicitly-declared
	// BD_ACTOR ("daemon" for the scheduler daemon) rather than a bare "unknown".
	t.Chdir(t.TempDir())

	t.Run("BD_ACTOR set is used verbatim", func(t *testing.T) {
		t.Setenv("BD_ACTOR", "daemon")
		if got := detectActor(); got != "daemon" {
			t.Errorf("detectActor() = %q, want %q", got, "daemon")
		}
	})

	t.Run("BD_ACTOR empty falls back to OS identity, never bare unknown", func(t *testing.T) {
		t.Setenv("BD_ACTOR", "")
		got := detectActor()
		if got == "unknown" {
			t.Errorf("detectActor() = %q, want an inspectable actor, not the bare literal", got)
		}
		if !strings.HasPrefix(got, "unknown(") {
			t.Errorf("detectActor() = %q, want fallbackActor format unknown(user@host)", got)
		}
	})
}

func TestFallbackActor(t *testing.T) {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "?"
	}

	tests := []struct {
		name     string
		userEnv  string
		wantUser string
	}{
		{"USER set", "jamel", "jamel"},
		{"USER empty falls back to placeholder", "", "?"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("USER", tt.userEnv)
			want := fmt.Sprintf("unknown(%s@%s)", tt.wantUser, host)
			if got := fallbackActor(); got != want {
				t.Errorf("fallbackActor() = %q, want %q", got, want)
			}
		})
	}

	t.Run("hostname lookup failure falls back to placeholder", func(t *testing.T) {
		t.Setenv("USER", "jamel")
		orig := osHostname
		osHostname = func() (string, error) { return "", fmt.Errorf("lookup failed") }
		defer func() { osHostname = orig }()

		want := "unknown(jamel@?)"
		if got := fallbackActor(); got != want {
			t.Errorf("fallbackActor() = %q, want %q", got, want)
		}
	})
}
