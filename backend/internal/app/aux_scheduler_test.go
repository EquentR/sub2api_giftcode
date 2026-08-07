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
	"sub2api-giftcode/backend/internal/models"
	"sub2api-giftcode/backend/internal/sub2api"
)

func TestAuxSchedulerActivatesBackupsThenDeactivatesAfterSuccess(t *testing.T) {
	ctx := context.Background()
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(ctx))

	var mu sync.Mutex
	var primary sub2api.Account
	var backup sub2api.Account
	var usageCreatedAt time.Time
	var schedulableUpdates []bool

	primary = sub2api.Account{
		ID: 1, Name: "primary", Platform: "openai", Type: "oauth", Status: "active",
		Extra: map[string]any{
			"model_rate_limits": map[string]any{
				"gpt-5": map[string]any{
					"rate_limit_reset_at": time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339),
				},
			},
		},
	}
	backup = sub2api.Account{ID: 2, Name: "backup", Platform: "openai", Type: "apikey", Status: "active", Schedulable: false}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/accounts":
			writeAuxTestEnvelope(w, map[string]any{
				"items":     []sub2api.Account{primary, backup},
				"total":     2,
				"page":      1,
				"page_size": 200,
				"pages":     1,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/accounts/1":
			writeAuxTestEnvelope(w, primary)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/accounts/2":
			writeAuxTestEnvelope(w, backup)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/accounts/2/schedulable":
			var body struct {
				Schedulable bool `json:"schedulable"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			backup.Schedulable = body.Schedulable
			schedulableUpdates = append(schedulableUpdates, body.Schedulable)
			writeAuxTestEnvelope(w, backup)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/usage":
			items := []map[string]any{}
			if !usageCreatedAt.IsZero() {
				items = append(items, map[string]any{"created_at": usageCreatedAt.Format(time.RFC3339Nano)})
			}
			writeAuxTestEnvelope(w, map[string]any{
				"items":     items,
				"total":     len(items),
				"page":      1,
				"page_size": 1,
				"pages":     len(items),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	ruleID := insertAuxTestRule(t, store, "primary fallback", true, []int64{1}, []int64{2}, AuxSchedulerStateIdle)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc, ruleID)
	require.Equal(t, AuxSchedulerStateBackupActive, rule.State)
	require.NotNil(t, rule.ActivatedAt)
	require.Equal(t, []bool{true}, schedulableUpdates)

	// Cooldown cleared but no successful call yet: backups must stay active.
	mu.Lock()
	primary.Extra = nil
	primary.LastUsedAt = nil
	mu.Unlock()
	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule = currentAuxSchedulerRule(t, svc, ruleID)
	require.Equal(t, AuxSchedulerStateBackupActive, rule.State)

	// A same-day usage log before activation must not close the backups.
	mu.Lock()
	usageCreatedAt = rule.ActivatedAt.Add(-time.Minute)
	mu.Unlock()
	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule = currentAuxSchedulerRule(t, svc, ruleID)
	require.Equal(t, AuxSchedulerStateBackupActive, rule.State)

	// A successful usage log after activation closes the backups.
	mu.Lock()
	usageCreatedAt = rule.ActivatedAt.Add(time.Minute)
	mu.Unlock()
	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule = currentAuxSchedulerRule(t, svc, ruleID)
	require.Equal(t, AuxSchedulerStateIdle, rule.State)
	require.False(t, schedulableUpdates[len(schedulableUpdates)-1])
}

func TestAuxSchedulerUsesLastUsedAtAsSuccessSignal(t *testing.T) {
	ctx := context.Background()
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(ctx))

	var mu sync.Mutex
	var schedulableUpdates []bool
	primary := sub2api.Account{
		ID: 1, Name: "primary", Platform: "openai", Type: "oauth", Status: "active",
		TempUnschedulableUntil: timePtr(time.Now().UTC().Add(5 * time.Minute)),
	}
	backup := sub2api.Account{ID: 2, Name: "backup", Platform: "openai", Type: "apikey", Status: "active", Schedulable: false}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/api/v1/admin/accounts":
			writeAuxTestEnvelope(w, map[string]any{
				"items":     []sub2api.Account{primary, backup},
				"total":     2,
				"page":      1,
				"page_size": 200,
				"pages":     1,
			})
		case r.URL.Path == "/api/v1/admin/accounts/1":
			writeAuxTestEnvelope(w, primary)
		case r.URL.Path == "/api/v1/admin/accounts/2" && r.Method == http.MethodGet:
			writeAuxTestEnvelope(w, backup)
		case r.URL.Path == "/api/v1/admin/accounts/2/schedulable":
			var body struct {
				Schedulable bool `json:"schedulable"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			backup.Schedulable = body.Schedulable
			schedulableUpdates = append(schedulableUpdates, body.Schedulable)
			writeAuxTestEnvelope(w, backup)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	ruleID := insertAuxTestRule(t, store, "oauth fallback", true, []int64{1}, []int64{2}, AuxSchedulerStateIdle)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc, ruleID)
	require.Equal(t, AuxSchedulerStateBackupActive, rule.State)

	mu.Lock()
	primary.TempUnschedulableUntil = nil
	lastUsed := time.Now().UTC().Add(2 * time.Minute)
	primary.LastUsedAt = &lastUsed
	mu.Unlock()

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule = currentAuxSchedulerRule(t, svc, ruleID)
	require.Equal(t, AuxSchedulerStateIdle, rule.State)
	require.False(t, schedulableUpdates[len(schedulableUpdates)-1])
}

func TestAuxSchedulerCreateUpdateDelete(t *testing.T) {
	ctx := context.Background()
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(ctx))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/accounts" {
			http.NotFound(w, r)
			return
		}
		writeAuxTestEnvelope(w, map[string]any{
			"items": []sub2api.Account{
				{ID: 1, Name: "primary", Platform: "openai", Type: "oauth", Status: "active"},
				{ID: 2, Name: "backup", Platform: "openai", Type: "apikey", Status: "active"},
			},
			"total":     2,
			"page":      1,
			"page_size": 200,
			"pages":     1,
		})
	}))
	defer upstream.Close()

	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	view, err := svc.CreateAuxSchedulerRule(ctx, AuxSchedulerRuleInput{
		Name: "fallback", Enabled: true, PrimaryAccountIDs: []int64{1}, BackupAccountIDs: []int64{2},
	})
	require.NoError(t, err)
	require.Equal(t, AuxSchedulerStateIdle, view.State)
	require.Equal(t, "primary", view.PrimaryAccounts[0].Name)
	require.Equal(t, "backup", view.BackupAccounts[0].Name)

	updated, err := svc.UpdateAuxSchedulerRule(ctx, view.ID, AuxSchedulerRuleInput{
		Name: "fallback v2", Enabled: false, PrimaryAccountIDs: []int64{1}, BackupAccountIDs: []int64{2},
	})
	require.NoError(t, err)
	require.False(t, updated.Enabled)
	require.Equal(t, "fallback v2", updated.Name)

	require.NoError(t, svc.DeleteAuxSchedulerRule(ctx, view.ID))
	remaining, err := svc.ListAuxSchedulerRules(ctx)
	require.NoError(t, err)
	for _, item := range remaining {
		require.NotEqual(t, view.ID, item.ID)
	}
}

