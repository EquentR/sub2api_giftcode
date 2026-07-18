package app

import (
	"context"
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

func TestLegacyResetBackfillRunCreatedOnlyForEligibleTierFirstPositiveChangeAndFreezesLimit(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertLegacyResetTierFixture(t, store, 50, 7, true, now)
	insertLegacyResetTierFixture(t, store, 51, 7, false, now)
	svc := newLegacyResetTierService(t, store)

	_, err := svc.ReplaceRedeemTiers(context.Background(), []models.RedeemTier{
		legacyResetTierInput(50, 7, 2),
		legacyResetTierInput(51, 7, 0),
	})
	require.NoError(t, err)
	var resetLimit int
	var status string
	require.NoError(t, store.DB.QueryRow(`SELECT reset_limit, status FROM subscription_reset_backfill_runs WHERE tier_id = 50`).Scan(&resetLimit, &status))
	require.Equal(t, 2, resetLimit)
	require.Equal(t, "pending", status)

	_, err = svc.ReplaceRedeemTiers(context.Background(), []models.RedeemTier{
		legacyResetTierInput(50, 7, 5),
		legacyResetTierInput(51, 7, 3),
	})
	require.NoError(t, err)
	require.NoError(t, store.DB.QueryRow(`SELECT reset_limit FROM subscription_reset_backfill_runs WHERE tier_id = 50`).Scan(&resetLimit))
	require.Equal(t, 2, resetLimit, "the first positive value is an immutable backfill snapshot")
	var runCount int
	require.NoError(t, store.DB.QueryRow(`SELECT COUNT(1) FROM subscription_reset_backfill_runs`).Scan(&runCount))
	require.Equal(t, 1, runCount, "post-feature tiers must never create legacy backfill runs")
}

func TestLegacyResetBackfillUsesAllGroupPeriodsAndGrantsOnlyCurrentTargetTier(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertLegacyResetTierFixture(t, store, 50, 7, true, now)
	insertLegacyResetTierFixture(t, store, 51, 7, true, now)
	insertResetAccessFixture(t, store, 101, 1, 7, 10, 0, fulfilledViaDirectCharge, fulfillmentResultDirectSucceeded, now.Add(-3*time.Hour))
	insertResetAccessFixture(t, store, 102, 1, 7, 10, 0, fulfilledViaDirectCharge, fulfillmentResultDirectSucceeded, now.Add(-2*time.Hour))
	insertResetAccessFixture(t, store, 103, 1, 7, 10, 0, fulfilledViaDirectCharge, fulfillmentResultDirectSucceeded, now.Add(-time.Hour))
	_, err := store.DB.Exec(`UPDATE redeem_access_requests SET tier_id = 50 WHERE id IN (101, 103)`)
	require.NoError(t, err)
	_, err = store.DB.Exec(`UPDATE redeem_access_requests SET tier_id = 51 WHERE id = 102`)
	require.NoError(t, err)

	expiresAt := now.Add(10 * 24 * time.Hour)
	startsAt := now.Add(-30 * 24 * time.Hour)
	svc := newLegacyResetBackfillService(t, store, func() ([]sub2api.Subscription, bool) {
		return []sub2api.Subscription{{ID: 77, UserID: 1, GroupID: 7, StartsAt: startsAt, ExpiresAt: expiresAt, Status: "active"}}, true
	})
	_, err = svc.ReplaceRedeemTiers(context.Background(), []models.RedeemTier{
		legacyResetTierInput(50, 7, 2),
		legacyResetTierInput(51, 7, 0),
	})
	require.NoError(t, err)

	require.NoError(t, svc.ReconcileSubscriptionResetPeriods(context.Background()))
	periods := loadResetPeriodsForTest(t, store)
	require.Len(t, periods, 3)
	require.Equal(t, int64(101), periods[0].AccessRequestID)
	require.Zero(t, periods[0].ResetLimit)
	require.True(t, periods[0].LegacyResetBackfilled)
	require.Equal(t, "expired", periods[0].Status)
	require.Equal(t, int64(102), periods[1].AccessRequestID)
	require.Zero(t, periods[1].ResetLimit, "another tier's zero-limit period must occupy time without receiving the grant")
	require.Equal(t, *periods[0].PeriodEnd, *periods[1].PeriodStart)
	require.Equal(t, int64(103), periods[2].AccessRequestID)
	require.Equal(t, *periods[1].PeriodEnd, *periods[2].PeriodStart)
	require.Equal(t, 2, periods[2].ResetLimit)
	require.True(t, periods[2].LegacyResetBackfilled)
	require.Equal(t, "active", periods[2].Status)
	for _, period := range periods {
		require.True(t, period.InferredFromLegacy)
		require.Equal(t, 1, period.MigrationVersion)
	}

	var runStatus string
	var total, processed, granted int
	require.NoError(t, store.DB.QueryRow(`
SELECT status, total_records, processed_records, granted_records
FROM subscription_reset_backfill_runs WHERE tier_id = 50
`).Scan(&runStatus, &total, &processed, &granted))
	require.Equal(t, "succeeded", runStatus)
	require.Equal(t, 2, total)
	require.Equal(t, 2, processed)
	require.Equal(t, 1, granted)
}

func TestLegacyResetBackfillRetriesWithoutDuplicateGrant(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertLegacyResetTierFixture(t, store, 50, 7, true, now)
	insertResetAccessFixture(t, store, 101, 1, 7, 30, 0, fulfilledViaDirectCharge, fulfillmentResultDirectSucceeded, now.Add(-time.Hour))
	_, err := store.DB.Exec(`UPDATE redeem_access_requests SET tier_id = 50 WHERE id = 101`)
	require.NoError(t, err)

	var stateMu sync.Mutex
	upstreamAvailable := false
	svc := newLegacyResetBackfillService(t, store, func() ([]sub2api.Subscription, bool) {
		stateMu.Lock()
		defer stateMu.Unlock()
		if !upstreamAvailable {
			return nil, false
		}
		return []sub2api.Subscription{{
			ID: 77, UserID: 1, GroupID: 7, StartsAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour), Status: "active",
		}}, true
	})
	_, err = svc.ReplaceRedeemTiers(context.Background(), []models.RedeemTier{legacyResetTierInput(50, 7, 2)})
	require.NoError(t, err)

	require.Error(t, svc.ReconcileSubscriptionResetPeriods(context.Background()))
	var status string
	var granted int
	require.NoError(t, store.DB.QueryRow(`SELECT status, granted_records FROM subscription_reset_backfill_runs WHERE tier_id = 50`).Scan(&status, &granted))
	require.Equal(t, "failed", status)
	require.Zero(t, granted)

	stateMu.Lock()
	upstreamAvailable = true
	stateMu.Unlock()
	require.NoError(t, svc.ReconcileSubscriptionResetPeriods(context.Background()))
	require.NoError(t, svc.ReconcileSubscriptionResetPeriods(context.Background()))
	var resetLimit, backfilled int
	require.NoError(t, store.DB.QueryRow(`SELECT reset_limit, legacy_reset_backfilled FROM subscription_reset_periods WHERE access_request_id = 101`).Scan(&resetLimit, &backfilled))
	require.Equal(t, 2, resetLimit)
	require.Equal(t, 1, backfilled)
	require.NoError(t, store.DB.QueryRow(`SELECT status, granted_records FROM subscription_reset_backfill_runs WHERE tier_id = 50`).Scan(&status, &granted))
	require.Equal(t, "succeeded", status)
	require.Equal(t, 1, granted)
}

