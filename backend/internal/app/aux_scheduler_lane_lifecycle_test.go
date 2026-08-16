package app

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/config"
	"sub2api-giftcode/backend/internal/sub2api"
)

func TestAuxSchedulerLaneDisableClosesHighLanesBeforeReleasingOwnership(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	state := newAuxLaneUpstreamState(
		auxModelAccount(1, "base", "gpt-5"),
		auxModelAccount(2, "high", "gpt-5"),
		auxModelAccount(3, "other-base", "gpt-5"),
		auxModelAccount(4, "other-high", "gpt-5"),
	)
	for _, id := range []int64{1, 2} {
		state.setSchedulable(id, true)
	}
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	id := insertAuxLaneRuleRaw(t, store, "disable", true, [][]int64{{1}, {2}}, []string{"gpt-5"}, 2, 2)

	view, err := svc.UpdateAuxSchedulerRule(ctx, id, AuxSchedulerRuleInput{
		Name: "disable", Enabled: false, ModelNames: []string{"gpt-5"}, Lanes: [][]int64{{1}, {2}}, MaximumAutoLane: 2,
	})
	require.NoError(t, err)
	require.False(t, view.Enabled)
	require.Equal(t, 1, view.ExpectedOpenThroughLane)
	state.mu.Lock()
	require.Len(t, state.calls, 1)
	require.Equal(t, int64(2), state.calls[0].AccountID)
	require.False(t, state.calls[0].Value)
	require.True(t, state.calls[0].Verified)
	require.True(t, state.accounts[1].Schedulable)
	require.False(t, state.accounts[2].Schedulable)
	state.mu.Unlock()

	claim, err := svc.CreateAuxSchedulerRule(ctx, AuxSchedulerRuleInput{
		Name: "claim", Enabled: true, ModelNames: []string{"gpt-5"}, Lanes: [][]int64{{3}, {2}}, MaximumAutoLane: 2,
	})
	require.NoError(t, err)
	require.Equal(t, [][]int64{{3}, {2}}, claim.Lanes)
}

func TestAuxSchedulerLaneDeleteClosesHighLanesThenDeletes(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	state := newAuxLaneUpstreamState(
		auxModelAccount(1, "base", "gpt-5"),
		auxModelAccount(2, "high", "gpt-5"),
	)
	for _, id := range []int64{1, 2} {
		state.setSchedulable(id, true)
	}
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	id := insertAuxLaneRuleRaw(t, store, "delete", true, [][]int64{{1}, {2}}, []string{"gpt-5"}, 2, 2)

	require.NoError(t, svc.DeleteAuxSchedulerRule(ctx, id))
	rules, err := svc.ListAuxSchedulerRules(ctx)
	require.NoError(t, err)
	for _, rule := range rules {
		require.NotEqual(t, id, rule.ID)
	}
	state.mu.Lock()
	require.Len(t, state.calls, 1)
	require.Equal(t, int64(2), state.calls[0].AccountID)
	require.False(t, state.calls[0].Value)
	require.True(t, state.calls[0].Verified)
	require.True(t, state.accounts[1].Schedulable)
	state.mu.Unlock()
}

func TestAuxSchedulerLaneDeleteUncertainPreservesRuleAndRetries(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	state := newAuxLaneUpstreamState(
		auxModelAccount(1, "base", "gpt-5"),
		auxModelAccount(2, "high", "gpt-5"),
	)
	for _, id := range []int64{1, 2} {
		state.setSchedulable(id, true)
	}
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	id := insertAuxLaneRuleRaw(t, store, "delete retry", true, [][]int64{{1}, {2}}, []string{"gpt-5"}, 2, 2)

	state.mu.Lock()
	state.postReadFail = true
	state.mu.Unlock()
	err = svc.DeleteAuxSchedulerRule(ctx, id)
	require.Error(t, err)
	rule := currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, "uncertain", rule.TransitionStatus)
	require.Equal(t, 1, rule.ExpectedOpenThroughLane)
	require.Equal(t, [][]int64{{1}, {2}}, rule.Lanes)
	state.mu.Lock()
	require.True(t, state.accounts[2].Schedulable)
	state.mu.Unlock()

	state.mu.Lock()
	state.postReadFail = false
	state.mu.Unlock()
	require.NoError(t, svc.DeleteAuxSchedulerRule(ctx, id))
	rules, err := svc.ListAuxSchedulerRules(ctx)
	require.NoError(t, err)
	for _, rule := range rules {
		require.NotEqual(t, id, rule.ID)
	}
	state.mu.Lock()
	require.False(t, state.accounts[2].Schedulable)
	state.mu.Unlock()
}

