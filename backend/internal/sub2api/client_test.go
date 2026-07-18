package sub2api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClientLoginAndAdminGenerate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			require.Equal(t, http.MethodPost, r.Method)
			body, _ := io.ReadAll(r.Body)
			var loginReq map[string]any
			require.NoError(t, json.Unmarshal(body, &loginReq))
			require.Equal(t, "alice@example.com", loginReq["email"])
			writeEnvelope(w, map[string]any{
				"access_token":  "access-1",
				"refresh_token": "refresh-1",
				"expires_in":    3600,
				"token_type":    "Bearer",
				"user": User{
					ID:        7,
					Email:     "alice@example.com",
					Username:  "alice",
					Role:      "user",
					Status:    "active",
					CreatedAt: time.Now().UTC(),
					UpdatedAt: time.Now().UTC(),
				},
			})
		case "/api/v1/auth/me":
			require.Equal(t, "Bearer access-1", r.Header.Get("Authorization"))
			writeEnvelope(w, User{ID: 7, Email: "alice@example.com", Username: "alice", Role: "user", Status: "active"})
		case "/api/v1/admin/redeem-codes/generate":
			require.Equal(t, "admin-key", r.Header.Get("x-api-key"))
			require.NotEmpty(t, r.Header.Get("Idempotency-Key"))
			body, _ := io.ReadAll(r.Body)
			var genReq map[string]any
			require.NoError(t, json.Unmarshal(body, &genReq))
			require.Equal(t, "balance", genReq["type"])
			writeEnvelope(w, []RedeemCode{{
				ID:        99,
				Code:      "code-99",
				Type:      "balance",
				Value:     120,
				Status:    "unused",
				CreatedAt: time.Now().UTC(),
			}})
		case "/api/v1/admin/redeem-codes":
			require.Equal(t, "admin-key", r.Header.Get("x-api-key"))
			writeEnvelope(w, paginatedEnvelope[RedeemCode]{Items: []RedeemCode{{Code: "code-99", Status: "used"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "admin-key")
	login, err := client.Login(context.Background(), "alice@example.com", "secret")
	require.NoError(t, err)
	require.Equal(t, "access-1", login.AccessToken)

	me, err := client.Me(context.Background(), "access-1")
	require.NoError(t, err)
	require.Equal(t, "alice", me.Username)

	codes, err := client.GenerateRedeemCodes(context.Background(), "idemp-1", GenerateRedeemCodesInput{Type: "balance", Value: 120})
	require.NoError(t, err)
	require.Len(t, codes, 1)
	require.Equal(t, "code-99", codes[0].Code)

	listed, err := client.ListRedeemCodes(context.Background(), "code-99", 10)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, "code-99", listed[0].Code)
}

func TestClientListsSubscriptionGroupsAndGeneratesSubscriptionCode(t *testing.T) {
	var sawGenerate map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/groups/all":
			require.Equal(t, "admin-key", r.Header.Get("x-api-key"))
			writeEnvelope(w, []Group{
				{
					ID:               1,
					Name:             "Standard balance group",
					Platform:         "openai",
					Status:           "active",
					SubscriptionType: "standard",
				},
				{
					ID:               2,
					Name:             "Claude monthly",
					Platform:         "anthropic",
					Status:           "active",
					SubscriptionType: "subscription",
					DailyLimitUSD:    floatPtr(10),
					WeeklyLimitUSD:   floatPtr(50),
					MonthlyLimitUSD:  floatPtr(120),
				},
			})
		case "/api/v1/admin/redeem-codes/generate":
			require.Equal(t, "admin-key", r.Header.Get("x-api-key"))
			require.Equal(t, "sub-idemp", r.Header.Get("Idempotency-Key"))
			require.NoError(t, json.NewDecoder(r.Body).Decode(&sawGenerate))
			writeEnvelope(w, []RedeemCode{{
				ID:           200,
				Code:         "sub-code-200",
				Type:         "subscription",
				Value:        0,
				Status:       "unused",
				GroupID:      int64Ptr(2),
				ValidityDays: 30,
				CreatedAt:    time.Now().UTC(),
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "admin-key")
	groups, err := client.ListGroupsAll(context.Background())
	require.NoError(t, err)
	require.Len(t, groups, 2)
	require.Equal(t, "Claude monthly", groups[1].Name)
	require.Equal(t, "subscription", groups[1].SubscriptionType)
	require.Equal(t, 10.0, *groups[1].DailyLimitUSD)
	require.Equal(t, 50.0, *groups[1].WeeklyLimitUSD)
	require.Equal(t, 120.0, *groups[1].MonthlyLimitUSD)

	codes, err := client.GenerateRedeemCodes(context.Background(), "sub-idemp", GenerateRedeemCodesInput{
		Type:         "subscription",
		Value:        0,
		GroupID:      int64Ptr(2),
		ValidityDays: 30,
	})
	require.NoError(t, err)
	require.Len(t, codes, 1)
	require.Equal(t, "sub-code-200", codes[0].Code)
	require.Equal(t, "subscription", sawGenerate["type"])
	require.Equal(t, float64(2), sawGenerate["group_id"])
	require.Equal(t, float64(30), sawGenerate["validity_days"])
	require.Equal(t, float64(0), sawGenerate["value"])
}

func TestClientListsOpenAIAccounts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/admin/accounts", r.URL.Path)
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "admin-key", r.Header.Get("x-api-key"))
		require.Equal(t, "openai", r.URL.Query().Get("platform"))
		require.Equal(t, "1", r.URL.Query().Get("page"))
		require.Equal(t, "200", r.URL.Query().Get("page_size"))
		writeEnvelope(w, paginatedEnvelope[Account]{
			Items: []Account{
				{
					ID:       42,
					Name:     "openai-api-key",
					Platform: "openai",
					Type:     "api_key",
					Status:   "active",
					Credentials: map[string]any{
						"user_agent": "fixed-upstream-ua",
					},
				},
			},
			Total:    1,
			Page:     1,
			PageSize: 200,
			Pages:    1,
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "admin-key")
	accounts, err := client.ListOpenAIAccounts(context.Background())
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	require.Equal(t, int64(42), accounts[0].ID)
	require.Equal(t, "api_key", accounts[0].Type)
	require.Equal(t, "fixed-upstream-ua", accounts[0].UserAgent())
}

func TestClientUpdatesOpenAIAccountUserAgentPreservingReturnedCredentials(t *testing.T) {
	var sawBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			require.Equal(t, "/api/v1/admin/accounts/42", r.URL.Path)
			require.Equal(t, "admin-key", r.Header.Get("x-api-key"))
			writeEnvelope(w, Account{
				ID:       42,
				Name:     "openai-api-key",
				Platform: "openai",
				Type:     "api_key",
				Status:   "active",
				Credentials: map[string]any{
					"base_url":                  "https://openai.example.com/v1",
					"api_key":                   "redacted-api-key",
					"model_mapping":             map[string]any{"gpt-5": "gpt-5"},
					"upstream_supported_models": []any{"gpt-5", "o3"},
					"user_agent":                "old-ua",
				},
			})
		case http.MethodPut:
			require.Equal(t, "/api/v1/admin/accounts/42", r.URL.Path)
			require.Equal(t, "admin-key", r.Header.Get("x-api-key"))
			require.Equal(t, "application/json", r.Header.Get("Content-Type"))
			require.NoError(t, json.NewDecoder(r.Body).Decode(&sawBody))
			writeEnvelope(w, Account{
				ID:       42,
				Name:     "openai-api-key",
				Platform: "openai",
				Type:     "api_key",
				Status:   "active",
				Credentials: map[string]any{
					"base_url":   "https://openai.example.com/v1",
					"user_agent": "fixed-upstream-ua",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "admin-key")
	account, err := client.UpdateOpenAIAccountUserAgent(context.Background(), 42, " fixed-upstream-ua ")
	require.NoError(t, err)
	require.Equal(t, "fixed-upstream-ua", account.UserAgent())
	require.Equal(t, map[string]any{
		"credentials": map[string]any{
			"base_url":                  "https://openai.example.com/v1",
			"model_mapping":             map[string]any{"gpt-5": "gpt-5"},
			"upstream_supported_models": []any{"gpt-5", "o3"},
			"user_agent":                "fixed-upstream-ua",
		},
	}, sawBody)
	credentials, ok := sawBody["credentials"].(map[string]any)
	require.True(t, ok)
	require.NotContains(t, credentials, "api_key")
	require.NotContains(t, credentials, "access_token")
	require.NotContains(t, credentials, "refresh_token")
}

func TestClientListsUsersSubscriptionsAndCompensates(t *testing.T) {
	var (
		userBalanceBody        map[string]any
		subscriptionExtendBody map[string]any
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/users":
			require.Equal(t, "admin-key", r.Header.Get("x-api-key"))
			require.Equal(t, "false", r.URL.Query().Get("include_subscriptions"))
			require.Equal(t, "1", r.URL.Query().Get("page"))
			require.Equal(t, "100", r.URL.Query().Get("page_size"))
			writeEnvelope(w, paginatedEnvelope[User]{
				Items: []User{{
					ID:       10,
					Email:    "alice@example.com",
					Username: "alice",
					Status:   "active",
					Balance:  15,
				}},
				Total:    1,
				Page:     1,
				PageSize: 100,
				Pages:    1,
			})
		case "/api/v1/admin/subscriptions":
			require.Equal(t, "admin-key", r.Header.Get("x-api-key"))
			require.Equal(t, "10", r.URL.Query().Get("user_id"))
			require.Equal(t, "active", r.URL.Query().Get("status"))
			writeEnvelope(w, paginatedEnvelope[Subscription]{
				Items: []Subscription{{
					ID:        55,
					UserID:    10,
					GroupID:   8,
					Status:    "active",
					ExpiresAt: time.Now().UTC().Add(30 * 24 * time.Hour),
				}},
				Total:    1,
				Page:     1,
				PageSize: 100,
				Pages:    1,
			})
		case "/api/v1/admin/users/10/balance":
			require.Equal(t, "admin-key", r.Header.Get("x-api-key"))
			require.Equal(t, "balance-idempotency", r.Header.Get("Idempotency-Key"))
			require.NoError(t, json.NewDecoder(r.Body).Decode(&userBalanceBody))
			writeEnvelope(w, User{
				ID:       10,
				Email:    "alice@example.com",
				Username: "alice",
				Status:   "active",
				Balance:  35,
			})
		case "/api/v1/admin/subscriptions/55/extend":
			require.Equal(t, "admin-key", r.Header.Get("x-api-key"))
			require.Equal(t, "subscription-idempotency", r.Header.Get("Idempotency-Key"))
			require.NoError(t, json.NewDecoder(r.Body).Decode(&subscriptionExtendBody))
			writeEnvelope(w, Subscription{
				ID:        55,
				UserID:    10,
				GroupID:   8,
				Status:    "active",
				ExpiresAt: time.Now().UTC().Add(60 * 24 * time.Hour),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "admin-key")
	users, err := client.ListUsersAll(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.Equal(t, int64(10), users[0].ID)

	subs, err := client.ListActiveUserSubscriptions(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, subs, 1)
	require.Equal(t, int64(55), subs[0].ID)

	updatedUser, err := client.AddUserBalance(context.Background(), "balance-idempotency", 10, 20, "bulk compensation")
	require.NoError(t, err)
	require.Equal(t, 35.0, updatedUser.Balance)
	require.Equal(t, map[string]any{
		"balance":   20.0,
		"operation": "add",
		"notes":     "bulk compensation",
	}, userBalanceBody)

	updatedSub, err := client.ExtendSubscription(context.Background(), "subscription-idempotency", 55, 30)
	require.NoError(t, err)
	require.Equal(t, int64(55), updatedSub.ID)
	require.Equal(t, map[string]any{"days": float64(30)}, subscriptionExtendBody)
}

func TestClientGetsDefaultConcurrency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/api/v1/admin/settings", r.URL.Path)
		require.Equal(t, "admin-key", r.Header.Get("x-api-key"))
		writeEnvelope(w, map[string]any{"default_concurrency": 7})
	}))
	defer server.Close()

	client := NewClient(server.URL, "admin-key")
	concurrency, err := client.GetDefaultConcurrency(context.Background())
	require.NoError(t, err)
	require.Equal(t, 7, concurrency)
}

func TestClientRejectsInvalidDefaultConcurrency(t *testing.T) {
	for _, value := range []int{0, -1} {
		t.Run(fmt.Sprintf("value_%d", value), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeEnvelope(w, map[string]any{"default_concurrency": value})
			}))
			defer server.Close()

			client := NewClient(server.URL, "admin-key")
			_, err := client.GetDefaultConcurrency(context.Background())
			require.ErrorContains(t, err, "default concurrency")
		})
	}
}