func TestLegacyResetBackfillResumesPartiallyProcessedRun(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertLegacyResetTierFixture(t, store, 50, 7, true, now)
	insertResetPeriodFixture(t, store, 1, 101, 1, 7, 77, 10, 0, now.Add(-24*time.Hour), now.Add(9*24*time.Hour), "active")
	insertResetPeriodFixture(t, store, 2, 102, 1, 7, 77, 10, 0, now.Add(9*24*time.Hour), now.Add(19*24*time.Hour), "scheduled")
	_, err := store.DB.Exec(`UPDATE subscription_reset_periods SET tier_id = 50`)
	require.NoError(t, err)
	_, err = store.DB.Exec(`
UPDATE subscription_reset_periods
SET fulfilled_at = CASE id WHEN 1 THEN ? ELSE ? END,
    fulfillment_order = CASE id WHEN 1 THEN 101 ELSE 102 END
`, formatTime(now.Add(-2*time.Hour)), formatTime(now.Add(-time.Hour)))
	require.NoError(t, err)
	_, err = store.DB.Exec(`
UPDATE subscription_reset_periods
SET reset_limit = 2, legacy_reset_backfilled = 1, inferred_from_legacy = 1, migration_version = 1
WHERE id = 1
`)
	require.NoError(t, err)
	_, err = store.DB.Exec(`
INSERT INTO subscription_reset_backfill_runs (
  tier_id, reset_limit, status, total_records, processed_records, granted_records,
  triggered_at, started_at, updated_at
) VALUES (50, 2, 'running', 2, 1, 1, ?, ?, ?)
`, formatTime(now.Add(time.Hour)), formatTime(now), formatTime(now))
	require.NoError(t, err)
	svc := newLegacyResetBackfillService(t, store, func() ([]sub2api.Subscription, bool) {
		return []sub2api.Subscription{{
			ID: 77, UserID: 1, GroupID: 7, StartsAt: now.Add(-24 * time.Hour), ExpiresAt: now.Add(19 * 24 * time.Hour), Status: "active",
		}}, true
	})

	require.NoError(t, svc.ReconcileSubscriptionResetPeriods(context.Background()))
	var status string
	var total, processed, granted int
	require.NoError(t, store.DB.QueryRow(`
SELECT status, total_records, processed_records, granted_records
FROM subscription_reset_backfill_runs WHERE tier_id = 50
`).Scan(&status, &total, &processed, &granted))
	require.Equal(t, "succeeded", status)
	require.Equal(t, 2, total)
	require.Equal(t, 2, processed)
	require.Equal(t, 2, granted)
}

