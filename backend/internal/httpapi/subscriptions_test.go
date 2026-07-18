package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/app"
	"sub2api-giftcode/backend/internal/config"
	"sub2api-giftcode/backend/internal/db"
	"sub2api-giftcode/backend/internal/sub2api"
)

func TestSubscriptionsHTTPStatusesAndIdempotency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newSubscriptionHTTPStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertSubscriptionHTTPPeriod(t, store, now)
	dailyLimit := 10.0
	dailyStart := now.Add(-time.Hour)
	var listUnavailable atomic.Bool
	var detailUnavailable atomic.Bool
	var resetCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/me":
			writeTestEnvelope(w, sub2api.User{ID: 1, Email: "user@example.com", Username: "user", Role: "user", Status: "active"})
		case "/api/v1/admin/subscriptions":
			if listUnavailable.Load() {
				http.Error(w, "unavailable", http.StatusBadGateway)
				return
			}
			writeTestEnvelope(w, map[string]any{"items": []sub2api.Subscription{{ID: 77, UserID: 1, GroupID: 7, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), Status: "active", DailyUsageUSD: 3, DailyWindowStart: &dailyStart, Group: &sub2api.Group{ID: 7, Name: "Daily", DailyLimitUSD: &dailyLimit}}}, "total": 1, "page": 1, "page_size": 100, "pages": 1})
		case "/api/v1/admin/subscriptions/77":
			if detailUnavailable.Load() {
				http.Error(w, "unavailable", http.StatusBadGateway)
				return
			}
			writeTestEnvelope(w, sub2api.Subscription{ID: 77, UserID: 1, GroupID: 7, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), Status: "active", DailyUsageUSD: 3, DailyWindowStart: &dailyStart, Group: &sub2api.Group{ID: 7, Name: "Daily", DailyLimitUSD: &dailyLimit}})
		case "/api/v1/admin/subscriptions/88":
			writeTestEnvelope(w, sub2api.Subscription{ID: 88, UserID: 2, GroupID: 7, Status: "active", Group: &sub2api.Group{ID: 7, DailyLimitUSD: &dailyLimit}})
		case "/api/v1/admin/subscriptions/77/progress":
			if detailUnavailable.Load() {
				http.Error(w, "unavailable", http.StatusBadGateway)
				return
			}
			writeTestEnvelope(w, sub2api.SubscriptionProgress{ID: 77, Daily: &sub2api.UsageWindowProgress{LimitUSD: 10, UsedUSD: 3, RemainingUSD: 7, WindowStart: &dailyStart}})
		case "/api/v1/admin/subscriptions/77/reset-quota":
			resetCalls.Add(1)
			writeTestEnvelope(w, sub2api.Subscription{ID: 77, UserID: 1, GroupID: 7, Status: "active", Group: &sub2api.Group{ID: 7, DailyLimitUSD: &dailyLimit}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	router, token := newSubscriptionHTTPRouter(t, store, upstream.URL, false)

	response := performSubscriptionRequest(t, router, token, http.MethodGet, "/api/subscriptions", nil)
	require.Equal(t, http.StatusOK, response.Code)

	requestID := "77777777-7777-4777-8777-777777777777"
	response = performSubscriptionRequest(t, router, token, http.MethodPost, "/api/subscriptions/77/reset-quota", map[string]string{"request_id": requestID})
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"upstream_status":200`)
	require.Contains(t, response.Body.String(), `"subscription"`)
	require.Contains(t, response.Body.String(), `"quota_windows"`)
	detailUnavailable.Store(true)
	response = performSubscriptionRequest(t, router, token, http.MethodPost, "/api/subscriptions/77/reset-quota", map[string]string{"request_id": requestID})
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"subscription"`)
	require.Contains(t, response.Body.String(), `"disabled_reason":"upstream_unavailable"`)
	require.Equal(t, int32(1), resetCalls.Load())

	response = performSubscriptionRequest(t, router, token, http.MethodPost, "/api/subscriptions/88/reset-quota", map[string]string{"request_id": "88888888-8888-4888-8888-888888888888"})
	require.Equal(t, http.StatusNotFound, response.Code)
	response = performSubscriptionRequest(t, router, token, http.MethodPost, "/api/subscriptions/88/reset-quota", map[string]string{"request_id": requestID})
	require.Equal(t, http.StatusConflict, response.Code)
	require.Contains(t, response.Body.String(), app.SubscriptionResetReasonRequestIDConflict)

	listUnavailable.Store(true)
	response = performSubscriptionRequest(t, router, token, http.MethodGet, "/api/subscriptions", nil)
	require.Equal(t, http.StatusBadGateway, response.Code)
}

