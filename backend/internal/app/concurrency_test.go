package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/config"
	"sub2api-giftcode/backend/internal/db"
	"sub2api-giftcode/backend/internal/mail"
	"sub2api-giftcode/backend/internal/sub2api"
)

func TestReconcileSubscriptionConcurrencyUsesMaximumGrant(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	now := time.Now().UTC().Truncate(time.Second)
	var updated int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/users/1":
			if r.Method == http.MethodGet {
				writeRedeemTestEnvelope(w, map[string]any{"id": 1, "concurrency": 2})
				return
			}
			require.Equal(t, http.MethodPut, r.Method)
			updated++
			writeRedeemTestEnvelope(w, map[string]any{"id": 1, "concurrency": 12})
		case "/api/v1/admin/subscriptions":
			writeRedeemTestEnvelope(w, map[string]any{"items": []map[string]any{{"id": 41, "user_id": 1, "group_id": 7, "status": "active", "expires_at": now.Add(time.Hour).Format(time.RFC3339Nano)}}, "total": 1, "pages": 1})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	require.NoError(t, insertConcurrencyGrantFixture(context.Background(), store, 101, 1, 7, 8, "direct_charge", now))
	require.NoError(t, insertConcurrencyGrantFixture(context.Background(), store, 102, 1, 7, 12, "direct_charge", now))
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "key"), mail.New(mail.Config{}))
	first, err := svc.getAccessRequestByID(context.Background(), 101)
	require.NoError(t, err)
	require.NoError(t, svc.ensureSubscriptionConcurrencyGrant(context.Background(), first))
	second, err := svc.getAccessRequestByID(context.Background(), 102)
	require.NoError(t, err)
	require.NoError(t, svc.ensureSubscriptionConcurrencyGrant(context.Background(), second))

	require.NoError(t, svc.ReconcileSubscriptionConcurrency(context.Background()))
	require.Equal(t, 1, updated)

	var active, desired int
	require.NoError(t, store.DB.QueryRow(`SELECT COUNT(1), MAX(desired_concurrency) FROM subscription_concurrency_grants WHERE status = 'active'`).Scan(&active, &desired))
	require.Equal(t, 2, active)
	require.Equal(t, 12, desired)
}

func TestSubscriptionConcurrencyMonitorStatusAggregatesGrantsAndLiveDefault(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	now := time.Now().UTC().Truncate(time.Second)
	for i, status := range []string{"active", "pending", "inactive", "inactive"} {
		_, err := store.DB.Exec(`INSERT INTO subscription_concurrency_grants (access_request_id, upstream_user_id, tier_id, sub2api_group_id, desired_concurrency, status, created_at, updated_at) VALUES (?, 1, 1, 7, 12, ?, ?, ?)`, i+1, status, formatTime(now), formatTime(now))
		require.NoError(t, err)
	}
	_, err = store.DB.Exec(`UPDATE subscription_concurrency_grants SET last_error = 'retry failed' WHERE access_request_id = 4`)
	require.NoError(t, err)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/admin/settings", r.URL.Path)
		writeRedeemTestEnvelope(w, map[string]any{"default_concurrency": 4})
	}))
	t.Cleanup(upstream.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "key"), mail.New(mail.Config{}))
	require.NoError(t, svc.setSyncState(context.Background(), subscriptionConcurrencyLastReconciliationAtKey, formatTime(now), now))
	require.NoError(t, svc.setSyncState(context.Background(), subscriptionConcurrencyLatestErrorKey, "user 9: upstream unavailable", now))
	require.NoError(t, svc.setSyncState(context.Background(), subscriptionConcurrencyLatestErrorAtKey, formatTime(now), now))

	status, err := svc.SubscriptionConcurrencyMonitorStatus(context.Background())
	require.NoError(t, err)
	require.Equal(t, 4, status.DefaultConcurrency)
	require.Empty(t, status.DefaultConcurrencyError)
	require.Equal(t, &now, status.LastReconciliationAt)
	require.Equal(t, 1, status.ActiveGrants)
	require.Equal(t, 1, status.PendingGrants)
	require.Equal(t, 2, status.InactiveGrants)
	require.Equal(t, 1, status.ErrorGrants)
	require.Equal(t, "user 9: upstream unavailable", status.LatestError)
	require.Equal(t, &now, status.LatestErrorAt)
}