func TestAuxSchedulerCreateRejectsInactiveBackupAccount(t *testing.T) {
	ctx := context.Background()
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(ctx))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeAuxTestEnvelope(w, map[string]any{
			"items": []sub2api.Account{
				{ID: 1, Name: "primary", Platform: "openai", Type: "oauth", Status: "active"},
				{ID: 2, Name: "backup", Platform: "openai", Type: "apikey", Status: "inactive"},
			},
			"total":     2,
			"page":      1,
			"page_size": 200,
			"pages":     1,
		})
	}))
	defer upstream.Close()

	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	_, err = svc.CreateAuxSchedulerRule(ctx, AuxSchedulerRuleInput{
		Name: "bad backup", Enabled: true, PrimaryAccountIDs: []int64{1}, BackupAccountIDs: []int64{2},
	})
	require.ErrorIs(t, err, ErrBadRequest)
	require.ErrorContains(t, err, "备用账号 backup (#2) 当前状态必须为 active 或 error")
}

func TestAuxSchedulerBackupAccountUniqueAcrossRules(t *testing.T) {
	ctx := context.Background()
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(ctx))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeAuxTestEnvelope(w, map[string]any{
			"items": []sub2api.Account{
				{ID: 1, Name: "primary-a", Platform: "openai", Type: "oauth", Status: "active"},
				{ID: 2, Name: "backup-a", Platform: "openai", Type: "apikey", Status: "active"},
				{ID: 3, Name: "primary-b", Platform: "openai", Type: "oauth", Status: "active"},
				{ID: 4, Name: "backup-b", Platform: "openai", Type: "apikey", Status: "active"},
			},
			"total":     4,
			"page":      1,
			"page_size": 200,
			"pages":     1,
		})
	}))
	defer upstream.Close()

	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	ruleA, err := svc.CreateAuxSchedulerRule(ctx, AuxSchedulerRuleInput{
		Name: "A", Enabled: true, PrimaryAccountIDs: []int64{1}, BackupAccountIDs: []int64{2},
	})
	require.NoError(t, err)
	_, err = svc.CreateAuxSchedulerRule(ctx, AuxSchedulerRuleInput{
		Name: "B", Enabled: true, PrimaryAccountIDs: []int64{3}, BackupAccountIDs: []int64{4},
	})
	require.NoError(t, err)

	_, err = svc.CreateAuxSchedulerRule(ctx, AuxSchedulerRuleInput{
		Name: "C", Enabled: true, PrimaryAccountIDs: []int64{3}, BackupAccountIDs: []int64{2},
	})
	require.ErrorIs(t, err, ErrBadRequest)
	require.ErrorContains(t, err, "备用账号 backup-a (#2) 已被规则 A 使用")

	_, err = svc.UpdateAuxSchedulerRule(ctx, ruleA.ID, AuxSchedulerRuleInput{
		Name: "A", Enabled: true, PrimaryAccountIDs: []int64{1}, BackupAccountIDs: []int64{4},
	})
	require.ErrorIs(t, err, ErrBadRequest)
	require.ErrorContains(t, err, "备用账号 backup-b (#4) 已被规则 B 使用")

	_, err = svc.UpdateAuxSchedulerRule(ctx, ruleA.ID, AuxSchedulerRuleInput{
		Name: "A", Enabled: true, PrimaryAccountIDs: []int64{1}, BackupAccountIDs: []int64{2},
	})
	require.NoError(t, err)
}

