package db

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAndMigrateSeedsTiers(t *testing.T) {
	store, err := Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.Migrate(context.Background()))

	var count int
	require.NoError(t, store.DB.QueryRowContext(context.Background(), `SELECT COUNT(1) FROM redeem_balance_tiers`).Scan(&count))
	require.Equal(t, 2, count)
}

func TestOpenAndMigrateSeedsTierPaidAmount(t *testing.T) {
	store, err := Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.Migrate(context.Background()))

	var amount float64
	var payAmount float64
	require.NoError(t, store.DB.QueryRowContext(context.Background(), `
SELECT amount, pay_amount_cny
FROM redeem_balance_tiers
ORDER BY id
LIMIT 1
`).Scan(&amount, &payAmount))
	require.Equal(t, amount, payAmount)
	require.Greater(t, payAmount, 0.0)
}

func TestOpenAndMigrateAddsAccessRequestTierColumn(t *testing.T) {
	store, err := Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.Migrate(context.Background()))

	rows, err := store.DB.QueryContext(context.Background(), `PRAGMA table_info(redeem_access_requests)`)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rows.Close() })

	found := false
	for rows.Next() {
		var (
			cid          int
			name         string
			typeName     string
			notNull      int
			defaultValue any
			pk           int
		)
		require.NoError(t, rows.Scan(&cid, &name, &typeName, &notNull, &defaultValue, &pk))
		if name == "tier_id" {
			found = true
			require.Equal(t, "INTEGER", typeName)
		}
	}
	require.NoError(t, rows.Err())
	require.True(t, found, "expected redeem_access_requests to include tier_id")
}

func TestOpenAndMigrateAddsAccessRequestAmountColumns(t *testing.T) {
	store, err := Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.Migrate(context.Background()))

	rows, err := store.DB.QueryContext(context.Background(), `PRAGMA table_info(redeem_access_requests)`)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rows.Close() })

	foundAmount := false
	foundPayAmount := false
	for rows.Next() {
		var (
			cid          int
			name         string
			typeName     string
			notNull      int
			defaultValue any
			pk           int
		)
		require.NoError(t, rows.Scan(&cid, &name, &typeName, &notNull, &defaultValue, &pk))
		switch name {
		case "amount":
			foundAmount = true
			require.Equal(t, "REAL", typeName)
		case "pay_amount_cny":
			foundPayAmount = true
			require.Equal(t, "REAL", typeName)
		}
	}
	require.NoError(t, rows.Err())
	require.True(t, foundAmount, "expected redeem_access_requests to include amount")
	require.True(t, foundPayAmount, "expected redeem_access_requests to include pay_amount_cny")
}
