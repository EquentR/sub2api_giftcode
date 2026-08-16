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

const legacyAuxSchedulerTableSQL = `CREATE TABLE aux_scheduler_rules (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  primary_account_ids_json TEXT NOT NULL DEFAULT '[]',
  backup_account_ids_json TEXT NOT NULL DEFAULT '[]',
  state TEXT NOT NULL DEFAULT 'idle' CHECK (state IN ('idle', 'backup_active')),
  activated_at TEXT NULL,
  last_checked_at TEXT NULL,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

func insertLegacyAuxSchedulerTestRule(t *testing.T, store *db.Store, name string, enabled bool, primaryIDs, backupIDs []int64, state string, createdAt, updatedAt time.Time) int64 {
	t.Helper()
	primaryJSON, err := json.Marshal(primaryIDs)
	require.NoError(t, err)
	backupJSON, err := json.Marshal(backupIDs)
	require.NoError(t, err)
	result, err := store.DB.ExecContext(context.Background(), `
INSERT INTO aux_scheduler_rules (
  name, enabled, primary_account_ids_json, backup_account_ids_json,
  state, last_error, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, '', ?, ?)
`, name, boolToInt(enabled), string(primaryJSON), string(backupJSON), state,
		formatTime(createdAt), formatTime(updatedAt))
	require.NoError(t, err)
	id, err := result.LastInsertId()
	require.NoError(t, err)
	return id
}

func openLegacyAuxSchedulerStore(t *testing.T) *db.Store {
	t.Helper()
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	_, err = store.DB.ExecContext(context.Background(), legacyAuxSchedulerTableSQL)
	require.NoError(t, err)
	return store
}

func TestAuxSchedulerLegacyMigrationConvertsToTwoLanesWithoutInferringModels(t *testing.T) {
	ctx := context.Background()
	store := openLegacyAuxSchedulerStore(t)
	createdAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	updatedAt := createdAt.Add(time.Hour)
	id := insertLegacyAuxSchedulerTestRule(t, store, "legacy fallback", true, []int64{11, 12}, []int64{13, 14}, AuxSchedulerStateBackupActive, createdAt, updatedAt)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/accounts" {
			http.NotFound(w, r)
			return
		}
		writeAuxTestEnvelope(w, map[string]any{
			"items": []sub2api.Account{
				{ID: 11, Name: "primary-a", Platform: "openai", Type: "oauth", Status: "active", Schedulable: true},
				{ID: 12, Name: "primary-b", Platform: "openai", Type: "oauth", Status: "active", Schedulable: true},
				{ID: 13, Name: "backup-a", Platform: "openai", Type: "apikey", Status: "active", Schedulable: true},
				{ID: 14, Name: "backup-b", Platform: "openai", Type: "apikey", Status: "active", Schedulable: false},
			},
			"total":     4,
			"page":      1,
			"page_size": 200,
			"pages":     1,
		})
	}))
	defer upstream.Close()

	require.NoError(t, store.Migrate(ctx))
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	rules, err := svc.ListAuxSchedulerRules(ctx)
	require.NoError(t, err)
	require.Len(t, rules, 1)
	rule := rules[0]

	require.Equal(t, id, rule.ID)
	require.Equal(t, "legacy fallback", rule.Name)
	require.True(t, rule.Enabled)
	require.Equal(t, [][]int64{{11, 12}, {13, 14}}, rule.Lanes)
	require.Empty(t, rule.ModelNames)
	require.Equal(t, "needs_migration", rule.MigrationStatus)
	require.Equal(t, 2, rule.MaximumAutoLane)
	require.Equal(t, createdAt, rule.CreatedAt)
	require.Equal(t, updatedAt, rule.UpdatedAt)
	require.Equal(t, AuxSchedulerStateIdle, rule.State)
	require.Nil(t, rule.ActivatedAt)
	require.NotNil(t, rule.MigrationSource)
	require.Equal(t, AuxSchedulerStateBackupActive, rule.MigrationSource.LegacyState)
	require.Equal(t, []int64{11, 12}, rule.MigrationSource.LegacyPrimaryAccountIDs)
	require.Equal(t, []int64{13, 14}, rule.MigrationSource.LegacyBackupAccountIDs)
	require.Len(t, rule.LaneAccounts, 2)
	require.Equal(t, "primary-a", rule.LaneAccounts[0].Accounts[0].Name)
	require.Equal(t, "backup-b", rule.LaneAccounts[1].Accounts[1].Name)
	require.NotNil(t, rule.LaneAccounts[1].Accounts[0].Schedulable)
	require.True(t, *rule.LaneAccounts[1].Accounts[0].Schedulable)
	require.NotNil(t, rule.LaneAccounts[1].Accounts[1].Schedulable)
	require.False(t, *rule.LaneAccounts[1].Accounts[1].Schedulable)
}

func TestAuxSchedulerLegacyMigrationNeverWritesSchedulableDuringReconcileOrCheck(t *testing.T) {
	ctx := context.Background()
	store := openLegacyAuxSchedulerStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	id := insertLegacyAuxSchedulerTestRule(t, store, "legacy active backups", true, []int64{1}, []int64{2}, AuxSchedulerStateBackupActive, now.Add(-time.Hour), now)

	var mu sync.Mutex
	var schedulableWrites int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/api/v1/admin/accounts":
			writeAuxTestEnvelope(w, map[string]any{
				"items": []sub2api.Account{
					{ID: 1, Name: "primary", Platform: "openai", Type: "oauth", Status: "active", Schedulable: true},
					{ID: 2, Name: "backup", Platform: "openai", Type: "apikey", Status: "active", Schedulable: true},
				},
				"total":     2,
				"page":      1,
				"page_size": 200,
				"pages":     1,
			})
		case r.URL.Path == "/api/v1/admin/accounts/1":
			writeAuxTestEnvelope(w, sub2api.Account{ID: 1, Name: "primary", Platform: "openai", Type: "oauth", Status: "active", Schedulable: true})
		case r.URL.Path == "/api/v1/admin/accounts/2":
			writeAuxTestEnvelope(w, sub2api.Account{ID: 2, Name: "backup", Platform: "openai", Type: "apikey", Status: "active", Schedulable: true})
		case r.URL.Path == "/api/v1/admin/accounts/2/schedulable":
			schedulableWrites++
			writeAuxTestEnvelope(w, sub2api.Account{ID: 2, Name: "backup", Platform: "openai", Type: "apikey", Status: "active", Schedulable: true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	require.NoError(t, store.Migrate(ctx))
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	view, err := svc.CheckAuxSchedulerRule(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "needs_migration", view.MigrationStatus)
	require.NotNil(t, view.LaneAccounts[1].Accounts[0].Schedulable)
	require.True(t, *view.LaneAccounts[1].Accounts[0].Schedulable)
	mu.Lock()
	require.Equal(t, 0, schedulableWrites)
	mu.Unlock()
}

func TestAuxSchedulerLegacyMigrationIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := openLegacyAuxSchedulerStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertLegacyAuxSchedulerTestRule(t, store, "idempotent legacy", true, []int64{1, 2}, []int64{3}, AuxSchedulerStateIdle, now.Add(-time.Hour), now)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/accounts" {
			http.NotFound(w, r)
			return
		}
		writeAuxTestEnvelope(w, map[string]any{"items": []sub2api.Account{}, "total": 0, "page": 1, "page_size": 200, "pages": 0})
	}))
	defer upstream.Close()

	require.NoError(t, store.Migrate(ctx))
	require.NoError(t, store.Migrate(ctx))
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	rules, err := svc.ListAuxSchedulerRules(ctx)
	require.NoError(t, err)
	require.Len(t, rules, 1)
	require.Equal(t, [][]int64{{1, 2}, {3}}, rules[0].Lanes)
	require.Equal(t, "needs_migration", rules[0].MigrationStatus)
	require.Len(t, rules[0].MigrationSource.LegacyPrimaryAccountIDs, 2)
	require.Len(t, rules[0].MigrationSource.LegacyBackupAccountIDs, 1)
}

func TestAuxSchedulerLegacyRuleRemainsVisibleWhenUpstreamAccountListFails(t *testing.T) {
	ctx := context.Background()
	store := openLegacyAuxSchedulerStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertLegacyAuxSchedulerTestRule(t, store, "visible legacy", true, []int64{1}, []int64{2}, AuxSchedulerStateIdle, now.Add(-time.Hour), now)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}))
	defer upstream.Close()

	require.NoError(t, store.Migrate(ctx))
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	rules, err := svc.ListAuxSchedulerRules(ctx)
	require.NoError(t, err)
	require.Len(t, rules, 1)
	require.Equal(t, "needs_migration", rules[0].MigrationStatus)
	require.NotEmpty(t, rules[0].UpstreamError)
	require.Len(t, rules[0].LaneAccounts, 2)
	require.Equal(t, int64(1), rules[0].LaneAccounts[0].Accounts[0].ID)
	require.Nil(t, rules[0].LaneAccounts[0].Accounts[0].Schedulable)
	raw, err := json.Marshal(rules[0])
	require.NoError(t, err)
	require.NotContains(t, string(raw), `"schedulable"`)
}
