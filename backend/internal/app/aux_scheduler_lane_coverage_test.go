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

func auxModelAccountWithExtra(id int64, name string, extra map[string]any, models ...string) sub2api.Account {
	account := auxModelAccount(id, name, models...)
	account.Extra = extra
	return account
}

func auxCooldownExtra(model string, now time.Time) map[string]any {
	return map[string]any{
		"model_rate_limits": map[string]any{
			model: map[string]any{
				"rate_limit_reset_at": now.Add(5 * time.Minute).Format(time.RFC3339),
			},
		},
	}
}

func auxCooldownsExtra(models []string, now time.Time) map[string]any {
	limits := make(map[string]any, len(models))
	for _, model := range models {
		limits[model] = map[string]any{
			"rate_limit_reset_at": now.Add(5 * time.Minute).Format(time.RFC3339),
		}
	}
	return map[string]any{"model_rate_limits": limits}
}

func (s *auxLaneUpstreamState) replaceAccount(account sub2api.Account) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accounts[account.ID] = account
}

func TestAuxSchedulerLaneCoverageEscalatesUntilUnionCoversModels(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	state := newAuxLaneUpstreamState(
		auxModelAccount(1, "gpt", "gpt-5"),
		auxModelAccount(2, "o3", "o3"),
		auxModelAccount(3, "both", "gpt-5", "o3"),
	)
	state.setSchedulable(1, true)
	state.setSchedulable(2, false)
	state.setSchedulable(3, false)
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	id := insertAuxLaneRuleRaw(t, store, "union", true, [][]int64{{1}, {2}, {3}}, []string{"gpt-5", "o3"}, 1, 3)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, 2, rule.ExpectedOpenThroughLane)
	require.Equal(t, "stable", rule.TransitionStatus)
	require.Empty(t, rule.MissingModels)
	state.mu.Lock()
	require.Len(t, state.calls, 1)
	require.Equal(t, int64(2), state.calls[0].AccountID)
	require.True(t, state.calls[0].Value)
	state.mu.Unlock()
}

func TestAuxSchedulerLaneCoverageWholeAccountFailureEscalatesImmediately(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	fixedNow := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	account1 := auxModelAccount(1, "down", "gpt-5")
	account1.TempUnschedulableUntil = timePtr(fixedNow.Add(5 * time.Minute))
	state := newAuxLaneUpstreamState(account1, auxModelAccount(2, "ok", "gpt-5", "o3"))
	state.setSchedulable(1, true)
	state.setSchedulable(2, false)
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	svc.nowFunc = func() time.Time { return fixedNow }
	id := insertAuxLaneRuleRaw(t, store, "whole down", true, [][]int64{{1}, {2}}, []string{"gpt-5", "o3"}, 1, 2)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, 2, rule.ExpectedOpenThroughLane)
	state.mu.Lock()
	require.Len(t, state.calls, 1)
	require.Equal(t, int64(2), state.calls[0].AccountID)
	require.True(t, state.calls[0].Value)
	state.mu.Unlock()
}

func TestAuxSchedulerLaneCoverageModelCooldownRequiresTwoObservations(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	fixedNow := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	state := newAuxLaneUpstreamState(
		auxModelAccountWithExtra(1, "cooling", auxCooldownExtra("gpt-5", fixedNow), "gpt-5"),
		auxModelAccount(2, "ok", "gpt-5"),
	)
	state.setSchedulable(1, true)
	state.setSchedulable(2, false)
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	svc.nowFunc = func() time.Time { return fixedNow }
	id := insertAuxLaneRuleRaw(t, store, "cooldown", true, [][]int64{{1}, {2}}, []string{"gpt-5"}, 1, 2)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, "blocked", rule.TransitionStatus)
	require.Equal(t, []string{"gpt-5"}, rule.MissingModels)
	require.Contains(t, rule.BlockedReason, "第二次观测")
	require.Equal(t, float64(1), rule.UpgradeEvidence["gpt-5_consecutive_unavailable"])
	state.mu.Lock()
	require.Empty(t, state.calls)
	state.mu.Unlock()

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule = currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, 2, rule.ExpectedOpenThroughLane)
	state.mu.Lock()
	require.Len(t, state.calls, 1)
	require.Equal(t, int64(2), state.calls[0].AccountID)
	require.True(t, state.calls[0].Value)
	state.mu.Unlock()
}

