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

func TestSubscriptionsListKeepsCardWhenOneProgressReadFailsAndOmitsUnsetWindows(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertResetPeriodFixture(t, store, 1, 101, 1, 7, 77, 30, 2, now.Add(-time.Hour), now.Add(30*24*time.Hour), "active")
	dailyLimit := 10.0
	monthlyLimit := 100.0
	weeklyLimit := 50.0
	dailyStart := now.Add(-2 * time.Hour)
	monthlyStart := now.Add(-5 * 24 * time.Hour)
	subscriptions := []sub2api.Subscription{
		{
			ID: 77, UserID: 1, GroupID: 7, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(30 * 24 * time.Hour), Status: "active",
			DailyUsageUSD: 3, MonthlyUsageUSD: 20, DailyWindowStart: &dailyStart, MonthlyWindowStart: &monthlyStart,
			Group: &sub2api.Group{ID: 7, Name: "Partial", Platform: "openai", DailyLimitUSD: &dailyLimit, MonthlyLimitUSD: &monthlyLimit},
		},
		{
			ID: 88, UserID: 1, GroupID: 8, StartsAt: now, ExpiresAt: now.Add(10 * 24 * time.Hour), Status: "active",
			Group: &sub2api.Group{ID: 8, Name: "Unavailable", Platform: "anthropic", WeeklyLimitUSD: &weeklyLimit},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/subscriptions":
			writeRedeemTestEnvelope(w, subscriptionPageForTest(subscriptions))
		case "/api/v1/admin/subscriptions/77/progress":
			writeRedeemTestEnvelope(w, sub2api.SubscriptionProgress{
				ID: 77, ExpiresAt: subscriptions[0].ExpiresAt, ExpiresInDays: 30,
				Daily:   &sub2api.UsageWindowProgress{LimitUSD: 10, UsedUSD: 3, RemainingUSD: 7, WindowStart: &dailyStart},
				Monthly: &sub2api.UsageWindowProgress{LimitUSD: 100, UsedUSD: 20, RemainingUSD: 80, WindowStart: &monthlyStart},
			})
		case "/api/v1/admin/subscriptions/88/progress":
			http.Error(w, "progress unavailable", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(server.URL, "admin-key"), nil)

	cards, err := svc.ListSubscriptions(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, cards, 2)
	require.Len(t, cards[0].QuotaWindows, 2)
	require.Equal(t, "daily", cards[0].QuotaWindows[0].Kind)
	require.Equal(t, "monthly", cards[0].QuotaWindows[1].Kind)
	require.True(t, cards[0].CanReset)
	require.Empty(t, cards[0].DisabledReason)
	require.NotNil(t, cards[0].CurrentPeriod)
	require.Equal(t, 2, cards[0].CurrentPeriod.ResetRemaining)
	require.False(t, cards[1].CanReset)
	require.Equal(t, SubscriptionResetReasonUpstreamUnavailable, cards[1].DisabledReason)
}

func TestSubscriptionsListReturnsAuthoritativeFailureWhenListFails(t *testing.T) {
	store := newResetPeriodTestStore(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "subscriptions unavailable", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(server.URL, "admin-key"), nil)

	_, err := svc.ListSubscriptions(context.Background(), 1)
	require.ErrorIs(t, err, ErrUpstreamUnavailable)
}

func TestSubscriptionsHydratesMissingGroupBeforeClassifyingUnlimited(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertResetPeriodFixture(t, store, 1, 101, 1, 7, 77, 30, 1, now.Add(-time.Hour), now.Add(30*24*time.Hour), "active")
	dailyLimit := 10.0
	dailyStart := now.Add(-time.Hour)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/subscriptions":
			writeRedeemTestEnvelope(w, subscriptionPageForTest([]sub2api.Subscription{{ID: 77, UserID: 1, GroupID: 7, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), Status: "active", DailyUsageUSD: 2, DailyWindowStart: &dailyStart}}))
		case "/api/v1/admin/groups/all":
			writeRedeemTestEnvelope(w, []sub2api.Group{{ID: 7, Name: "Hydrated", DailyLimitUSD: &dailyLimit}})
		case "/api/v1/admin/subscriptions/77/progress":
			writeRedeemTestEnvelope(w, sub2api.SubscriptionProgress{ID: 77, Daily: &sub2api.UsageWindowProgress{LimitUSD: 10, UsedUSD: 2, RemainingUSD: 8, WindowStart: &dailyStart}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(server.URL, "admin-key"), nil)

	cards, err := svc.ListSubscriptions(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, cards, 1)
	require.False(t, cards[0].Unlimited)
	require.Len(t, cards[0].QuotaWindows, 1)
	require.Equal(t, "Hydrated", cards[0].GroupName)
}

func TestResetQuotaSuccessIsIdempotentAndConsumesOnce(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertResetPeriodFixture(t, store, 1, 101, 1, 7, 77, 30, 2, now.Add(-time.Hour), now.Add(30*24*time.Hour), "active")
	dailyLimit := 10.0
	dailyStart := now.Add(-2 * time.Hour)
	newDailyStart := now
	resetCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/subscriptions/77":
			writeRedeemTestEnvelope(w, sub2api.Subscription{
				ID: 77, UserID: 1, GroupID: 7, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(30 * 24 * time.Hour), Status: "active",
				DailyUsageUSD: 4, DailyWindowStart: &dailyStart,
				Group: &sub2api.Group{ID: 7, Name: "Daily", DailyLimitUSD: &dailyLimit},
			})
		case "/api/v1/admin/subscriptions/77/progress":
			writeRedeemTestEnvelope(w, sub2api.SubscriptionProgress{ID: 77, Daily: &sub2api.UsageWindowProgress{
				LimitUSD: 10, UsedUSD: 4, RemainingUSD: 6, WindowStart: &dailyStart,
			}})
		case "/api/v1/admin/subscriptions/77/reset-quota":
			resetCalls++
			writeRedeemTestEnvelope(w, sub2api.Subscription{
				ID: 77, UserID: 1, GroupID: 7, Status: "active", DailyUsageUSD: 0, DailyWindowStart: &newDailyStart,
				Group: &sub2api.Group{ID: 7, DailyLimitUSD: &dailyLimit},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(server.URL, "admin-key"), nil)
	requestID := "11111111-1111-4111-8111-111111111111"

	result, err := svc.ResetSubscriptionQuota(context.Background(), 1, 77, requestID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", result.Operation.Status)
	replayed, err := svc.ResetSubscriptionQuota(context.Background(), 1, 77, requestID)
	require.NoError(t, err)
	require.Equal(t, result.Operation.ID, replayed.Operation.ID)
	require.Equal(t, 1, resetCalls)
	var resetUsed int
	require.NoError(t, store.DB.QueryRow(`SELECT reset_used FROM subscription_reset_periods WHERE id = 1`).Scan(&resetUsed))
	require.Equal(t, 1, resetUsed)
}

func TestResetQuotaExplicitFailureReleasesReservedCount(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertResetPeriodFixture(t, store, 1, 101, 1, 7, 77, 30, 1, now.Add(-time.Hour), now.Add(30*24*time.Hour), "active")
	svc := newResetTransactionTestService(t, store, now, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 409, "message": "quota locked", "reason": "quota_locked"})
	})

	_, err := svc.ResetSubscriptionQuota(context.Background(), 1, 77, "22222222-2222-4222-8222-222222222222")
	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, SubscriptionResetReasonUpstreamRejected, StableReason(err))
	var resetUsed int
	var status string
	require.NoError(t, store.DB.QueryRow(`SELECT reset_used FROM subscription_reset_periods WHERE id = 1`).Scan(&resetUsed))
	require.NoError(t, store.DB.QueryRow(`SELECT status FROM subscription_reset_attempts`).Scan(&status))
	require.Zero(t, resetUsed)
	require.Equal(t, "failed", status)
}

