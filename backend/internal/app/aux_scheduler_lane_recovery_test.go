package app

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/config"
	"sub2api-giftcode/backend/internal/sub2api"
)

func TestAuxSchedulerLaneRecoverySelectsMinimalPrefixAndWaitsTwoMinutes(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	current := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	state := newAuxLaneUpstreamState(
		auxModelAccount(1, "gpt", "gpt-5"),
		auxModelAccount(2, "o3", "o3"),
		auxModelAccount(3, "both", "gpt-5", "o3"),
	)
	state.setSchedulable(1, true)
	state.setSchedulable(2, true)
	state.setSchedulable(3, true)
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	svc.nowFunc = func() time.Time { return current }
	id := insertAuxLaneRuleRaw(t, store, "recovery", true, [][]int64{{1}, {2}, {3}}, []string{"gpt-5", "o3"}, 3, 3)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, 3, rule.ExpectedOpenThroughLane)
	require.NotNil(t, rule.RecoveryCandidateLane)
	require.Equal(t, 2, *rule.RecoveryCandidateLane)
	require.NotNil(t, rule.RecoveryCandidateSince)

	current = current.Add(90 * time.Second)
	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule = currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, 3, rule.ExpectedOpenThroughLane)
	state.mu.Lock()
	require.Empty(t, state.calls)
	state.mu.Unlock()

	current = current.Add(31 * time.Second)
	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule = currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, 2, rule.ExpectedOpenThroughLane)
	require.Equal(t, "stable", rule.TransitionStatus)
	state.mu.Lock()
	require.Len(t, state.calls, 1)
	require.Equal(t, int64(3), state.calls[0].AccountID)
	require.False(t, state.calls[0].Value)
	require.True(t, state.calls[0].Verified)
	require.False(t, state.accounts[3].Schedulable)
	state.mu.Unlock()
}

func TestAuxSchedulerLaneRecoveryCandidateChangeRestartsStability(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	current := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	account1 := auxModelAccount(1, "gpt", "gpt-5")
	account2 := auxModelAccount(2, "o3", "o3")
	account3 := auxModelAccount(3, "both", "gpt-5", "o3")
	state := newAuxLaneUpstreamState(account1, account2, account3)
	state.setSchedulable(1, true)
	state.setSchedulable(2, true)
	state.setSchedulable(3, true)
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	svc.nowFunc = func() time.Time { return current }
	id := insertAuxLaneRuleRaw(t, store, "candidate change", true, [][]int64{{1}, {2}, {3}}, []string{"gpt-5", "o3"}, 3, 3)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc, id)
	require.NotNil(t, rule.RecoveryCandidateSince)
	firstSince := *rule.RecoveryCandidateSince

	current = current.Add(time.Minute)
	unavailableO3 := auxModelAccount(2, "o3", "o3")
	unavailableO3.Extra = auxCooldownExtra("o3", current)
	unavailableO3.Schedulable = true
	state.replaceAccount(unavailableO3)
	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule = currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, 3, rule.ExpectedOpenThroughLane)
	require.Nil(t, rule.RecoveryCandidateLane)
	require.Nil(t, rule.RecoveryCandidateSince)

	current = current.Add(time.Minute)
	recoveredO3 := auxModelAccount(2, "o3", "o3")
	recoveredO3.Schedulable = true
	state.replaceAccount(recoveredO3)
	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule = currentAuxSchedulerRule(t, svc, id)
	require.NotNil(t, rule.RecoveryCandidateLane)
	require.Equal(t, 2, *rule.RecoveryCandidateLane)
	require.NotNil(t, rule.RecoveryCandidateSince)
	require.NotEqual(t, firstSince, *rule.RecoveryCandidateSince)

	current = current.Add(90 * time.Second)
	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	require.Equal(t, 3, currentAuxSchedulerRule(t, svc, id).ExpectedOpenThroughLane)
}

func TestAuxSchedulerLaneRecoveryClosesHighLanesInOrder(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	current := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	state := newAuxLaneUpstreamState(
		auxModelAccount(1, "gpt", "gpt-5"),
		auxModelAccount(2, "o3", "o3"),
		auxModelAccount(3, "gpt-b", "gpt-5"),
		auxModelAccount(4, "o3-b", "o3"),
	)
	for _, id := range []int64{1, 2, 3, 4} {
		state.setSchedulable(id, true)
	}
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	svc.nowFunc = func() time.Time { return current }
	insertAuxLaneRuleRaw(t, store, "close order", true, [][]int64{{1}, {2}, {3}, {4}}, []string{"gpt-5", "o3"}, 4, 4)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	current = current.Add(2*time.Minute + time.Second)
	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	state.mu.Lock()
	require.Len(t, state.calls, 2)
	require.Equal(t, int64(4), state.calls[0].AccountID)
	require.False(t, state.calls[0].Value)
	require.Equal(t, int64(3), state.calls[1].AccountID)
	require.False(t, state.calls[1].Value)
	state.mu.Unlock()
}

