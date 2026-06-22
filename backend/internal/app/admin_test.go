package app

import (
	"database/sql"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sub2api-giftcode/backend/internal/config"
	"sub2api-giftcode/backend/internal/db"
	"sub2api-giftcode/backend/internal/models"
)

func TestListUsersReturnsSummaries(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.Migrate(context.Background()))

	now := time.Now().UTC().Truncate(time.Second)
	_, err = store.DB.ExecContext(context.Background(), `
INSERT INTO upstream_users (
  upstream_user_id, email, username, role, status, profile_json, last_seen_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`, 1, "alice@example.com", "alice", "user", "active", `{"id":1}`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	require.NoError(t, err)

	svc := New(&config.RuntimeConfig{}, store, nil, nil)
	users, err := svc.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.Equal(t, int64(1), users[0].UpstreamUserID)
	require.Equal(t, "alice@example.com", users[0].Email)
}

func TestReplaceBalanceTiersPersistsPaidAmount(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.Migrate(context.Background()))

	svc := New(&config.RuntimeConfig{}, store, nil, nil)
	tiers, err := svc.ListBalanceTiers(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, tiers)

	tiers[0].PayAmountCny = 95.25
	updated, err := svc.ReplaceBalanceTiers(context.Background(), tiers)
	require.NoError(t, err)
	require.InDelta(t, 95.25, updated[0].PayAmountCny, 0.0001)

	reloaded, err := svc.ListBalanceTiers(context.Background())
	require.NoError(t, err)
	require.InDelta(t, 95.25, reloaded[0].PayAmountCny, 0.0001)
}

func TestReplaceRedeemTiersPersistsOptionalOriginalPaidAmount(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.Migrate(context.Background()))

	svc := New(&config.RuntimeConfig{}, store, nil, nil)
	updated, err := svc.ReplaceRedeemTiers(context.Background(), []models.RedeemTier{{
		CodeType:             "balance",
		Amount:               120,
		PayAmountCny:         95,
		OriginalPayAmountCny: float64Ptr(120),
		Label:                "Promo balance",
		Enabled:              true,
		SortOrder:            10,
	}})
	require.NoError(t, err)
	require.Len(t, updated, 1)
	require.NotNil(t, updated[0].OriginalPayAmountCny)
	require.InDelta(t, 120, *updated[0].OriginalPayAmountCny, 0.0001)

	reloaded, err := svc.ListRedeemTiers(context.Background(), true)
	require.NoError(t, err)
	require.Len(t, reloaded, 1)
	require.NotNil(t, reloaded[0].OriginalPayAmountCny)
	require.InDelta(t, 120, *reloaded[0].OriginalPayAmountCny, 0.0001)

	reloaded[0].OriginalPayAmountCny = nil
	updated, err = svc.ReplaceRedeemTiers(context.Background(), reloaded)
	require.NoError(t, err)
	require.Nil(t, updated[0].OriginalPayAmountCny)

	reloaded, err = svc.ListRedeemTiers(context.Background(), true)
	require.NoError(t, err)
	require.Nil(t, reloaded[0].OriginalPayAmountCny)
}

func TestReplaceRedeemTiersPersistsSubscriptionTier(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.Migrate(context.Background()))

	svc := New(&config.RuntimeConfig{}, store, nil, nil)
	updated, err := svc.ReplaceRedeemTiers(context.Background(), []models.RedeemTier{{
		CodeType:       "subscription",
		PayAmountCny:   88,
		Label:          "Claude 30 days",
		Enabled:        true,
		SortOrder:      10,
		Sub2APIGroupID: int64Ptr(2),
		ValidityDays:   30,
	}})
	require.NoError(t, err)
	require.Len(t, updated, 1)
	require.Equal(t, "subscription", updated[0].CodeType)
	require.Equal(t, int64(2), *updated[0].Sub2APIGroupID)
	require.Equal(t, 30, updated[0].ValidityDays)

	reloaded, err := svc.ListRedeemTiers(context.Background(), true)
	require.NoError(t, err)
	require.Len(t, reloaded, 1)
	require.Equal(t, "Claude 30 days", reloaded[0].Label)
	require.Equal(t, "subscription", reloaded[0].CodeType)
}