func TestResetQuotaUnknownResultKeepsReservationAndCanBeConfirmedByReconcile(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertResetPeriodFixture(t, store, 1, 101, 1, 7, 77, 30, 1, now.Add(-time.Hour), now.Add(30*24*time.Hour), "active")
	dailyLimit := 10.0
	dailyStart := now.Add(-2 * time.Hour)
	changedStart := now
	var stateMu sync.Mutex
	windowStart := dailyStart
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/subscriptions/77":
			stateMu.Lock()
			start := windowStart
			stateMu.Unlock()
			writeRedeemTestEnvelope(w, sub2api.Subscription{
				ID: 77, UserID: 1, GroupID: 7, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(30 * 24 * time.Hour), Status: "active",
				DailyUsageUSD: 4, DailyWindowStart: &start, Group: &sub2api.Group{ID: 7, DailyLimitUSD: &dailyLimit},
			})
		case "/api/v1/admin/subscriptions/77/progress":
			stateMu.Lock()
			start := windowStart
			stateMu.Unlock()
			writeRedeemTestEnvelope(w, sub2api.SubscriptionProgress{ID: 77, Daily: &sub2api.UsageWindowProgress{
				LimitUSD: 10, UsedUSD: 4, RemainingUSD: 6, WindowStart: &start,
			}})
		case "/api/v1/admin/subscriptions/77/reset-quota":
			stateMu.Lock()
			windowStart = changedStart
			stateMu.Unlock()
			hijacker, ok := w.(http.Hijacker)
			require.True(t, ok)
			conn, _, hijackErr := hijacker.Hijack()
			require.NoError(t, hijackErr)
			_ = conn.Close()
		case "/api/v1/admin/subscriptions":
			stateMu.Lock()
			start := windowStart
			stateMu.Unlock()
			writeRedeemTestEnvelope(w, subscriptionPageForTest([]sub2api.Subscription{{
				ID: 77, UserID: 1, GroupID: 7, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(30 * 24 * time.Hour), Status: "active",
				DailyWindowStart: &start, Group: &sub2api.Group{ID: 7, DailyLimitUSD: &dailyLimit},
			}}))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(server.URL, "admin-key"), nil)

	result, err := svc.ResetSubscriptionQuota(context.Background(), 1, 77, "33333333-3333-4333-8333-333333333333")
	require.NoError(t, err)
	require.Equal(t, "uncertain", result.Operation.Status)
	var resetUsed int
	require.NoError(t, store.DB.QueryRow(`SELECT reset_used FROM subscription_reset_periods WHERE id = 1`).Scan(&resetUsed))
	require.Equal(t, 1, resetUsed)
	require.NoError(t, svc.ReconcileSubscriptionResetPeriods(context.Background()))
	var status string
	require.NoError(t, store.DB.QueryRow(`SELECT status FROM subscription_reset_attempts`).Scan(&status))
	require.Equal(t, "succeeded", status)
}

