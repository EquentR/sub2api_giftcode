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

	codes, err := client.GenerateRedeemCodes(context.Background(), "idemp-1", "balance", 120)
	require.NoError(t, err)
	require.Len(t, codes, 1)
	require.Equal(t, "code-99", codes[0].Code)

	listed, err := client.ListRedeemCodes(context.Background(), "code-99", 10)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, "code-99", listed[0].Code)
}

func writeEnvelope(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(envelope{Code: 0, Message: "success", Data: mustJSON(data)})
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