func TestClientGetsAndUpdatesUserConcurrency(t *testing.T) {
	var updateBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/admin/users/10", r.URL.Path)
		require.Equal(t, "admin-key", r.Header.Get("x-api-key"))

		switch r.Method {
		case http.MethodGet:
			writeEnvelope(w, User{ID: 10, Email: "alice@example.com", Concurrency: 3})
		case http.MethodPut:
			require.Equal(t, "application/json", r.Header.Get("Content-Type"))
			require.NoError(t, json.NewDecoder(r.Body).Decode(&updateBody))
			writeEnvelope(w, User{ID: 10, Email: "alice@example.com", Concurrency: 8})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "admin-key")
	user, err := client.GetUser(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, 3, user.Concurrency)

	updated, err := client.UpdateUserConcurrency(context.Background(), 10, 8)
	require.NoError(t, err)
	require.Equal(t, 8, updated.Concurrency)
	require.Equal(t, map[string]any{"concurrency": float64(8)}, updateBody)
}

func TestClientRejectsInvalidUserConcurrencyArguments(t *testing.T) {
	client := NewClient("http://127.0.0.1:1", "admin-key")

	_, err := client.GetUser(context.Background(), 0)
	require.ErrorContains(t, err, "user ID")

	_, err = client.UpdateUserConcurrency(context.Background(), 0, 3)
	require.ErrorContains(t, err, "user ID")

	_, err = client.UpdateUserConcurrency(context.Background(), 10, 0)
	require.ErrorContains(t, err, "concurrency")
}

