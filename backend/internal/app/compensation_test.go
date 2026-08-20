package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/config"
	"sub2api-giftcode/backend/internal/db"
	"sub2api-giftcode/backend/internal/models"
	"sub2api-giftcode/backend/internal/sub2api"
)

func TestRunCompensationBatchRecordsSummaryAndDetails(t *testing.T) {
	var (
		balanceBodies []map[string]any
		balanceKeys   []string
		extendedIDs   []int64
		extendKeys    []string
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/users":
			writeEnvelope(w, map[string]any{
				"items": []sub2api.User{
					{ID: 1, Email: "skip@blocked.com", Username: "skip", Status: "active", Balance: 10},
					{ID: 2, Email: "sub@example.com", Username: "sub", Status: "active", Balance: 20},
					{ID: 3, Email: "bal@example.com", Username: "bal", Status: "active", Balance: 5},
					{ID: 4, Email: "zero@example.com", Username: "zero", Status: "active", Balance: 0},
				},
				"total":     4,
				"page":      1,
				"page_size": 100,
				"pages":     1,
			})
		case "/api/v1/admin/subscriptions":
			switch r.URL.Query().Get("user_id") {
			case "2":
				writeEnvelope(w, map[string]any{
					"items": []sub2api.Subscription{
						{ID: 101, UserID: 2, GroupID: 7, Status: "active", ExpiresAt: time.Now().UTC().Add(30 * 24 * time.Hour)},
						{ID: 102, UserID: 2, GroupID: 8, Status: "active", ExpiresAt: time.Now().UTC().Add(40 * 24 * time.Hour)},
					},
					"total":     2,
					"page":      1,
					"page_size": 100,
					"pages":     1,
				})
			default:
				writeEnvelope(w, map[string]any{
					"items":     []sub2api.Subscription{},
					"total":     0,
					"page":      1,
					"page_size": 100,
					"pages":     0,
				})
			}
		case "/api/v1/admin/users/3/balance":
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			balanceBodies = append(balanceBodies, body)
			balanceKeys = append(balanceKeys, r.Header.Get("Idempotency-Key"))
			writeEnvelope(w, sub2api.User{ID: 3, Email: "bal@example.com", Username: "bal", Status: "active", Balance: 15})
		case "/api/v1/admin/subscriptions/101/extend":
			extendedIDs = append(extendedIDs, 101)
			extendKeys = append(extendKeys, r.Header.Get("Idempotency-Key"))
			writeEnvelope(w, sub2api.Subscription{ID: 101, UserID: 2, GroupID: 7, Status: "active", ExpiresAt: time.Now().UTC().Add(60 * 24 * time.Hour)})
		case "/api/v1/admin/subscriptions/102/extend":
			extendedIDs = append(extendedIDs, 102)
			extendKeys = append(extendKeys, r.Header.Get("Idempotency-Key"))
			writeEnvelope(w, sub2api.Subscription{ID: 102, UserID: 2, GroupID: 8, Status: "active", ExpiresAt: time.Now().UTC().Add(70 * 24 * time.Hour)})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	operator := &SessionUser{
		User:    sub2api.User{ID: 900, Email: "admin@example.com", Username: "admin", Role: "admin", Status: "active"},
		IsAdmin: true,
	}

	batch, err := svc.RunCompensationBatch(context.Background(), operator, CompensationBatchInput{
		CompensateSubscriptions: true,
		CompensateBalance:       true,
		SubscriptionDays:        15,
		BalanceAmount:           10,
		ExcludedDomains:         []string{"blocked.com"},
		Note:                    "bulk compensation",
	})
	require.NoError(t, err)
	require.Equal(t, compensationBatchStatusCompleted, batch.Status)
	require.Equal(t, 4, batch.TotalUsers)
	require.Equal(t, 1, batch.ExcludedUsers)
	require.Equal(t, 1, batch.SubscriptionCompensatedUsers)
	require.Equal(t, 1, batch.BalanceCompensatedUsers)
	require.Equal(t, 1, batch.SkippedZeroBalanceUsers)
	require.Equal(t, 0, batch.FailedUsers)
	require.Equal(t, 4, batch.DetailCount)
	require.NotNil(t, batch.CompletedAt)

	require.Equal(t, []int64{101, 102}, extendedIDs)
	require.Equal(t, []string{
		fmt.Sprintf("giftcode-compensation-batch-%s-user-2-subscription-101-extend", batch.BatchKey),
		fmt.Sprintf("giftcode-compensation-batch-%s-user-2-subscription-102-extend", batch.BatchKey),
	}, extendKeys)
	require.Len(t, balanceBodies, 1)
	require.Equal(t, map[string]any{
		"balance":   10.0,
		"operation": "add",
		"notes":     "bulk compensation",
	}, balanceBodies[0])
	require.Equal(t, []string{
		fmt.Sprintf("giftcode-compensation-batch-%s-user-3-balance-with-note", batch.BatchKey),
	}, balanceKeys)

	details, err := svc.ListCompensationBatchDetails(context.Background(), batch.ID)
	require.NoError(t, err)
	require.Len(t, details, 4)
	require.Equal(t, "excluded_domain", details[0].DecisionType)
	require.False(t, details[0].RemarkRequested)
	require.Equal(t, "active_subscription", details[1].DecisionType)
	require.Equal(t, 2, details[1].ActiveSubscriptionCount)
	require.False(t, details[1].RemarkRequested)
	require.Equal(t, "positive_balance", details[2].DecisionType)
	require.True(t, details[2].RemarkRequested)
	require.True(t, details[2].RemarkApplied)
	require.Equal(t, "non_positive_balance", details[3].DecisionType)
	require.False(t, details[3].RemarkRequested)
}

func TestCompensationExtensionEventExtendsResetEntitlements(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	beforeExpires := now.Add(10 * 24 * time.Hour)
	afterExpires := beforeExpires.Add(5 * 24 * time.Hour)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/users":
			writeEnvelope(w, map[string]any{"items": []sub2api.User{{ID: 2, Email: "sub@example.com", Status: "active", Balance: 20}}, "total": 1, "page": 1, "page_size": 100, "pages": 1})
		case "/api/v1/admin/subscriptions":
			writeEnvelope(w, map[string]any{"items": []sub2api.Subscription{{ID: 101, UserID: 2, GroupID: 7, StartsAt: now.Add(-20 * 24 * time.Hour), ExpiresAt: beforeExpires, Status: "active"}}, "total": 1, "page": 1, "page_size": 100, "pages": 1})
		case "/api/v1/admin/subscriptions/101/extend":
			writeEnvelope(w, sub2api.Subscription{ID: 101, UserID: 2, GroupID: 7, StartsAt: now.Add(-20 * 24 * time.Hour), ExpiresAt: afterExpires, Status: "active"})
		default:
			http.NotFound(w, r)
		}

	}))
	t.Cleanup(upstream.Close)
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))
	insertResetPeriodFixture(t, store, 1, 101, 2, 7, 101, 30, 2, now.Add(-20*24*time.Hour), beforeExpires, "active")
	insertResetPeriodFixture(t, store, 2, 102, 2, 7, 101, 30, 1, beforeExpires, beforeExpires.Add(30*24*time.Hour), "scheduled")
	_, err = store.DB.Exec(`UPDATE subscription_reset_periods SET created_at = ?, updated_at = ? WHERE id = 2`, formatTime(now.Add(-time.Hour)), formatTime(now.Add(-time.Hour)))
	require.NoError(t, err)
	insertBonusGrantFixture(t, store, 10, 100, 2, 7, 101, 3, 1, now.Add(-time.Hour), beforeExpires, "active")
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	svc.nowFunc = func() time.Time { return now }
	operator := &SessionUser{User: sub2api.User{ID: 900, Email: "admin@example.com"}, IsAdmin: true}

	batch, err := svc.RunCompensationBatch(context.Background(), operator, CompensationBatchInput{CompensateSubscriptions: true, CompensateBalance: true, SubscriptionDays: 5, BalanceAmount: 10, Note: "incident"})
	require.NoError(t, err)
	require.Equal(t, compensationBatchStatusCompleted, batch.Status)
	var eventStatus, resolution, beforeRaw, afterRaw string
	var baseCount, bonusCount int
	require.NoError(t, store.DB.QueryRow(`SELECT status, resolution, before_expires_at, after_expires_at, applied_base_periods, applied_bonus_grants FROM subscription_extension_events WHERE upstream_subscription_id = 101`).Scan(&eventStatus, &resolution, &beforeRaw, &afterRaw, &baseCount, &bonusCount))
	require.Equal(t, "succeeded", eventStatus)
	require.Equal(t, "applied", resolution)
	require.Equal(t, formatTime(beforeExpires), beforeRaw)
	require.Equal(t, formatTime(afterExpires), afterRaw)
	require.Equal(t, 2, baseCount)
	require.Equal(t, 1, bonusCount)

	var currentEnd, futureStart, futureEnd, bonusEnd string
	var currentLimit, futureLimit, bonusLimit, bonusUsed int
	require.NoError(t, store.DB.QueryRow(`SELECT period_end, reset_limit FROM subscription_reset_periods WHERE id = 1`).Scan(&currentEnd, &currentLimit))
	require.NoError(t, store.DB.QueryRow(`SELECT period_start, period_end, reset_limit FROM subscription_reset_periods WHERE id = 2`).Scan(&futureStart, &futureEnd, &futureLimit))
	require.NoError(t, store.DB.QueryRow(`SELECT expires_at, reset_limit, reset_used FROM subscription_reset_bonus_grants WHERE id = 10`).Scan(&bonusEnd, &bonusLimit, &bonusUsed))
	require.Equal(t, formatTime(beforeExpires.Add(5*24*time.Hour)), currentEnd)
	require.Equal(t, formatTime(beforeExpires.Add(5*24*time.Hour)), futureStart)
	require.Equal(t, formatTime(beforeExpires.Add(35*24*time.Hour)), futureEnd)
	require.Equal(t, formatTime(beforeExpires.Add(5*24*time.Hour)), bonusEnd)
	require.Equal(t, 2, currentLimit)
	require.Equal(t, 1, futureLimit)
	require.Equal(t, 3, bonusLimit)
	require.Equal(t, 1, bonusUsed)

	require.NoError(t, svc.MigrateLegacySubscriptionExtensionEvents(context.Background()))
	var eventCount int
	require.NoError(t, store.DB.QueryRow(`SELECT COUNT(1) FROM subscription_extension_events WHERE upstream_subscription_id = 101`).Scan(&eventCount))
	require.Equal(t, 1, eventCount, "a live extension event must not be migrated again as legacy")
	require.NoError(t, store.DB.QueryRow(`SELECT period_end FROM subscription_reset_periods WHERE id = 1`).Scan(&currentEnd))
	require.Equal(t, formatTime(beforeExpires.Add(5*24*time.Hour)), currentEnd)
}