func TestAuxSchedulerLaneCoverageModelRecoveryResetsConsecutiveCounter(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	fixedNow := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	account1 := auxModelAccountWithExtra(1, "both", auxCooldownsExtra([]string{"gpt-5", "o3"}, fixedNow), "gpt-5", "o3")
	account2 := auxModelAccount(2, "o3-only", "o3")
	account3 := auxModelAccount(3, "gpt-only", "gpt-5")
	state := newAuxLaneUpstreamState(account1, account2, account3)
	state.setSchedulable(1, true)
	state.setSchedulable(2, false)
	state.setSchedulable(3, false)
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	svc.nowFunc = func() time.Time { return fixedNow }
	id := insertAuxLaneRuleRaw(t, store, "reset counter", true, [][]int64{{1}, {2}, {3}}, []string{"gpt-5", "o3"}, 1, 3)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, "blocked", rule.TransitionStatus)
	require.Equal(t, float64(1), rule.UpgradeEvidence["gpt-5_consecutive_unavailable"])
	require.Equal(t, float64(1), rule.UpgradeEvidence["o3_consecutive_unavailable"])

	gptRecovered := auxModelAccount(1, "both", "gpt-5", "o3")
	gptRecovered.Extra = auxCooldownExtra("o3", fixedNow)
	gptRecovered.Schedulable = true
	state.replaceAccount(gptRecovered)
	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule = currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, 2, rule.ExpectedOpenThroughLane)
	require.NotContains(t, rule.UpgradeEvidence, "gpt-5_consecutive_unavailable")
	require.Equal(t, float64(2), rule.UpgradeEvidence["o3_consecutive_unavailable"])

	freshGpt := auxModelAccount(1, "both", "gpt-5", "o3")
	freshGpt.Extra = auxCooldownExtra("gpt-5", fixedNow)
	freshGpt.Schedulable = true
	state.replaceAccount(freshGpt)
	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule = currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, 2, rule.ExpectedOpenThroughLane)
	require.Equal(t, "blocked", rule.TransitionStatus)
	require.Contains(t, rule.BlockedReason, "第二次观测")
	require.Equal(t, float64(1), rule.UpgradeEvidence["gpt-5_consecutive_unavailable"])
	state.mu.Lock()
	require.Len(t, state.calls, 1)
	require.Equal(t, int64(2), state.calls[0].AccountID)
	state.mu.Unlock()
}

func TestAuxSchedulerLaneCoverageUnknownBreaksConsecutiveCounter(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	fixedNow := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	account1 := auxModelAccountWithExtra(1, "both", auxCooldownsExtra([]string{"gpt-5", "o3"}, fixedNow), "gpt-5", "o3")
	account2 := auxModelAccount(2, "o3-only", "o3")
	account3 := auxModelAccount(3, "gpt-only", "gpt-5")
	state := newAuxLaneUpstreamState(account1, account2, account3)
	state.setSchedulable(1, true)
	state.setSchedulable(2, false)
	state.setSchedulable(3, false)
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	svc.nowFunc = func() time.Time { return fixedNow }
	id := insertAuxLaneRuleRaw(t, store, "unknown breaks counter", true, [][]int64{{1}, {2}, {3}}, []string{"gpt-5", "o3"}, 1, 3)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, float64(1), rule.UpgradeEvidence["gpt-5_consecutive_unavailable"])

	unknownGpt := auxModelAccount(1, "both", "gpt-5", "o3")
	unknownGpt.Extra = auxCooldownExtra("o3", fixedNow)
	unknownGpt.Credentials["model_mapping"] = "malformed"
	unknownGpt.Credentials["upstream_supported_models"] = "malformed"
	unknownGpt.Schedulable = true
	state.replaceAccount(unknownGpt)
	state.mu.Lock()
	storedUnknown := state.accounts[1]
	state.mu.Unlock()
	require.Equal(t, availabilityUnknown, auxAccountModelAvailability(storedUnknown, "gpt-5", fixedNow))
	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule = currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, "blocked", rule.TransitionStatus)
	require.Contains(t, rule.BlockedReason, "未知观测")
	require.NotContains(t, rule.UpgradeEvidence, "gpt-5_consecutive_unavailable")

	freshGpt := auxModelAccount(1, "both", "gpt-5", "o3")
	freshGpt.Extra = auxCooldownExtra("gpt-5", fixedNow)
	freshGpt.Schedulable = true
	state.replaceAccount(freshGpt)
	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule = currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, "blocked", rule.TransitionStatus)
	require.Contains(t, rule.BlockedReason, "第二次观测")
	require.Equal(t, float64(1), rule.UpgradeEvidence["gpt-5_consecutive_unavailable"])
	state.mu.Lock()
	require.Empty(t, state.calls)
	state.mu.Unlock()
}