func TestClientListsSubscriptionPagesAndParsesUsage(t *testing.T) {
	windowStart := time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)
	expiresAt := windowStart.Add(30 * 24 * time.Hour)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/api/v1/admin/subscriptions", r.URL.Path)
		require.Equal(t, "admin-key", r.Header.Get("x-api-key"))
		require.Equal(t, "12", r.URL.Query().Get("user_id"))
		require.Equal(t, "active", r.URL.Query().Get("status"))
		page := r.URL.Query().Get("page")
		switch page {
		case "1":
			writeEnvelope(w, paginatedEnvelope[Subscription]{
				Items: []Subscription{{
					ID: 101, UserID: 12, GroupID: 21, Status: "active", ExpiresAt: expiresAt,
					DailyUsageUSD: 4.5, DailyWindowStart: &windowStart,
					Group: &Group{ID: 21, Name: "Daily", DailyLimitUSD: floatPtr(10)},
				}},
				Total: 101, Page: 1, PageSize: 100, Pages: 2,
			})
		case "2":
			writeEnvelope(w, paginatedEnvelope[Subscription]{
				Items: []Subscription{{ID: 102, UserID: 12, GroupID: 22, Status: "active", ExpiresAt: expiresAt}},
				Total: 101, Page: 2, PageSize: 100, Pages: 2,
			})
		default:
			t.Fatalf("unexpected page %q", page)
		}
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, "admin-key")
	subscriptions, err := client.ListActiveUserSubscriptions(context.Background(), 12)
	require.NoError(t, err)
	require.Len(t, subscriptions, 2)
	require.Equal(t, 4.5, subscriptions[0].DailyUsageUSD)
	require.Equal(t, windowStart, *subscriptions[0].DailyWindowStart)
	require.Equal(t, 10.0, *subscriptions[0].Group.DailyLimitUSD)
}