func TestSubscriptionConcurrencyMonitorStatusReportsDefaultReadFailure(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeRedeemTestEnvelope(w, map[string]any{"default_concurrency": 0})
	}))
	t.Cleanup(upstream.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "key"), mail.New(mail.Config{}))

	status, err := svc.SubscriptionConcurrencyMonitorStatus(context.Background())
	require.NoError(t, err)
	require.Zero(t, status.DefaultConcurrency)
	require.Contains(t, status.DefaultConcurrencyError, "invalid default concurrency")
}

func TestReconcileSubscriptionConcurrencyRecordsRunFailureAndReturnsIt(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, insertConcurrencyGrantFixture(context.Background(), store, 101, 1, 7, 12, "direct_charge", now))
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}))
	t.Cleanup(upstream.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "key"), mail.New(mail.Config{}))

	err = svc.ReconcileSubscriptionConcurrency(context.Background())
	require.Error(t, err)
	status, statusErr := svc.SubscriptionConcurrencyMonitorStatus(context.Background())
	require.NoError(t, statusErr)
	require.NotNil(t, status.LastReconciliationAt)
	require.Contains(t, status.LatestError, "user 1")
	require.NotNil(t, status.LatestErrorAt)
}

func TestReconcileSubscriptionConcurrencyReturnsMetadataPersistenceFailures(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))
	_, err = store.DB.Exec(`DROP TABLE sync_state`)
	require.NoError(t, err)

	svc := New(&config.RuntimeConfig{}, store, nil, mail.New(mail.Config{}))
	err = svc.ReconcileSubscriptionConcurrency(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "sub2api client not configured")
	require.Contains(t, err.Error(), "sync_state")
}

func TestRecordSubscriptionConcurrencyReconciliationRollsBackMetadataAsATuple(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	oldTime := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	for _, state := range []struct{ key, value string }{
		{subscriptionConcurrencyLastReconciliationAtKey, formatTime(oldTime)},
		{subscriptionConcurrencyLatestErrorKey, "old error"},
		{subscriptionConcurrencyLatestErrorAtKey, formatTime(oldTime)},
	} {
		require.NoError(t, (&Service{store: store}).setSyncState(context.Background(), state.key, state.value, oldTime))
	}
	_, err = store.DB.Exec(`CREATE TRIGGER reject_monitor_error_update BEFORE UPDATE ON sync_state
		WHEN NEW.key = 'subscription_concurrency_latest_error'
		BEGIN SELECT RAISE(ABORT, 'metadata write failed'); END`)
	require.NoError(t, err)

	err = (&Service{store: store}).recordSubscriptionConcurrencyReconciliation(errors.New("new error"))
	require.ErrorContains(t, err, "metadata write failed")
	rows, err := store.DB.Query(`SELECT key, value FROM sync_state WHERE key LIKE 'subscription_concurrency_%' ORDER BY key`)
	require.NoError(t, err)
	defer rows.Close()
	states := map[string]string{}
	for rows.Next() {
		var key, value string
		require.NoError(t, rows.Scan(&key, &value))
		states[key] = value
	}
	require.NoError(t, rows.Err())
	require.Equal(t, formatTime(oldTime), states[subscriptionConcurrencyLastReconciliationAtKey])
	require.Equal(t, "old error", states[subscriptionConcurrencyLatestErrorKey])
	require.Equal(t, formatTime(oldTime), states[subscriptionConcurrencyLatestErrorAtKey])
}