func TestAuxSchedulerLaneCoverageUnknownObservationBlocksEscalation(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	malformed := map[string]any{"model_rate_limits": "not-a-map"}
	state := newAuxLaneUpstreamState(
		auxModelAccountWithExtra(1, "malformed", malformed, "gpt-5"),
		auxModelAccount(2, "ok", "gpt-5"),
	)
	state.setSchedulable(1, true)
	state.setSchedulable(2, false)
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	id := insertAuxLaneRuleRaw(t, store, "unknown", true, [][]int64{{1}, {2}}, []string{"gpt-5"}, 1, 2)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, "blocked", rule.TransitionStatus)
	require.Contains(t, rule.BlockedReason, "未知")
	require.Equal(t, []string{"gpt-5"}, rule.MissingModels)
	state.mu.Lock()
	require.Empty(t, state.calls)
	state.mu.Unlock()
}

func TestAuxSchedulerLaneCoverageMaximumAutoLaneBlocksWithoutWrites(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	account1 := auxModelAccount(1, "down", "gpt-5")
	account1.TempUnschedulableUntil = timePtr(time.Now().UTC().Add(5 * time.Minute))
	state := newAuxLaneUpstreamState(account1, auxModelAccount(2, "ok", "gpt-5"))
	state.setSchedulable(1, true)
	state.setSchedulable(2, false)
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	id := insertAuxLaneRuleRaw(t, store, "max cap", true, [][]int64{{1}, {2}}, []string{"gpt-5"}, 1, 1)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, "blocked", rule.TransitionStatus)
	require.Contains(t, rule.BlockedReason, "自动上限")
	require.Equal(t, []string{"gpt-5"}, rule.MissingModels)
	state.mu.Lock()
	require.Empty(t, state.calls)
	state.mu.Unlock()
}

func TestAuxSchedulerLaneCoverageOpensOnlyOneLanePerReconcile(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	account1 := auxModelAccount(1, "down-a", "gpt-5")
	account1.TempUnschedulableUntil = timePtr(time.Now().UTC().Add(5 * time.Minute))
	account2 := auxModelAccount(2, "down-b", "gpt-5")
	account2.TempUnschedulableUntil = timePtr(time.Now().UTC().Add(5 * time.Minute))
	state := newAuxLaneUpstreamState(account1, account2, auxModelAccount(3, "ok", "gpt-5"))
	state.setSchedulable(1, true)
	state.setSchedulable(2, false)
	state.setSchedulable(3, false)
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	id := insertAuxLaneRuleRaw(t, store, "one lane", true, [][]int64{{1}, {2}, {3}}, []string{"gpt-5"}, 1, 3)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, 2, rule.ExpectedOpenThroughLane)
	state.mu.Lock()
	require.Len(t, state.calls, 1)
	require.Equal(t, int64(2), state.calls[0].AccountID)
	state.mu.Unlock()

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule = currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, 3, rule.ExpectedOpenThroughLane)
	state.mu.Lock()
	require.Len(t, state.calls, 2)
	require.Equal(t, int64(3), state.calls[1].AccountID)
	state.mu.Unlock()
}

