package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/app"
	"sub2api-giftcode/backend/internal/config"
	"sub2api-giftcode/backend/internal/db"
	"sub2api-giftcode/backend/internal/mail"
	"sub2api-giftcode/backend/internal/sub2api"
)

func TestSubscriptionConcurrencyMonitorStatusEndpointRequiresAdminAndReturnsStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))
	now := time.Now().UTC().Truncate(time.Second)
	for i, status := range []string{"active", "pending", "inactive", "inactive"} {
		_, err := store.DB.Exec(`INSERT INTO subscription_concurrency_grants (access_request_id, upstream_user_id, tier_id, sub2api_group_id, desired_concurrency, status, last_error, created_at, updated_at) VALUES (?, 1, 1, 7, 12, ?, ?, ?, ?)`, i+1, status, map[bool]string{true: "retry failed", false: ""}[i == 3], now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
		require.NoError(t, err)
	}
	_, err = store.DB.Exec(`INSERT INTO subscription_concurrency_user_states (upstream_user_id, manual_override, manual_override_concurrency, created_at, updated_at) VALUES (1, 1, 20, ?, ?)`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	require.NoError(t, err)
	_, err = store.DB.Exec(`INSERT INTO sync_state (key, value, updated_at) VALUES (?, ?, ?), (?, ?, ?), (?, ?, ?)`,
		"subscription_concurrency_last_reconciliation_at", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		"subscription_concurrency_latest_error", "user 2: upstream unavailable", now.Format(time.RFC3339Nano),
		"subscription_concurrency_latest_error_at", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	require.NoError(t, err)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/me":
			writeTestEnvelope(w, sub2api.User{ID: 1, Email: "admin@example.com", Username: "admin", Role: "admin", Status: "active"})
		case "/api/v1/admin/settings":
			writeTestEnvelope(w, map[string]any{"default_concurrency": 3})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	cfg := &config.RuntimeConfig{Config: config.Config{}}
	cfg.Session.CookieSecret = "test-secret"
	svc := app.New(cfg, store, sub2api.NewClient(upstream.URL, "admin-key"), mail.New(mail.Config{}))
	sessionUser, err := svc.LoginWithAccessToken(context.Background(), "admin-token", nil)
	require.NoError(t, err)
	token, err := sessionTokenFor(cfg.Session.CookieSecret, sessionUser.Session.ID)
	require.NoError(t, err)

	r := NewRouter(cfg, svc)
	unauthenticated := httptest.NewRecorder()
	r.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/admin/subscription-concurrency/status", nil))
	require.Equal(t, http.StatusUnauthorized, unauthenticated.Code)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/subscription-concurrency/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	r.ServeHTTP(response, req)
	require.Equal(t, http.StatusOK, response.Code)
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			DefaultConcurrency      int    `json:"default_concurrency"`
			DefaultConcurrencyError string `json:"default_concurrency_error"`
			LastReconciliationAt    string `json:"last_reconciliation_at"`
			ActiveGrants            int    `json:"active_grants"`
			PendingGrants           int    `json:"pending_grants"`
			InactiveGrants          int    `json:"inactive_grants"`
			ErrorGrants             int    `json:"error_grants"`
			ManualOverrideUsers     int    `json:"manual_override_users"`
			LatestError             string `json:"latest_error"`
			LatestErrorAt           string `json:"latest_error_at"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.Equal(t, 0, envelope.Code)
	require.Equal(t, 3, envelope.Data.DefaultConcurrency)
	require.Empty(t, envelope.Data.DefaultConcurrencyError)
	require.Equal(t, now.Format(time.RFC3339Nano), envelope.Data.LastReconciliationAt)
	require.Equal(t, 1, envelope.Data.ActiveGrants)
	require.Equal(t, 1, envelope.Data.PendingGrants)
	require.Equal(t, 2, envelope.Data.InactiveGrants)
	require.Equal(t, 1, envelope.Data.ErrorGrants)
	require.Equal(t, 1, envelope.Data.ManualOverrideUsers)
	require.Equal(t, "user 2: upstream unavailable", envelope.Data.LatestError)
	require.Equal(t, now.Format(time.RFC3339Nano), envelope.Data.LatestErrorAt)

	detailsRequest := httptest.NewRequest(http.MethodGet, "/api/admin/subscription-concurrency/details", nil)
	detailsRequest.Header.Set("Authorization", "Bearer "+token)
	detailsResponse := httptest.NewRecorder()
	r.ServeHTTP(detailsResponse, detailsRequest)
	require.Equal(t, http.StatusOK, detailsResponse.Code)
	var detailsEnvelope struct {
		Data []struct {
			UpstreamUserID      int64 `json:"upstream_user_id"`
			ManualOverrideUsers bool  `json:"manual_override"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(detailsResponse.Body.Bytes(), &detailsEnvelope))
	require.Len(t, detailsEnvelope.Data, 1)
	require.Equal(t, int64(1), detailsEnvelope.Data[0].UpstreamUserID)
	require.True(t, detailsEnvelope.Data[0].ManualOverrideUsers)
}

func TestSubscriptionConcurrencyMonitorStatusEndpointReportsDefaultFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/me":
			writeTestEnvelope(w, sub2api.User{ID: 1, Email: "admin@example.com", Username: "admin", Role: "admin", Status: "active"})
		case "/api/v1/admin/settings":
			writeTestEnvelope(w, map[string]any{"default_concurrency": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	cfg := &config.RuntimeConfig{Config: config.Config{}}
	cfg.Session.CookieSecret = "test-secret"
	svc := app.New(cfg, store, sub2api.NewClient(upstream.URL, "admin-key"), mail.New(mail.Config{}))
	sessionUser, err := svc.LoginWithAccessToken(context.Background(), "admin-token", nil)
	require.NoError(t, err)
	token, err := sessionTokenFor(cfg.Session.CookieSecret, sessionUser.Session.ID)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/subscription-concurrency/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	NewRouter(cfg, svc).ServeHTTP(response, req)
	require.Equal(t, http.StatusOK, response.Code)
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			DefaultConcurrency      int    `json:"default_concurrency"`
			DefaultConcurrencyError string `json:"default_concurrency_error"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.Equal(t, 0, envelope.Code)
	require.Zero(t, envelope.Data.DefaultConcurrency)
	require.Contains(t, envelope.Data.DefaultConcurrencyError, "invalid default concurrency")
}
