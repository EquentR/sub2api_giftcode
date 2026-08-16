package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/db"
)

func TestRunSubscriptionConcurrencyLoopReconcilesImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		runSubscriptionConcurrencyLoop(ctx, func(context.Context) error {
			calls.Add(1)
			return nil
		}, 5*time.Millisecond)
	}()

	require.Eventually(t, func() bool { return calls.Load() >= 1 }, time.Second, time.Millisecond, "reconciliation was not invoked immediately")
	require.Eventually(t, func() bool { return calls.Load() >= 2 }, time.Second, time.Millisecond, "reconciliation did not run on the interval")
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("concurrency monitor did not exit after cancellation")
	}
	callsAfterExit := calls.Load()
	time.Sleep(20 * time.Millisecond)
	require.Equal(t, callsAfterExit, calls.Load(), "reconciliation continued after cancellation")
}

func TestRunWALCheckpointLoopRunsImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		runWALCheckpointLoop(ctx, func(context.Context) (db.WALCheckpointStats, error) {
			calls.Add(1)
			return db.WALCheckpointStats{}, nil
		}, 5*time.Millisecond)
	}()

	require.Eventually(t, func() bool { return calls.Load() >= 2 }, time.Second, time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("WAL checkpoint loop did not exit after cancellation")
	}
}