func TestReconcileSubscriptionConcurrencyFallsBackToLiveDefault(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))
	now := time.Now().UTC().Truncate(time.Second)
	var target int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/users/1":
			if r.Method == http.MethodGet {
				writeRedeemTestEnvelope(w, map[string]any{"id": 1, "concurrency": 12})
				return
			}
			var body map[string]int
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			target = body["concurrency"]
			writeRedeemTestEnvelope(w, map[string]any{"id": 1, "concurrency": 3})
		case "/api/v1/admin/subscriptions":
			writeRedeemTestEnvelope(w, map[string]any{"items": []any{}, "total": 0, "pages": 1})
		case "/api/v1/admin/settings":
			writeRedeemTestEnvelope(w, map[string]any{"default_concurrency": 3})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	require.NoError(t, insertConcurrencyGrantFixture(context.Background(), store, 101, 1, 7, 12, "direct_charge", now))
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "key"), mail.New(mail.Config{}))
	req, err := svc.getAccessRequestByID(context.Background(), 101)
	require.NoError(t, err)
	require.NoError(t, svc.ensureSubscriptionConcurrencyGrant(context.Background(), req))
	require.NoError(t, svc.ReconcileSubscriptionConcurrency(context.Background()))
	require.Equal(t, 3, target)
	var status string
	require.NoError(t, store.DB.QueryRow(`SELECT status FROM subscription_concurrency_grants`).Scan(&status))
	require.Equal(t, "inactive", status)
}

func TestReconcileSubscriptionConcurrencyReadFailurePreservesStatus(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, insertConcurrencyGrantFixture(context.Background(), store, 101, 1, 7, 12, "direct_charge", now))
	_, err = store.DB.Exec(`INSERT INTO subscription_concurrency_grants (access_request_id, upstream_user_id, tier_id, sub2api_group_id, desired_concurrency, status, created_at, updated_at) VALUES (101, 1, 1, 7, 12, 'active', ?, ?)`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	require.NoError(t, err)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "offline", http.StatusBadGateway) }))
	t.Cleanup(upstream.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "key"), mail.New(mail.Config{}))
	require.Error(t, svc.ReconcileSubscriptionConcurrency(context.Background()))
	var status, lastError string
	require.NoError(t, store.DB.QueryRow(`SELECT status, last_error FROM subscription_concurrency_grants`).Scan(&status, &lastError))
	require.Equal(t, "active", status)
	require.NotEmpty(t, lastError)
}

func TestReconcileSubscriptionConcurrencySubscriptionReadFailurePreservesActiveGrantAndSkipsUpdate(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, insertConcurrencyGrantFixture(context.Background(), store, 101, 1, 7, 12, "direct_charge", now))
	_, err = store.DB.Exec(`INSERT INTO subscription_concurrency_grants (access_request_id, upstream_user_id, tier_id, sub2api_group_id, desired_concurrency, status, created_at, updated_at) VALUES (101, 1, 1, 7, 12, 'active', ?, ?)`, formatTime(now), formatTime(now))
	require.NoError(t, err)
	putCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/users/1":
			if r.Method == http.MethodPut {
				putCalls++
			}
			writeRedeemTestEnvelope(w, map[string]any{"id": 1, "concurrency": 2})
		case "/api/v1/admin/subscriptions":
			http.Error(w, "subscriptions unavailable", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "key"), mail.New(mail.Config{}))
	require.Error(t, svc.ReconcileSubscriptionConcurrency(context.Background()))
	var status, lastError string
	require.NoError(t, store.DB.QueryRow(`SELECT status, last_error FROM subscription_concurrency_grants`).Scan(&status, &lastError))
	require.Equal(t, "active", status)
	require.NotEmpty(t, lastError)
	require.Equal(t, 0, putCalls)
}