func TestLegacyResetBackfillExcludesUnconfirmedRedeemer(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertLegacyResetTierFixture(t, store, 50, 7, true, now)
	insertResetAccessFixture(t, store, 101, 1, 7, 10, 0, fulfilledViaRedeemCode, fulfillmentResultCodeIssued, now.Add(-time.Hour))
	insertUsedSubscriptionCodeFixture(t, store, 101, 1, 7, 10, now.Add(-time.Hour))
	_, err := store.DB.Exec(`UPDATE redeem_access_requests SET tier_id = 50 WHERE id = 101`)
	require.NoError(t, err)
	_, err = store.DB.Exec(`UPDATE redeem_codes SET used_by_upstream_user_id = 2`)
	require.NoError(t, err)
	svc := newLegacyResetBackfillService(t, store, func() ([]sub2api.Subscription, bool) {
		return []sub2api.Subscription{{
			ID: 77, UserID: 1, GroupID: 7, StartsAt: now, ExpiresAt: now.Add(10 * 24 * time.Hour), Status: "active",
		}}, true
	})
	_, err = svc.ReplaceRedeemTiers(context.Background(), []models.RedeemTier{legacyResetTierInput(50, 7, 2)})
	require.NoError(t, err)

	require.NoError(t, svc.ReconcileSubscriptionResetPeriods(context.Background()))
	var periodCount int
	require.NoError(t, store.DB.QueryRow(`SELECT COUNT(1) FROM subscription_reset_periods`).Scan(&periodCount))
	require.Zero(t, periodCount)
	var status string
	var total, granted int
	require.NoError(t, store.DB.QueryRow(`SELECT status, total_records, granted_records FROM subscription_reset_backfill_runs WHERE tier_id = 50`).Scan(&status, &total, &granted))
	require.Equal(t, "succeeded", status)
	require.Zero(t, total)
	require.Zero(t, granted)
}

func newLegacyResetTierService(t *testing.T, store *db.Store) *Service {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/admin/groups/all", r.URL.Path)
		limit := 10.0
		writeRedeemTestEnvelope(w, []sub2api.Group{{ID: 7, Name: "Legacy", Status: "active", SubscriptionType: "subscription", DailyLimitUSD: &limit}})
	}))
	t.Cleanup(server.Close)
	return New(&config.RuntimeConfig{}, store, sub2api.NewClient(server.URL, "admin-key"), nil)
}

func newLegacyResetBackfillService(t *testing.T, store *db.Store, subscriptions func() ([]sub2api.Subscription, bool)) *Service {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/groups/all":
			limit := 10.0
			writeRedeemTestEnvelope(w, []sub2api.Group{{ID: 7, Name: "Legacy", Status: "active", SubscriptionType: "subscription", DailyLimitUSD: &limit}})
		case "/api/v1/admin/subscriptions":
			items, ok := subscriptions()
			if !ok {
				http.Error(w, "subscriptions unavailable", http.StatusBadGateway)
				return
			}
			writeRedeemTestEnvelope(w, subscriptionPageForTest(items))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return New(&config.RuntimeConfig{}, store, sub2api.NewClient(server.URL, "admin-key"), nil)
}

func insertLegacyResetTierFixture(t *testing.T, store *db.Store, id, groupID int64, eligible bool, createdAt time.Time) {
	t.Helper()
	_, err := store.DB.Exec(`
INSERT INTO redeem_tiers (
  id, code_type, pay_amount_cny, label, enabled, sub2api_group_id, validity_days, concurrency,
  reset_count, legacy_reset_backfill_eligible, created_at, updated_at
) VALUES (?, 'subscription', 88, ?, 1, ?, 10, 10, 0, ?, ?, ?)
`, id, "Legacy tier", groupID, boolToInt(eligible), formatTime(createdAt), formatTime(createdAt))
	require.NoError(t, err)
}

func legacyResetTierInput(id, groupID int64, resetCount int) models.RedeemTier {
	return models.RedeemTier{
		ID: id, CodeType: "subscription", PayAmountCny: 88, Label: "Legacy tier",
		Enabled: true, Sub2APIGroupID: &groupID, ValidityDays: 10, Concurrency: 10, ResetCount: resetCount,
	}
}