func TestAuxSchedulerActivationRejectsRuntimeInactiveBackup(t *testing.T) {
	ctx := context.Background()
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(ctx))

	primary := sub2api.Account{
		ID: 1, Name: "primary", Platform: "openai", Type: "oauth", Status: "active",
		TempUnschedulableUntil: timePtr(time.Now().UTC().Add(5 * time.Minute)),
	}
	backup := sub2api.Account{ID: 2, Name: "backup", Platform: "openai", Type: "apikey", Status: "inactive", Schedulable: false}
	var schedulableCalls int

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/accounts/1":
			writeAuxTestEnvelope(w, primary)
		case "/api/v1/admin/accounts/2":
			writeAuxTestEnvelope(w, backup)
		case "/api/v1/admin/accounts/2/schedulable":
			schedulableCalls++
			writeAuxTestEnvelope(w, backup)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	ruleID := insertAuxTestRule(t, store, "runtime inactive backup", true, []int64{1}, []int64{2}, AuxSchedulerStateIdle)

	err = svc.ReconcileAuxScheduler(ctx)
	require.Error(t, err)
	rule := currentAuxSchedulerRule(t, svc, ruleID)
	require.Equal(t, AuxSchedulerStateIdle, rule.State)
	require.NotEmpty(t, rule.LastError)
	require.Equal(t, 0, schedulableCalls)
}

func TestAuxSchedulerAccountRoleConflictAcrossRules(t *testing.T) {
	ctx := context.Background()
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(ctx))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeAuxTestEnvelope(w, map[string]any{
			"items": []sub2api.Account{
				{ID: 1, Name: "a", Platform: "openai", Type: "oauth", Status: "active"},
				{ID: 2, Name: "b", Platform: "openai", Type: "apikey", Status: "active"},
				{ID: 3, Name: "c", Platform: "openai", Type: "oauth", Status: "active"},
			},
			"total":     3,
			"page":      1,
			"page_size": 200,
			"pages":     1,
		})
	}))
	defer upstream.Close()

	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	_, err = svc.CreateAuxSchedulerRule(ctx, AuxSchedulerRuleInput{
		Name: "A", Enabled: true, PrimaryAccountIDs: []int64{1}, BackupAccountIDs: []int64{2},
	})
	require.NoError(t, err)

	_, err = svc.CreateAuxSchedulerRule(ctx, AuxSchedulerRuleInput{
		Name: "B", Enabled: true, PrimaryAccountIDs: []int64{2}, BackupAccountIDs: []int64{3},
	})
	require.ErrorIs(t, err, ErrBadRequest)
	require.ErrorContains(t, err, "账号 b (#2) 已在规则 A 中作为备用账号使用")

	_, err = svc.CreateAuxSchedulerRule(ctx, AuxSchedulerRuleInput{
		Name: "C", Enabled: true, PrimaryAccountIDs: []int64{3}, BackupAccountIDs: []int64{1},
	})
	require.ErrorIs(t, err, ErrBadRequest)
	require.ErrorContains(t, err, "账号 a (#1) 已在规则 A 中作为主力账号使用")
}

func TestAuxSchedulerCreateAllowsErrorBackupWithWarning(t *testing.T) {
	ctx := context.Background()
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(ctx))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeAuxTestEnvelope(w, map[string]any{
			"items": []sub2api.Account{
				{ID: 1, Name: "primary", Platform: "openai", Type: "oauth", Status: "active"},
				{ID: 2, Name: "unpaid-backup", Platform: "openai", Type: "apikey", Status: "error"},
			},
			"total":     2,
			"page":      1,
			"page_size": 200,
			"pages":     1,
		})
	}))
	defer upstream.Close()

	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	view, err := svc.CreateAuxSchedulerRule(ctx, AuxSchedulerRuleInput{
		Name: "warn backup", Enabled: true, PrimaryAccountIDs: []int64{1}, BackupAccountIDs: []int64{2},
	})
	require.NoError(t, err)
	require.Equal(t, AuxSchedulerStateIdle, view.State)
	require.Contains(t, view.LastError, "备用账号 unpaid-backup (#2) 当前状态为 error")
}