func TestRunCompensationBatchBalanceOnlyOptionCompensatesNonPositiveBalances(t *testing.T) {
	var subscriptionLookups, balanceUpdates int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/users":
			writeEnvelope(w, map[string]any{
				"items": []sub2api.User{
					{ID: 11, Email: "sub@example.com", Status: "active", Balance: 10},
					{ID: 12, Email: "balance@example.com", Status: "active", Balance: 5},
					{ID: 13, Email: "zero@example.com", Status: "active", Balance: 0},
				},
				"total": 3, "page": 1, "page_size": 100, "pages": 1,
			})
		case "/api/v1/admin/subscriptions":
			subscriptionLookups++
			writeEnvelope(w, map[string]any{"items": []sub2api.Subscription{{ID: 101, UserID: 11, Status: "active"}}, "total": 1, "page": 1, "page_size": 100, "pages": 1})
		case "/api/v1/admin/users/11/balance", "/api/v1/admin/users/12/balance", "/api/v1/admin/users/13/balance":
			balanceUpdates++
			writeEnvelope(w, sub2api.User{Balance: 15})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	operator := &SessionUser{User: sub2api.User{ID: 900}, IsAdmin: true}
	batch, err := svc.RunCompensationBatch(context.Background(), operator, CompensationBatchInput{
		CompensateBalance:            true,
		CompensateNonPositiveBalance: true,
		BalanceAmount:                5,
	})
	require.NoError(t, err)
	require.False(t, batch.CompensateSubscriptions)
	require.True(t, batch.CompensateBalance)
	require.True(t, batch.CompensateNonPositiveBalance)
	require.Equal(t, 0, subscriptionLookups)
	require.Equal(t, 3, balanceUpdates)
	require.Equal(t, 3, batch.BalanceCompensatedUsers)
	require.Equal(t, 0, batch.SkippedZeroBalanceUsers)

	details, err := svc.ListCompensationBatchDetails(context.Background(), batch.ID)
	require.NoError(t, err)
	var nonPositiveBalanceDetail *models.CompensationBatchDetail
	for i := range details {
		if details[i].UpstreamUserID == 13 {
			nonPositiveBalanceDetail = &details[i]
			break
		}
	}
	require.NotNil(t, nonPositiveBalanceDetail)
	require.Equal(t, "non_positive_balance", nonPositiveBalanceDetail.DecisionType)
	require.Equal(t, "success", nonPositiveBalanceDetail.Status)
}