func TestResetQuotaOwnershipMismatchIsNotFoundAndRequestIDConflictIsDetectedFirst(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertResetPeriodFixture(t, store, 1, 101, 1, 7, 77, 30, 1, now.Add(-time.Hour), now.Add(30*24*time.Hour), "active")
	svc := newResetTransactionTestService(t, store, now, func(w http.ResponseWriter, r *http.Request) {
		writeRedeemTestEnvelope(w, sub2api.Subscription{ID: 77})
	})
	_, err := store.DB.Exec(`
INSERT INTO subscription_reset_attempts (
  request_id, period_id, upstream_user_id, upstream_subscription_id, status, reserved_at, created_at, updated_at
) VALUES ('44444444-4444-4444-8444-444444444444', 1, 1, 77, 'succeeded', ?, ?, ?)
`, formatTime(now), formatTime(now), formatTime(now))
	require.NoError(t, err)

	_, err = svc.ResetSubscriptionQuota(context.Background(), 2, 77, "55555555-5555-4555-8555-555555555555")
	require.ErrorIs(t, err, ErrNotFound)
	_, err = svc.ResetSubscriptionQuota(context.Background(), 2, 88, "44444444-4444-4444-8444-444444444444")
	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, SubscriptionResetReasonRequestIDConflict, StableReason(err))
}

func TestResolveSubscriptionResetAttemptReleasedIsAtomicAndIdempotent(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertResetPeriodFixture(t, store, 1, 101, 1, 7, 77, 30, 1, now.Add(-time.Hour), now.Add(30*24*time.Hour), "active")
	_, err := store.DB.Exec(`UPDATE subscription_reset_periods SET reset_used = 1 WHERE id = 1`)
	require.NoError(t, err)
	_, err = store.DB.Exec(`
INSERT INTO subscription_reset_attempts (
  request_id, period_id, upstream_user_id, upstream_subscription_id, status, reserved_at, created_at, updated_at
) VALUES ('66666666-6666-4666-8666-666666666666', 1, 1, 77, 'uncertain', ?, ?, ?)
`, formatTime(now), formatTime(now), formatTime(now))
	require.NoError(t, err)
	svc := New(&config.RuntimeConfig{}, store, nil, nil)

	attempt, err := svc.ResolveSubscriptionResetAttempt(context.Background(), 1, 99, "released")
	require.NoError(t, err)
	require.Equal(t, "failed", attempt.Status)
	require.Equal(t, "released", attempt.Resolution)
	_, err = svc.ResolveSubscriptionResetAttempt(context.Background(), 1, 99, "released")
	require.NoError(t, err)
	_, err = svc.ResolveSubscriptionResetAttempt(context.Background(), 1, 99, "consumed")
	require.ErrorIs(t, err, ErrConflict)
	var resetUsed int
	require.NoError(t, store.DB.QueryRow(`SELECT reset_used FROM subscription_reset_periods WHERE id = 1`).Scan(&resetUsed))
	require.Zero(t, resetUsed)
}