func TestClientGetsSubscriptionAndProgress(t *testing.T) {
	windowStart := time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)
	resetsAt := windowStart.Add(24 * time.Hour)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "admin-key", r.Header.Get("x-api-key"))
		switch r.URL.Path {
		case "/api/v1/admin/subscriptions/55":
			writeEnvelope(w, Subscription{ID: 55, UserID: 12, GroupID: 21, Status: "active", DailyUsageUSD: 3})
		case "/api/v1/admin/subscriptions/55/progress":
			writeEnvelope(w, SubscriptionProgress{
				ID: 55, GroupName: "Daily", ExpiresInDays: 29,
				Daily: &UsageWindowProgress{
					LimitUSD: 10, UsedUSD: 3, RemainingUSD: 7, Percentage: 30,
					WindowStart: &windowStart, ResetsAt: &resetsAt, ResetsInSeconds: 3600,
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, "admin-key")
	subscription, err := client.GetSubscription(context.Background(), 55)
	require.NoError(t, err)
	require.Equal(t, int64(12), subscription.UserID)

	progress, err := client.GetSubscriptionProgress(context.Background(), 55)
	require.NoError(t, err)
	require.Equal(t, 3.0, progress.Daily.UsedUSD)
	require.Equal(t, resetsAt, *progress.Daily.ResetsAt)
}

func TestClientResetQuotaSendsOnlyConfiguredWindowsWithAdminKey(t *testing.T) {
	var body map[string]bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/v1/admin/subscriptions/55/reset-quota", r.URL.Path)
		require.Equal(t, "admin-key", r.Header.Get("x-api-key"))
		require.Empty(t, r.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		writeEnvelope(w, Subscription{ID: 55, UserID: 12, GroupID: 21, Status: "active"})
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, "admin-key")
	updated, err := client.ResetSubscriptionQuota(context.Background(), 55, Group{
		DailyLimitUSD: floatPtr(10), MonthlyLimitUSD: floatPtr(100),
	})
	require.NoError(t, err)
	require.Equal(t, int64(55), updated.ID)
	require.Equal(t, map[string]bool{"daily": true, "weekly": false, "monthly": true}, body)
}

func TestClientExtendSubscriptionClassifiesRejectedAndUnknownResults(t *testing.T) {
	t.Run("explicit rejection", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 409, "message": "cannot extend", "reason": "locked"})
		}))
		defer server.Close()
		_, err := NewClient(server.URL, "admin-key").ExtendSubscription(context.Background(), "extend-key", 55, 3)
		require.ErrorIs(t, err, ErrUpstreamRejected)
	})

	t.Run("unverifiable success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeEnvelope(w, Subscription{ID: 56, ExpiresAt: time.Now().UTC().Add(24 * time.Hour)})
		}))
		defer server.Close()
		_, err := NewClient(server.URL, "admin-key").ExtendSubscription(context.Background(), "extend-key", 55, 3)
		require.ErrorIs(t, err, ErrResultUnknown)
	})
}

