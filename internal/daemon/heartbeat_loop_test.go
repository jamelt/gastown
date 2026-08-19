package daemon

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestRunHeartbeatLoop_HungCycleDoesNotBlockBoundedDrain is the gt-kj5j
// regression: previously the heartbeat ran inline on Run()'s shared select
// loop, so a hung/slow cycle blocked that loop's ctx.Done() arm and delayed
// shutdown() indefinitely (bounded only by StopDaemon's outer 60s force-kill
// timeout). Now the heartbeat is isolated on its own dogWg-enrolled goroutine
// like every other patrol dog, so shutdown()'s existing bounded drainDogs
// covers it: a hung cycle no longer holds up graceful shutdown past the
// drain budget. This mirrors TestDrainDogs_BoundedForUninterruptibleDog for
// the heartbeat goroutine specifically.
func TestRunHeartbeatLoop_HungCycleDoesNotBlockBoundedDrain(t *testing.T) {
	d := newLoopTestDaemon(t)
	ctx, cancel := context.WithCancel(context.Background())
	d.ctx = ctx

	blocking := make(chan struct{})
	defer close(blocking) // let the stuck goroutine exit at test end

	lifecycleCh := make(chan struct{}, 1)
	d.dogWg.Add(1)
	go d.runHeartbeatLoop(ctx, lifecycleCh,
		func() { /* heartbeatFn: not exercised in this test */ },
		func() {
			// Simulate a hung lifecycle-request cycle that ignores ctx cancellation,
			// standing in for a hung heartbeat cycle (same shared code path in
			// production: heartbeat() itself calls processLifecycleRequests()).
			<-blocking
		},
	)

	// Trigger a cycle the same way Run()'s signal branch does, then let it start
	// before canceling.
	lifecycleCh <- struct{}{}
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

// TestRunHeartbeatLoop_LifecycleTriggerRunsLifecycleFn verifies the new
// convergence path: a send on lifecycleCh (what Run()'s signal branch does
// instead of calling ProcessLifecycleRequests directly) reaches lifecycleFn
// on the isolated goroutine.
func TestRunHeartbeatLoop_LifecycleTriggerRunsLifecycleFn(t *testing.T) {
	d := newLoopTestDaemon(t)
	ctx, cancel := context.WithCancel(context.Background())
	d.ctx = ctx
	defer cancel()

	var lifecycleCalls int64
	lifecycleCh := make(chan struct{}, 1)
	d.dogWg.Add(1)
	go d.runHeartbeatLoop(ctx, lifecycleCh,
		func() {},
		func() { atomic.AddInt64(&lifecycleCalls, 1) },
	)

	lifecycleCh <- struct{}{}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&lifecycleCalls) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := atomic.LoadInt64(&lifecycleCalls); got != 1 {
		t.Errorf("lifecycleFn call count = %d, want 1", got)
	}

	cancel()
	drainWithin(t, d, 2*time.Second)
}

// TestRunHeartbeatLoop_StopsNewCyclesOnCancel mirrors
// TestRunDogLoop_StopsNewCyclesOnCancel for the heartbeat loop's ctx.Err()
// guard: once ctx is canceled, a pending lifecycle trigger must not start a
// new lifecycleFn cycle, even though the buffered non-blocking send to
// lifecycleCh still succeeds.
func TestRunHeartbeatLoop_StopsNewCyclesOnCancel(t *testing.T) {
	d := newLoopTestDaemon(t)
	ctx, cancel := context.WithCancel(context.Background())
	d.ctx = ctx

	var lifecycleCalls int64
	lifecycleCh := make(chan struct{}, 1)
	d.dogWg.Add(1)
	go d.runHeartbeatLoop(ctx, lifecycleCh,
		func() {},
		func() { atomic.AddInt64(&lifecycleCalls, 1) },
	)

	cancel()
	// Non-blocking send, same as Run()'s signal branch — must succeed
	// regardless of the loop goroutine's state.
	select {
	case lifecycleCh <- struct{}{}:
	default:
		t.Fatal("buffered lifecycleCh send should not block")
	}

	drainWithin(t, d, 2*time.Second)

	if got := atomic.LoadInt64(&lifecycleCalls); got != 0 {
		t.Errorf("lifecycleFn ran after cancel: got %d calls, want 0", got)
	}
}
