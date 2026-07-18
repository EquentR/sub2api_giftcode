package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/config"
	"sub2api-giftcode/backend/internal/sub2api"
)

func TestSubscriptionResetBonusCreateReplayAfterPreviewExpiry(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	dailyLimit, monthlyLimit := 10.0, 100.0
	groups := []sub2api.Group{
		{ID: 7, Name: "Std", Status: "active", DailyLimitUSD: &dailyLimit},
		{ID: 8, Name: "Pro", Status: "active", MonthlyLimitUSD: &monthlyLimit},
		{ID: 9, Name: "Unlimited", Status: "active"},
	}
	var stateMu sync.Mutex
	subscriptions := []sub2api.Subscription{
		{ID: 71, UserID: 1, GroupID: 7, StartsAt: now.Add(-24 * time.Hour), ExpiresAt: now.Add(20 * 24 * time.Hour), Status: "active", Group: &groups[0]},
		{ID: 81, UserID: 1, GroupID: 8, StartsAt: now.Add(-24 * time.Hour), ExpiresAt: now.Add(25 * 24 * time.Hour), Status: "active", Group: &groups[1]},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stateMu.Lock()
		defer stateMu.Unlock()
		switch r.URL.Path {
		case "/api/v1/admin/groups/all":
			writeRedeemTestEnvelope(w, groups)
		case "/api/v1/admin/users/1":
			writeRedeemTestEnvelope(w, sub2api.User{ID: 1, Email: "user@example.com", Status: "active"})
		case "/api/v1/admin/subscriptions":
			userID, _ := strconv.ParseInt(r.URL.Query().Get("user_id"), 10, 64)
			items := make([]sub2api.Subscription, 0)
			for _, subscription := range subscriptions {
				if subscription.UserID == userID {
					items = append(items, subscription)
				}
			}
			writeRedeemTestEnvelope(w, subscriptionPageForTest(items))
		case "/api/v1/admin/subscriptions/71":
			writeRedeemTestEnvelope(w, subscriptions[0])
		case "/api/v1/admin/subscriptions/81":
			writeRedeemTestEnvelope(w, subscriptions[1])
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	cfg := &config.RuntimeConfig{}
	cfg.Session.CookieSecret = "bonus-preview-secret"
	svc := New(cfg, store, sub2api.NewClient(server.URL, "admin-key"), nil)
	svc.nowFunc = func() time.Time { return now }
	operator := &SessionUser{User: sub2api.User{ID: 99, Email: "admin@example.com", Username: "admin"}, IsAdmin: true}
	input := SubscriptionResetBonusPreviewInput{
		TargetScope: "selected", SelectedUserIDs: []int64{1, 1}, GroupIDs: []int64{8, 7, 8}, ResetCount: 3, Note: "legacy repair",
	}

	preview, err := svc.PreviewSubscriptionResetBonus(context.Background(), operator, input)
	require.NoError(t, err)
	require.Equal(t, 1, preview.UserCount)
	require.Equal(t, 2, preview.SubscriptionCount)
	require.Equal(t, map[int64]int{7: 1, 8: 1}, preview.GroupCounts)
	require.NotEmpty(t, preview.PreviewToken)
	require.Equal(t, now.Add(10*time.Minute), preview.ExpiresAt)

	_, err = svc.PreviewSubscriptionResetBonus(context.Background(), operator, SubscriptionResetBonusPreviewInput{
		TargetScope: "selected", SelectedUserIDs: []int64{1}, GroupIDs: []int64{9}, ResetCount: 1,
	})
	require.ErrorIs(t, err, ErrBadRequest)
	require.Equal(t, SubscriptionResetBonusReasonUnlimitedGroup, StableReason(err))

	stateMu.Lock()
	subscriptions[1].ExpiresAt = subscriptions[1].ExpiresAt.Add(24 * time.Hour)
	stateMu.Unlock()
	_, err = svc.CreateSubscriptionResetBonusBatch(context.Background(), operator, preview.PreviewToken)
	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, SubscriptionResetBonusReasonPreviewStale, StableReason(err))

	freshPreview, err := svc.PreviewSubscriptionResetBonus(context.Background(), operator, input)
	require.NoError(t, err)
	batch, err := svc.CreateSubscriptionResetBonusBatch(context.Background(), operator, freshPreview.PreviewToken)
	require.NoError(t, err)
	require.Equal(t, "pending", batch.Status)
	replayed, err := svc.CreateSubscriptionResetBonusBatch(context.Background(), operator, freshPreview.PreviewToken)
	require.NoError(t, err)
	require.Equal(t, batch.ID, replayed.ID)

	require.NoError(t, svc.ProcessSubscriptionResetBonusBatches(context.Background()))
	require.NoError(t, svc.ProcessSubscriptionResetBonusBatches(context.Background()))
	batches, err := svc.ListSubscriptionResetBonusBatches(context.Background())
	require.NoError(t, err)
	require.Len(t, batches, 1)
	require.Equal(t, "completed", batches[0].Status)
	require.Equal(t, 2, batches[0].GrantedSubscriptions)
	details, err := svc.ListSubscriptionResetBonusBatchDetails(context.Background(), batch.ID)
	require.NoError(t, err)
	require.Len(t, details, 2)
	for _, detail := range details {
		require.Equal(t, "granted", detail.Status)
		require.NotNil(t, detail.BonusGrantID)
	}
	var grantCount, resetLimitSum int
	require.NoError(t, store.DB.QueryRow(`SELECT COUNT(1), COALESCE(SUM(reset_limit), 0) FROM subscription_reset_bonus_grants`).Scan(&grantCount, &resetLimitSum))
	require.Equal(t, 2, grantCount)
	require.Equal(t, 6, resetLimitSum)

	svc.nowFunc = func() time.Time { return now.Add(11 * time.Minute) }
	expiredReplay, err := svc.CreateSubscriptionResetBonusBatch(context.Background(), operator, freshPreview.PreviewToken)
	require.NoError(t, err)
	require.Equal(t, batch.ID, expiredReplay.ID)
}

func TestSubscriptionResetBonusPreviewTokenBindsOperatorAndExpires(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	limit := 10.0
	group := sub2api.Group{ID: 7, Name: "Std", Status: "active", DailyLimitUSD: &limit}
	subscription := sub2api.Subscription{ID: 71, UserID: 1, GroupID: 7, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), Status: "active", Group: &group}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/groups/all":
			writeRedeemTestEnvelope(w, []sub2api.Group{group})
		case "/api/v1/admin/users/1":
			writeRedeemTestEnvelope(w, sub2api.User{ID: 1, Email: "user@example.com"})
		case "/api/v1/admin/subscriptions":
			writeRedeemTestEnvelope(w, subscriptionPageForTest([]sub2api.Subscription{subscription}))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	cfg := &config.RuntimeConfig{}
	cfg.Session.CookieSecret = "bonus-preview-secret"
	svc := New(cfg, store, sub2api.NewClient(server.URL, "admin-key"), nil)
	svc.nowFunc = func() time.Time { return now }
	operator := &SessionUser{User: sub2api.User{ID: 99}, IsAdmin: true}
	preview, err := svc.PreviewSubscriptionResetBonus(context.Background(), operator, SubscriptionResetBonusPreviewInput{
		TargetScope: "selected", SelectedUserIDs: []int64{1}, GroupIDs: []int64{7}, ResetCount: 1,
	})
	require.NoError(t, err)

	_, err = svc.CreateSubscriptionResetBonusBatch(context.Background(), &SessionUser{User: sub2api.User{ID: 100}, IsAdmin: true}, preview.PreviewToken)
	require.ErrorIs(t, err, ErrForbidden)
	svc.nowFunc = func() time.Time { return now.Add(11 * time.Minute) }
	_, err = svc.CreateSubscriptionResetBonusBatch(context.Background(), operator, preview.PreviewToken)
	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, SubscriptionResetBonusReasonPreviewExpired, StableReason(err))
}