func TestReconcileSubscriptionConcurrencyRedeemGateAndRetry(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, insertConcurrencyGrantFixture(context.Background(), store, 101, 1, 7, 12, "redeem_code", now))
	_, err = store.DB.Exec(`INSERT INTO subscription_concurrency_grants (access_request_id, upstream_user_id, tier_id, sub2api_group_id, desired_concurrency, status, created_at, updated_at) VALUES (101, 1, 1, 7, 12, 'pending', ?, ?)`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	require.NoError(t, err)
	_, err = store.DB.Exec(`INSERT INTO redeem_requests (access_request_id, requestor_upstream_user_id, requestor_email, requestor_username, code_type, tier_id, value, sub2api_group_id, validity_days, status, note, upstream_code, error_message, created_at, updated_at) VALUES (101, 1, 'user@example.com', 'user', 'subscription', 1, 0, 7, 30, 'issued', '', 'code', '', ?, ?)`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	require.NoError(t, err)
	_, err = store.DB.Exec(`INSERT INTO redeem_codes (request_id, code, code_type, value, status, used_by_upstream_user_id, validity_days, created_at, updated_at) VALUES (1, 'code', 'subscription', 0, 'unused', NULL, 30, ?, ?)`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	require.NoError(t, err)
	updates := 0
	reads := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/users/1":
			if r.Method == http.MethodGet {
				reads++
				concurrency := 3
				if reads > 1 {
					concurrency = 2
				}
				writeRedeemTestEnvelope(w, map[string]any{"id": 1, "concurrency": concurrency})
			} else {
				updates++
				if updates == 1 {
					http.Error(w, "retry", http.StatusBadGateway)
				} else {
					writeRedeemTestEnvelope(w, map[string]any{"id": 1, "concurrency": 12})
				}
			}
		case "/api/v1/admin/subscriptions":
			writeRedeemTestEnvelope(w, map[string]any{"items": []map[string]any{{"id": 41, "user_id": 1, "group_id": 7, "status": "active", "expires_at": now.Add(time.Hour).Format(time.RFC3339Nano)}}, "total": 1, "pages": 1})
		case "/api/v1/admin/settings":
			writeRedeemTestEnvelope(w, map[string]any{"default_concurrency": 3})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "key"), mail.New(mail.Config{}))
	require.NoError(t, svc.ReconcileSubscriptionConcurrency(context.Background()))
	require.Equal(t, 0, updates)
	var pendingStatus string
	require.NoError(t, store.DB.QueryRow(`SELECT status FROM subscription_concurrency_grants`).Scan(&pendingStatus))
	require.Equal(t, "pending", pendingStatus)
	_, err = store.DB.Exec(`UPDATE redeem_codes SET status = 'used', used_by_upstream_user_id = 1`)
	require.NoError(t, err)
	require.Error(t, svc.ReconcileSubscriptionConcurrency(context.Background()))
	require.Equal(t, 1, updates)
	var status, lastError string
	require.NoError(t, store.DB.QueryRow(`SELECT status, last_error FROM subscription_concurrency_grants`).Scan(&status, &lastError))
	require.Equal(t, "active", status)
	require.NotEmpty(t, lastError)
	require.NoError(t, svc.ReconcileSubscriptionConcurrency(context.Background()))
	require.Equal(t, 2, updates)
	require.NoError(t, store.DB.QueryRow(`SELECT status, last_error FROM subscription_concurrency_grants`).Scan(&status, &lastError))
	require.Equal(t, "active", status)
	require.Empty(t, lastError)
}