func TestAuxSchedulerActivationSkipsErrorBackupAndActivatesOthers(t *testing.T) {
	ctx := context.Background()
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(ctx))

	primary := sub2api.Account{
		ID: 1, Name: "primary", Platform: "openai", Type: "oauth", Status: "active",
		TempUnschedulableUntil: timePtr(time.Now().UTC().Add(5 * time.Minute)),
	}
	goodBackup := sub2api.Account{ID: 2, Name: "good-backup", Platform: "openai", Type: "apikey", Status: "active", Schedulable: false}
	errorBackup := sub2api.Account{ID: 3, Name: "error-backup", Platform: "openai", Type: "apikey", Status: "error", Schedulable: false}
	var schedulableIDs []int64

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/accounts":
			writeAuxTestEnvelope(w, map[string]any{
				"items":     []sub2api.Account{primary, goodBackup, errorBackup},
				"total":     3,
				"page":      1,
				"page_size": 200,
				"pages":     1,
			})
		case "/api/v1/admin/accounts/1":
			writeAuxTestEnvelope(w, primary)
		case "/api/v1/admin/accounts/2":
			writeAuxTestEnvelope(w, goodBackup)
		case "/api/v1/admin/accounts/3":
			writeAuxTestEnvelope(w, errorBackup)
		case "/api/v1/admin/accounts/2/schedulable":
			var body struct {
				Schedulable bool `json:"schedulable"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.True(t, body.Schedulable)
			schedulableIDs = append(schedulableIDs, 2)
			goodBackup.Schedulable = true
			writeAuxTestEnvelope(w, goodBackup)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	ruleID := insertAuxTestRule(t, store, "mixed backups", true, []int64{1}, []int64{2, 3}, AuxSchedulerStateIdle)

	require.NoError(t, svc.ReconcileAuxScheduler(ctx))
	rule := currentAuxSchedulerRule(t, svc, ruleID)
	require.Equal(t, AuxSchedulerStateBackupActive, rule.State)
	require.Contains(t, rule.LastError, "备用账号 error-backup (#3) 当前状态为 error")
	require.Equal(t, []int64{2}, schedulableIDs)
}

func TestAuxSchedulerCreateRejectsNonPositiveOnlyAccountIDs(t *testing.T) {
	ctx := context.Background()
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(ctx))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeAuxTestEnvelope(w, map[string]any{
			"items": []sub2api.Account{
				{ID: 2, Name: "backup", Platform: "openai", Type: "apikey", Status: "active"},
			},
			"total":     1,
			"page":      1,
			"page_size": 200,
			"pages":     1,
		})
	}))
	defer upstream.Close()

	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	_, err = svc.CreateAuxSchedulerRule(ctx, AuxSchedulerRuleInput{
		Name: "bad", Enabled: true, PrimaryAccountIDs: []int64{0}, BackupAccountIDs: []int64{2},
	})
	require.ErrorIs(t, err, ErrBadRequest)
}

func currentAuxSchedulerRule(t *testing.T, svc *Service, id int64) models.AuxSchedulerRule {
	t.Helper()
	views, err := svc.ListAuxSchedulerRules(context.Background())
	require.NoError(t, err)
	for _, view := range views {
		if view.ID == id {
			return view.AuxSchedulerRule
		}
	}
	require.FailNowf(t, "aux scheduler rule not found", "rule %d", id)
	return models.AuxSchedulerRule{}
}

func insertAuxTestRule(t *testing.T, store *db.Store, name string, enabled bool, primaryIDs, backupIDs []int64, state string) int64 {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	primaryJSON, err := json.Marshal(primaryIDs)
	require.NoError(t, err)
	backupJSON, err := json.Marshal(backupIDs)
	require.NoError(t, err)
	result, err := store.DB.ExecContext(context.Background(), `
INSERT INTO aux_scheduler_rules (
  name, enabled, primary_account_ids_json, backup_account_ids_json,
  state, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?)
`, name, boolToInt(enabled), string(primaryJSON), string(backupJSON), state,
		formatTime(now), formatTime(now))
	require.NoError(t, err)
	id, err := result.LastInsertId()
	require.NoError(t, err)
	return id
}

func writeAuxTestEnvelope(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "success", "data": data})
}

func timePtr(value time.Time) *time.Time {
	return &value
}