func TestStatsCountsEnabledRedeemTiers(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.Migrate(context.Background()))

	svc := New(&config.RuntimeConfig{}, store, nil, nil)
	_, err = svc.ReplaceRedeemTiers(context.Background(), []models.RedeemTier{{
		CodeType:     "balance",
		Amount:       120,
		PayAmountCny: 120,
		Label:        "Balance",
		Enabled:      true,
		SortOrder:    10,
	}, {
		CodeType:       "subscription",
		PayAmountCny:   88,
		Label:          "Claude 30 days",
		Enabled:        true,
		SortOrder:      20,
		Sub2APIGroupID: int64Ptr(2),
		ValidityDays:   30,
	}})
	require.NoError(t, err)

	stats, err := svc.Stats(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, stats.ActiveTiers)
}

func TestReplaceRedeemTiersDisablesBalanceMirrorWhenTypeChanges(t *testing.T) {
	store, err := db.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.Migrate(context.Background()))

	svc := New(&config.RuntimeConfig{}, store, nil, nil)
	tiers, err := svc.ListRedeemTiers(context.Background(), true)
	require.NoError(t, err)
	require.NotEmpty(t, tiers)
	id := tiers[0].ID

	_, err = svc.ReplaceRedeemTiers(context.Background(), []models.RedeemTier{{
		ID:             id,
		CodeType:       "subscription",
		PayAmountCny:   88,
		Label:          "Changed to subscription",
		Enabled:        true,
		SortOrder:      10,
		Sub2APIGroupID: int64Ptr(2),
		ValidityDays:   30,
	}})
	require.NoError(t, err)

	var enabled int
	err = store.DB.QueryRowContext(context.Background(), `SELECT enabled FROM redeem_balance_tiers WHERE id = ?`, id).Scan(&enabled)
	require.NoError(t, err)
	require.Zero(t, enabled)
}

func TestReplaceRedeemTiersCompletesAfterExternalReadLockReleases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "locked.sqlite") + "?_pragma=busy_timeout(1000)"

	store, err := db.Open("sqlite", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.Migrate(context.Background()))

	svc := New(&config.RuntimeConfig{}, store, nil, nil)
	tiers, err := svc.ListRedeemTiers(context.Background(), true)
	require.NoError(t, err)
	require.NotEmpty(t, tiers)
	tiers[0].Label = tiers[0].Label + " updated"

	lockDB, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lockDB.Close() })

	lockTx, err := lockDB.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	require.NoError(t, err)
	rows, err := lockTx.QueryContext(context.Background(), `SELECT id FROM redeem_tiers`)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rows.Close() })
	t.Cleanup(func() { _ = lockTx.Rollback() })
	require.True(t, rows.Next())

	done := make(chan error, 1)
	go func() {
		_, err := svc.ReplaceRedeemTiers(context.Background(), tiers)
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	require.NoError(t, rows.Close())
	require.NoError(t, lockTx.Rollback())

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("ReplaceRedeemTiers did not finish after releasing the read lock")
	}

	reloaded, err := svc.ListRedeemTiers(context.Background(), true)
	require.NoError(t, err)
	byID := make(map[int64]models.RedeemTier, len(reloaded))
	for _, tier := range reloaded {
		byID[tier.ID] = tier
	}
	require.Equal(t, tiers[0].Label, byID[tiers[0].ID].Label)
}

func TestReplaceRedeemTiersReleasesConnectionAfterCommitBusy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "locked.sqlite") + "?_pragma=busy_timeout(50)"

	store, err := db.Open("sqlite", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.Migrate(context.Background()))

	svc := New(&config.RuntimeConfig{}, store, nil, nil)
	tiers, err := svc.ListRedeemTiers(context.Background(), true)
	require.NoError(t, err)
	require.NotEmpty(t, tiers)
	tiers[0].Label = tiers[0].Label + " commit busy"

	lockDB, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lockDB.Close() })

	lockTx, err := lockDB.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	require.NoError(t, err)
	rows, err := lockTx.QueryContext(context.Background(), `SELECT id FROM redeem_tiers`)
	require.NoError(t, err)
	require.True(t, rows.Next())

	done := make(chan error, 1)
	go func() {
		_, err := svc.ReplaceRedeemTiers(context.Background(), tiers)
		done <- err
	}()

	err = <-done
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "locked")

	require.NoError(t, rows.Close())
	require.NoError(t, lockTx.Rollback())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err = store.DB.ExecContext(ctx, `UPDATE redeem_tiers SET updated_at = updated_at WHERE id = ?`, tiers[0].ID)
	require.NoError(t, err)
}

func int64Ptr(v int64) *int64 {
	return &v
}

func float64Ptr(v float64) *float64 {
	return &v
}
