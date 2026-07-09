package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/config"
	"sub2api-giftcode/backend/internal/db"
	"sub2api-giftcode/backend/internal/sub2api"
)

func TestRunCompensationBatchRecordsSummaryAndDetails(t *testing.T) {
	var (
		balanceBodies []map[string]any
		balanceKeys   []string
		extendedIDs   []int64
		extendKeys    []string
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/users":
			writeEnvelope(w, map[string]any{
				"items": []sub2api.User{
					{ID: 1, Email: "skip@blocked.com", Username: "skip", Status: "active", Balance: 10},
					{ID: 2, Email: "sub@example.com", Username: "sub", Status: "active", Balance: 20},
					{ID: 3, Email: "bal@example.com", Username: "bal", Status: "active", Balance: 5},
					{ID: 4, Email: "zero@example.com", Username: "zero", Status: "active", Balance: 0},
				},
				"total":     4,
				"page":      1,
				"page_size": 100,
				"pages":     1,
			})
		case "/api/v1/admin/subscriptions":
			switch r.URL.Query().Get("user_id") {
			case "2":
				writeEnvelope(w, map[string]any{
					"items": []sub2api.Subscription{
						{ID: 101, UserID: 2, GroupID: 7, Status: "active", ExpiresAt: time.Now().UTC().Add(30 * 24 * time.Hour)},
						{ID: 102, UserID: 2, GroupID: 8, Status: "active", ExpiresAt: time.Now().UTC().Add(40 * 24 * time.Hour)},
					},
					"total":     2,
					"page":      1,
					"page_size": 100,
					"pages":     1,
				})
			default:
				writeEnvelope(w, map[string]any{
					"items":     []sub2api.Subscription{},
					"total":     0,
					"page":      1,
					"page_size": 100,
					"pages":     0,
				})
			}
		case "/api/v1/admin/users/3/balance":
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			balanceBodies = append(balanceBodies, body)
			balanceKeys = append(balanceKeys, r.Header.Get("Idempotency-Key"))
			writeEnvelope(w, sub2api.User{ID: 3, Email: "bal@example.com", Username: "bal", Status: "active", Balance: 15})
		case "/api/v1/admin/subscriptions/101/extend":
			extendedIDs = append(extendedIDs, 101)
			extendKeys = append(extendKeys, r.Header.Get("Idempotency-Key"))
			writeEnvelope(w, sub2api.Subscription{ID: 101, UserID: 2, GroupID: 7, Status: "active", ExpiresAt: time.Now().UTC().Add(60 * 24 * time.Hour)})
		case "/api/v1/admin/subscriptions/102/extend":
			extendedIDs = append(extendedIDs, 102)
			extendKeys = append(extendKeys, r.Header.Get("Idempotency-Key"))
			writeEnvelope(w, sub2api.Subscription{ID: 102, UserID: 2, GroupID: 8, Status: "active", ExpiresAt: time.Now().UTC().Add(70 * 24 * time.Hour)})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	operator := &SessionUser{
		User:    sub2api.User{ID: 900, Email: "admin@example.com", Username: "admin", Role: "admin", Status: "active"},
		IsAdmin: true,
	}

	batch, err := svc.RunCompensationBatch(context.Background(), operator, CompensationBatchInput{
		SubscriptionDays: 15,
		BalanceAmount:    10,
		ExcludedDomains:  []string{"blocked.com"},
		Note:             "bulk compensation",
	})
	require.NoError(t, err)
	require.Equal(t, compensationBatchStatusCompleted, batch.Status)
	require.Equal(t, 4, batch.TotalUsers)
	require.Equal(t, 1, batch.ExcludedUsers)
	require.Equal(t, 1, batch.SubscriptionCompensatedUsers)
	require.Equal(t, 1, batch.BalanceCompensatedUsers)
	require.Equal(t, 1, batch.SkippedZeroBalanceUsers)
	require.Equal(t, 0, batch.FailedUsers)
	require.Equal(t, 4, batch.DetailCount)
	require.NotNil(t, batch.CompletedAt)

	require.Equal(t, []int64{101, 102}, extendedIDs)
	require.Equal(t, []string{
		fmt.Sprintf("giftcode-compensation-batch-%s-user-2-subscription-101-extend", batch.BatchKey),
		fmt.Sprintf("giftcode-compensation-batch-%s-user-2-subscription-102-extend", batch.BatchKey),
	}, extendKeys)
	require.Len(t, balanceBodies, 1)
	require.Equal(t, map[string]any{
		"balance":   10.0,
		"operation": "add",
		"notes":     "bulk compensation",
	}, balanceBodies[0])
	require.Equal(t, []string{
		fmt.Sprintf("giftcode-compensation-batch-%s-user-3-balance-with-note", batch.BatchKey),
	}, balanceKeys)

	details, err := svc.ListCompensationBatchDetails(context.Background(), batch.ID)
	require.NoError(t, err)
	require.Len(t, details, 4)
	require.Equal(t, "excluded_domain", details[0].DecisionType)
	require.False(t, details[0].RemarkRequested)
	require.Equal(t, "active_subscription", details[1].DecisionType)
	require.Equal(t, 2, details[1].ActiveSubscriptionCount)
	require.False(t, details[1].RemarkRequested)
	require.Equal(t, "positive_balance", details[2].DecisionType)
	require.True(t, details[2].RemarkRequested)
	require.True(t, details[2].RemarkApplied)
	require.Equal(t, "non_positive_balance", details[3].DecisionType)
	require.False(t, details[3].RemarkRequested)
}

func TestRunCompensationBatchRetriesBalanceWithoutRemarkWhenUpstreamRejectsNotes(t *testing.T) {
	var (
		balanceBodies []map[string]any
		balanceKeys   []string
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/users":
			writeEnvelope(w, map[string]any{
				"items": []sub2api.User{
					{ID: 3, Email: "bal@example.com", Username: "bal", Status: "active", Balance: 5},
				},
				"total":     1,
				"page":      1,
				"page_size": 100,
				"pages":     1,
			})
		case "/api/v1/admin/subscriptions":
			writeEnvelope(w, map[string]any{
				"items":     []sub2api.Subscription{},
				"total":     0,
				"page":      1,
				"page_size": 100,
				"pages":     0,
			})
		case "/api/v1/admin/users/3/balance":
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			balanceBodies = append(balanceBodies, body)
			balanceKeys = append(balanceKeys, r.Header.Get("Idempotency-Key"))
			if len(balanceBodies) == 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code":    123,
					"message": "notes not supported",
				})
				return
			}
			writeEnvelope(w, sub2api.User{ID: 3, Email: "bal@example.com", Username: "bal", Status: "active", Balance: 15})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	operator := &SessionUser{
		User:    sub2api.User{ID: 900, Email: "admin@example.com", Username: "admin", Role: "admin", Status: "active"},
		IsAdmin: true,
	}

	batch, err := svc.RunCompensationBatch(context.Background(), operator, CompensationBatchInput{
		SubscriptionDays: 15,
		BalanceAmount:    10,
		Note:             "bulk compensation",
	})
	require.NoError(t, err)
	require.Equal(t, compensationBatchStatusCompleted, batch.Status)
	require.Equal(t, 1, batch.BalanceCompensatedUsers)
	require.Equal(t, 0, batch.FailedUsers)
	require.Len(t, balanceBodies, 2)
	require.Equal(t, map[string]any{
		"balance":   10.0,
		"operation": "add",
		"notes":     "bulk compensation",
	}, balanceBodies[0])
	require.Equal(t, map[string]any{
		"balance":   10.0,
		"operation": "add",
	}, balanceBodies[1])
	require.Equal(t, []string{
		fmt.Sprintf("giftcode-compensation-batch-%s-user-3-balance-with-note", batch.BatchKey),
		fmt.Sprintf("giftcode-compensation-batch-%s-user-3-balance-without-note", batch.BatchKey),
	}, balanceKeys)

	details, err := svc.ListCompensationBatchDetails(context.Background(), batch.ID)
	require.NoError(t, err)
	require.Len(t, details, 1)
	require.Equal(t, "positive_balance", details[0].DecisionType)
	require.Equal(t, "success", details[0].Status)
	require.True(t, details[0].RemarkRequested)
	require.False(t, details[0].RemarkApplied)
	require.Equal(t, "upstream balance endpoint rejected notes, retried without notes", details[0].RemarkError)
}

