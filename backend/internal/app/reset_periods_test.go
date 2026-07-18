package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/config"
	"sub2api-giftcode/backend/internal/db"
	"sub2api-giftcode/backend/internal/models"
	"sub2api-giftcode/backend/internal/sub2api"
)

func TestReconcileSubscriptionResetPeriodsSchedulesRedeemCodesInUsageOrderAndKeepsZeroLimitSlot(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	usedFirst := now.Add(-2 * time.Hour)
	usedSecond := now.Add(-time.Hour)
	insertResetAccessFixture(t, store, 102, 1, 7, 10, 2, fulfilledViaRedeemCode, fulfillmentResultCodeIssued, usedSecond)
	insertUsedSubscriptionCodeFixture(t, store, 102, 1, 7, 10, usedSecond)
	insertResetAccessFixture(t, store, 101, 1, 7, 10, 0, fulfilledViaRedeemCode, fulfillmentResultCodeIssued, usedFirst)
	insertUsedSubscriptionCodeFixture(t, store, 101, 1, 7, 10, usedFirst)

	expiresAt := now.Add(20 * 24 * time.Hour)
	startsAt := now.Add(-5 * 24 * time.Hour)
	svc := newResetPeriodTestService(t, store, []sub2api.Subscription{{
		ID: 77, UserID: 1, GroupID: 7, StartsAt: startsAt, ExpiresAt: expiresAt, Status: "active",
	}})

	require.NoError(t, svc.ReconcileSubscriptionResetPeriods(context.Background()))

	periods := loadResetPeriodsForTest(t, store)
	require.Len(t, periods, 2)
	require.Equal(t, int64(101), periods[0].AccessRequestID)
	require.Zero(t, periods[0].ResetLimit)
	require.Equal(t, now, *periods[0].PeriodStart)
	require.Equal(t, now.Add(10*24*time.Hour), *periods[0].PeriodEnd)
	require.Equal(t, "active", periods[0].Status)
	require.Equal(t, int64(102), periods[1].AccessRequestID)
	require.Equal(t, 2, periods[1].ResetLimit)
	require.Equal(t, *periods[0].PeriodEnd, *periods[1].PeriodStart)
	require.Equal(t, expiresAt, *periods[1].PeriodEnd)
	require.Equal(t, "scheduled", periods[1].Status)
}

func TestReconcileSubscriptionResetPeriodsBreaksRedeemCodeTimestampTiesByAccessRequestID(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	usedAt := now.Add(-time.Hour)
	for _, id := range []int64{202, 201} {
		insertResetAccessFixture(t, store, id, 1, 7, 5, int(id-200), fulfilledViaRedeemCode, fulfillmentResultCodeIssued, usedAt)
		insertUsedSubscriptionCodeFixture(t, store, id, 1, 7, 5, usedAt)
	}
	svc := newResetPeriodTestService(t, store, []sub2api.Subscription{{
		ID: 77, UserID: 1, GroupID: 7, StartsAt: now, ExpiresAt: now.Add(10 * 24 * time.Hour), Status: "active",
	}})

	require.NoError(t, svc.ReconcileSubscriptionResetPeriods(context.Background()))
	periods := loadResetPeriodsForTest(t, store)
	require.Len(t, periods, 2)
	require.Equal(t, int64(201), periods[0].AccessRequestID)
	require.Equal(t, int64(202), periods[1].AccessRequestID)
}

func TestReconcileSubscriptionResetPeriodsDoesNotMoveEstablishedBoundaryAfterExternalExtension(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(-24 * time.Hour)
	end := now.Add(9 * 24 * time.Hour)
	insertResetPeriodFixture(t, store, 1, 101, 1, 7, 77, 10, 1, start, end, "active")
	svc := newResetPeriodTestService(t, store, []sub2api.Subscription{{
		ID: 77, UserID: 1, GroupID: 7, StartsAt: start.Add(-30 * 24 * time.Hour), ExpiresAt: end.Add(30 * 24 * time.Hour), Status: "active",
	}})

	require.NoError(t, svc.ReconcileSubscriptionResetPeriods(context.Background()))
	periods := loadResetPeriodsForTest(t, store)
	require.Len(t, periods, 1)
	require.Equal(t, start, *periods[0].PeriodStart)
	require.Equal(t, end, *periods[0].PeriodEnd)
}

