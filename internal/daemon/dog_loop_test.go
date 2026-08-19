package daemon

import (
	"context"
	"io"
	"log"
	"sync/atomic"
	"testing"
	"time"
)

// newLoopTestDaemon builds the minimal Daemon that runDogLoop needs: a logger
// and a config with a TownRoot (isShutdownInProgress reads TownRoot and returns
// false when no shutdown.lock exists).
func newLoopTestDaemon(t *testing.T) *Daemon {
	t.Helper()
	return &Daemon{
		config: &Config{TownRoot: t.TempDir()},
		logger: log.New(io.Discard, "", 0),
	}
}

func drainWithin(t *testing.T, d *Daemon, timeout time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		d.dogWg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf("dog goroutines did not exit within %v after ctx cancel", timeout)
	}
}

// TestRunDogLoop_DogsIsolatedFromEachOther is the core gt-4ecf regression: two
// dogs on their own runDogLoop goroutines must not starve each other. A dog that
// blocks for far longer than its interval must not stop a fast sibling from
// firing on schedule — the exact starvation the shared select loop caused.
func TestRunDogLoop_DogsIsolatedFromEachOther(t *testing.T) {
	d := newLoopTestDaemon(t)
	ctx, cancel := context.WithCancel(context.Background())
	d.ctx = ctx
	defer cancel()

	var slowCalls, fastCalls int64
	fifteenMs := func() time.Duration { return 15 * time.Millisecond }

	// Slow dog: every cycle blocks ~20x its interval.
	d.dogWg.Add(1)
	go d.runDogLoop(ctx, "slow_dog", fifteenMs, func() {
		atomic.AddInt64(&slowCalls, 1)
		time.Sleep(300 * time.Millisecond)
	})

	// Fast dog: must keep firing while the slow dog is blocked.
	d.dogWg.Add(1)
	go d.runDogLoop(ctx, "fast_dog", fifteenMs, func() {
		atomic.AddInt64(&fastCalls, 1)
	})

	time.Sleep(250 * time.Millisecond)
	cancel()
	drainWithin(t, d, 2*time.Second)

	// In 250ms at a 15ms interval the fast dog could fire ~16 times; assert a
	// conservative lower bound to prove it fired repeatedly (not zero/one) while
	// the slow dog was blocked. Under the pre-fix shared loop it would have been
	// blocked to a near-standstill by the slow dog's 300ms sleeps.
	if got := atomic.LoadInt64(&fastCalls); got < 5 {
		t.Errorf("fast_dog starved by blocked slow_dog: got %d cycles, want >= 5", got)
	}
}

// TestRunDogLoop_StopsNewCyclesOnCancel verifies the ctx.Err() gate: once ctx is
// canceled, runDogLoop must not start a NEW cycle even though isShutdownInProgress
// stays false on the internal-shutdown path. This is what keeps a dog (e.g.
// dolt_health's EnsureRunning) from racing shutdown()'s teardown.
func TestRunDogLoop_StopsNewCyclesOnCancel(t *testing.T) {
	d := newLoopTestDaemon(t)
	ctx, cancel := context.WithCancel(context.Background())
	d.ctx = ctx

	var calls int64
	// Tiny interval so the ticker is essentially always ready; if the loop
	// ignored ctx.Err() it would keep incrementing after cancel.
	d.dogWg.Add(1)
	go d.runDogLoop(ctx, "gated_dog", func() time.Duration { return time.Millisecond }, func() {
		atomic.AddInt64(&calls, 1)
	})

	time.Sleep(50 * time.Millisecond)
	cancel()
	drainWithin(t, d, 2*time.Second)

	after := atomic.LoadInt64(&calls)
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt64(&calls); got != after {
		t.Errorf("runDogLoop ran a cycle after cancel: calls went %d -> %d", after, got)
	}
}

// TestDrainDogs_ReturnsWhenDogsExit checks the common path: with idle dogs, the
// bounded drain returns promptly once ctx is canceled (dogs exit on ctx.Done).
func TestDrainDogs_ReturnsWhenDogsExit(t *testing.T) {
	d := newLoopTestDaemon(t)
	ctx, cancel := context.WithCancel(context.Background())
	d.ctx = ctx

	d.dogWg.Add(1)
	go d.runDogLoop(ctx, "idle_dog", func() time.Duration { return time.Hour }, func() {})

	time.Sleep(20 * time.Millisecond)
	cancel()

	start := time.Now()
	d.drainDogs(2 * time.Second)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("drainDogs took %v for an idle dog; expected a prompt return on ctx cancel", elapsed)
	}
}

// TestDrainDogs_BoundedForUninterruptibleDog verifies the bounded behavior: a dog
// stuck in ctx-ignoring work must NOT hold shutdown open past the drain budget.
// This is why the drain is bounded rather than an open-ended WaitGroup.Wait().
func TestDrainDogs_BoundedForUninterruptibleDog(t *testing.T) {
	d := newLoopTestDaemon(t)
	ctx, cancel := context.WithCancel(context.Background())
	d.ctx = ctx

	blocking := make(chan struct{})
	defer close(blocking) // let the stuck goroutine exit at test end

	d.dogWg.Add(1)
	go d.runDogLoop(ctx, "stuck_dog", func() time.Duration { return 10 * time.Millisecond }, func() {
		// Simulate uninterruptible work that ignores ctx cancellation.
		<-blocking
	})

	// Let the stuck cycle start, then cancel and drain with a short budget.
	time.Sleep(40 * time.Millisecond)
	cancel()

	start := time.Now()
	d.drainDogs(150 * time.Millisecond)
	elapsed := time.Since(start)
	if elapsed < 150*time.Millisecond {
		t.Errorf("drainDogs returned too early (%v); the stuck cycle should force the full budget", elapsed)
	}
	if elapsed > time.Second {
		t.Errorf("drainDogs did not respect its bound: took %v, budget was 150ms", elapsed)
	}
}