func TestReconcileSubscriptionConcurrencySameGroupGrantsBindMergedSubscription(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, insertConcurrencyGrantFixture(context.Background(), store, 101, 1, 7, 12, "direct_charge", now))
	require.NoError(t, insertConcurrencyGrantFixture(context.Background(), store, 102, 1, 7, 12, "direct_charge", now))
	svc := newConcurrencyTestService(t, store, now, 2, []map[string]any{{"id": 77, "user_id": 1, "group_id": 7, "status": "active", "expires_at": now.Add(48 * time.Hour).Format(time.RFC3339Nano)}})
	for _, id := range []int64{101, 102} {
		req, err := svc.getAccessRequestByID(context.Background(), id)
		require.NoError(t, err)
		require.NoError(t, svc.ensureSubscriptionConcurrencyGrant(context.Background(), req))
	}
	require.NoError(t, svc.ReconcileSubscriptionConcurrency(context.Background()))
	rows, err := store.DB.Query(`SELECT status, upstream_subscription_id FROM subscription_concurrency_grants ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()
	count := 0
	for rows.Next() {
		var status string
		var subscriptionID int64
		require.NoError(t, rows.Scan(&status, &subscriptionID))
		require.Equal(t, "active", status)
		require.Equal(t, int64(77), subscriptionID)
		count++
	}
	require.Equal(t, 2, count)
}

func TestReconcileSubscriptionConcurrencyDoesNotRebindMissingSubscription(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, insertConcurrencyGrantFixture(context.Background(), store, 101, 1, 7, 12, "direct_charge", now))
	_, err = store.DB.Exec(`INSERT INTO subscription_concurrency_grants (access_request_id, upstream_user_id, tier_id, sub2api_group_id, desired_concurrency, upstream_subscription_id, status, created_at, updated_at) VALUES (101, 1, 1, 7, 12, 41, 'active', ?, ?)`, formatTime(now), formatTime(now))
	require.NoError(t, err)
	updated := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/users/1":
			if r.Method == http.MethodGet {
				writeRedeemTestEnvelope(w, map[string]any{"id": 1, "concurrency": 12})
				return
			}
			updated++
			var input map[string]int
			require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			require.Equal(t, 3, input["concurrency"])
			writeRedeemTestEnvelope(w, map[string]any{"id": 1, "concurrency": 3})
		case "/api/v1/admin/subscriptions":
			writeRedeemTestEnvelope(w, map[string]any{"items": []map[string]any{{"id": 99, "user_id": 1, "group_id": 7, "status": "active", "expires_at": formatTime(now.Add(time.Hour))}}, "total": 1, "pages": 1})
		case "/api/v1/admin/settings":
			writeRedeemTestEnvelope(w, map[string]any{"default_concurrency": 3})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "key"), mail.New(mail.Config{}))
	require.NoError(t, svc.ReconcileSubscriptionConcurrency(context.Background()))
	require.Equal(t, 1, updated)
	var status string
	var subscriptionID *int64
	require.NoError(t, store.DB.QueryRow(`SELECT status, upstream_subscription_id FROM subscription_concurrency_grants WHERE access_request_id = 101`).Scan(&status, &subscriptionID))
	require.Equal(t, "inactive", status)
	require.Nil(t, subscriptionID)
}

func TestReconcileSubscriptionConcurrencyWrongUserRedeemDoesNotActivateFallbackGrant(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, insertConcurrencyGrantFixture(context.Background(), store, 101, 1, 7, 12, "redeem_code_fallback", now))
	_, err = store.DB.Exec(`INSERT INTO subscription_concurrency_grants (access_request_id, upstream_user_id, tier_id, sub2api_group_id, desired_concurrency, status, created_at, updated_at) VALUES (101, 1, 1, 7, 12, 'pending', ?, ?)`, formatTime(now), formatTime(now))
	require.NoError(t, err)
	_, err = store.DB.Exec(`INSERT INTO redeem_requests (access_request_id, requestor_upstream_user_id, requestor_email, requestor_username, code_type, tier_id, value, sub2api_group_id, validity_days, status, note, upstream_code, error_message, created_at, updated_at) VALUES (101, 1, 'user@example.com', 'user', 'subscription', 1, 0, 7, 30, 'issued', '', 'code', '', ?, ?)`, formatTime(now), formatTime(now))
	require.NoError(t, err)
	_, err = store.DB.Exec(`INSERT INTO redeem_codes (request_id, code, code_type, value, status, used_by_upstream_user_id, validity_days, created_at, updated_at) VALUES (1, 'code', 'subscription', 0, 'used', 2, 30, ?, ?)`, formatTime(now), formatTime(now))
	require.NoError(t, err)
	svc := newConcurrencyTestService(t, store, now, 3, []map[string]any{{"id": 77, "user_id": 1, "group_id": 7, "status": "active", "expires_at": now.Add(time.Hour).Format(time.RFC3339Nano)}})
	require.NoError(t, svc.ReconcileSubscriptionConcurrency(context.Background()))
	var status string
	require.NoError(t, store.DB.QueryRow(`SELECT status FROM subscription_concurrency_grants`).Scan(&status))
	require.Equal(t, "inactive", status)
}

func newConcurrencyTestService(t *testing.T, store *db.Store, now time.Time, currentConcurrency int, subscriptions []map[string]any) *Service {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/users/1":
			writeRedeemTestEnvelope(w, map[string]any{"id": 1, "concurrency": currentConcurrency})
		case "/api/v1/admin/subscriptions":
			writeRedeemTestEnvelope(w, map[string]any{"items": subscriptions, "total": len(subscriptions), "pages": 1})
		case "/api/v1/admin/settings":
			writeRedeemTestEnvelope(w, map[string]any{"default_concurrency": 3})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	return New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "key"), mail.New(mail.Config{}))
}

func TestEnsureSubscriptionConcurrencyGrantIsIdempotentAndUsesLegacyTierValue(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))
	now := time.Now().UTC().Truncate(time.Second)
	result, err := store.DB.Exec(`INSERT INTO redeem_tiers (code_type, amount, pay_amount_cny, label, enabled, sort_order, sub2api_group_id, validity_days, concurrency, created_at, updated_at) VALUES ('subscription', 0, 1, 'Legacy', 1, 1, 7, 30, 9, ?, ?)`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	require.NoError(t, err)
	tierID, err := result.LastInsertId()
	require.NoError(t, err)
	require.NoError(t, insertConcurrencyGrantFixture(context.Background(), store, 101, 1, 7, 0, "redeem_code", now))
	_, err = store.DB.Exec(`UPDATE redeem_access_requests SET tier_id = ? WHERE id = 101`, tierID)
	require.NoError(t, err)
	svc := New(&config.RuntimeConfig{}, store, nil, mail.New(mail.Config{}))
	req, err := svc.getAccessRequestByID(context.Background(), 101)
	require.NoError(t, err)
	require.NoError(t, svc.ensureSubscriptionConcurrencyGrant(context.Background(), req))
	require.NoError(t, svc.ensureSubscriptionConcurrencyGrant(context.Background(), req))
	var count, desired int
	require.NoError(t, store.DB.QueryRow(`SELECT COUNT(1), MAX(desired_concurrency) FROM subscription_concurrency_grants WHERE access_request_id = 101`).Scan(&count, &desired))
	require.Equal(t, 1, count)
	require.Equal(t, 9, desired)
}

func TestConsumedApprovalReplayKeepsSuccessfulResultWhenGrantRepairFails(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, insertConcurrencyGrantFixture(context.Background(), store, 101, 1, 7, 12, "direct_charge", now))
	_, err = store.DB.Exec(`DROP TABLE subscription_concurrency_grants`)
	require.NoError(t, err)
	svc := New(&config.RuntimeConfig{}, store, nil, mail.New(mail.Config{}))

	req, code, err := svc.ApproveAccessRequestByID(context.Background(), 101)
	require.NoError(t, err)
	require.NotNil(t, req)
	require.Nil(t, code)
}

func TestDirectApprovalGrantFailureDoesNotIssueFallbackCode(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))
	now := time.Now().UTC().Truncate(time.Second)
	result, err := store.DB.Exec(`INSERT INTO redeem_tiers (code_type, amount, pay_amount_cny, label, enabled, sort_order, sub2api_group_id, validity_days, concurrency, created_at, updated_at) VALUES ('subscription', 0, 88, 'Subscription', 1, 1, 7, 30, 12, ?, ?)`, formatTime(now), formatTime(now))
	require.NoError(t, err)
	tierID, err := result.LastInsertId()
	require.NoError(t, err)
	_, err = store.DB.Exec(`INSERT INTO redeem_access_requests (id, requestor_upstream_user_id, requestor_email, requestor_username, tier_id, code_type, tier_label, amount, pay_amount_cny, sub2api_group_id, validity_days, concurrency, status, fulfillment_mode, approval_token_hash, approval_token_expires_at, notification_status, notification_error, created_at, updated_at) VALUES (101, 1, 'user@example.com', 'user', ?, 'subscription', 'Subscription', 0, 88, 7, 30, 12, 'pending', 'direct_charge', 'token', ?, 'sent', '', ?, ?)`, tierID, formatTime(now.Add(time.Hour)), formatTime(now), formatTime(now))
	require.NoError(t, err)
	_, err = store.DB.Exec(`CREATE TRIGGER fail_concurrency_grant BEFORE INSERT ON subscription_concurrency_grants BEGIN SELECT RAISE(ABORT, 'grant persistence failed'); END`)
	require.NoError(t, err)
	fallbackCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/redeem-codes/create-and-redeem":
			writeRedeemTestEnvelope(w, map[string]any{"redeem_code": map[string]any{"id": 501, "code": "giftcode-access-101", "type": "subscription", "value": 88, "status": "used", "used_by": 1, "used_at": formatTime(now), "group_id": 7, "validity_days": 30, "created_at": formatTime(now)}})
		case "/api/v1/admin/redeem-codes/generate":
			fallbackCalls++
			writeRedeemTestEnvelope(w, []any{})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "key"), mail.New(mail.Config{}))
	req, code, err := svc.ApproveAccessRequestByID(context.Background(), 101)
	require.NoError(t, err)
	require.NotNil(t, req)
	require.NotNil(t, code)
	require.Equal(t, "consumed", req.Status)
	require.Equal(t, 0, fallbackCalls)
	require.ErrorContains(t, svc.ReconcileSubscriptionConcurrency(context.Background()), "repair request 101 grant")
}

func TestDirectApprovalReconciliationFailureIsNonfatalAndDoesNotIssueFallback(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))
	now := time.Now().UTC().Truncate(time.Second)
	result, err := store.DB.Exec(`INSERT INTO redeem_tiers (code_type, amount, pay_amount_cny, label, enabled, sort_order, sub2api_group_id, validity_days, concurrency, created_at, updated_at) VALUES ('subscription', 0, 88, 'Subscription', 1, 1, 7, 30, 12, ?, ?)`, formatTime(now), formatTime(now))
	require.NoError(t, err)
	tierID, err := result.LastInsertId()
	require.NoError(t, err)
	_, err = store.DB.Exec(`INSERT INTO redeem_access_requests (id, requestor_upstream_user_id, requestor_email, requestor_username, tier_id, code_type, tier_label, amount, pay_amount_cny, sub2api_group_id, validity_days, concurrency, status, fulfillment_mode, approval_token_hash, approval_token_expires_at, notification_status, notification_error, created_at, updated_at) VALUES (101, 1, 'user@example.com', 'user', ?, 'subscription', 'Subscription', 0, 88, 7, 30, 12, 'pending', 'direct_charge', 'token', ?, 'sent', '', ?, ?)`, tierID, formatTime(now.Add(time.Hour)), formatTime(now), formatTime(now))
	require.NoError(t, err)
	fallbackCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/redeem-codes/create-and-redeem":
			writeRedeemTestEnvelope(w, map[string]any{"redeem_code": map[string]any{"id": 501, "code": "giftcode-access-101", "type": "subscription", "value": 88, "status": "used", "used_by": 1, "used_at": formatTime(now), "group_id": 7, "validity_days": 30, "created_at": formatTime(now)}})
		case "/api/v1/admin/users/1":
			writeRedeemTestEnvelope(w, map[string]any{"id": 1, "concurrency": 2})
		case "/api/v1/admin/subscriptions":
			http.Error(w, "subscriptions unavailable", http.StatusBadGateway)
		case "/api/v1/admin/redeem-codes/generate":
			fallbackCalls++
			writeRedeemTestEnvelope(w, []any{})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "key"), mail.New(mail.Config{}))
	req, code, err := svc.ApproveAccessRequestByID(context.Background(), 101)
	require.NoError(t, err)
	require.NotNil(t, req)
	require.NotNil(t, code)
	require.Equal(t, "consumed", req.Status)
	require.Equal(t, 0, fallbackCalls)
	var count int
	var lastError string
	require.NoError(t, store.DB.QueryRow(`SELECT COUNT(1), MAX(last_error) FROM subscription_concurrency_grants WHERE access_request_id = 101`).Scan(&count, &lastError))
	require.Equal(t, 1, count)
	require.NotEmpty(t, lastError)
}

func TestRedeemCodeApprovalKeepsSuccessfulResultWhenGrantPersistenceFails(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))
	now := time.Now().UTC().Truncate(time.Second)
	result, err := store.DB.Exec(`INSERT INTO redeem_tiers (code_type, amount, pay_amount_cny, label, enabled, sort_order, sub2api_group_id, validity_days, concurrency, created_at, updated_at) VALUES ('subscription', 0, 88, 'Subscription', 1, 1, 7, 30, 12, ?, ?)`, formatTime(now), formatTime(now))
	require.NoError(t, err)
	tierID, err := result.LastInsertId()
	require.NoError(t, err)
	_, err = store.DB.Exec(`INSERT INTO redeem_access_requests (id, requestor_upstream_user_id, requestor_email, requestor_username, tier_id, code_type, tier_label, amount, pay_amount_cny, sub2api_group_id, validity_days, concurrency, status, fulfillment_mode, approval_token_hash, approval_token_expires_at, notification_status, notification_error, created_at, updated_at) VALUES (101, 1, 'user@example.com', 'user', ?, 'subscription', 'Subscription', 0, 88, 7, 30, 12, 'pending', 'redeem_code', 'token', ?, 'sent', '', ?, ?)`, tierID, formatTime(now.Add(time.Hour)), formatTime(now), formatTime(now))
	require.NoError(t, err)
	_, err = store.DB.Exec(`CREATE TRIGGER fail_concurrency_grant BEFORE INSERT ON subscription_concurrency_grants BEGIN SELECT RAISE(ABORT, 'grant persistence failed'); END`)
	require.NoError(t, err)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/admin/groups/all" {
			writeRedeemTestEnvelope(w, []map[string]any{{"id": 7, "name": "Subscription", "status": "active", "subscription_type": "subscription"}})
			return
		}
		if r.URL.Path != "/api/v1/admin/redeem-codes/generate" {
			http.NotFound(w, r)
			return
		}
		writeRedeemTestEnvelope(w, []map[string]any{{"id": 501, "code": "code-501", "type": "subscription", "value": 0, "status": "unused", "group_id": 7, "validity_days": 30, "created_at": formatTime(now)}})
	}))
	t.Cleanup(upstream.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "key"), mail.New(mail.Config{}))
	req, code, err := svc.ApproveAccessRequestByID(context.Background(), 101)
	require.NoError(t, err)
	require.NotNil(t, req)
	require.NotNil(t, code)
	require.Equal(t, "code-501", code.Code)
	var status string
	require.NoError(t, store.DB.QueryRow(`SELECT status FROM redeem_access_requests WHERE id = 101`).Scan(&status))
	require.Equal(t, "consumed", status)
}

func TestReconcileSubscriptionConcurrencyRepairsMissingFulfilledGrant(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, insertConcurrencyGrantFixture(context.Background(), store, 101, 1, 7, 12, "direct_charge", now))
	svc := newConcurrencyTestService(t, store, now, 12, []map[string]any{{"id": 77, "user_id": 1, "group_id": 7, "status": "active", "expires_at": formatTime(now.Add(time.Hour))}})

	require.NoError(t, svc.ReconcileSubscriptionConcurrency(context.Background()))
	var count int
	var status string
	require.NoError(t, store.DB.QueryRow(`SELECT COUNT(1), MAX(status) FROM subscription_concurrency_grants WHERE access_request_id = 101`).Scan(&count, &status))
	require.Equal(t, 1, count)
	require.Equal(t, "active", status)
}

func insertConcurrencyGrantFixture(ctx context.Context, store *db.Store, accessRequestID, userID, groupID int64, concurrency int, fulfilledVia string, now time.Time) error {
	_, err := store.DB.ExecContext(ctx, `
INSERT INTO redeem_access_requests (
  id, requestor_upstream_user_id, requestor_email, requestor_username, tier_id, code_type, tier_label,
  amount, pay_amount_cny, sub2api_group_id, validity_days, concurrency, status, fulfillment_mode,
  fulfillment_result, fulfilled_via, fulfillment_error, approval_token_hash, approval_token_expires_at,
  notification_status, notification_error, created_at, updated_at
) VALUES (?, ?, 'user@example.com', 'user', 1, 'subscription', 'Subscription', 0, 1, ?, 30, ?, 'consumed', 'direct_charge', 'direct_charge_succeeded', ?, '', 'token', ?, 'sent', '', ?, ?)
`, accessRequestID, userID, groupID, concurrency, fulfilledVia, now.Add(time.Hour).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	return err
}
