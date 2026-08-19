package convoy

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// writeBdStub installs a fake `bd` on PATH that tracks a single convoy's
// CompletionNotifiedAt state via a marker file, mirroring the real bd
// show/update round trip closely enough to exercise ClaimCompletionNotification
// end to end without a real Dolt-backed beads store.
func writeBdStub(t *testing.T, convoyID string) (updateCount func() int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows - shell stubs")
	}

	binDir := t.TempDir()
	statePath := filepath.Join(binDir, "notified.state")
	updateLogPath := filepath.Join(binDir, "update.log")

	bdScript := `#!/bin/sh
STATE="` + statePath + `"
UPDATE_LOG="` + updateLogPath + `"
case "$1" in
  show)
    if [ -f "$STATE" ]; then
      printf '%s\n' '[{"id":"` + convoyID + `","description":"Owner: mayor/\ncompletion_notified_at: 2026-05-25T02:30:00Z"}]'
    else
      printf '%s\n' '[{"id":"` + convoyID + `","description":"Owner: mayor/"}]'
    fi
    exit 0
    ;;
  update)
    echo update >> "$UPDATE_LOG"
    touch "$STATE"
    exit 0
    ;;
  export)
    exit 0
    ;;
esac
exit 0
`
	bdPath := filepath.Join(binDir, "bd")
	if err := os.WriteFile(bdPath, []byte(bdScript), 0755); err != nil {
		t.Fatalf("write bd stub: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	return func() int {
		data, err := os.ReadFile(updateLogPath)
		if err != nil {
			if os.IsNotExist(err) {
				return 0
			}
			t.Fatalf("read update log: %v", err)
		}
		return strings.Count(string(data), "update")
	}
}

func TestClaimCompletionNotification_SecondCallIsNoOp(t *testing.T) {
	townRoot := t.TempDir()
	updateCount := writeBdStub(t, "hq-cv-claim")

	claimed, fields, err := ClaimCompletionNotification(townRoot, "hq-cv-claim")
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if !claimed {
		t.Fatal("first claim should have succeeded")
	}
	if fields == nil || fields.CompletionNotifiedAt == "" {
		t.Fatal("first claim should return fields with CompletionNotifiedAt set")
	}

	claimed, _, err = ClaimCompletionNotification(townRoot, "hq-cv-claim")
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if claimed {
		t.Fatal("second claim should be a no-op — convoy already claimed")
	}

	if got := updateCount(); got != 1 {
		t.Fatalf("bd update calls = %d, want exactly 1", got)
	}
}

// TestClaimCompletionNotification_ConcurrentCallsExactlyOneWins is the
// scenario ClaimCompletionNotification exists to make safe: multiple
// callers (representing the CLI path, deacon's periodic sweep, and the
// refinery's post-merge check) racing to claim the same convoy's completion
// notification must result in exactly one winner, never zero and never more
// than one — the bug this fix closes was duplicate convoy-complete mail from
// more than one of these racing and all seeing "not yet notified".
func TestClaimCompletionNotification_ConcurrentCallsExactlyOneWins(t *testing.T) {
	townRoot := t.TempDir()
	writeBdStub(t, "hq-cv-race")

	const count = 10
	var wg sync.WaitGroup
	results := make([]bool, count)
	errs := make(chan error, count)

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			claimed, _, err := ClaimCompletionNotification(townRoot, "hq-cv-race")
			if err != nil {
				errs <- err
				return
			}
			results[i] = claimed
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent ClaimCompletionNotification failed: %v", err)
	}

	winners := 0
	for _, claimed := range results {
		if claimed {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("winners = %d, want exactly 1 (duplicate or lost claim under race)", winners)
	}
}