func TestAssignSubscriptionResetPeriodBoundaryRejectsOverlap(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertResetPeriodFixture(t, store, 1, 101, 1, 7, 77, 10, 1, now, now.Add(10*24*time.Hour), "active")
	insertPendingResetPeriodFixture(t, store, 2, 102, 1, 7, 10, 1, now.Add(-time.Hour))
	svc := New(&config.RuntimeConfig{}, store, nil, nil)

	err := svc.assignSubscriptionResetPeriodBoundary(context.Background(), 2, 77, now.Add(5*24*time.Hour), now.Add(15*24*time.Hour))
	require.ErrorContains(t, err, "overlap")

	var start sql.NullString
	var lastError string
	require.NoError(t, store.DB.QueryRow(`SELECT period_start, last_error FROM subscription_reset_periods WHERE id = 2`).Scan(&start, &lastError))
	require.False(t, start.Valid)
	require.Contains(t, lastError, "overlap")
}

func TestReconcileSubscriptionResetPeriodsRepairsMissingDirectChargePeriodAfterCrash(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertResetAccessFixture(t, store, 101, 1, 7, 30, 3, fulfilledViaDirectCharge, fulfillmentResultDirectSucceeded, now.Add(-time.Hour))
	svc := newResetPeriodTestService(t, store, []sub2api.Subscription{{
		ID: 77, UserID: 1, GroupID: 7, StartsAt: now.Add(-5 * 24 * time.Hour), ExpiresAt: now.Add(25 * 24 * time.Hour), Status: "active",
	}})

	require.NoError(t, svc.ReconcileSubscriptionResetPeriods(context.Background()))
	require.NoError(t, svc.ReconcileSubscriptionResetPeriods(context.Background()))
	periods := loadResetPeriodsForTest(t, store)
	require.Len(t, periods, 1)
	require.Equal(t, int64(101), periods[0].AccessRequestID)
	require.Equal(t, 3, periods[0].ResetLimit)
	require.Equal(t, int64(77), *periods[0].UpstreamSubscriptionID)
}

func TestReconcileSubscriptionResetPeriodsRepairsMissingPeriodBeforeEstablishedLaterPeriod(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertResetAccessFixture(t, store, 101, 1, 7, 10, 1, fulfilledViaDirectCharge, fulfillmentResultDirectSucceeded, now.Add(-2*time.Hour))
	insertResetPeriodFixture(t, store, 2, 102, 1, 7, 77, 10, 2, now.Add(10*24*time.Hour), now.Add(20*24*time.Hour), "scheduled")
	svc := newResetPeriodTestService(t, store, []sub2api.Subscription{{
		ID: 77, UserID: 1, GroupID: 7, StartsAt: now, ExpiresAt: now.Add(20 * 24 * time.Hour), Status: "active",
	}})

	require.NoError(t, svc.ReconcileSubscriptionResetPeriods(context.Background()))
	periods := loadResetPeriodsForTest(t, store)
	require.Len(t, periods, 2)
	require.Equal(t, int64(101), periods[0].AccessRequestID)
	require.Equal(t, now, *periods[0].PeriodStart)
	require.Equal(t, *periods[1].PeriodStart, *periods[0].PeriodEnd)
	require.Equal(t, now.Add(20*24*time.Hour), *periods[1].PeriodEnd)
}

func TestReconcileSubscriptionResetPeriodsMarksStaleReservedAttemptUncertainWithoutRetry(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertResetPeriodFixture(t, store, 1, 101, 1, 7, 77, 10, 1, now.Add(-time.Hour), now.Add(10*24*time.Hour), "active")
	_, err := store.DB.Exec(`
INSERT INTO subscription_reset_attempts (
  request_id, period_id, upstream_user_id, upstream_subscription_id, status, reserved_at, created_at, updated_at
) VALUES ('11111111-1111-4111-8111-111111111111', 1, 1, 77, 'reserved', ?, ?, ?)
`, formatTime(now.Add(-3*time.Minute)), formatTime(now.Add(-3*time.Minute)), formatTime(now.Add(-3*time.Minute)))
	require.NoError(t, err)
	resetCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/admin/subscriptions/77/reset-quota" {
			resetCalls++
			t.Fatalf("stale reservation must never be resent")
		}
		writeRedeemTestEnvelope(w, subscriptionPageForTest([]sub2api.Subscription{{
			ID: 77, UserID: 1, GroupID: 7, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(10 * 24 * time.Hour), Status: "active",
		}}))
	}))
	t.Cleanup(server.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(server.URL, "admin-key"), nil)

	require.NoError(t, svc.ReconcileSubscriptionResetPeriods(context.Background()))
	var status, reason string
	require.NoError(t, store.DB.QueryRow(`SELECT status, response_reason FROM subscription_reset_attempts WHERE period_id = 1`).Scan(&status, &reason))
	require.Equal(t, "uncertain", status)
	require.Equal(t, "reservation_timeout", reason)
	require.Zero(t, resetCalls)
}