func TestClientResetQuotaClassifiesExplicitRejectionAndRedactsKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(envelope{Code: http.StatusConflict, Message: "rejected admin-key", Reason: "quota_locked"})
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, "admin-key")
	_, err := client.ResetSubscriptionQuota(context.Background(), 55, Group{DailyLimitUSD: floatPtr(10)})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrUpstreamRejected)
	require.NotContains(t, err.Error(), "admin-key")
	require.Contains(t, err.Error(), "[REDACTED]")
}

func TestClientResetQuotaTreatsNonSuccessHTTPWithInvalidBodyAsExplicitRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("not-json"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, "admin-key")
	_, err := client.ResetSubscriptionQuota(context.Background(), 55, Group{DailyLimitUSD: floatPtr(10)})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrUpstreamRejected)
}

func TestClientResetQuotaClassifiesUnknownTransportResult(t *testing.T) {
	client := NewClient("http://sub2api.invalid", "secret-admin-key")
	client.HTTPClient = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection reset after sending secret-admin-key")
	})}

	_, err := client.ResetSubscriptionQuota(context.Background(), 55, Group{WeeklyLimitUSD: floatPtr(50)})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrResultUnknown)
	require.NotContains(t, err.Error(), "secret-admin-key")
}

func TestClientResetQuotaRejectsUnlimitedGroupBeforeRequest(t *testing.T) {
	client := NewClient("http://127.0.0.1:1", "admin-key")

	_, err := client.ResetSubscriptionQuota(context.Background(), 55, Group{})
	require.ErrorContains(t, err, "configured quota window")
	require.NotErrorIs(t, err, ErrResultUnknown)
}

