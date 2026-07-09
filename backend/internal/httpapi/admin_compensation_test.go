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

func TestCompensationHandlersCreateListAndDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/users":
			writeTestEnvelope(w, map[string]any{
				"items": []sub2api.User{
					{ID: 11, Email: "sub@example.com", Username: "sub", Status: "active", Balance: 10},
					{ID: 12, Email: "bal@example.com", Username: "bal", Status: "active", Balance: 8},
				},
				"total":     2,
				"page":      1,
				"page_size": 100,
				"pages":     1,
			})
		case "/api/v1/admin/subscriptions":
			if r.URL.Query().Get("user_id") == "11" {
				writeTestEnvelope(w, map[string]any{
					"items": []sub2api.Subscription{
						{ID: 301, UserID: 11, GroupID: 3, Status: "active", ExpiresAt: time.Now().UTC().Add(20 * 24 * time.Hour)},
					},
					"total":     1,
					"page":      1,
					"page_size": 100,
					"pages":     1,
				})
				return
			}
			writeTestEnvelope(w, map[string]any{
				"items":     []sub2api.Subscription{},
				"total":     0,
				"page":      1,
				"page_size": 100,
				"pages":     0,
			})
		case "/api/v1/admin/subscriptions/301/extend":
			writeTestEnvelope(w, sub2api.Subscription{ID: 301, UserID: 11, GroupID: 3, Status: "active", ExpiresAt: time.Now().UTC().Add(50 * 24 * time.Hour)})
		case "/api/v1/admin/users/12/balance":
			writeTestEnvelope(w, sub2api.User{ID: 12, Email: "bal@example.com", Username: "bal", Status: "active", Balance: 18})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	cfg := &config.RuntimeConfig{}
	svc := app.New(cfg, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	handlers := &Handlers{cfg: cfg, service: svc}
	sessionUser := &app.SessionUser{
		User:    sub2api.User{ID: 1, Email: "admin@example.com", Username: "admin", Role: "admin", Status: "active"},
		IsAdmin: true,
	}

	createRecorder := httptest.NewRecorder()
	createCtx, _ := gin.CreateTestContext(createRecorder)
	createCtx.Request = httptest.NewRequest(http.MethodPost, "/api/admin/compensation-batches", strings.NewReader(`{"subscription_days":30,"balance_amount":10,"excluded_domains":["blocked.com"],"note":"ops note"}`))
	createCtx.Request.Header.Set("Content-Type", "application/json")
	withSessionUser(createCtx, sessionUser)
	handlers.CreateCompensationBatch(createCtx)
	require.Equal(t, http.StatusCreated, createRecorder.Code)

	var createEnvelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(createRecorder.Body.Bytes(), &createEnvelope))
	require.Equal(t, 0, createEnvelope.Code)

	var created struct {
		ID                      int64  `json:"id"`
		Status                  string `json:"status"`
		SubscriptionCompensated int    `json:"subscription_compensated_users"`
		BalanceCompensated      int    `json:"balance_compensated_users"`
	}
	require.NoError(t, json.Unmarshal(createEnvelope.Data, &created))
	require.Equal(t, "completed", created.Status)
	require.Equal(t, 1, created.SubscriptionCompensated)
	require.Equal(t, 1, created.BalanceCompensated)

	listRecorder := httptest.NewRecorder()
	listCtx, _ := gin.CreateTestContext(listRecorder)
	listCtx.Request = httptest.NewRequest(http.MethodGet, "/api/admin/compensation-batches", nil)
	handlers.ListCompensationBatches(listCtx)
	require.Equal(t, http.StatusOK, listRecorder.Code)

	detailRecorder := httptest.NewRecorder()
	detailCtx, _ := gin.CreateTestContext(detailRecorder)
	detailCtx.Request = httptest.NewRequest(http.MethodGet, "/api/admin/compensation-batches/1/details", nil)
	detailCtx.Params = gin.Params{{Key: "id", Value: "1"}}
	handlers.ListCompensationBatchDetails(detailCtx)
	require.Equal(t, http.StatusOK, detailRecorder.Code)

	var detailEnvelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(detailRecorder.Body.Bytes(), &detailEnvelope))
	var details []map[string]any
	require.NoError(t, json.Unmarshal(detailEnvelope.Data, &details))
	require.Len(t, details, 2)
}
