package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/app"
	"sub2api-giftcode/backend/internal/config"
	"sub2api-giftcode/backend/internal/db"
	"sub2api-giftcode/backend/internal/sub2api"
)

func TestSubscriptionResetBonusHandlersPreviewCreateListAndDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC().Truncate(time.Second)
	limit := 10.0
	group := sub2api.Group{ID: 7, Name: "Std", Status: "active", DailyLimitUSD: &limit}
	subscription := sub2api.Subscription{ID: 71, UserID: 1, GroupID: 7, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), Status: "active", Group: &group}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/groups/all":
			writeTestEnvelope(w, []sub2api.Group{group})
		case "/api/v1/admin/users/1":
			writeTestEnvelope(w, sub2api.User{ID: 1, Email: "user@example.com"})
		case "/api/v1/admin/subscriptions":
			writeTestEnvelope(w, map[string]any{"items": []sub2api.Subscription{subscription}, "total": 1, "page": 1, "page_size": 100, "pages": 1})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))
	cfg := &config.RuntimeConfig{}
	cfg.Session.CookieSecret = "bonus-handler-secret"
	svc := app.New(cfg, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	handlers := &Handlers{cfg: cfg, service: svc}
	operator := &app.SessionUser{User: sub2api.User{ID: 99, Email: "admin@example.com"}, IsAdmin: true}

	previewRecorder := httptest.NewRecorder()
	previewCtx, _ := gin.CreateTestContext(previewRecorder)
	previewCtx.Request = httptest.NewRequest(http.MethodPost, "/api/admin/subscription-reset-bonus-batches/preview", strings.NewReader(`{"target_scope":"selected","selected_user_ids":[1],"group_ids":[7],"reset_count":2,"note":"campaign"}`))
	previewCtx.Request.Header.Set("Content-Type", "application/json")
	withSessionUser(previewCtx, operator)
	handlers.PreviewSubscriptionResetBonusBatch(previewCtx)
	require.Equal(t, http.StatusOK, previewRecorder.Code)
	var previewEnvelope struct {
		Data struct {
			PreviewToken string `json:"preview_token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(previewRecorder.Body.Bytes(), &previewEnvelope))
	require.NotEmpty(t, previewEnvelope.Data.PreviewToken)

	createRecorder := httptest.NewRecorder()
	createCtx, _ := gin.CreateTestContext(createRecorder)
	createCtx.Request = httptest.NewRequest(http.MethodPost, "/api/admin/subscription-reset-bonus-batches", strings.NewReader(`{"preview_token":"`+previewEnvelope.Data.PreviewToken+`"}`))
	createCtx.Request.Header.Set("Content-Type", "application/json")
	withSessionUser(createCtx, operator)
	handlers.CreateSubscriptionResetBonusBatch(createCtx)
	require.Equal(t, http.StatusAccepted, createRecorder.Code)

	listRecorder := httptest.NewRecorder()
	listCtx, _ := gin.CreateTestContext(listRecorder)
	listCtx.Request = httptest.NewRequest(http.MethodGet, "/api/admin/subscription-reset-bonus-batches", nil)
	handlers.ListSubscriptionResetBonusBatches(listCtx)
	require.Equal(t, http.StatusOK, listRecorder.Code)

	detailRecorder := httptest.NewRecorder()
	detailCtx, _ := gin.CreateTestContext(detailRecorder)
	detailCtx.Request = httptest.NewRequest(http.MethodGet, "/api/admin/subscription-reset-bonus-batches/1/details", nil)
	detailCtx.Params = gin.Params{{Key: "id", Value: "1"}}
	handlers.ListSubscriptionResetBonusBatchDetails(detailCtx)
	require.Equal(t, http.StatusOK, detailRecorder.Code)
}