func TestAuxSchedulerModelAvailabilityAdapter(t *testing.T) {
	now := time.Now().UTC()
	future := now.Add(5 * time.Minute)
	stale := now.Add(-time.Minute)
	tests := []struct {
		name     string
		account  sub2api.Account
		model    string
		expected auxModelAvailability
	}{
		{
			name:     "active supported usable",
			account:  auxModelAccount(1, "ok", "gpt-5"),
			model:    "gpt-5",
			expected: availabilityUsable,
		},
		{
			name: "account error unavailable",
			account: func() sub2api.Account {
				account := auxModelAccount(1, "error", "gpt-5")
				account.Status = "error"
				return account
			}(),
			model:    "gpt-5",
			expected: availabilityUnavailable,
		},
		{
			name: "temp unschedulable unavailable",
			account: func() sub2api.Account {
				account := auxModelAccount(1, "temp", "gpt-5")
				account.TempUnschedulableUntil = &future
				return account
			}(),
			model:    "gpt-5",
			expected: availabilityUnavailable,
		},
		{
			name:     "model cooldown unavailable",
			account:  auxModelAccountWithExtra(1, "cooldown", auxCooldownExtra("gpt-5", now), "gpt-5"),
			model:    "gpt-5",
			expected: availabilityUnavailable,
		},
		{
			name: "mapped cooldown unavailable",
			account: func() sub2api.Account {
				account := auxModelAccount(1, "mapped", "gpt-5")
				account.Credentials["model_mapping"] = map[string]any{"gpt-5": "upstream-gpt-5"}
				account.Extra = map[string]any{
					"model_rate_limits": map[string]any{
						"upstream-gpt-5": map[string]any{"rate_limit_reset_at": future.Format(time.RFC3339)},
					},
				}
				return account
			}(),
			model:    "gpt-5",
			expected: availabilityUnavailable,
		},
		{
			name: "stale cooldown usable",
			account: auxModelAccountWithExtra(1, "stale", map[string]any{
				"model_rate_limits": map[string]any{
					"gpt-5": map[string]any{"rate_limit_reset_at": stale.Format(time.RFC3339)},
				},
			}, "gpt-5"),
			model:    "gpt-5",
			expected: availabilityUsable,
		},
		{
			name: "malformed limits unknown",
			account: auxModelAccountWithExtra(1, "bad", map[string]any{
				"model_rate_limits": "bad",
			}, "gpt-5"),
			model:    "gpt-5",
			expected: availabilityUnknown,
		},
		{
			name: "unknown account status unknown",
			account: func() sub2api.Account {
				account := auxModelAccount(1, "unknown", "gpt-5")
				account.Status = "suspended"
				return account
			}(),
			model:    "gpt-5",
			expected: availabilityUnknown,
		},
		{
			name:     "missing model metadata unknown",
			account:  sub2api.Account{ID: 1, Name: "none", Platform: "openai", Type: "apikey", Status: "active"},
			model:    "gpt-5",
			expected: availabilityUnknown,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, auxAccountModelAvailability(tc.account, tc.model, now))
		})
	}
}

func TestAuxSchedulerLaneCoverageUnknownSupportMetadataBlocksEscalation(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	missingMetadata := sub2api.Account{ID: 1, Name: "missing metadata", Platform: "openai", Type: "apikey", Status: "active"}
	state := newAuxLaneUpstreamState(missingMetadata, auxModelAccount(2, "ok", "gpt-5"))
	state.setSchedulable(1, true)
	state.setSchedulable(2, false)
	upstream := httptest.NewServer(state.serve(t, "/api/v1/admin/accounts"))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	id := insertAuxLaneRuleRaw(t, store, "missing support", true, [][]int64{{1}, {2}}, []string{"gpt-5"}, 1, 2)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc, id)
	require.Equal(t, "blocked", rule.TransitionStatus)
	require.Contains(t, rule.BlockedReason, "未知观测")
	state.mu.Lock()
	require.Empty(t, state.calls)
	state.mu.Unlock()
}

func TestAuxSchedulerModelCoverageUnion(t *testing.T) {
	observations := map[int64]map[string]auxModelAvailability{
		1: {"gpt-5": availabilityUsable, "o3": availabilityUnavailable},
		2: {"gpt-5": availabilityUnavailable, "o3": availabilityUsable},
	}
	lanes := [][]int64{{1}, {2}}
	missing := auxMissingModelsForPrefix(lanes, observations, []string{"gpt-5", "o3"}, 1)
	require.Equal(t, []string{"o3"}, missing)
	require.Empty(t, auxMissingModelsForPrefix(lanes, observations, []string{"gpt-5", "o3"}, 2))
}