func TestRunCompensationBatchIgnoresNonPositiveBalanceOptionWhenSubscriptionsAreCompensated(t *testing.T) {
	input := normalizeCompensationBatchInput(CompensationBatchInput{
		CompensateSubscriptions:      true,
		CompensateBalance:            true,
		CompensateNonPositiveBalance: true,
	})
	require.False(t, input.CompensateNonPositiveBalance)
}

func TestSubscriptionExtensionAppliedAfterOriginalBonusExpiryIsIdempotentAndSkipsLaterGrant(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	store := newResetPeriodTestStore(t)
	insertResetPeriodFixture(t, store, 1, 101, 2, 7, 101, 30, 2, now.Add(-20*24*time.Hour), now.Add(10*24*time.Hour), "active")
	insertBonusGrantFixture(t, store, 10, 100, 2, 7, 101, 2, 0, now.Add(-time.Hour), now.Add(24*time.Hour), "expired")
	_, err := store.DB.Exec(`
INSERT INTO subscription_extension_events (
  id, event_key, source_type, upstream_user_id, sub2api_group_id, upstream_subscription_id,
  extension_days, before_expires_at, status, reserved_at, created_at, updated_at
) VALUES (1, 'manual-uncertain', 'compensation', 2, 7, 101, 5, ?, 'uncertain', ?, ?, ?)
`, formatTime(now.Add(10*24*time.Hour)), formatTime(now), formatTime(now), formatTime(now))
	require.NoError(t, err)
	insertBonusGrantFixture(t, store, 11, 101, 2, 7, 101, 1, 0, now, now.Add(10*24*time.Hour), "active")
	_, err = store.DB.Exec(`UPDATE subscription_reset_bonus_grants SET created_at = ?, updated_at = ? WHERE id = 11`, formatTime(now.Add(time.Minute)), formatTime(now.Add(time.Minute)))
	require.NoError(t, err)
	svc := New(&config.RuntimeConfig{}, store, nil, nil)
	svc.nowFunc = func() time.Time { return now.Add(2 * 24 * time.Hour) }

	event, err := svc.ResolveSubscriptionExtensionEvent(context.Background(), 1, 99, "applied")
	require.NoError(t, err)
	require.Equal(t, "succeeded", event.Status)
	require.Equal(t, "applied", event.Resolution)
	replayed, err := svc.ResolveSubscriptionExtensionEvent(context.Background(), 1, 99, "applied")
	require.NoError(t, err)
	require.Equal(t, event.ID, replayed.ID)
	_, err = svc.ResolveSubscriptionExtensionEvent(context.Background(), 1, 99, "released")
	require.ErrorIs(t, err, ErrConflict)

	var earlyExpiry, lateExpiry string
	require.NoError(t, store.DB.QueryRow(`SELECT expires_at FROM subscription_reset_bonus_grants WHERE id = 10`).Scan(&earlyExpiry))
	require.NoError(t, store.DB.QueryRow(`SELECT expires_at FROM subscription_reset_bonus_grants WHERE id = 11`).Scan(&lateExpiry))
	require.Equal(t, formatTime(now.Add(6*24*time.Hour)), earlyExpiry)
	require.Equal(t, formatTime(now.Add(10*24*time.Hour)), lateExpiry)
}

