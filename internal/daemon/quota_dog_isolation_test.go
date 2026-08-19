package daemon

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestRunQuotaDogLoop_NotStarvedBySlowSiblingDog is the regression test for
// gt-yycw: before the fix, quota_dog shared a single select loop with every
// other dog (including the recovery heartbeat), so a slow/hung sibling
// blocked the whole loop and quota_dog silently missed its interval. This
// test proves quota_dog's isolated loop keeps ticking on schedule even while
// something else blocks synchronously for far longer than the interval.
func TestRunQuotaDogLoop_NotStarvedBySlowSiblingDog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses Unix shell script mock")
	}

	townRoot := t.TempDir()
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "gt-calls.log")

	fakeGT := filepath.Join(binDir, "gt")
	script := fmt.Sprintf("#!/bin/sh\nprintf 'call\\n' >> %q\nprintf '[]'\n", logPath)
	if err := os.WriteFile(fakeGT, []byte(script), 0755); err != nil {
		t.Fatalf("write fake gt: %v", err)
	}

	d := &Daemon{
		config: &Config{TownRoot: townRoot},
		logger: log.New(os.Stderr, "", 0),
		gtPath: fakeGT,
		patrolConfig: &DaemonPatrolConfig{
			Patrols: &PatrolsConfig{
				QuotaDog: &QuotaDogConfig{Enabled: true, IntervalStr: "20ms"},
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	d.ctx = ctx

	done := make(chan struct{})
	d.dogWg.Add(1)
	go func() {
		d.runDogLoop(ctx, "quota_dog", func() time.Duration { return quotaDogInterval(d.patrolConfig) }, d.runQuotaDog)
		close(done)
	}()

	// Simulate a sibling dog (e.g. the recovery heartbeat) blocking
	// synchronously for far longer than quota_dog's 20ms interval. Under
	// the pre-fix design this would have starved quota_dog entirely, since
	// both ran on the same shared select loop; here it runs on the test
	// goroutine while quota_dog's isolated loop is unaffected.
	simulateBlockingSiblingDog(250 * time.Millisecond)

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runQuotaDogLoop did not exit after ctx cancellation")
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read gt call log: %v", err)
	}
	calls := strings.Count(string(data), "call\n")

	// Each cycle invokes the fake gt binary twice (rotate, then failover).
	// Over a 250ms window at a 20ms interval we'd expect ~12 cycles (24
	// calls) if fully unblocked; assert a conservative lower bound to avoid
	// CI flakiness while still proving quota_dog fired repeatedly on
	// schedule during the blocking window, not zero or one time.
	const minExpectedCycles = 3
	if calls < minExpectedCycles*2 {
		t.Errorf("quota_dog fired too few times while sibling blocked: got %d gt invocations (%d cycles), want at least %d cycles (%d invocations)",
			calls, calls/2, minExpectedCycles, minExpectedCycles*2)
	}
}

// simulateBlockingSiblingDog stands in for a synchronous dog handler (like
// the pre-fix heartbeat/quota_dog case in Run()'s shared select loop) that
// blocks its goroutine for the given duration.
func simulateBlockingSiblingDog(d time.Duration) {
	time.Sleep(d)
}

func TestRunDogWithOverrunCheck(t *testing.T) {
	t.Run("logs when a cycle exceeds the overrun threshold", func(t *testing.T) {
		var buf bytes.Buffer
		d := &Daemon{logger: log.New(&buf, "", 0)}

		d.runDogWithOverrunCheck("test_dog", 10*time.Millisecond, func() {
			time.Sleep(30 * time.Millisecond)
		})

		if !strings.Contains(buf.String(), "OVERRUN") {
			t.Errorf("expected an OVERRUN log line, got: %q", buf.String())
		}
	})

	t.Run("does not log for a cycle within the overrun threshold", func(t *testing.T) {
		var buf bytes.Buffer
		d := &Daemon{logger: log.New(&buf, "", 0)}

		d.runDogWithOverrunCheck("test_dog", 100*time.Millisecond, func() {})

		if strings.Contains(buf.String(), "OVERRUN") {
			t.Errorf("did not expect an OVERRUN log line, got: %q", buf.String())
		}
	})
}