func TestResetQuotaConcurrentSameRequestConsumesOnce(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertResetPeriodFixture(t, store, 1, 101, 1, 7, 77, 30, 2, now.Add(-time.Hour), now.Add(30*24*time.Hour), "active")
	dailyLimit := 10.0
	dailyStart := now.Add(-2 * time.Hour)
	resetStarted := make(chan struct{})
	releaseReset := make(chan struct{})
	var callsMu sync.Mutex
	resetCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/subscriptions/77":
			writeRedeemTestEnvelope(w, sub2api.Subscription{ID: 77, UserID: 1, GroupID: 7, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(30 * 24 * time.Hour), Status: "active", DailyUsageUSD: 4, DailyWindowStart: &dailyStart, Group: &sub2api.Group{ID: 7, DailyLimitUSD: &dailyLimit}})
		case "/api/v1/admin/subscriptions/77/progress":
			writeRedeemTestEnvelope(w, sub2api.SubscriptionProgress{ID: 77, Daily: &sub2api.UsageWindowProgress{LimitUSD: 10, UsedUSD: 4, RemainingUSD: 6, WindowStart: &dailyStart}})
		case "/api/v1/admin/subscriptions/77/reset-quota":
			callsMu.Lock()
			resetCalls++
			callsMu.Unlock()
			select {
			case <-resetStarted:
			default:
				close(resetStarted)
			}
			<-releaseReset
			writeRedeemTestEnvelope(w, sub2api.Subscription{ID: 77, UserID: 1, GroupID: 7, Status: "active", Group: &sub2api.Group{ID: 7, DailyLimitUSD: &dailyLimit}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(server.URL, "admin-key"), nil)
	requestID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	type outcome struct {
		result *SubscriptionResetResult
		err    error
	}
	outcomes := make(chan outcome, 2)
	go func() {
		result, err := svc.ResetSubscriptionQuota(context.Background(), 1, 77, requestID)
		outcomes <- outcome{result: result, err: err}
	}()
	<-resetStarted
	go func() {
		result, err := svc.ResetSubscriptionQuota(context.Background(), 1, 77, requestID)
		outcomes <- outcome{result: result, err: err}
	}()
	close(releaseReset)
	first := <-outcomes
	second := <-outcomes
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.Equal(t, first.result.Operation.ID, second.result.Operation.ID)
	callsMu.Lock()
	require.Equal(t, 1, resetCalls)
	callsMu.Unlock()
	var resetUsed int
	require.NoError(t, store.DB.QueryRow(`SELECT reset_used FROM subscription_reset_periods WHERE id = 1`).Scan(&resetUsed))
	require.Equal(t, 1, resetUsed)
}

func TestResetQuotaRejectsNonCanonicalRequestIDBeforeUpstream(t *testing.T) {
	store := newResetPeriodTestStore(t)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(server.URL, "admin-key"), nil)

	_, err := svc.ResetSubscriptionQuota(context.Background(), 1, 77, "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA")
	require.ErrorIs(t, err, ErrBadRequest)
	require.Zero(t, calls)
}