func TestResolveSubscriptionExtensionEventReleasedDoesNotChangeEntitlements(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	store := newResetPeriodTestStore(t)
	insertBonusGrantFixture(t, store, 10, 100, 2, 7, 101, 2, 0, now.Add(-time.Hour), now.Add(10*24*time.Hour), "active")
	_, err := store.DB.Exec(`
INSERT INTO subscription_extension_events (
  id, event_key, source_type, upstream_user_id, sub2api_group_id, upstream_subscription_id,
  extension_days, before_expires_at, status, reserved_at, created_at, updated_at
) VALUES (1, 'manual-release', 'compensation', 2, 7, 101, 5, ?, 'uncertain', ?, ?, ?)
`, formatTime(now.Add(10*24*time.Hour)), formatTime(now), formatTime(now), formatTime(now))
	require.NoError(t, err)
	svc := New(&config.RuntimeConfig{}, store, nil, nil)

	event, err := svc.ResolveSubscriptionExtensionEvent(context.Background(), 1, 99, "released")
	require.NoError(t, err)
	require.Equal(t, "failed", event.Status)
	require.Equal(t, "released", event.Resolution)
	var expiry string
	require.NoError(t, store.DB.QueryRow(`SELECT expires_at FROM subscription_reset_bonus_grants WHERE id = 10`).Scan(&expiry))
	require.Equal(t, formatTime(now.Add(10*24*time.Hour)), expiry)
}

