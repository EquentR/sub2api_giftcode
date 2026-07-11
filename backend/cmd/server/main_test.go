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
	defer cancel()
	called := make(chan struct{}, 1)
	var calls atomic.Int32
	go runSubscriptionConcurrencyLoop(ctx, func(context.Context) error {
		calls.Add(1)
		called <- struct{}{}
		return nil
	}, time.Hour)

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("reconciliation was not invoked immediately")
	}
	require.Equal(t, int32(1), calls.Load())
}