func TestClientResetQuotaTreatsZeroLimitsAsUnconfigured(t *testing.T) {
	zero := 0.0
	weekly := 50.0
	var body map[string]bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		writeEnvelope(w, Subscription{ID: 55})
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, "admin-key")
	_, err := client.ResetSubscriptionQuota(context.Background(), 55, Group{
		DailyLimitUSD: &zero, WeeklyLimitUSD: &weekly, MonthlyLimitUSD: &zero,
	})
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"daily": false, "weekly": true, "monthly": false}, body)

	_, err = client.ResetSubscriptionQuota(context.Background(), 55, Group{
		DailyLimitUSD: &zero, WeeklyLimitUSD: &zero, MonthlyLimitUSD: &zero,
	})
	require.ErrorContains(t, err, "configured quota window")
	require.NotErrorIs(t, err, ErrResultUnknown)
}

func TestClientResetQuotaRejectsUnverifiableSuccessData(t *testing.T) {
	for _, test := range []struct {
		name string
		data json.RawMessage
	}{
		{name: "missing", data: nil},
		{name: "null", data: json.RawMessage("null")},
		{name: "empty_object", data: json.RawMessage("{}")},
		{name: "wrong_subscription", data: mustJSON(Subscription{ID: 99})},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(envelope{Code: 0, Message: "success", Data: test.data})
			}))
			t.Cleanup(server.Close)

			client := NewClient(server.URL, "admin-key")
			_, err := client.ResetSubscriptionQuota(context.Background(), 55, Group{DailyLimitUSD: floatPtr(10)})
			require.ErrorIs(t, err, ErrResultUnknown)
		})
	}
}

func TestClientSubscriptionReadsRejectUnverifiableSuccessData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(envelope{Code: 0, Message: "success", Data: json.RawMessage("{}")})
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, "admin-key")
	_, err := client.GetSubscription(context.Background(), 55)
	require.ErrorIs(t, err, ErrAuthoritativeReadFailed)

	_, err = client.GetSubscriptionProgress(context.Background(), 55)
	require.ErrorIs(t, err, ErrAuthoritativeReadFailed)
}

func TestClientListActiveSubscriptionsRejectsUnverifiableSuccessData(t *testing.T) {
	for _, test := range []struct {
		name string
		data json.RawMessage
	}{
		{name: "missing", data: nil},
		{name: "null", data: json.RawMessage("null")},
		{name: "empty_object", data: json.RawMessage("{}")},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(envelope{Code: 0, Message: "success", Data: test.data})
			}))
			t.Cleanup(server.Close)

			client := NewClient(server.URL, "admin-key")
			_, err := client.ListActiveUserSubscriptions(context.Background(), 12)
			require.ErrorIs(t, err, ErrAuthoritativeReadFailed)
		})
	}
}

func TestClientListActiveSubscriptionsAcceptsVerifiedEmptyPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(w, paginatedEnvelope[Subscription]{
			Items: []Subscription{}, Total: 0, Page: 1, PageSize: 100, Pages: 0,
		})
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, "admin-key")
	items, err := client.ListActiveUserSubscriptions(context.Background(), 12)
	require.NoError(t, err)
	require.Empty(t, items)
}

func TestClientSubscriptionProgressClassifiesAuthoritativeReadFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(envelope{Code: http.StatusBadGateway, Message: "progress unavailable", Reason: "upstream_down"})
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, "admin-key")
	_, err := client.GetSubscriptionProgress(context.Background(), 55)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrAuthoritativeReadFailed)
	require.True(t, strings.Contains(err.Error(), "progress unavailable"))
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func floatPtr(v float64) *float64 {
	return &v
}

func int64Ptr(v int64) *int64 {
	return &v
}

func writeEnvelope(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(envelope{Code: 0, Message: "success", Data: mustJSON(data)})
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