func TestAuxSchedulerLaneRecoveryConfirmedCapStillShowsAutoLaneBlock(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	fixedNow := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	wholeDown := auxModelAccount(1, "whole down", "gpt-5")
	wholeDown.TempUnschedulableUntil = timePtr(fixedNow.Add(5 * time.Minute))
	unknownMetadata := sub2api.Account{ID: 2, Name: "unknown metadata", Platform: "openai", Type: "apikey", Status: "active"}
	state := newAuxLaneUpstreamState(wholeDown, unknownMetadata, auxModelAccount(3, "backup", "gpt-5"))
	state.setSchedulable(1, true)
	state.setSchedulable(2, true)
	state.setSchedulable(3, false)
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	svc.nowFunc = func() time.Time { return fixedNow }
	id := insertAuxLaneRuleRaw(t, store, "cap with unknown", true, [][]int64{{1, 2}, {3}}, []string{"gpt-5"}, 1, 1)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, "blocked", rule.TransitionStatus)
	require.Contains(t, rule.BlockedReason, "自动上限")
	require.Equal(t, []string{"gpt-5"}, rule.MissingModels)
	state.mu.Lock()
	require.Empty(t, state.calls)
	state.mu.Unlock()
}

func TestAuxSchedulerLaneRecoveryUnknownObservationPausesShrink(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	current := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	account2 := auxModelAccount(2, "o3", "o3")
	state := newAuxLaneUpstreamState(
		auxModelAccount(1, "gpt", "gpt-5"),
		account2,
		auxModelAccount(3, "both", "gpt-5", "o3"),
	)
	for _, id := range []int64{1, 2, 3} {
		state.setSchedulable(id, true)
	}
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	svc.nowFunc = func() time.Time { return current }
	id := insertAuxLaneRuleRaw(t, store, "unknown pause", true, [][]int64{{1}, {2}, {3}}, []string{"gpt-5", "o3"}, 3, 3)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc, id)
	require.NotNil(t, rule.RecoveryCandidateLane)

	unknownO3 := sub2api.Account{ID: 2, Name: "o3-unknown", Platform: "openai", Type: "apikey", Status: "active", Schedulable: true}
	state.replaceAccount(unknownO3)
	current = current.Add(2*time.Minute + time.Second)
	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule = currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, "blocked", rule.TransitionStatus)
	require.Equal(t, 3, rule.ExpectedOpenThroughLane)
	require.Nil(t, rule.RecoveryCandidateLane)
	state.mu.Lock()
	require.Empty(t, state.calls)
	state.mu.Unlock()

	state.replaceAccount(auxModelAccount(2, "o3", "o3"))
	state.setSchedulable(2, true)
	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule = currentAuxSchedulerRule(t, svc, id)
	require.NotNil(t, rule.RecoveryCandidateLane)
	require.Equal(t, 2, *rule.RecoveryCandidateLane)
	current = current.Add(2*time.Minute + time.Second)
	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	require.Equal(t, 2, currentAuxSchedulerRule(t, svc, id).ExpectedOpenThroughLane)
}

func TestAuxSchedulerLaneRecoveryUncertainCloseRetriesToStable(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	current := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	state := newAuxLaneUpstreamState(
		auxModelAccount(1, "gpt", "gpt-5"),
		auxModelAccount(2, "o3", "o3"),
		auxModelAccount(3, "both", "gpt-5", "o3"),
	)
	for _, id := range []int64{1, 2, 3} {
		state.setSchedulable(id, true)
	}
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	svc.nowFunc = func() time.Time { return current }
	id := insertAuxLaneRuleRaw(t, store, "uncertain close", true, [][]int64{{1}, {2}, {3}}, []string{"gpt-5", "o3"}, 3, 3)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	current = current.Add(2*time.Minute + time.Second)
	state.mu.Lock()
	state.readMismatch = true
	state.mu.Unlock()
	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, "uncertain", rule.TransitionStatus)
	require.Equal(t, 2, rule.ExpectedOpenThroughLane)

	state.mu.Lock()
	state.readMismatch = false
	state.mu.Unlock()
	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule = currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, "stable", rule.TransitionStatus)
	require.Equal(t, 2, rule.ExpectedOpenThroughLane)
	state.mu.Lock()
	require.Len(t, state.calls, 2)
	require.Equal(t, int64(3), state.calls[0].AccountID)
	require.False(t, state.calls[0].Value)
	require.False(t, state.calls[0].Verified)
	require.Equal(t, int64(3), state.calls[1].AccountID)
	require.False(t, state.calls[1].Value)
	require.True(t, state.calls[1].Verified)
	state.mu.Unlock()
}