func TestRunCompensationBatchMarksPartialSubscriptionExtensionFailures(t *testing.T) {
	var extendKeys []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/users":
			writeEnvelope(w, map[string]any{
				"items": []sub2api.User{
					{ID: 2, Email: "sub@example.com", Username: "sub", Status: "active", Balance: 20},
				},
				"total":     1,
				"page":      1,
				"page_size": 100,
				"pages":     1,
			})
		case "/api/v1/admin/subscriptions":
			writeEnvelope(w, map[string]any{
				"items": []sub2api.Subscription{
					{ID: 101, UserID: 2, GroupID: 7, Status: "active", ExpiresAt: time.Now().UTC().Add(30 * 24 * time.Hour)},
					{ID: 102, UserID: 2, GroupID: 8, Status: "active", ExpiresAt: time.Now().UTC().Add(40 * 24 * time.Hour)},
				},
				"total":     2,
				"page":      1,
				"page_size": 100,
				"pages":     1,
			})
		case "/api/v1/admin/subscriptions/101/extend":
			extendKeys = append(extendKeys, r.Header.Get("Idempotency-Key"))
			writeEnvelope(w, sub2api.Subscription{ID: 101, UserID: 2, GroupID: 7, Status: "active", ExpiresAt: time.Now().UTC().Add(60 * 24 * time.Hour)})
		case "/api/v1/admin/subscriptions/102/extend":
			extendKeys = append(extendKeys, r.Header.Get("Idempotency-Key"))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    5001,
				"message": "extend failed",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(upstream.URL, "admin-key"), nil)
	operator := &SessionUser{
		User:    sub2api.User{ID: 900, Email: "admin@example.com", Username: "admin", Role: "admin", Status: "active"},
		IsAdmin: true,
	}

	batch, err := svc.RunCompensationBatch(context.Background(), operator, CompensationBatchInput{
		SubscriptionDays: 15,
		BalanceAmount:    10,
		Note:             "bulk compensation",
	})
	require.NoError(t, err)
	require.Equal(t, compensationBatchStatusCompletedWithFailures, batch.Status)
	require.Equal(t, 0, batch.SubscriptionCompensatedUsers)
	require.Equal(t, 1, batch.FailedUsers)
	require.Equal(t, []string{
		fmt.Sprintf("giftcode-compensation-batch-%s-user-2-subscription-101-extend", batch.BatchKey),
		fmt.Sprintf("giftcode-compensation-batch-%s-user-2-subscription-102-extend", batch.BatchKey),
	}, extendKeys)

	details, err := svc.ListCompensationBatchDetails(context.Background(), batch.ID)
	require.NoError(t, err)
	require.Len(t, details, 1)
	require.Equal(t, "active_subscription", details[0].DecisionType)
	require.Equal(t, "failed", details[0].Status)
	require.Equal(t, 2, details[0].ActiveSubscriptionCount)
	require.False(t, details[0].RemarkRequested)

	var upstreamRef map[string]any
	require.NoError(t, json.Unmarshal([]byte(details[0].UpstreamReferenceJSON), &upstreamRef))
	require.Equal(t, []any{float64(101), float64(102)}, upstreamRef["subscription_ids"])
	require.Equal(t, []any{float64(101)}, upstreamRef["extended_ids"])
	failedExtends, ok := upstreamRef["failed_extensions"].([]any)
	require.True(t, ok)
	require.Len(t, failedExtends, 1)
	failedEntry, ok := failedExtends[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(102), failedEntry["subscription_id"])
	require.Equal(t, "extend failed", failedEntry["error"])
}