func TestLegacyExtensionAppliesToRebuiltBasePeriodAndSkipsLaterBonus(t *testing.T) {
	store := newResetPeriodTestStore(t)
	eventAt := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	periodEnd := eventAt.Add(10 * 24 * time.Hour)
	_, err := store.DB.Exec(`
INSERT INTO compensation_batches (
  id, batch_key, subscription_days, balance_amount, status, operator_upstream_user_id,
  created_at, updated_at
) VALUES (1, 'legacy-batch', 4, 10, 'completed', 99, ?, ?);
INSERT INTO compensation_batch_details (
  id, batch_id, detail_key, upstream_user_id, action_type, subscription_days,
  status, upstream_reference_json, created_at, updated_at
) VALUES (1, 1, 'legacy-detail', 2, 'subscription', 4, 'success',
  '{"extended_ids":[101]}', ?, ?);
`, formatTime(eventAt), formatTime(eventAt), formatTime(eventAt), formatTime(eventAt))
	require.NoError(t, err)
	insertResetPeriodFixture(t, store, 1, 101, 2, 7, 101, 30, 2, eventAt.Add(-20*24*time.Hour), periodEnd, "active")
	_, err = store.DB.Exec(`UPDATE subscription_reset_periods SET status = 'expired', created_at = ?, updated_at = ? WHERE id = 1`, formatTime(eventAt.Add(24*time.Hour)), formatTime(eventAt.Add(24*time.Hour)))
	require.NoError(t, err)
	insertBonusGrantFixture(t, store, 10, 100, 2, 7, 101, 2, 0, eventAt, periodEnd, "active")
	_, err = store.DB.Exec(`UPDATE subscription_reset_bonus_grants SET created_at = ?, updated_at = ? WHERE id = 10`, formatTime(eventAt.Add(time.Hour)), formatTime(eventAt.Add(time.Hour)))
	require.NoError(t, err)
	svc := New(&config.RuntimeConfig{}, store, nil, nil)

	require.NoError(t, svc.MigrateLegacySubscriptionExtensionEvents(context.Background()))
	require.NoError(t, svc.MigrateLegacySubscriptionExtensionEvents(context.Background()))
	var eventCount int
	var source, status, resolution string
	var inferred, version int
	require.NoError(t, store.DB.QueryRow(`SELECT COUNT(1), source_type, status, resolution, inferred_from_legacy, migration_version FROM subscription_extension_events`).Scan(&eventCount, &source, &status, &resolution, &inferred, &version))
	require.Equal(t, 1, eventCount)
	require.Equal(t, "legacy_compensation", source)
	require.Equal(t, "succeeded", status)
	require.Equal(t, "applied", resolution)
	require.Equal(t, 1, inferred)
	require.Equal(t, 1, version)
	var migratedPeriodEnd, laterBonusEnd string
	require.NoError(t, store.DB.QueryRow(`SELECT period_end FROM subscription_reset_periods WHERE id = 1`).Scan(&migratedPeriodEnd))
	require.NoError(t, store.DB.QueryRow(`SELECT expires_at FROM subscription_reset_bonus_grants WHERE id = 10`).Scan(&laterBonusEnd))
	require.Equal(t, formatTime(periodEnd.Add(4*24*time.Hour)), migratedPeriodEnd)
	require.Equal(t, formatTime(periodEnd), laterBonusEnd)
}

