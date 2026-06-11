package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/config"
	"sub2api-giftcode/backend/internal/db"
	"sub2api-giftcode/backend/internal/mail"
	"sub2api-giftcode/backend/internal/sub2api"
)

func TestApproveAccessRequestUsesStoredRedeemValueForRetry(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	now := time.Now().UTC().Truncate(time.Second)
	_, err = store.DB.ExecContext(context.Background(), `
UPDATE redeem_balance_tiers
SET amount = 130, pay_amount_cny = 130, updated_at = ?
WHERE id = 1
`, now.Format(time.RFC3339Nano))
	require.NoError(t, err)

	var sawValue float64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/admin/redeem-codes/generate", r.URL.Path)
		require.Equal(t, "admin-key", r.Header.Get("x-api-key"))
		var payload map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		sawValue = payload["value"].(float64)
		require.Equal(t, 120.0, sawValue)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "success",
			"data": []map[string]any{{
				"id":         99,
				"code":       "code-99",
				"type":       "balance",
				"value":      sawValue,
				"status":     "unused",
				"created_at": now.Format(time.RFC3339Nano),
			}},
		})
	}))
	t.Cleanup(upstream.Close)

	svc := New(
		&config.RuntimeConfig{},
		store,
		sub2api.NewClient(upstream.URL, "admin-key"),
		mail.New(mail.Config{}),
	)

	_, err = store.DB.ExecContext(context.Background(), `
INSERT INTO redeem_access_requests (
  requestor_upstream_user_id, requestor_email, requestor_username, tier_id, note, status,
  approval_token_hash, approval_token_expires_at, notification_status, notification_error,
  created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, 1, "alice@example.com", "alice", 1, "please approve", "pending", "token-hash", now.Add(time.Hour).Format(time.RFC3339Nano), "sent", "", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	require.NoError(t, err)

	_, err = store.DB.ExecContext(context.Background(), `
INSERT INTO redeem_requests (
  access_request_id, requestor_upstream_user_id, requestor_email, requestor_username,
  code_type, tier_id, value, status, note, upstream_code, upstream_code_id,
  error_message, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, 1, 1, "alice@example.com", "alice", "balance", 1, 120.0, "pending", "please approve", "", nil, "", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	require.NoError(t, err)

	req, code, err := svc.ApproveAccessRequestByID(context.Background(), 1)
	require.NoError(t, err)
	require.NotNil(t, req)
	require.NotNil(t, code)
	require.Equal(t, 120.0, code.Value)
	require.Equal(t, 120.0, sawValue)
}

func TestApproveAccessRequestAvoidsLocalOnlyIdempotencyKey(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	now := time.Now().UTC().Truncate(time.Second)
	const oldKey = "giftcode-redeem-request-1"
	const tokenHash = "approval-token-hash-1"
	var sawKey string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/admin/redeem-codes/generate", r.URL.Path)
		sawKey = r.Header.Get("Idempotency-Key")
		if sawKey == oldKey {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    http.StatusConflict,
				"message": "idempotency key reused with different payload",
			})
			return
		}
		var payload map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		require.Equal(t, 120.0, payload["value"].(float64))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "success",
			"data": []map[string]any{{
				"id":         99,
				"code":       "code-99",
				"type":       "balance",
				"value":      payload["value"].(float64),
				"status":     "unused",
				"created_at": now.Format(time.RFC3339Nano),
			}},
		})
	}))
	t.Cleanup(upstream.Close)

	svc := New(
		&config.RuntimeConfig{},
		store,
		sub2api.NewClient(upstream.URL, "admin-key"),
		mail.New(mail.Config{}),
	)

	_, err = store.DB.ExecContext(context.Background(), `
INSERT INTO redeem_access_requests (
  requestor_upstream_user_id, requestor_email, requestor_username, tier_id, amount, pay_amount_cny, note, status,
  approval_token_hash, approval_token_expires_at, notification_status, notification_error,
  created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, 1, "alice@example.com", "alice", 1, 120.0, 120.0, "please approve", "pending", tokenHash, now.Add(time.Hour).Format(time.RFC3339Nano), "sent", "", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	require.NoError(t, err)

	req, code, err := svc.ApproveAccessRequestByID(context.Background(), 1)
	require.NoError(t, err)
	require.NotNil(t, req)
	require.NotNil(t, code)
	require.NotEqual(t, oldKey, sawKey)
	require.Contains(t, sawKey, tokenHash)
}

func TestApproveAccessRequestUsesSnapshotAmount(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	now := time.Now().UTC().Truncate(time.Second)
	_, err = store.DB.ExecContext(context.Background(), `
UPDATE redeem_balance_tiers
SET amount = 120, pay_amount_cny = 120, updated_at = ?
WHERE id = 1
`, now.Format(time.RFC3339Nano))
	require.NoError(t, err)

	var sawValue float64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/me":
			require.Equal(t, "Bearer access-1", r.Header.Get("Authorization"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    0,
				"message": "success",
				"data": map[string]any{
					"id":         1,
					"email":      "alice@example.com",
					"username":   "alice",
					"role":       "user",
					"status":     "active",
					"created_at": now.Format(time.RFC3339Nano),
					"updated_at": now.Format(time.RFC3339Nano),
				},
			})
		case "/api/v1/admin/redeem-codes/generate":
			require.Equal(t, "admin-key", r.Header.Get("x-api-key"))
			var payload map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			sawValue = payload["value"].(float64)
			require.Equal(t, 120.0, sawValue)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    0,
				"message": "success",
				"data": []map[string]any{{
					"id":         100,
					"code":       "code-100",
					"type":       "balance",
					"value":      sawValue,
					"status":     "unused",
					"created_at": now.Format(time.RFC3339Nano),
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	svc := New(
		&config.RuntimeConfig{},
		store,
		sub2api.NewClient(upstream.URL, "admin-key"),
		mail.New(mail.Config{}),
	)

	_, err = store.DB.ExecContext(context.Background(), `
INSERT INTO upstream_users (
  upstream_user_id, email, username, role, status, profile_json, last_seen_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`, 1, "alice@example.com", "alice", "user", "active", "{}", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	require.NoError(t, err)

	sessionUser, err := svc.LoginWithAccessToken(context.Background(), "access-1", nil)
	require.NoError(t, err)

	req, err := svc.CreateAccessRequest(context.Background(), sessionUser.Session.ID, 1, "please approve")
	require.NoError(t, err)

	_, err = store.DB.ExecContext(context.Background(), `
UPDATE redeem_balance_tiers
SET amount = 130, pay_amount_cny = 130, updated_at = ?
WHERE id = 1
`, now.Add(time.Minute).Format(time.RFC3339Nano))
	require.NoError(t, err)

	approved, code, err := svc.ApproveAccessRequestByID(context.Background(), req.ID)
	require.NoError(t, err)
	require.NotNil(t, approved)
	require.NotNil(t, code)
	require.Equal(t, 120.0, code.Value)
	require.Equal(t, 120.0, sawValue)
	require.Equal(t, 120.0, approved.Amount)
	require.Equal(t, 120.0, approved.PayAmountCny)
}