func TestListCompensationBatchesReturnsMalformedExcludedDomainsJSON(t *testing.T) {
	ctx := context.Background()
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(ctx))

	now := formatTime(time.Now().UTC())
	_, err = store.DB.ExecContext(ctx, `
INSERT INTO compensation_batches (
  batch_key, subscription_days, balance_amount, excluded_domains_json,
  operator_upstream_user_id, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`, "bad-excluded-domains", 15, 10.0, "{not-json", 900, compensationBatchStatusCompleted, now, now)
	require.NoError(t, err)

	svc := New(&config.RuntimeConfig{}, store, nil, nil)
	_, err = svc.ListCompensationBatches(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "excluded_domains_json")
}

func TestListCompensationBatchDetailsReturnsMalformedActiveSubscriptionIDsJSON(t *testing.T) {
	ctx := context.Background()
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(ctx))

	now := formatTime(time.Now().UTC())
	result, err := store.DB.ExecContext(ctx, `
INSERT INTO compensation_batches (
  batch_key, subscription_days, balance_amount, excluded_domains_json,
  operator_upstream_user_id, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`, "bad-active-subscription-ids", 15, 10.0, "[]", 900, compensationBatchStatusCompleted, now, now)
	require.NoError(t, err)
	batchID, err := result.LastInsertId()
	require.NoError(t, err)

	_, err = store.DB.ExecContext(ctx, `
INSERT INTO compensation_batch_details (
  batch_id, detail_key, upstream_user_id, active_subscription_ids_json,
  decision_type, action_type, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`, batchID, "bad-detail", 2, "[not-json", "active_subscription", "subscription", "success", now, now)
	require.NoError(t, err)

	svc := New(&config.RuntimeConfig{}, store, nil, nil)
	_, err = svc.ListCompensationBatchDetails(ctx, batchID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "active_subscription_ids_json")
}

func writeEnvelope(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":    0,
		"message": "success",
		"data":    data,
	})
}