func TestRecoverStaleSubscriptionExtensionEventMarksUncertainWithoutRetry(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	_, err := store.DB.Exec(`
INSERT INTO subscription_extension_events (
  event_key, source_type, upstream_user_id, sub2api_group_id, upstream_subscription_id,
  extension_days, before_expires_at, status, reserved_at, created_at, updated_at
) VALUES ('stale-reserved', 'compensation', 2, 7, 101, 5, ?, 'reserved', ?, ?, ?)
`, formatTime(now.Add(10*24*time.Hour)), formatTime(now.Add(-3*time.Minute)), formatTime(now.Add(-3*time.Minute)), formatTime(now.Add(-3*time.Minute)))
	require.NoError(t, err)
	svc := New(&config.RuntimeConfig{}, store, nil, nil)
	svc.nowFunc = func() time.Time { return now }

	require.NoError(t, svc.RecoverStaleSubscriptionExtensionEvents(context.Background()))
	var status, reason string
	require.NoError(t, store.DB.QueryRow(`SELECT status, error_message FROM subscription_extension_events WHERE event_key = 'stale-reserved'`).Scan(&status, &reason))
	require.Equal(t, "uncertain", status)
	require.Contains(t, reason, "interrupted")
}

func TestRunCompensationBatchRetriesBalanceWithoutRemarkWhenUpstreamRejectsNotes(t *testing.T) {
	var (
		balanceBodies []map[string]any
		balanceKeys   []string
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/users":
			writeEnvelope(w, map[string]any{
				"items": []sub2api.User{
					{ID: 3, Email: "bal@example.com", Username: "bal", Status: "active", Balance: 5},
				},
				"total":     1,
				"page":      1,
				"page_size": 100,
				"pages":     1,
			})
		case "/api/v1/admin/subscriptions":
			writeEnvelope(w, map[string]any{
				"items":     []sub2api.Subscription{},
				"total":     0,
				"page":      1,
				"page_size": 100,
				"pages":     0,
			})
		case "/api/v1/admin/users/3/balance":
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			balanceBodies = append(balanceBodies, body)
			balanceKeys = append(balanceKeys, r.Header.Get("Idempotency-Key"))
			if len(balanceBodies) == 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code":    123,
					"message": "notes not supported",
				})
				return
			}
			writeEnvelope(w, sub2api.User{ID: 3, Email: "bal@example.com", Username: "bal", Status: "active", Balance: 15})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	operator := &SessionUser{
		User:    sub2api.User{ID: 900, Email: "admin@example.com", Username: "admin", Role: "admin", Status: "active"},
		IsAdmin: true,
	}

	batch, err := svc.RunCompensationBatch(context.Background(), operator, CompensationBatchInput{
		CompensateSubscriptions: true,
		CompensateBalance:       true,
		SubscriptionDays:        15,
		BalanceAmount:           10,
		Note:                    "bulk compensation",
	})
	require.NoError(t, err)
	require.Equal(t, compensationBatchStatusCompleted, batch.Status)
	require.Equal(t, 1, batch.BalanceCompensatedUsers)
	require.Equal(t, 0, batch.FailedUsers)
	require.Len(t, balanceBodies, 2)
	require.Equal(t, map[string]any{
		"balance":   10.0,
		"operation": "add",
		"notes":     "bulk compensation",
	}, balanceBodies[0])
	require.Equal(t, map[string]any{
		"balance":   10.0,
		"operation": "add",
	}, balanceBodies[1])
	require.Equal(t, []string{
		fmt.Sprintf("giftcode-compensation-batch-%s-user-3-balance-with-note", batch.BatchKey),
		fmt.Sprintf("giftcode-compensation-batch-%s-user-3-balance-without-note", batch.BatchKey),
	}, balanceKeys)

	details, err := svc.ListCompensationBatchDetails(context.Background(), batch.ID)
	require.NoError(t, err)
	require.Len(t, details, 1)
	require.Equal(t, "positive_balance", details[0].DecisionType)
	require.Equal(t, "success", details[0].Status)
	require.True(t, details[0].RemarkRequested)
	require.False(t, details[0].RemarkApplied)
	require.Equal(t, "upstream balance endpoint rejected notes, retried without notes", details[0].RemarkError)
}