func TestDirectChargeCreatesResetPeriodFromPrechargeExpiry(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	beforeEnd := now.Add(5 * 24 * time.Hour)
	afterEnd := beforeEnd.Add(30 * 24 * time.Hour)
	groupID := int64(7)
	_, err := store.DB.Exec(`
INSERT INTO redeem_tiers (
  id, code_type, pay_amount_cny, label, enabled, sub2api_group_id, validity_days, concurrency, reset_count, created_at, updated_at
) VALUES (50, 'subscription', 88, 'Monthly', 1, 7, 30, 10, 3, ?, ?)
`, formatTime(now), formatTime(now))
	require.NoError(t, err)
	insertPendingDirectAccessFixture(t, store, 101, 1, 7, 30, 3, now)

	listCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/subscriptions":
			listCalls++
			expiresAt := beforeEnd
			if listCalls > 1 {
				expiresAt = afterEnd
			}
			writeRedeemTestEnvelope(w, subscriptionPageForTest([]sub2api.Subscription{{
				ID: 77, UserID: 1, GroupID: 7, StartsAt: now.Add(-25 * 24 * time.Hour), ExpiresAt: expiresAt, Status: "active",
			}}))
		case "/api/v1/admin/redeem-codes/create-and-redeem":
			usedBy := int64(1)
			writeRedeemTestEnvelope(w, map[string]any{"redeem_code": sub2api.RedeemCode{
				ID: 900, Code: "direct-900", Type: "subscription", Status: "used", UsedBy: &usedBy,
				UsedAt: &now, GroupID: &groupID, ValidityDays: 30,
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(server.URL, "admin-key"), nil)
	req, err := svc.getAccessRequestByID(context.Background(), 101)
	require.NoError(t, err)
	tier, err := svc.getRedeemTierByID(context.Background(), 50)
	require.NoError(t, err)

	_, err = svc.issueDirectCharge(context.Background(), req, tier)
	require.NoError(t, err)
	require.Equal(t, 2, listCalls)
	periods := loadResetPeriodsForTest(t, store)
	require.Len(t, periods, 1)
	require.Equal(t, beforeEnd, *periods[0].PeriodStart)
	require.Equal(t, afterEnd, *periods[0].PeriodEnd)
	require.Equal(t, 3, periods[0].ResetLimit)
	select {
	case <-svc.resetWake:
	default:
		t.Fatal("direct charge should wake subscription reset reconciliation")
	}
}

func TestDirectChargeFulfilledAtUsesSuccessTime(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	successAt := now.Add(-5 * time.Minute)
	groupID := int64(7)
	insertResetTierFixture(t, store, 50, 7, 30, 3, now)
	insertPendingDirectAccessFixture(t, store, 501, 1, 7, 30, 3, now)
	listCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/subscriptions":
			listCalls++
			expiresAt := now.Add(5 * 24 * time.Hour)
			if listCalls > 1 {
				expiresAt = expiresAt.Add(30 * 24 * time.Hour)
			}
			writeRedeemTestEnvelope(w, subscriptionPageForTest([]sub2api.Subscription{{
				ID: 77, UserID: 1, GroupID: 7, StartsAt: now.Add(-25 * 24 * time.Hour), ExpiresAt: expiresAt, Status: "active",
			}}))
		case "/api/v1/admin/redeem-codes/create-and-redeem":
			usedBy := int64(1)
			writeRedeemTestEnvelope(w, map[string]any{"redeem_code": sub2api.RedeemCode{
				ID: 950, Code: "direct-950", Type: "subscription", Status: "used", UsedBy: &usedBy,
				UsedAt: &successAt, GroupID: &groupID, ValidityDays: 30,
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(server.URL, "admin-key"), nil)
	req, err := svc.getAccessRequestByID(context.Background(), 501)
	require.NoError(t, err)
	tier, err := svc.getRedeemTierByID(context.Background(), 50)
	require.NoError(t, err)

	_, err = svc.issueDirectCharge(context.Background(), req, tier)
	require.NoError(t, err)
	periods := loadResetPeriodsForTest(t, store)
	require.Len(t, periods, 1)
	require.Equal(t, successAt, periods[0].FulfilledAt)
}

func TestDirectChargeConcurrentResetPeriodsKeepChargeOrder(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	baseExpiry := now.Add(5 * 24 * time.Hour)
	insertResetTierFixture(t, store, 50, 7, 30, 3, now)
	for _, id := range []int64{501, 502} {
		insertPendingDirectAccessFixture(t, store, id, 1, 7, 30, 3, now)
	}

	var stateMu sync.Mutex
	currentExpiry := baseExpiry
	getCalls := 0
	postCalls := 0
	firstBeforeSeen := make(chan struct{})
	secondReadSeen := make(chan struct{})
	var firstOnce, secondOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/subscriptions":
			stateMu.Lock()
			getCalls++
			call := getCalls
			expiresAt := currentExpiry
			stateMu.Unlock()
			if call == 1 {
				firstOnce.Do(func() { close(firstBeforeSeen) })
			}
			if call == 2 {
				secondOnce.Do(func() { close(secondReadSeen) })
			}
			writeRedeemTestEnvelope(w, subscriptionPageForTest([]sub2api.Subscription{{
				ID: 77, UserID: 1, GroupID: 7, StartsAt: now.Add(-25 * 24 * time.Hour), ExpiresAt: expiresAt, Status: "active",
			}}))
		case "/api/v1/admin/redeem-codes/create-and-redeem":
			var input map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			code := input["code"].(string)
			if code == "giftcode-access-501" {
				select {
				case <-secondReadSeen:
				case <-time.After(150 * time.Millisecond):
				}
			}
			stateMu.Lock()
			postCalls++
			postNumber := postCalls
			currentExpiry = currentExpiry.Add(30 * 24 * time.Hour)
			usedAt := now.Add(time.Duration(postNumber) * time.Second)
			stateMu.Unlock()
			usedBy := int64(1)
			groupID := int64(7)
			writeRedeemTestEnvelope(w, map[string]any{"redeem_code": sub2api.RedeemCode{
				ID: int64(950 + postNumber), Code: code, Type: "subscription", Status: "used", UsedBy: &usedBy,
				UsedAt: &usedAt, GroupID: &groupID, ValidityDays: 30,
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(server.URL, "admin-key"), nil)
	tier, err := svc.getRedeemTierByID(context.Background(), 50)
	require.NoError(t, err)
	req501, err := svc.getAccessRequestByID(context.Background(), 501)
	require.NoError(t, err)
	req502, err := svc.getAccessRequestByID(context.Background(), 502)
	require.NoError(t, err)

	errCh := make(chan error, 2)
	go func() {
		_, chargeErr := svc.issueDirectCharge(context.Background(), req501, tier)
		errCh <- chargeErr
	}()
	<-firstBeforeSeen
	go func() {
		_, chargeErr := svc.issueDirectCharge(context.Background(), req502, tier)
		errCh <- chargeErr
	}()
	require.NoError(t, <-errCh)
	require.NoError(t, <-errCh)

	periods := loadResetPeriodsForTest(t, store)
	require.Len(t, periods, 2)
	byAccess := map[int64]models.SubscriptionResetPeriod{}
	for _, period := range periods {
		byAccess[period.AccessRequestID] = period
	}
	require.NotNil(t, byAccess[501].PeriodStart)
	require.NotNil(t, byAccess[501].PeriodEnd)
	require.NotNil(t, byAccess[502].PeriodStart)
	require.NotNil(t, byAccess[502].PeriodEnd)
	require.Equal(t, baseExpiry, *byAccess[501].PeriodStart)
	require.Equal(t, baseExpiry.Add(30*24*time.Hour), *byAccess[501].PeriodEnd)
	require.Equal(t, *byAccess[501].PeriodEnd, *byAccess[502].PeriodStart)
	require.Equal(t, baseExpiry.Add(60*24*time.Hour), *byAccess[502].PeriodEnd)
}

func TestDirectChargeAmbiguousExtensionRemainsPendingAfterReconcile(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	baseExpiry := now.Add(5 * 24 * time.Hour)
	groupID := int64(7)
	insertResetTierFixture(t, store, 50, 7, 30, 3, now)
	insertPendingDirectAccessFixture(t, store, 601, 1, 7, 30, 3, now)
	listCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/subscriptions":
			listCalls++
			expiresAt := baseExpiry
			if listCalls > 1 {
				expiresAt = baseExpiry.Add(60 * 24 * time.Hour)
			}
			writeRedeemTestEnvelope(w, subscriptionPageForTest([]sub2api.Subscription{{
				ID: 77, UserID: 1, GroupID: 7, StartsAt: now.Add(-25 * 24 * time.Hour), ExpiresAt: expiresAt, Status: "active",
			}}))
		case "/api/v1/admin/redeem-codes/create-and-redeem":
			usedBy := int64(1)
			writeRedeemTestEnvelope(w, map[string]any{"redeem_code": sub2api.RedeemCode{
				ID: 960, Code: "direct-960", Type: "subscription", Status: "used", UsedBy: &usedBy,
				UsedAt: &now, GroupID: &groupID, ValidityDays: 30,
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(server.URL, "admin-key"), nil)
	req, err := svc.getAccessRequestByID(context.Background(), 601)
	require.NoError(t, err)
	tier, err := svc.getRedeemTierByID(context.Background(), 50)
	require.NoError(t, err)

	_, err = svc.issueDirectCharge(context.Background(), req, tier)
	require.NoError(t, err)
	require.NoError(t, svc.ReconcileSubscriptionResetPeriods(context.Background()))
	periods := loadResetPeriodsForTest(t, store)
	require.Len(t, periods, 1)
	require.Nil(t, periods[0].PeriodStart)
	require.Nil(t, periods[0].PeriodEnd)
	require.Equal(t, "pending_binding", periods[0].Status)
	require.Contains(t, periods[0].LastError, ambiguousDirectResetBoundaryPrefix)
}

func TestDirectChargeResetOrderingDoesNotSerializeIndependentSubscriptions(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertResetTierFixture(t, store, 50, 7, 30, 1, now)
	insertResetTierFixture(t, store, 51, 8, 30, 1, now)
	insertPendingDirectAccessFixture(t, store, 701, 1, 7, 30, 1, now)
	insertPendingDirectAccessFixture(t, store, 702, 2, 8, 30, 1, now)
	_, err := store.DB.Exec(`UPDATE redeem_access_requests SET tier_id = 51 WHERE id = 702`)
	require.NoError(t, err)

	var stateMu sync.Mutex
	expiresByUser := map[int64]time.Time{1: now.Add(5 * 24 * time.Hour), 2: now.Add(7 * 24 * time.Hour)}
	postAStarted := make(chan struct{})
	postBStarted := make(chan struct{})
	releaseA := make(chan struct{})
	var postAOnce, postBOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/subscriptions":
			userID, parseErr := strconv.ParseInt(r.URL.Query().Get("user_id"), 10, 64)
			require.NoError(t, parseErr)
			groupID := int64(7)
			if userID == 2 {
				groupID = 8
			}
			stateMu.Lock()
			expiresAt := expiresByUser[userID]
			stateMu.Unlock()
			writeRedeemTestEnvelope(w, subscriptionPageForTest([]sub2api.Subscription{{
				ID: 70 + userID, UserID: userID, GroupID: groupID, StartsAt: now.Add(-25 * 24 * time.Hour), ExpiresAt: expiresAt, Status: "active",
			}}))
		case "/api/v1/admin/redeem-codes/create-and-redeem":
			var input map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			code := input["code"].(string)
			userID := int64(input["user_id"].(float64))
			groupID := int64(input["group_id"].(float64))
			if userID == 1 {
				postAOnce.Do(func() { close(postAStarted) })
				<-releaseA
			} else {
				postBOnce.Do(func() { close(postBStarted) })
			}
			stateMu.Lock()
			expiresByUser[userID] = expiresByUser[userID].Add(30 * 24 * time.Hour)
			stateMu.Unlock()
			usedBy := userID
			writeRedeemTestEnvelope(w, map[string]any{"redeem_code": sub2api.RedeemCode{
				ID: 970 + userID, Code: code, Type: "subscription", Status: "used", UsedBy: &usedBy,
				UsedAt: &now, GroupID: &groupID, ValidityDays: 30,
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(server.URL, "admin-key"), nil)
	tierA, err := svc.getRedeemTierByID(context.Background(), 50)
	require.NoError(t, err)
	tierB, err := svc.getRedeemTierByID(context.Background(), 51)
	require.NoError(t, err)
	reqA, err := svc.getAccessRequestByID(context.Background(), 701)
	require.NoError(t, err)
	reqB, err := svc.getAccessRequestByID(context.Background(), 702)
	require.NoError(t, err)

	errCh := make(chan error, 2)
	go func() {
		_, chargeErr := svc.issueDirectCharge(context.Background(), reqA, tierA)
		errCh <- chargeErr
	}()
	<-postAStarted
	go func() {
		_, chargeErr := svc.issueDirectCharge(context.Background(), reqB, tierB)
		errCh <- chargeErr
	}()
	independentStarted := false
	select {
	case <-postBStarted:
		independentStarted = true
	case <-time.After(500 * time.Millisecond):
	}
	close(releaseA)
	require.NoError(t, <-errCh)
	require.NoError(t, <-errCh)
	require.True(t, independentStarted, "independent user/group charge should not wait for another charge")
}

func TestDirectChargeNewSubscriptionRejectsAmbiguousDuration(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	groupID := int64(7)
	insertResetTierFixture(t, store, 50, 7, 30, 2, now)
	insertPendingDirectAccessFixture(t, store, 801, 1, 7, 30, 2, now)
	listCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/subscriptions":
			listCalls++
			if listCalls == 1 {
				writeRedeemTestEnvelope(w, subscriptionPageForTest(nil))
				return
			}
			writeRedeemTestEnvelope(w, subscriptionPageForTest([]sub2api.Subscription{{
				ID: 77, UserID: 1, GroupID: 7, StartsAt: now, ExpiresAt: now.Add(60 * 24 * time.Hour), Status: "active",
			}}))
		case "/api/v1/admin/redeem-codes/create-and-redeem":
			usedBy := int64(1)
			writeRedeemTestEnvelope(w, map[string]any{"redeem_code": sub2api.RedeemCode{
				ID: 980, Code: "direct-980", Type: "subscription", Status: "used", UsedBy: &usedBy,
				UsedAt: &now, GroupID: &groupID, ValidityDays: 30,
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(server.URL, "admin-key"), nil)
	req, err := svc.getAccessRequestByID(context.Background(), 801)
	require.NoError(t, err)
	tier, err := svc.getRedeemTierByID(context.Background(), 50)
	require.NoError(t, err)

	_, err = svc.issueDirectCharge(context.Background(), req, tier)
	require.NoError(t, err)
	require.NoError(t, svc.ReconcileSubscriptionResetPeriods(context.Background()))
	periods := loadResetPeriodsForTest(t, store)
	require.Len(t, periods, 1)
	require.Nil(t, periods[0].PeriodStart)
	require.Nil(t, periods[0].PeriodEnd)
	require.Contains(t, periods[0].LastError, ambiguousDirectResetBoundaryPrefix)
}

func TestSubscriptionResetPeriodLoopRunsImmediatelyAndOnWake(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	svc := newResetPeriodTestService(t, store, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.RunSubscriptionResetLoop(ctx, time.Hour)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	require.Eventually(t, func() bool {
		var value string
		return store.DB.QueryRow(`SELECT value FROM sync_state WHERE key = ?`, subscriptionResetLastReconciliationAtKey).Scan(&value) == nil
	}, time.Second, 10*time.Millisecond)

	insertResetAccessFixture(t, store, 301, 1, 7, 30, 1, fulfilledViaDirectCharge, fulfillmentResultDirectSucceeded, now)
	svc.WakeSubscriptionResetReconcile()
	require.Eventually(t, func() bool {
		var count int
		return store.DB.QueryRow(`SELECT COUNT(1) FROM subscription_reset_periods WHERE access_request_id = 301`).Scan(&count) == nil && count == 1
	}, time.Second, 10*time.Millisecond)
}

func TestSubscriptionResetPeriodTierSaveUsesNonBlockingWake(t *testing.T) {
	store := newResetPeriodTestStore(t)
	svc := New(&config.RuntimeConfig{}, store, nil, nil)

	_, err := svc.ReplaceRedeemTiers(context.Background(), []models.RedeemTier{{
		CodeType: "balance", Amount: 120, PayAmountCny: 120, Enabled: true,
	}})
	require.NoError(t, err)
	select {
	case <-svc.resetWake:
	default:
		t.Fatal("tier save should wake subscription reset reconciliation")
	}

	svc.WakeSubscriptionResetReconcile()
	svc.WakeSubscriptionResetReconcile()
	require.Len(t, svc.resetWake, 1)
}

func TestSubscriptionResetPeriodRedeemCodeSyncUsesNonBlockingWake(t *testing.T) {
	store := newResetPeriodTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertResetAccessFixture(t, store, 401, 1, 7, 30, 1, fulfilledViaRedeemCode, fulfillmentResultCodeIssued, now)
	insertUsedSubscriptionCodeFixture(t, store, 401, 1, 7, 30, now)
	_, err := store.DB.Exec(`UPDATE redeem_codes SET status = 'unused', used_by_upstream_user_id = NULL, used_at = NULL`)
	require.NoError(t, err)
	groupID := int64(7)
	usedBy := int64(1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/admin/redeem-codes", r.URL.Path)
		writeRedeemTestEnvelope(w, map[string]any{
			"items": []sub2api.RedeemCode{{
				ID: 901, Code: "code-401", Type: "subscription", Status: "used", UsedBy: &usedBy,
				UsedAt: &now, GroupID: &groupID, ValidityDays: 30,
			}},
			"total": 1, "page": 1, "page_size": 20, "pages": 1,
		})
	}))
	t.Cleanup(server.Close)
	svc := New(&config.RuntimeConfig{}, store, sub2api.NewClient(server.URL, "admin-key"), nil)

	updated, err := svc.SyncRedeemCodes(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, updated)
	select {
	case <-svc.resetWake:
	default:
		t.Fatal("redeem-code sync should wake subscription reset reconciliation")
	}
}

func newResetPeriodTestStore(t *testing.T) *db.Store {
	t.Helper()
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))
	return store
}

func newResetPeriodTestService(t *testing.T, store *db.Store, subscriptions []sub2api.Subscription) *Service {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/admin/subscriptions", r.URL.Path)
		require.Equal(t, "admin-key", r.Header.Get("x-api-key"))
		writeRedeemTestEnvelope(w, subscriptionPageForTest(subscriptions))
	}))
	t.Cleanup(server.Close)
	return New(&config.RuntimeConfig{}, store, sub2api.NewClient(server.URL, "admin-key"), nil)
}

func subscriptionPageForTest(subscriptions []sub2api.Subscription) map[string]any {
	if subscriptions == nil {
		subscriptions = []sub2api.Subscription{}
	}
	return map[string]any{
		"items": subscriptions, "total": len(subscriptions), "page": 1, "page_size": 100,
		"pages": func() int {
			if len(subscriptions) == 0 {
				return 0
			}
			return 1
		}(),
	}
}

func insertResetAccessFixture(t *testing.T, store *db.Store, id, userID, groupID int64, days, resetCount int, via, result string, fulfilledAt time.Time) {
	t.Helper()
	_, err := store.DB.Exec(`
INSERT INTO redeem_access_requests (
  id, requestor_upstream_user_id, requestor_email, requestor_username, tier_id, code_type, tier_label,
  pay_amount_cny, sub2api_group_id, validity_days, concurrency, reset_count, fulfillment_mode,
  fulfillment_result, fulfilled_via, status, approval_token_hash, approval_token_expires_at,
  approved_at, consumed_at, created_at, updated_at
) VALUES (?, ?, 'user@example.com', 'user', 1, 'subscription', 'Subscription', 88, ?, ?, 10, ?, 'redeem_code', ?, ?, 'consumed', 'hash', ?, ?, ?, ?, ?)
`, id, userID, groupID, days, resetCount, result, via, formatTime(fulfilledAt.Add(24*time.Hour)), formatTime(fulfilledAt), formatTime(fulfilledAt), formatTime(fulfilledAt), formatTime(fulfilledAt))
	require.NoError(t, err)
}

func insertPendingDirectAccessFixture(t *testing.T, store *db.Store, id, userID, groupID int64, days, resetCount int, createdAt time.Time) {
	t.Helper()
	_, err := store.DB.Exec(`
INSERT INTO redeem_access_requests (
  id, requestor_upstream_user_id, requestor_email, requestor_username, tier_id, code_type, tier_label,
  pay_amount_cny, sub2api_group_id, validity_days, concurrency, reset_count, fulfillment_mode,
  status, approval_token_hash, approval_token_expires_at, created_at, updated_at
) VALUES (?, ?, 'user@example.com', 'user', 50, 'subscription', 'Subscription', 88, ?, ?, 10, ?, 'direct_charge',
          'pending', 'hash', ?, ?, ?)
`, id, userID, groupID, days, resetCount, formatTime(createdAt.Add(24*time.Hour)), formatTime(createdAt), formatTime(createdAt))
	require.NoError(t, err)
}

func insertResetTierFixture(t *testing.T, store *db.Store, id, groupID int64, days, resetCount int, createdAt time.Time) {
	t.Helper()
	_, err := store.DB.Exec(`
INSERT INTO redeem_tiers (
  id, code_type, pay_amount_cny, label, enabled, sub2api_group_id, validity_days, concurrency, reset_count, created_at, updated_at
) VALUES (?, 'subscription', 88, 'Monthly', 1, ?, ?, 10, ?, ?, ?)
`, id, groupID, days, resetCount, formatTime(createdAt), formatTime(createdAt))
	require.NoError(t, err)
}

func insertUsedSubscriptionCodeFixture(t *testing.T, store *db.Store, accessRequestID, userID, groupID int64, days int, usedAt time.Time) {
	t.Helper()
	result, err := store.DB.Exec(`
INSERT INTO redeem_requests (
  access_request_id, requestor_upstream_user_id, requestor_email, requestor_username, code_type,
  tier_id, value, sub2api_group_id, validity_days, status, note, upstream_code, error_message, created_at, updated_at
) VALUES (?, ?, 'user@example.com', 'user', 'subscription', 1, 0, ?, ?, 'issued', '', ?, '', ?, ?)
`, accessRequestID, userID, groupID, days, "code-"+strconv.FormatInt(accessRequestID, 10), formatTime(usedAt), formatTime(usedAt))
	require.NoError(t, err)
	redeemRequestID, err := result.LastInsertId()
	require.NoError(t, err)
	_, err = store.DB.Exec(`
INSERT INTO redeem_codes (
  request_id, code, code_type, value, status, used_by_upstream_user_id, used_at,
  sub2api_group_id, validity_days, created_at, updated_at
) VALUES (?, ?, 'subscription', 0, 'used', ?, ?, ?, ?, ?, ?)
`, redeemRequestID, "code-"+strconv.FormatInt(accessRequestID, 10), userID, formatTime(usedAt), groupID, days, formatTime(usedAt), formatTime(usedAt))
	require.NoError(t, err)
}

func insertPendingResetPeriodFixture(t *testing.T, store *db.Store, id, accessRequestID, userID, groupID int64, days, resetLimit int, fulfilledAt time.Time) {
	t.Helper()
	_, err := store.DB.Exec(`
INSERT INTO subscription_reset_periods (
  id, access_request_id, upstream_user_id, tier_id, sub2api_group_id, validity_days,
  reset_limit, fulfilled_at, fulfillment_order, status, created_at, updated_at
) VALUES (?, ?, ?, 1, ?, ?, ?, ?, ?, 'pending_binding', ?, ?)
`, id, accessRequestID, userID, groupID, days, resetLimit, formatTime(fulfilledAt), accessRequestID, formatTime(fulfilledAt), formatTime(fulfilledAt))
	require.NoError(t, err)
}

func insertResetPeriodFixture(t *testing.T, store *db.Store, id, accessRequestID, userID, groupID, subscriptionID int64, days, resetLimit int, start, end time.Time, status string) {
	t.Helper()
	_, err := store.DB.Exec(`
INSERT INTO subscription_reset_periods (
  id, access_request_id, upstream_user_id, tier_id, sub2api_group_id, upstream_subscription_id,
  validity_days, reset_limit, fulfilled_at, fulfillment_order, period_start, period_end,
  status, created_at, updated_at
) VALUES (?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, id, accessRequestID, userID, groupID, subscriptionID, days, resetLimit, formatTime(start), accessRequestID, formatTime(start), formatTime(end), status, formatTime(start), formatTime(start))
	require.NoError(t, err)
}

func loadResetPeriodsForTest(t *testing.T, store *db.Store) []models.SubscriptionResetPeriod {
	t.Helper()
	rows, err := store.DB.Query(`
SELECT id, access_request_id, upstream_user_id, tier_id, sub2api_group_id, upstream_subscription_id,
       validity_days, reset_limit, reset_used, fulfilled_at, fulfillment_order, period_start, period_end,
       status, inferred_from_legacy, migration_version, legacy_reset_backfilled, last_synced_at,
       last_error, created_at, updated_at
FROM subscription_reset_periods
ORDER BY period_start ASC, fulfilled_at ASC, access_request_id ASC
`)
	require.NoError(t, err)
	defer rows.Close()
	var out []models.SubscriptionResetPeriod
	for rows.Next() {
		period, scanErr := scanSubscriptionResetPeriod(rows)
		require.NoError(t, scanErr)
		out = append(out, *period)
	}
	require.NoError(t, rows.Err())
	return out
}

func writeRawEnvelopeForTest(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "success", "data": data})
}
