package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/app"
	"sub2api-giftcode/backend/internal/config"
	"sub2api-giftcode/backend/internal/db"
	"sub2api-giftcode/backend/internal/mail"
	"sub2api-giftcode/backend/internal/sub2api"
)

func httpAuxModelAccount(id int64, name string, models ...string) sub2api.Account {
	credentials := map[string]any{}
	if len(models) > 0 {
		raw := make([]any, 0, len(models))
		mapping := map[string]any{}
		for _, model := range models {
			raw = append(raw, model)
			mapping[model] = model
		}
		credentials["upstream_supported_models"] = raw
		credentials["model_mapping"] = mapping
	}
	return sub2api.Account{ID: id, Name: name, Platform: "openai", Type: "apikey", Status: "active", Credentials: credentials}
}

func TestAuxSchedulerLaneRuleAuthenticatedCRUDAndUpstreamFailureVisibility(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	var mu sync.Mutex
	accountsUnavailable := false
	lane2Open := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/auth/me":
			writeTestEnvelope(w, sub2api.User{ID: 1, Email: "admin@example.com", Username: "admin", Role: "admin", Status: "active"})
		case "/api/v1/admin/accounts":
			if accountsUnavailable {
				http.Error(w, "accounts unavailable", http.StatusBadGateway)
				return
			}
			account2 := httpAuxModelAccount(2, "two", "o3")
			account2.Schedulable = lane2Open
			writeTestEnvelope(w, map[string]any{
				"items": []sub2api.Account{
					httpAuxModelAccount(1, "one", "gpt-5"),
					account2,
				},
				"total":     2,
				"page":      1,
				"page_size": 200,
				"pages":     1,
			})
		case "/api/v1/admin/accounts/1":
			if accountsUnavailable {
				http.Error(w, "account unavailable", http.StatusBadGateway)
				return
			}
			account := httpAuxModelAccount(1, "one", "gpt-5")
			account.Schedulable = true
			writeTestEnvelope(w, account)
		case "/api/v1/admin/accounts/2":
			if accountsUnavailable {
				http.Error(w, "account unavailable", http.StatusBadGateway)
				return
			}
			account := httpAuxModelAccount(2, "two", "o3")
			account.Schedulable = lane2Open
			writeTestEnvelope(w, account)
		default:
			if accountsUnavailable && strings.HasPrefix(r.URL.Path, "/api/v1/admin/accounts/") {
				http.Error(w, "account unavailable", http.StatusBadGateway)
				return
			}
			if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/schedulable") {
				var body struct {
					Schedulable bool `json:"schedulable"`
				}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				lane2Open = body.Schedulable
				account := httpAuxModelAccount(2, "two", "o3")
				account.Schedulable = lane2Open
				writeTestEnvelope(w, account)
				return
			}
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
	r.ServeHTTP(unauth, httptest.NewRequest(http.MethodPost, "/api/admin/aux-scheduler/rules", strings.NewReader(`{}`)))
	require.Equal(t, http.StatusUnauthorized, unauth.Code)

	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/aux-scheduler/rules", strings.NewReader(`{
	  "name":"lane rule","enabled":true,"model_names":["gpt-5","o3"],
	  "lanes":[[1],[2]],"maximum_auto_lane":2
	}`))
	createReq.Header.Set("Authorization", "Bearer "+token)
	createReq.Header.Set("Content-Type", "application/json")
	createResponse := httptest.NewRecorder()
	r.ServeHTTP(createResponse, createReq)
	require.Equal(t, http.StatusCreated, createResponse.Code)
	var createdEnvelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(createResponse.Body.Bytes(), &createdEnvelope))
	var created struct {
		ID                      int64     `json:"id"`
		ModelNames              []string  `json:"model_names"`
		Lanes                   [][]int64 `json:"lanes"`
		MaximumAutoLane         int       `json:"maximum_auto_lane"`
		MigrationStatus         string    `json:"migration_status"`
		ExpectedOpenThroughLane int       `json:"expected_open_through_lane"`
		ObservedOpenThroughLane int       `json:"observed_open_through_lane"`
		VerifiedOpenThroughLane int       `json:"verified_open_through_lane"`
		TargetOpenThroughLane   int       `json:"target_open_through_lane"`
		TransitionStatus        string    `json:"transition_status"`
		TransitionGeneration    int64     `json:"transition_generation"`
		MissingModels           []string  `json:"missing_models"`
		BlockedReason           string    `json:"blocked_reason"`
	}
	require.NoError(t, json.Unmarshal(createdEnvelope.Data, &created))
	require.Equal(t, []string{"gpt-5", "o3"}, created.ModelNames)
	require.Equal(t, [][]int64{{1}, {2}}, created.Lanes)
	require.Equal(t, 2, created.MaximumAutoLane)
	require.Empty(t, created.MigrationStatus)
	require.Equal(t, 1, created.ExpectedOpenThroughLane)
	require.Equal(t, 1, created.ObservedOpenThroughLane)
	require.Equal(t, 1, created.VerifiedOpenThroughLane)
	require.Equal(t, 1, created.TargetOpenThroughLane)
	require.Equal(t, "stable", created.TransitionStatus)
	require.Empty(t, created.MissingModels)
	require.Empty(t, created.BlockedReason)

	updateReq := httptest.NewRequest(http.MethodPut, "/api/admin/aux-scheduler/rules/1", strings.NewReader(`{
	  "name":"lane rule v2","enabled":true,"model_names":["gpt-5","o3"],
	  "lanes":[[1],[2]],"maximum_auto_lane":2
	}`))
	updateReq.Header.Set("Authorization", "Bearer "+token)
	updateReq.Header.Set("Content-Type", "application/json")
	updateResponse := httptest.NewRecorder()
	r.ServeHTTP(updateResponse, updateReq)
	require.Equal(t, http.StatusOK, updateResponse.Code)

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
		ExpectedOpenThroughLane int      `json:"expected_open_through_lane"`
		VerifiedOpenThroughLane int      `json:"verified_open_through_lane"`
		ObservedOpenThroughLane int      `json:"observed_open_through_lane"`
		TargetOpenThroughLane   int      `json:"target_open_through_lane"`
		TransitionStatus        string   `json:"transition_status"`
		TransitionGeneration    int64    `json:"transition_generation"`
		MissingModels           []string `json:"missing_models"`
		BlockedReason           string   `json:"blocked_reason"`
	}
	require.NoError(t, json.Unmarshal(checkEnvelope.Data, &checked))
	require.Equal(t, "stable", checked.TransitionStatus)
	require.Empty(t, checked.MissingModels)
	require.Empty(t, checked.BlockedReason)

	mu.Lock()
	accountsUnavailable = true
	mu.Unlock()
	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/aux-scheduler/rules", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listResponse := httptest.NewRecorder()
	r.ServeHTTP(listResponse, listReq)
	require.Equal(t, http.StatusOK, listResponse.Code)
	var listEnvelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(listResponse.Body.Bytes(), &listEnvelope))
	var rules []struct {
		ID            int64  `json:"id"`
		Name          string `json:"name"`
		UpstreamError string `json:"upstream_error"`
		LaneAccounts  []struct {
			Accounts []struct {
				ID int64 `json:"id"`
			} `json:"accounts"`
		} `json:"lane_accounts"`
	}
	require.NoError(t, json.Unmarshal(listEnvelope.Data, &rules))
	require.Len(t, rules, 1)
	require.Equal(t, "lane rule v2", rules[0].Name)
	require.NotEmpty(t, rules[0].UpstreamError)
	require.Len(t, rules[0].LaneAccounts[0].Accounts, 1)

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/aux-scheduler/rules/1", nil)
	deleteReq.Header.Set("Authorization", "Bearer "+token)
	deleteResponse := httptest.NewRecorder()
	r.ServeHTTP(deleteResponse, deleteReq)
	require.Equal(t, http.StatusInternalServerError, deleteResponse.Code)

	mu.Lock()
	accountsUnavailable = false
	mu.Unlock()
	deleteReq = httptest.NewRequest(http.MethodDelete, "/api/admin/aux-scheduler/rules/1", nil)
	deleteReq.Header.Set("Authorization", "Bearer "+token)
	deleteResponse = httptest.NewRecorder()
	r.ServeHTTP(deleteResponse, deleteReq)
	require.Equal(t, http.StatusOK, deleteResponse.Code)

	afterDeleteReq := httptest.NewRequest(http.MethodGet, "/api/admin/aux-scheduler/rules", nil)
	afterDeleteReq.Header.Set("Authorization", "Bearer "+token)
	afterDeleteResponse := httptest.NewRecorder()
	r.ServeHTTP(afterDeleteResponse, afterDeleteReq)
	require.Equal(t, http.StatusOK, afterDeleteResponse.Code)
	var afterDeleteEnvelope struct {
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(afterDeleteResponse.Body.Bytes(), &afterDeleteEnvelope))
	var remaining []map[string]any
	require.NoError(t, json.Unmarshal(afterDeleteEnvelope.Data, &remaining))
	require.Empty(t, remaining)
}

func TestAuxSchedulerLaneRuleConflictThroughAuthenticatedRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/me":
			writeTestEnvelope(w, sub2api.User{ID: 1, Email: "admin@example.com", Username: "admin", Role: "admin", Status: "active"})
		case "/api/v1/admin/accounts":
			writeTestEnvelope(w, map[string]any{
				"items": []sub2api.Account{
					httpAuxModelAccount(1, "one", "gpt-5"),
					httpAuxModelAccount(2, "two", "o3"),
					httpAuxModelAccount(3, "three", "gpt-5", "o3"),
				},
				"total":     3,
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

	create := func(name string, lanes string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/aux-scheduler/rules", strings.NewReader(`{
		  "name":"`+name+`","enabled":true,"model_names":["gpt-5","o3"],
		  "lanes":`+lanes+`,"maximum_auto_lane":2
		}`))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		r.ServeHTTP(response, req)
		return response.Code
	}
	require.Equal(t, http.StatusCreated, create("A", `[[1],[2]]`))
	require.Equal(t, http.StatusBadRequest, create("B", `[[3],[2]]`))
}