func TestRunCompensationBatchMarksPartialSubscriptionExtensionFailures(t *testing.T) {
	var extendKeys []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/users":
			writeEnvelope(w, map[string]any{
				"items": []sub2api.User{
					{ID: 2, Email: "sub@example.com", Username: "sub", Status: "active", Balance: 20},
				},
				"total":     1,
				"page":      1,
				"page_size": 100,
				"pages":     1,
			})
		case "/api/v1/admin/subscriptions":
			writeEnvelope(w, map[string]any{
				"items": []sub2api.Subscription{
					{ID: 101, UserID: 2, GroupID: 7, Status: "active", ExpiresAt: time.Now().UTC().Add(30 * 24 * time.Hour)},
					{ID: 102, UserID: 2, GroupID: 8, Status: "active", ExpiresAt: time.Now().UTC().Add(40 * 24 * time.Hour)},
				},
				"total":     2,
				"page":      1,
				"page_size": 100,
				"pages":     1,
			})
		case "/api/v1/admin/subscriptions/101/extend":
			extendKeys = append(extendKeys, r.Header.Get("Idempotency-Key"))
			writeEnvelope(w, sub2api.Subscription{ID: 101, UserID: 2, GroupID: 7, Status: "active", ExpiresAt: time.Now().UTC().Add(60 * 24 * time.Hour)})
		case "/api/v1/admin/subscriptions/102/extend":
			extendKeys = append(extendKeys, r.Header.Get("Idempotency-Key"))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    5001,
				"message": "extend failed",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	operator := &SessionUser{
		User:    sub2api.User{ID: 900, Email: "admin@example.com", Username: "admin", Role: "admin", Status: "active"},
		IsAdmin: true,
	}

	batch, err := svc.RunCompensationBatch(context.Background(), operator, CompensationBatchInput{
		CompensateSubscriptions: true,
		CompensateBalance:       true,
		SubscriptionDays:        15,
		BalanceAmount:           10,
		Note:                    "bulk compensation",
	})
	require.NoError(t, err)
	require.Equal(t, compensationBatchStatusCompletedWithFailures, batch.Status)
	require.Equal(t, 0, batch.SubscriptionCompensatedUsers)
	require.Equal(t, 1, batch.FailedUsers)
	require.Equal(t, []string{
		fmt.Sprintf("giftcode-compensation-batch-%s-user-2-subscription-101-extend", batch.BatchKey),
		fmt.Sprintf("giftcode-compensation-batch-%s-user-2-subscription-102-extend", batch.BatchKey),
	}, extendKeys)

	details, err := svc.ListCompensationBatchDetails(context.Background(), batch.ID)
	require.NoError(t, err)
	require.Len(t, details, 1)
	require.Equal(t, "active_subscription", details[0].DecisionType)
	require.Equal(t, "failed", details[0].Status)
	require.Equal(t, 2, details[0].ActiveSubscriptionCount)
	require.False(t, details[0].RemarkRequested)

	var upstreamRef map[string]any
	require.NoError(t, json.Unmarshal([]byte(details[0].UpstreamReferenceJSON), &upstreamRef))
	require.Equal(t, []any{float64(101), float64(102)}, upstreamRef["subscription_ids"])
	require.Equal(t, []any{float64(101)}, upstreamRef["extended_ids"])
	failedExtends, ok := upstreamRef["failed_extensions"].([]any)
	require.True(t, ok)
	require.Len(t, failedExtends, 1)
	failedEntry, ok := failedExtends[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(102), failedEntry["subscription_id"])
	require.Equal(t, "extend failed", failedEntry["error"])
}

