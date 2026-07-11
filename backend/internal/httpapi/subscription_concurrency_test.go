package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.Equal(t, 0, envelope.Code)
	require.Equal(t, 3, envelope.Data.DefaultConcurrency)
	require.Empty(t, envelope.Data.DefaultConcurrencyError)
}
