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

const httpAPILegacyAuxSchedulerTableSQL = `CREATE TABLE aux_scheduler_rules (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  primary_account_ids_json TEXT NOT NULL DEFAULT '[]',
  backup_account_ids_json TEXT NOT NULL DEFAULT '[]',
  state TEXT NOT NULL DEFAULT 'idle' CHECK (state IN ('idle', 'backup_active')),
  activated_at TEXT NULL,
  last_checked_at TEXT NULL,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

func TestAuxSchedulerMigrationStatusThroughAuthenticatedRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	_, err = store.DB.ExecContext(context.Background(), httpAPILegacyAuxSchedulerTableSQL)
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Second)
	_, err = store.DB.ExecContext(context.Background(), `
INSERT INTO aux_scheduler_rules (
  name, enabled, primary_account_ids_json, backup_account_ids_json, state,
  last_error, created_at, updated_at
) VALUES ('legacy route', 1, '[1]', '[2]', 'backup_active', '', ?, ?)
`, now.Add(-time.Hour).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	require.NoError(t, err)
	require.NoError(t, store.Migrate(context.Background()))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/me":
			writeTestEnvelope(w, sub2api.User{ID: 1, Email: "admin@example.com", Username: "admin", Role: "admin", Status: "active"})
		case "/api/v1/admin/accounts":
			writeTestEnvelope(w, map[string]any{
				"items": []sub2api.Account{
					{ID: 1, Name: "primary", Platform: "openai", Type: "oauth", Status: "active", Schedulable: true},
					{ID: 2, Name: "backup", Platform: "openai", Type: "apikey", Status: "active", Schedulable: true},
				},
				"total":     2,
				"page":      1,
				"page_size": 200,
				"pages":     1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	cfg := &config.RuntimeConfig{Config: config.Config{}}
	cfg.Session.CookieSecret = "test-secret"
	svc := app.New(cfg, store, sub2api.NewClient(upstream.URL, "admin-key"), mail.New(mail.Config{}))
	sessionUser, err := svc.LoginWithAccessToken(context.Background(), "admin-token", nil)
	require.NoError(t, err)
	token, err := sessionTokenFor(cfg.Session.CookieSecret, sessionUser.Session.ID)
	require.NoError(t, err)

	r := NewRouter(cfg, svc)
	unauth := httptest.NewRecorder()
	r.ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, "/api/admin/aux-scheduler/rules", nil))
	require.Equal(t, http.StatusUnauthorized, unauth.Code)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/aux-scheduler/rules", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	r.ServeHTTP(response, req)
	require.Equal(t, http.StatusOK, response.Code)
	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.Equal(t, 0, envelope.Code)
	var rules []struct {
		ID              int64     `json:"id"`
		MigrationStatus string    `json:"migration_status"`
		Lanes           [][]int64 `json:"lanes"`
		ModelNames      []string  `json:"model_names"`
		LaneAccounts    []struct {
			Number   int `json:"number"`
			Accounts []struct {
				ID          int64  `json:"id"`
				Name        string `json:"name"`
				Schedulable bool   `json:"schedulable"`
			} `json:"accounts"`
		} `json:"lane_accounts"`
	}
	require.NoError(t, json.Unmarshal(envelope.Data, &rules))
	require.Len(t, rules, 1)
	require.Equal(t, "needs_migration", rules[0].MigrationStatus)
	require.Equal(t, [][]int64{{1}, {2}}, rules[0].Lanes)
	require.Empty(t, rules[0].ModelNames)
	require.Len(t, rules[0].LaneAccounts, 2)
	require.True(t, rules[0].LaneAccounts[1].Accounts[0].Schedulable)

	checkReq := httptest.NewRequest(http.MethodPost, "/api/admin/aux-scheduler/rules/1/check", nil)
	checkReq.Header.Set("Authorization", "Bearer "+token)
	checkResponse := httptest.NewRecorder()
	r.ServeHTTP(checkResponse, checkReq)
	require.Equal(t, http.StatusOK, checkResponse.Code)
	var checkEnvelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(checkResponse.Body.Bytes(), &checkEnvelope))
	var checked struct {
		MigrationStatus string `json:"migration_status"`
	}
	require.NoError(t, json.Unmarshal(checkEnvelope.Data, &checked))
	require.Equal(t, "needs_migration", checked.MigrationStatus)
	var liveState struct {
		State string `json:"state"`
	}
	require.NoError(t, json.Unmarshal(checkEnvelope.Data, &liveState))
	require.Equal(t, "idle", liveState.State)
}
