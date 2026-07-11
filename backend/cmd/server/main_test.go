package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunSubscriptionConcurrencyLoopReconcilesImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	go runSubscriptionConcurrencyLoop(ctx, func(context.Context) error {
		calls.Add(1)
		return nil
	}, 5*time.Millisecond)

	require.Eventually(t, func() bool { return calls.Load() >= 1 }, time.Second, time.Millisecond, "reconciliation was not invoked immediately")
	require.Eventually(t, func() bool { return calls.Load() >= 2 }, time.Second, time.Millisecond, "reconciliation did not run on the interval")
	cancel()
	time.Sleep(20 * time.Millisecond)
	callsAfterCancel := calls.Load()
	time.Sleep(20 * time.Millisecond)
	require.Equal(t, callsAfterCancel, calls.Load(), "reconciliation continued after cancellation")
}
