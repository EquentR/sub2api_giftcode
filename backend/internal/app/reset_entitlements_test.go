package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/config"
	"sub2api-giftcode/backend/internal/sub2api"
)

func TestSubscriptionResetEntitlementsAggregatesAllEffectiveBucketsAndSorts(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	insertResetPeriodFixture(t, store, 1, 101, 1, 7, 77, 30, 5, now.Add(-time.Hour), now.Add(48*time.Hour), "active")
	_, err := store.DB.Exec(`UPDATE subscription_reset_periods SET reset_used = 2 WHERE id = 1`)
	require.NoError(t, err)

	insertBonusGrantFixture(t, store, 10, 110, 1, 7, 77, 4, 1, now.Add(-time.Hour), now.Add(10*time.Hour), "active")
	insertBonusGrantFixture(t, store, 11, 111, 1, 7, 77, 2, 2, now.Add(-2*time.Hour), now.Add(5*time.Hour), "exhausted")
	insertBonusGrantFixture(t, store, 12, 112, 1, 7, 77, 10, 0, now.Add(time.Hour), now.Add(20*time.Hour), "active")
	insertBonusGrantFixture(t, store, 13, 113, 1, 7, 77, 8, 1, now.Add(-time.Hour), now.Add(20*time.Hour), "expired")
	insertBonusGrantFixture(t, store, 14, 114, 1, 7, 77, 7, 1, now.Add(-time.Hour), now.Add(20*time.Hour), "revoked")
	insertBonusGrantFixture(t, store, 15, 115, 2, 7, 99, 1, 0, now.Add(-time.Hour), now.Add(20*time.Hour), "active")

	subscriptions := []sub2api.Subscription{
		{ID: 99, UserID: 2, GroupID: 7, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), Status: "active", User: &sub2api.User{ID: 2, Username: "bob", Email: "bob@example.com"}, Group: &sub2api.Group{ID: 7, Name: "Standard"}},
		{ID: 88, UserID: 1, GroupID: 8, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(72 * time.Hour), Status: "active"},
		{ID: 77, UserID: 1, GroupID: 7, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(48 * time.Hour), Status: "active", User: &sub2api.User{ID: 1, Username: "alice", Email: "alice@example.com"}, Group: &sub2api.Group{ID: 7, Name: "Standard"}},
		{ID: 66, UserID: 1, GroupID: 6, StartsAt: now.Add(time.Hour), ExpiresAt: now.Add(72 * time.Hour), Status: "active"},
		{ID: 55, UserID: 1, GroupID: 5, StartsAt: now.Add(-72 * time.Hour), ExpiresAt: now.Add(-time.Hour), Status: "active"},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeRedeemTestEnvelope(w, subscriptionPageForTest(subscriptions))
	}))
	t.Cleanup(server.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(server.URL, "admin-key"), nil)
	svc.nowFunc = func() time.Time { return now }

	items, err := svc.ListSubscriptionResetEntitlements(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 3)

	require.Equal(t, int64(77), items[0].UpstreamSubscriptionID)
	require.Equal(t, int64(1), items[0].UpstreamUserID)
	require.Equal(t, "alice", items[0].Username)
	require.Equal(t, "alice@example.com", items[0].Email)
	require.Equal(t, int64(7), items[0].Sub2APIGroupID)
	require.Equal(t, "Standard", items[0].GroupName)
	require.Equal(t, now.Add(-time.Hour), items[0].StartsAt)
	require.Equal(t, now.Add(48*time.Hour), items[0].ExpiresAt)
	require.Equal(t, 2, items[0].RemainingDays)
	require.Equal(t, 5, items[0].BaseResetLimit)
	require.Equal(t, 2, items[0].BaseResetUsed)
	require.Equal(t, 3, items[0].BaseResetRemaining)
	require.Equal(t, 6, items[0].BonusResetLimit)
	require.Equal(t, 3, items[0].BonusResetUsed)
	require.Equal(t, 3, items[0].BonusResetRemaining)
	require.Equal(t, 6, items[0].TotalResetRemaining)

	require.Equal(t, int64(88), items[1].UpstreamSubscriptionID)
	require.Empty(t, items[1].Username)
	require.Empty(t, items[1].GroupName)
	require.Zero(t, items[1].TotalResetRemaining)

	require.Equal(t, int64(99), items[2].UpstreamSubscriptionID)
	require.Zero(t, items[2].BaseResetLimit)
	require.Equal(t, 1, items[2].BonusResetRemaining)
	require.Equal(t, 1, items[2].TotalResetRemaining)
}

func TestSubscriptionResetEntitlementsMapsUpstreamFailure(t *testing.T) {
	store := newResetPeriodTestStore(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(server.URL, "admin-key"), nil)

	_, err := svc.ListSubscriptionResetEntitlements(context.Background())
	require.ErrorIs(t, err, ErrUpstreamUnavailable)
	require.Equal(t, SubscriptionResetReasonUpstreamUnavailable, StableReason(err))
}

func TestSubscriptionResetEntitlementsReturnsLocalQueryFailure(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeRedeemTestEnvelope(w, subscriptionPageForTest([]sub2api.Subscription{{
			ID: 77, UserID: 1, GroupID: 7, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Status: "active",
		}}))
	}))
	t.Cleanup(server.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(server.URL, "admin-key"), nil)
	svc.nowFunc = func() time.Time { return now }
	require.NoError(t, store.Close())

	_, err := svc.ListSubscriptionResetEntitlements(context.Background())
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrUpstreamUnavailable)
}

func TestSubscriptionResetEntitlementsHandlesMoreThanSQLiteVariableLimit(t *testing.T) {
	const subscriptionCount = 33000
	store := newResetPeriodTestStore(t)
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	insertResetPeriodFixture(t, store, 1, 101, 1, 7, 1, 30, 2, now.Add(-time.Hour), now.Add(24*time.Hour), "active")
	insertResetPeriodFixture(t, store, 2, 102, subscriptionCount, 7, subscriptionCount, 30, 3, now.Add(-time.Hour), now.Add(24*time.Hour), "active")
	insertBonusGrantFixture(t, store, 20, 120, subscriptionCount, 7, subscriptionCount, 4, 1, now.Add(-time.Hour), now.Add(20*time.Hour), "active")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		require.NoError(t, err)
		start := (page - 1) * 100
		end := start + 100
		if end > subscriptionCount {
			end = subscriptionCount
		}
		items := make([]sub2api.Subscription, 0, end-start)
		for index := start; index < end; index++ {
			id := int64(index + 1)
			items = append(items, sub2api.Subscription{
				ID: id, UserID: id, GroupID: 7, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), Status: "active",
			})
		}
		writeRedeemTestEnvelope(w, map[string]any{
			"items": items, "total": subscriptionCount, "page": page, "page_size": 100, "pages": subscriptionCount / 100,
		})
	}))
	t.Cleanup(server.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(server.URL, "admin-key"), nil)
	svc.nowFunc = func() time.Time { return now }

	items, err := svc.ListSubscriptionResetEntitlements(context.Background())
	require.NoError(t, err)
	require.Len(t, items, subscriptionCount)
	require.Equal(t, 2, items[0].BaseResetRemaining)
	require.Equal(t, 3, items[subscriptionCount-1].BaseResetRemaining)
	require.Equal(t, 3, items[subscriptionCount-1].BonusResetRemaining)
	require.Equal(t, 6, items[subscriptionCount-1].TotalResetRemaining)
}