func TestListCompensationBatchesReturnsMalformedExcludedDomainsJSON(t *testing.T) {
	ctx := context.Background()
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(ctx))

	now := formatTime(time.Now().UTC())
	_, err = store.DB.ExecContext(ctx, `
INSERT INTO compensation_batches (
  batch_key, subscription_days, balance_amount, excluded_domains_json,
  operator_upstream_user_id, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`, "bad-excluded-domains", 15, 10.0, "{not-json", 900, compensationBatchStatusCompleted, now, now)
	require.NoError(t, err)

	svc := New(&config.RuntimeConfig{}, store, nil, nil)
	_, err = svc.ListCompensationBatches(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "excluded_domains_json")
}

func TestListCompensationBatchDetailsReturnsMalformedActiveSubscriptionIDsJSON(t *testing.T) {
	ctx := context.Background()
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(ctx))

	now := formatTime(time.Now().UTC())
	result, err := store.DB.ExecContext(ctx, `
INSERT INTO compensation_batches (
  batch_key, subscription_days, balance_amount, excluded_domains_json,
  operator_upstream_user_id, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`, "bad-active-subscription-ids", 15, 10.0, "[]", 900, compensationBatchStatusCompleted, now, now)
	require.NoError(t, err)
	batchID, err := result.LastInsertId()
	require.NoError(t, err)

	_, err = store.DB.ExecContext(ctx, `
INSERT INTO compensation_batch_details (
  batch_id, detail_key, upstream_user_id, active_subscription_ids_json,
  decision_type, action_type, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`, batchID, "bad-detail", 2, "[not-json", "active_subscription", "subscription", "success", now, now)
	require.NoError(t, err)

	svc := New(&config.RuntimeConfig{}, store, nil, nil)
	_, err = svc.ListCompensationBatchDetails(ctx, batchID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "active_subscription_ids_json")
}

func writeEnvelope(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":    0,
		"message": "success",
		"data":    data,
	})
}
