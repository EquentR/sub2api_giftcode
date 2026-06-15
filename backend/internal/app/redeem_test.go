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
	"sub2api-giftcode/backend/internal/models"
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

func TestApproveAccessRequestIssuesSubscriptionRedeemCode(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	now := time.Now().UTC().Truncate(time.Second)
	var sawPayload map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/groups/all":
			writeRedeemTestEnvelope(w, []sub2api.Group{{
				ID:               2,
				Name:             "Claude monthly",
				Platform:         "anthropic",
				Status:           "active",
				SubscriptionType: "subscription",
				DailyLimitUSD:    floatPtr(10),
				WeeklyLimitUSD:   floatPtr(50),
				MonthlyLimitUSD:  floatPtr(120),
			}})
		case "/api/v1/admin/redeem-codes/generate":
			require.Equal(t, "admin-key", r.Header.Get("x-api-key"))
			require.NoError(t, json.NewDecoder(r.Body).Decode(&sawPayload))
			require.Equal(t, "subscription", sawPayload["type"])
			require.Equal(t, float64(0), sawPayload["value"])
			require.Equal(t, float64(2), sawPayload["group_id"])
			require.Equal(t, float64(30), sawPayload["validity_days"])
			writeRedeemTestEnvelope(w, []sub2api.RedeemCode{{
				ID:           200,
				Code:         "sub-code-200",
				Type:         "subscription",
				Value:        0,
				Status:       "unused",
				GroupID:      int64Ptr(2),
				ValidityDays: 30,
				CreatedAt:    now,
			}})
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
	tiers, err := svc.ReplaceRedeemTiers(context.Background(), []models.RedeemTier{{
		CodeType:       "subscription",
		PayAmountCny:   88,
		Label:          "Claude 30 days",
		Enabled:        true,
		SortOrder:      10,
		Sub2APIGroupID: int64Ptr(2),
		ValidityDays:   30,
	}})
	require.NoError(t, err)
	require.Len(t, tiers, 1)

	_, err = store.DB.ExecContext(context.Background(), `
INSERT INTO redeem_access_requests (
  requestor_upstream_user_id, requestor_email, requestor_username, tier_id, code_type,
  tier_label, amount, pay_amount_cny, sub2api_group_id, sub2api_group_name, sub2api_group_platform,
  sub2api_daily_limit_usd, sub2api_weekly_limit_usd, sub2api_monthly_limit_usd, validity_days,
  note, status, approval_token_hash, approval_token_expires_at, notification_status, notification_error,
  created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, 1, "alice@example.com", "alice", tiers[0].ID, "subscription", "Claude 30 days", 0.0, 88.0, 2, "Claude monthly", "anthropic", 10.0, 50.0, 120.0, 30, "please approve", "pending", "token-hash", now.Add(time.Hour).Format(time.RFC3339Nano), "sent", "", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	require.NoError(t, err)

	req, code, err := svc.ApproveAccessRequestByID(context.Background(), 1)
	require.NoError(t, err)
	require.NotNil(t, req)
	require.NotNil(t, code)
	require.Equal(t, "subscription", code.CodeType)
	require.Equal(t, 0.0, code.Value)
	require.Equal(t, int64(2), *code.Sub2APIGroupID)
	require.Equal(t, 30, code.ValidityDays)
	require.Equal(t, "subscription", req.CodeType)
	require.Equal(t, "Claude monthly", req.Sub2APIGroupName)
}

func floatPtr(v float64) *float64 {
	return &v
}

func writeRedeemTestEnvelope(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":    0,
		"message": "success",
		"data":    data,
	})
}
