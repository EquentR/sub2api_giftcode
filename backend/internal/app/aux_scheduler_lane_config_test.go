package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/config"
	"sub2api-giftcode/backend/internal/db"
	"sub2api-giftcode/backend/internal/sub2api"
)

func auxModelAccount(id int64, name string, models ...string) sub2api.Account {
	credentials := map[string]any{}
	if len(models) > 0 {
		raw := make([]any, 0, len(models))
		mapping := map[string]any{}
		for _, model := range models {
			raw = append(raw, model)
			mapping[model] = model
		}
		credentials["upstream_supported_models"] = raw
		credentials["model_mapping"] = mapping
	}
	return sub2api.Account{ID: id, Name: name, Platform: "openai", Type: "apikey", Status: "active", Credentials: credentials}
}

func newAuxLaneUpstream(t *testing.T, accounts ...sub2api.Account) *httptest.Server {
	t.Helper()
	byID := make(map[int64]sub2api.Account, len(accounts))
	for _, account := range accounts {
		byID[account.ID] = account
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/admin/accounts":
			items := make([]sub2api.Account, 0, len(byID))
			for _, account := range byID {
				items = append(items, account)
			}
			writeAuxTestEnvelope(w, map[string]any{
				"items": items, "total": len(items), "page": 1, "page_size": 200, "pages": 1,
			})
		case r.Method == http.MethodGet:
			id, ok := accountIDFromPath(r.URL.Path)
			if !ok {
				http.NotFound(w, r)
				return
			}
			account, ok := byID[id]
			if !ok {
				http.NotFound(w, r)
				return
			}
			writeAuxTestEnvelope(w, account)
		case r.Method == http.MethodPost:
			id, ok := accountIDFromPath(r.URL.Path)
			if !ok {
				http.NotFound(w, r)
				return
			}
			var body struct {
				Schedulable bool `json:"schedulable"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			account := byID[id]
			account.Schedulable = body.Schedulable
			byID[id] = account
			writeAuxTestEnvelope(w, account)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestAuxSchedulerLaneRuleCreateUpdateDelete(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	upstream := newAuxLaneUpstream(t,
		auxModelAccount(1, "lane-one", "gpt-5"),
		auxModelAccount(2, "lane-two", "o3"),
		auxModelAccount(3, "lane-three", "gpt-5", "o3"),
	)
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)

	view, err := svc.CreateAuxSchedulerRule(ctx, AuxSchedulerRuleInput{
		Name:            "model lanes",
		Enabled:         true,
		ModelNames:      []string{"gpt-5", "o3"},
		Lanes:           [][]int64{{1}, {2}},
		MaximumAutoLane: 2,
	})
	require.NoError(t, err)
	require.Empty(t, view.MigrationStatus)
	require.Equal(t, []string{"gpt-5", "o3"}, view.ModelNames)
	require.Equal(t, [][]int64{{1}, {2}}, view.Lanes)
	require.Equal(t, 2, view.MaximumAutoLane)
	require.Equal(t, AuxSchedulerStateIdle, view.State)
	require.Len(t, view.LaneAccounts, 2)

	updated, err := svc.UpdateAuxSchedulerRule(ctx, view.ID, AuxSchedulerRuleInput{
		Name:            "model lanes v2",
		Enabled:         true,
		ModelNames:      []string{"gpt-5", "o3"},
		Lanes:           [][]int64{{1}, {3}},
		MaximumAutoLane: 2,
	})
	require.NoError(t, err)
	require.Equal(t, "model lanes v2", updated.Name)
	require.Equal(t, [][]int64{{1}, {3}}, updated.Lanes)

	require.NoError(t, svc.DeleteAuxSchedulerRule(ctx, view.ID))
	rules, err := svc.ListAuxSchedulerRules(ctx)
	require.NoError(t, err)
	for _, rule := range rules {
		require.NotEqual(t, view.ID, rule.ID)
	}
}

func TestAuxSchedulerLaneRuleRejectsInvalidConfiguration(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	upstream := newAuxLaneUpstream(t,
		auxModelAccount(1, "one", "gpt-5"),
		auxModelAccount(2, "two", "o3"),
	)
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)

	tests := []struct {
		name  string
		input AuxSchedulerRuleInput
	}{
		{name: "empty models", input: AuxSchedulerRuleInput{Name: "bad", Enabled: true, Lanes: [][]int64{{1}, {2}}, MaximumAutoLane: 2}},
		{name: "single lane", input: AuxSchedulerRuleInput{Name: "bad", Enabled: true, ModelNames: []string{"gpt-5"}, Lanes: [][]int64{{1}}, MaximumAutoLane: 1}},
		{name: "empty lane", input: AuxSchedulerRuleInput{Name: "bad", Enabled: true, ModelNames: []string{"gpt-5"}, Lanes: [][]int64{{1}, {}}, MaximumAutoLane: 2}},
		{name: "duplicate account", input: AuxSchedulerRuleInput{Name: "bad", Enabled: true, ModelNames: []string{"gpt-5"}, Lanes: [][]int64{{1, 2}, {2}}, MaximumAutoLane: 2}},
		{name: "auto lane out of range", input: AuxSchedulerRuleInput{Name: "bad", Enabled: true, ModelNames: []string{"gpt-5"}, Lanes: [][]int64{{1}, {2}}, MaximumAutoLane: 3}},
		{name: "auto lane zero", input: AuxSchedulerRuleInput{Name: "bad", Enabled: true, ModelNames: []string{"gpt-5"}, Lanes: [][]int64{{1}, {2}}, MaximumAutoLane: 0}},
		{name: "unsupported model", input: AuxSchedulerRuleInput{Name: "bad", Enabled: true, ModelNames: []string{"claude-3"}, Lanes: [][]int64{{1}, {2}}, MaximumAutoLane: 2}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CreateAuxSchedulerRule(ctx, tc.input)
			require.ErrorIs(t, err, ErrBadRequest)
			rules, err := svc.ListAuxSchedulerRules(ctx)
			require.NoError(t, err)
			require.Empty(t, rules)
		})
	}
}

func TestAuxSchedulerLaneOwnershipIsTransactionalAndReleasedOnDisable(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	upstream := newAuxLaneUpstream(t,
		auxModelAccount(1, "one", "gpt-5"),
		auxModelAccount(2, "two", "o3"),
		auxModelAccount(3, "three", "gpt-5", "o3"),
	)
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)

	ruleA, err := svc.CreateAuxSchedulerRule(ctx, AuxSchedulerRuleInput{
		Name: "A", Enabled: true, ModelNames: []string{"gpt-5", "o3"}, Lanes: [][]int64{{1}, {2}}, MaximumAutoLane: 2,
	})
	require.NoError(t, err)

	_, err = svc.CreateAuxSchedulerRule(ctx, AuxSchedulerRuleInput{
		Name: "B", Enabled: true, ModelNames: []string{"gpt-5", "o3"}, Lanes: [][]int64{{3}, {2}}, MaximumAutoLane: 2,
	})
	require.ErrorIs(t, err, ErrBadRequest)
	rules, err := svc.ListAuxSchedulerRules(ctx)
	require.NoError(t, err)
	require.Len(t, rules, 1)

	_, err = svc.UpdateAuxSchedulerRule(ctx, ruleA.ID, AuxSchedulerRuleInput{
		Name: "A", Enabled: false, ModelNames: []string{"gpt-5", "o3"}, Lanes: [][]int64{{1}, {2}}, MaximumAutoLane: 2,
	})
	require.NoError(t, err)
	ruleB, err := svc.CreateAuxSchedulerRule(ctx, AuxSchedulerRuleInput{
		Name: "B", Enabled: true, ModelNames: []string{"gpt-5", "o3"}, Lanes: [][]int64{{3}, {2}}, MaximumAutoLane: 2,
	})
	require.NoError(t, err)
	require.Equal(t, [][]int64{{3}, {2}}, ruleB.Lanes)
}

func TestAuxSchedulerNeedsMigrationRuleDoesNotOwnAccounts(t *testing.T) {
	ctx := context.Background()
	store := openLegacyAuxSchedulerStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertLegacyAuxSchedulerTestRule(t, store, "legacy", true, []int64{1}, []int64{2}, AuxSchedulerStateBackupActive, now.Add(-time.Hour), now)
	require.NoError(t, store.Migrate(ctx))

	upstream := newAuxLaneUpstream(t,
		auxModelAccount(1, "one", "gpt-5"),
		auxModelAccount(2, "two", "o3"),
	)
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	view, err := svc.CreateAuxSchedulerRule(ctx, AuxSchedulerRuleInput{
		Name: "new", Enabled: true, ModelNames: []string{"gpt-5", "o3"}, Lanes: [][]int64{{1}, {2}}, MaximumAutoLane: 2,
	})
	require.NoError(t, err)
	require.Equal(t, [][]int64{{1}, {2}}, view.Lanes)
}

func TestAuxSchedulerSavingMigratedRuleWithModelsActivatesWithoutUpstreamWrites(t *testing.T) {
	ctx := context.Background()
	store := openLegacyAuxSchedulerStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	id := insertLegacyAuxSchedulerTestRule(t, store, "legacy", true, []int64{1}, []int64{2}, AuxSchedulerStateIdle, now.Add(-time.Hour), now)
	require.NoError(t, store.Migrate(ctx))

	var mu sync.Mutex
	var schedulableWrites int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/api/v1/admin/accounts":
			writeAuxTestEnvelope(w, map[string]any{
				"items": []sub2api.Account{
					auxModelAccount(1, "one", "gpt-5"),
					auxModelAccount(2, "two", "o3"),
				},
				"total":     2,
				"page":      1,
				"page_size": 200,
				"pages":     1,
			})
		case r.URL.Path == "/api/v1/admin/accounts/2/schedulable":
			schedulableWrites++
			writeAuxTestEnvelope(w, auxModelAccount(2, "two", "o3"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)

	view, err := svc.UpdateAuxSchedulerRule(ctx, id, AuxSchedulerRuleInput{
		Name: "legacy activated", Enabled: true, ModelNames: []string{"gpt-5", "o3"}, Lanes: [][]int64{{1}, {2}}, MaximumAutoLane: 2,
	})
	require.NoError(t, err)
	require.Equal(t, "legacy activated", view.Name)
	require.Empty(t, view.MigrationStatus)
	require.Equal(t, []string{"gpt-5", "o3"}, view.ModelNames)
	mu.Lock()
	require.Equal(t, 0, schedulableWrites)
	mu.Unlock()
}

func TestAuxSchedulerLaneRuleNotReconciledByLegacyPathBeforeExecutor(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	primary := auxModelAccount(1, "one", "gpt-5")
	primary.TempUnschedulableUntil = timePtr(time.Now().UTC().Add(5 * time.Minute))
	backup := auxModelAccount(2, "two", "o3")
	var mu sync.Mutex
	var schedulableWrites int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/admin/accounts":
			writeAuxTestEnvelope(w, map[string]any{
				"items":     []sub2api.Account{primary, backup},
				"total":     2,
				"page":      1,
				"page_size": 200,
				"pages":     1,
			})
		case "/api/v1/admin/accounts/1":
			writeAuxTestEnvelope(w, primary)
		case "/api/v1/admin/accounts/2":
			writeAuxTestEnvelope(w, backup)
		case "/api/v1/admin/accounts/2/schedulable":
			schedulableWrites++
			writeAuxTestEnvelope(w, backup)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)

	view, err := svc.CreateAuxSchedulerRule(ctx, AuxSchedulerRuleInput{
		Name: "protected", Enabled: true, ModelNames: []string{"gpt-5"}, Lanes: [][]int64{{1}, {2}}, MaximumAutoLane: 1,
	})
	require.NoError(t, err)
	require.NotZero(t, view.ID)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	_, err = svc.CheckAuxSchedulerRule(ctx, view.ID)
	require.NoError(t, err)
	mu.Lock()
	require.Equal(t, 0, schedulableWrites)
	mu.Unlock()
}

func TestAuxSchedulerLegacyShapeWithPersistedLanesStillReconciles(t *testing.T) {
	ctx := context.Background()
	store, err := dbOpenMemory(t)
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Second)
	_, err = store.DB.ExecContext(ctx, `
INSERT INTO aux_scheduler_rules (
  name, enabled, primary_account_ids_json, backup_account_ids_json,
  model_names_json, lanes_json, maximum_auto_lane, migration_status, migration_source_json,
  state, expected_open_through_lane, observed_open_through_lane, verified_open_through_lane,
  target_open_through_lane, transition_status, transition_generation,
  upgrade_evidence_json, missing_models_json,
  created_at, updated_at
) VALUES (?, 1, '[1]', '[2]', '[]', '[[1],[2]]', 2, '', '',
         'idle', 1, 1, 1, 1, 'stable', 0, '{}', '[]', ?, ?)
`, "legacy shape", formatTime(now), formatTime(now))
	require.NoError(t, err)

	primary := auxModelAccount(1, "one", "gpt-5")
	primary.TempUnschedulableUntil = timePtr(time.Now().UTC().Add(5 * time.Minute))
	backup := auxModelAccount(2, "two", "o3")
	var mu sync.Mutex
	var schedulableWrites []bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/admin/accounts":
			writeAuxTestEnvelope(w, map[string]any{
				"items":     []sub2api.Account{primary, backup},
				"total":     2,
				"page":      1,
				"page_size": 200,
				"pages":     1,
			})
		case "/api/v1/admin/accounts/1":
			writeAuxTestEnvelope(w, primary)
		case "/api/v1/admin/accounts/2":
			writeAuxTestEnvelope(w, backup)
		case "/api/v1/admin/accounts/2/schedulable":
			var body struct {
				Schedulable bool `json:"schedulable"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			backup.Schedulable = body.Schedulable
			schedulableWrites = append(schedulableWrites, body.Schedulable)
			writeAuxTestEnvelope(w, backup)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rules, err := svc.ListAuxSchedulerRules(ctx)
	require.NoError(t, err)
	require.Len(t, rules, 1)
	require.Equal(t, AuxSchedulerStateBackupActive, rules[0].State)
	mu.Lock()
	require.Equal(t, []bool{true}, schedulableWrites)
	mu.Unlock()
}

func dbOpenMemory(t *testing.T) (*db.Store, error) {
	t.Helper()
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))
	return store, nil
}