func TestSubscriptionResetBonusWorkerSkipsGroupThatBecameUnlimited(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	limit := 10.0
	group := sub2api.Group{ID: 7, Name: "Std", Status: "active", DailyLimitUSD: &limit}
	subscription := sub2api.Subscription{ID: 71, UserID: 1, GroupID: 7, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), Status: "active", Group: &group}
	var stateMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stateMu.Lock()
		defer stateMu.Unlock()
		switch r.URL.Path {
		case "/api/v1/admin/groups/all":
			writeRedeemTestEnvelope(w, []sub2api.Group{group})
		case "/api/v1/admin/users/1":
			writeRedeemTestEnvelope(w, sub2api.User{ID: 1})
		case "/api/v1/admin/subscriptions":
			writeRedeemTestEnvelope(w, subscriptionPageForTest([]sub2api.Subscription{subscription}))
		case "/api/v1/admin/subscriptions/71":
			writeRedeemTestEnvelope(w, subscription)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	cfg := &config.RuntimeConfig{}
	cfg.Session.CookieSecret = "bonus-preview-secret"
	svc := New(cfg, store, sub2api.NewClient(server.URL, "admin-key"), nil)
	svc.nowFunc = func() time.Time { return now }
	operator := &SessionUser{User: sub2api.User{ID: 99}, IsAdmin: true}
	preview, err := svc.PreviewSubscriptionResetBonus(context.Background(), operator, SubscriptionResetBonusPreviewInput{
		TargetScope: "selected", SelectedUserIDs: []int64{1}, GroupIDs: []int64{7}, ResetCount: 2,
	})
	require.NoError(t, err)
	batch, err := svc.CreateSubscriptionResetBonusBatch(context.Background(), operator, preview.PreviewToken)
	require.NoError(t, err)

	stateMu.Lock()
	group.DailyLimitUSD = nil
	subscription.Group = &group
	stateMu.Unlock()
	require.NoError(t, svc.ProcessSubscriptionResetBonusBatches(context.Background()))
	details, err := svc.ListSubscriptionResetBonusBatchDetails(context.Background(), batch.ID)
	require.NoError(t, err)
	require.Len(t, details, 1)
	require.Equal(t, "skipped", details[0].Status)
	require.Equal(t, SubscriptionResetBonusReasonUnlimitedGroup, details[0].Reason)
	var grantCount int
	require.NoError(t, store.DB.QueryRow(`SELECT COUNT(1) FROM subscription_reset_bonus_grants`).Scan(&grantCount))
	require.Zero(t, grantCount)
}