func TestResolveReservedAttemptCannotRaceLiveUpstreamReset(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertResetPeriodFixture(t, store, 1, 101, 1, 7, 77, 30, 2, now.Add(-time.Hour), now.Add(30*24*time.Hour), "active")
	_, err := store.DB.Exec(`UPDATE subscription_reset_periods SET reset_used = 1 WHERE id = 1`)
	require.NoError(t, err)
	dailyLimit := 10.0
	dailyStart := now.Add(-time.Hour)
	resetStarted := make(chan struct{})
	releaseReset := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/subscriptions/77":
			writeRedeemTestEnvelope(w, sub2api.Subscription{ID: 77, UserID: 1, GroupID: 7, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(30 * 24 * time.Hour), Status: "active", DailyUsageUSD: 3, DailyWindowStart: &dailyStart, Group: &sub2api.Group{ID: 7, DailyLimitUSD: &dailyLimit}})
		case "/api/v1/admin/subscriptions/77/progress":
			writeRedeemTestEnvelope(w, sub2api.SubscriptionProgress{ID: 77, Daily: &sub2api.UsageWindowProgress{LimitUSD: 10, UsedUSD: 3, RemainingUSD: 7, WindowStart: &dailyStart}})
		case "/api/v1/admin/subscriptions/77/reset-quota":
			close(resetStarted)
			<-releaseReset
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 409, "message": "rejected", "reason": "quota_locked"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(server.URL, "admin-key"), nil)
	resetDone := make(chan error, 1)
	go func() {
		_, resetErr := svc.ResetSubscriptionQuota(context.Background(), 1, 77, "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
		resetDone <- resetErr
	}()
	<-resetStarted
	resolveDone := make(chan error, 1)
	go func() {
		_, resolveErr := svc.ResolveSubscriptionResetAttempt(context.Background(), 1, 99, "released")
		resolveDone <- resolveErr
	}()
	var earlyResolveErr error
	resolvedEarly := false
	select {
	case resolveErr := <-resolveDone:
		earlyResolveErr = resolveErr
		resolvedEarly = true
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseReset)
	require.ErrorIs(t, <-resetDone, ErrConflict)
	if !resolvedEarly {
		earlyResolveErr = <-resolveDone
	}
	require.Falsef(t, resolvedEarly, "resolution returned before upstream operation completed: %v", earlyResolveErr)
	require.ErrorIs(t, earlyResolveErr, ErrConflict)
	var resetUsed int
	var status, resolution string
	require.NoError(t, store.DB.QueryRow(`SELECT reset_used FROM subscription_reset_periods WHERE id = 1`).Scan(&resetUsed))
	require.NoError(t, store.DB.QueryRow(`SELECT status, resolution FROM subscription_reset_attempts WHERE id = 1`).Scan(&status, &resolution))
	require.Equal(t, 1, resetUsed)
	require.Equal(t, "failed", status)
	require.Empty(t, resolution)
}

func TestResetQuotaRejectsPeriodBoundToDifferentSubscription(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertResetPeriodFixture(t, store, 1, 101, 1, 7, 77, 30, 2, now.Add(-time.Hour), now.Add(30*24*time.Hour), "active")
	dailyLimit := 10.0
	dailyStart := now.Add(-time.Hour)
	resetCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/subscriptions/88":
			writeRedeemTestEnvelope(w, sub2api.Subscription{ID: 88, UserID: 1, GroupID: 7, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(10 * 24 * time.Hour), Status: "active", DailyUsageUSD: 3, DailyWindowStart: &dailyStart, Group: &sub2api.Group{ID: 7, DailyLimitUSD: &dailyLimit}})
		case "/api/v1/admin/subscriptions/88/progress":
			writeRedeemTestEnvelope(w, sub2api.SubscriptionProgress{ID: 88, Daily: &sub2api.UsageWindowProgress{LimitUSD: 10, UsedUSD: 3, RemainingUSD: 7, WindowStart: &dailyStart}})
		case "/api/v1/admin/subscriptions/88/reset-quota":
			resetCalls++
			writeRedeemTestEnvelope(w, sub2api.Subscription{ID: 88})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(server.URL, "admin-key"), nil)

	_, err := svc.ResetSubscriptionQuota(context.Background(), 1, 88, "cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, SubscriptionResetReasonExternalPeriod, StableReason(err))
	require.Zero(t, resetCalls)
	var resetUsed int
	require.NoError(t, store.DB.QueryRow(`SELECT reset_used FROM subscription_reset_periods WHERE id = 1`).Scan(&resetUsed))
	require.Zero(t, resetUsed)
}

func newResetTransactionTestService(t *testing.T, store *db.Store, now time.Time, resetHandler http.HandlerFunc) *Service {
	t.Helper()
	dailyLimit := 10.0
	dailyStart := now.Add(-2 * time.Hour)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/subscriptions/77":
			writeRedeemTestEnvelope(w, sub2api.Subscription{
				ID: 77, UserID: 1, GroupID: 7, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(30 * 24 * time.Hour), Status: "active",
				DailyUsageUSD: 4, DailyWindowStart: &dailyStart, Group: &sub2api.Group{ID: 7, DailyLimitUSD: &dailyLimit},
			})
		case "/api/v1/admin/subscriptions/77/progress":
			writeRedeemTestEnvelope(w, sub2api.SubscriptionProgress{ID: 77, Daily: &sub2api.UsageWindowProgress{
				LimitUSD: 10, UsedUSD: 4, RemainingUSD: 6, WindowStart: &dailyStart,
			}})
		case "/api/v1/admin/subscriptions/77/reset-quota":
			resetHandler(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return New(&config.RuntimeConfig{}, store, sub2api.NewClient(server.URL, "admin-key"), nil)
}
