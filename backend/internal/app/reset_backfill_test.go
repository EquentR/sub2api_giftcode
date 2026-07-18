package app

import (
	"context"
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

func TestLegacyResetBackfillIsNotCreatedWhenTierResetCountChanges(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertLegacyResetTierFixture(t, store, 50, 7, true, now)
	svc := newLegacyResetTierService(t, store)

	_, err := svc.ReplaceRedeemTiers(context.Background(), []models.RedeemTier{
		legacyResetTierInput(50, 7, 3),
	})
	require.NoError(t, err)

	var runCount int
	require.NoError(t, store.DB.QueryRow(`SELECT COUNT(1) FROM subscription_reset_backfill_runs`).Scan(&runCount))
	require.Zero(t, runCount)
}

func TestLegacyIgnoredResetPeriodsDoNotParticipateInScheduling(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertResetPeriodFixture(t, store, 1, 101, 1, 7, 77, 10, 0, now, now.Add(10*24*time.Hour), "active")
	insertResetPeriodFixture(t, store, 2, 102, 1, 7, 77, 10, 2, now.Add(10*24*time.Hour), now.Add(20*24*time.Hour), "scheduled")
	_, err := store.DB.Exec(`UPDATE subscription_reset_periods SET legacy_ignored = 1, legacy_ignored_at = ?, legacy_ignore_reason = 'automatic backfill disabled' WHERE id = 1`, formatTime(now))
	require.NoError(t, err)

	svc := New(&config.RuntimeConfig{}, store, nil, nil)
	periods, err := svc.listSubscriptionResetPeriodsForGroup(context.Background(), 1, 7)
	require.NoError(t, err)
	require.Len(t, periods, 1)
	require.Equal(t, int64(2), periods[0].ID)
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