func TestAuxSchedulerLaneUpdateClosesRemovedAccountBeforeOwnershipRelease(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	state := newAuxLaneUpstreamState(
		auxModelAccount(1, "base", "gpt-5"),
		auxModelAccount(2, "old-high", "gpt-5"),
		auxModelAccount(3, "new-high", "gpt-5"),
		auxModelAccount(4, "other-high", "gpt-5"),
	)
	for _, id := range []int64{1, 2} {
		state.setSchedulable(id, true)
	}
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	id := insertAuxLaneRuleRaw(t, store, "update remove", true, [][]int64{{1}, {2}}, []string{"gpt-5"}, 2, 2)

	view, err := svc.UpdateAuxSchedulerRule(ctx, id, AuxSchedulerRuleInput{
		Name: "update remove", Enabled: true, ModelNames: []string{"gpt-5"}, Lanes: [][]int64{{1}, {3}}, MaximumAutoLane: 2,
	})
	require.NoError(t, err)
	require.Equal(t, [][]int64{{1}, {3}}, view.Lanes)
	state.mu.Lock()
	require.Len(t, state.calls, 1)
	require.Equal(t, int64(2), state.calls[0].AccountID)
	require.False(t, state.calls[0].Value)
	require.True(t, state.calls[0].Verified)
	require.False(t, state.accounts[2].Schedulable)
	require.True(t, state.accounts[1].Schedulable)
	state.mu.Unlock()

	claim, err := svc.CreateAuxSchedulerRule(ctx, AuxSchedulerRuleInput{
		Name: "claim old high", Enabled: true, ModelNames: []string{"gpt-5"}, Lanes: [][]int64{{4}, {2}}, MaximumAutoLane: 2,
	})
	require.NoError(t, err)
	require.Equal(t, [][]int64{{4}, {2}}, claim.Lanes)
}

func TestAuxSchedulerLaneUpdateKeepsRemovedBaseAccountSchedulable(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	state := newAuxLaneUpstreamState(
		auxModelAccount(1, "old-base", "gpt-5"),
		auxModelAccount(2, "high", "gpt-5"),
		auxModelAccount(3, "new-base", "gpt-5"),
		auxModelAccount(4, "other-high", "gpt-5"),
		auxModelAccount(5, "claim-high", "gpt-5"),
	)
	for _, id := range []int64{1, 2} {
		state.setSchedulable(id, true)
	}
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	id := insertAuxLaneRuleRaw(t, store, "remove base", true, [][]int64{{1}, {2}}, []string{"gpt-5"}, 2, 2)

	view, err := svc.UpdateAuxSchedulerRule(ctx, id, AuxSchedulerRuleInput{
		Name: "remove base", Enabled: true, ModelNames: []string{"gpt-5"}, Lanes: [][]int64{{3}, {4}}, MaximumAutoLane: 2,
	})
	require.NoError(t, err)
	require.Equal(t, [][]int64{{3}, {4}}, view.Lanes)
	state.mu.Lock()
	require.Len(t, state.calls, 1)
	require.Equal(t, int64(2), state.calls[0].AccountID)
	require.False(t, state.calls[0].Value)
	require.True(t, state.accounts[1].Schedulable)
	state.mu.Unlock()

	claim, err := svc.CreateAuxSchedulerRule(ctx, AuxSchedulerRuleInput{
		Name: "claim old base", Enabled: true, ModelNames: []string{"gpt-5"}, Lanes: [][]int64{{1}, {5}}, MaximumAutoLane: 2,
	})
	require.NoError(t, err)
	require.Equal(t, [][]int64{{1}, {5}}, claim.Lanes)
}

func TestAuxSchedulerLaneUpdateCleanupFailurePreservesOriginalRuleAndOwnership(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	state := newAuxLaneUpstreamState(
		auxModelAccount(1, "base", "gpt-5"),
		auxModelAccount(2, "old-high", "gpt-5"),
		auxModelAccount(3, "new-high", "gpt-5"),
		auxModelAccount(4, "other-high", "gpt-5"),
	)
	for _, id := range []int64{1, 2} {
		state.setSchedulable(id, true)
	}
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	id := insertAuxLaneRuleRaw(t, store, "update fail", true, [][]int64{{1}, {2}}, []string{"gpt-5"}, 2, 2)

	state.mu.Lock()
	state.postReadFail = true
	state.mu.Unlock()
	_, err = svc.UpdateAuxSchedulerRule(ctx, id, AuxSchedulerRuleInput{
		Name: "update fail", Enabled: true, ModelNames: []string{"gpt-5"}, Lanes: [][]int64{{1}, {3}}, MaximumAutoLane: 2,
	})
	require.Error(t, err)
	rule := currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, "uncertain", rule.TransitionStatus)
	require.Equal(t, [][]int64{{1}, {2}}, rule.Lanes)
	state.mu.Lock()
	require.True(t, state.accounts[2].Schedulable)
	state.mu.Unlock()

	_, err = svc.CreateAuxSchedulerRule(ctx, AuxSchedulerRuleInput{
		Name: "conflict", Enabled: true, ModelNames: []string{"gpt-5"}, Lanes: [][]int64{{4}, {2}}, MaximumAutoLane: 2,
	})
	require.ErrorIs(t, err, ErrBadRequest)

	state.mu.Lock()
	state.postReadFail = false
	state.mu.Unlock()
	view, err := svc.UpdateAuxSchedulerRule(ctx, id, AuxSchedulerRuleInput{
		Name: "update fail", Enabled: true, ModelNames: []string{"gpt-5"}, Lanes: [][]int64{{1}, {3}}, MaximumAutoLane: 2,
	})
	require.NoError(t, err)
	require.Equal(t, [][]int64{{1}, {3}}, view.Lanes)
	state.mu.Lock()
	require.False(t, state.accounts[2].Schedulable)
	state.mu.Unlock()

	claim, err := svc.CreateAuxSchedulerRule(ctx, AuxSchedulerRuleInput{
		Name: "claim after cleanup", Enabled: true, ModelNames: []string{"gpt-5"}, Lanes: [][]int64{{4}, {2}}, MaximumAutoLane: 2,
	})
	require.NoError(t, err)
	require.Equal(t, [][]int64{{4}, {2}}, claim.Lanes)
}
