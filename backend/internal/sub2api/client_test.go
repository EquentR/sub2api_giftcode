package sub2api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