func TestResetQuotaHTTPReturnsAcceptedForUnknownResultAndAdminCanRelease(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newSubscriptionHTTPStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertSubscriptionHTTPPeriod(t, store, now)
	dailyLimit := 10.0
	dailyStart := now.Add(-time.Hour)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/me":
			writeTestEnvelope(w, sub2api.User{ID: 1, Email: "admin@example.com", Username: "admin", Role: "admin", Status: "active"})
		case "/api/v1/admin/subscriptions/77":
			writeTestEnvelope(w, sub2api.Subscription{ID: 77, UserID: 1, GroupID: 7, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), Status: "active", DailyUsageUSD: 3, DailyWindowStart: &dailyStart, Group: &sub2api.Group{ID: 7, DailyLimitUSD: &dailyLimit}})
		case "/api/v1/admin/subscriptions/77/progress":
			writeTestEnvelope(w, sub2api.SubscriptionProgress{ID: 77, Daily: &sub2api.UsageWindowProgress{LimitUSD: 10, UsedUSD: 3, RemainingUSD: 7, WindowStart: &dailyStart}})
		case "/api/v1/admin/subscriptions/77/reset-quota":
			hijacker, ok := w.(http.Hijacker)
			require.True(t, ok)
			conn, _, err := hijacker.Hijack()
			require.NoError(t, err)
			_ = conn.Close()
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	router, token := newSubscriptionHTTPRouter(t, store, upstream.URL, true)

	response := performSubscriptionRequest(t, router, token, http.MethodPost, "/api/subscriptions/77/reset-quota", map[string]string{"request_id": "99999999-9999-4999-8999-999999999999"})
	require.Equal(t, http.StatusAccepted, response.Code)
	response = performSubscriptionRequest(t, router, token, http.MethodGet, "/api/admin/subscription-reset-attempts", nil)
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "uncertain")
	require.Contains(t, response.Body.String(), `"username":"admin"`)
	require.Contains(t, response.Body.String(), `"before_snapshot"`)
	require.Contains(t, response.Body.String(), `"current_snapshot"`)
	require.Contains(t, response.Body.String(), `"period"`)
	response = performSubscriptionRequest(t, router, token, http.MethodPost, "/api/admin/subscription-reset-attempts/1/resolve", map[string]string{"resolution": "released"})
	require.Equal(t, http.StatusOK, response.Code)
	response = performSubscriptionRequest(t, router, token, http.MethodGet, "/api/admin/subscription-reset-backfills", nil)
	require.Equal(t, http.StatusOK, response.Code)
}

func newSubscriptionHTTPStore(t *testing.T) *db.Store {
	t.Helper()
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))
	return store
}

func insertSubscriptionHTTPPeriod(t *testing.T, store *db.Store, now time.Time) {
	t.Helper()
	_, err := store.DB.Exec(`INSERT INTO subscription_reset_periods (id, access_request_id, upstream_user_id, tier_id, sub2api_group_id, upstream_subscription_id, validity_days, reset_limit, fulfilled_at, fulfillment_order, period_start, period_end, status, created_at, updated_at) VALUES (1, 101, 1, 1, 7, 77, 30, 2, ?, 101, ?, ?, 'active', ?, ?)`, now.Format(time.RFC3339Nano), now.Add(-time.Hour).Format(time.RFC3339Nano), now.Add(24*time.Hour).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	require.NoError(t, err)
}

func newSubscriptionHTTPRouter(t *testing.T, store *db.Store, upstreamURL string, admin bool) (*gin.Engine, string) {
	t.Helper()
	cfg := &config.RuntimeConfig{Config: config.Config{}}
	cfg.Session.CookieSecret = "subscription-test-secret"
	svc := app.New(cfg, store, sub2api.NewClient(upstreamURL, "admin-key"), nil)
	sessionUser, err := svc.LoginWithAccessToken(context.Background(), "access-token", nil)
	require.NoError(t, err)
	require.Equal(t, admin, sessionUser.IsAdmin)
	token, err := sessionTokenFor(cfg.Session.CookieSecret, sessionUser.Session.ID)
	require.NoError(t, err)
	return NewRouter(cfg, svc), token
}

func performSubscriptionRequest(t *testing.T, router http.Handler, token, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		require.NoError(t, err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response
}
