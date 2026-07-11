package db

import (
	"context"
	"path/filepath"
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

func TestOpenAndMigrateAddsRedeemTierSubscriptionColumns(t *testing.T) {
	store, err := Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.Migrate(context.Background()))

	rows, err := store.DB.QueryContext(context.Background(), `PRAGMA table_info(redeem_tiers)`)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rows.Close() })

	foundCodeType := false
	foundGroupID := false
	foundValidityDays := false
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
		case "code_type":
			foundCodeType = true
			require.Equal(t, "TEXT", typeName)
		case "sub2api_group_id":
			foundGroupID = true
			require.Equal(t, "INTEGER", typeName)
		case "validity_days":
			foundValidityDays = true
			require.Equal(t, "INTEGER", typeName)
		}
	}
	require.NoError(t, rows.Err())
	require.True(t, foundCodeType, "expected redeem_tiers to include code_type")
	require.True(t, foundGroupID, "expected redeem_tiers to include sub2api_group_id")
	require.True(t, foundValidityDays, "expected redeem_tiers to include validity_days")
}

func TestOpenAndMigrateAddsSubscriptionConcurrencySchema(t *testing.T) {
	store, err := Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.Migrate(context.Background()))

	for _, table := range []string{"redeem_tiers", "redeem_access_requests"} {
		var count int
		require.NoError(t, store.DB.QueryRowContext(context.Background(), `
SELECT COUNT(1) FROM pragma_table_info(?)
WHERE name = 'concurrency' AND type = 'INTEGER' AND [notnull] = 1 AND dflt_value = '0'
`, table).Scan(&count))
		require.Equal(t, 1, count, "expected %s.concurrency", table)
	}

	var grantColumns int
	require.NoError(t, store.DB.QueryRowContext(context.Background(), `
SELECT COUNT(1) FROM pragma_table_info('subscription_concurrency_grants')
WHERE name IN (
  'id', 'access_request_id', 'upstream_user_id', 'tier_id', 'sub2api_group_id',
  'desired_concurrency', 'upstream_subscription_id', 'status', 'upstream_expires_at',
  'last_synced_at', 'last_error', 'created_at', 'updated_at'
)
`).Scan(&grantColumns))
	require.Equal(t, 13, grantColumns)

	var uniqueIndexNames []string
	rows, err := store.DB.QueryContext(context.Background(), `PRAGMA index_list(subscription_concurrency_grants)`)
	require.NoError(t, err)
	for rows.Next() {
		var seq int
		var name string
		var unique int
		var origin string
		var partial int
		require.NoError(t, rows.Scan(&seq, &name, &unique, &origin, &partial))
		if unique != 1 {
			continue
		}
		uniqueIndexNames = append(uniqueIndexNames, name)
	}
	require.NoError(t, rows.Close())
	require.Len(t, uniqueIndexNames, 1)
	for _, name := range uniqueIndexNames {
		indexRows, indexErr := store.DB.QueryContext(context.Background(), `PRAGMA index_info(`+name+`)`)
		require.NoError(t, indexErr)
		var columnNames []string
		for indexRows.Next() {
			var indexSeq, columnSeq int
			var columnName string
			require.NoError(t, indexRows.Scan(&indexSeq, &columnSeq, &columnName))
			columnNames = append(columnNames, columnName)
		}
		require.NoError(t, indexRows.Close())
		require.Equal(t, []string{"access_request_id"}, columnNames)
	}

	indexRows, err := store.DB.QueryContext(context.Background(), `PRAGMA index_info(idx_subscription_concurrency_grants_user_status)`)
	require.NoError(t, err)
	var userStatusColumns []string
	for indexRows.Next() {
		var indexSeq, columnSeq int
		var columnName string
		require.NoError(t, indexRows.Scan(&indexSeq, &columnSeq, &columnName))
		userStatusColumns = append(userStatusColumns, columnName)
	}
	require.NoError(t, indexRows.Close())
	require.Equal(t, []string{"upstream_user_id", "status"}, userStatusColumns)
}

func TestMigrateUpgradesLegacyConcurrencyColumnsWithoutLosingRows(t *testing.T) {
	store, err := Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	_, err = store.DB.ExecContext(ctx, `
CREATE TABLE redeem_tiers (
  id INTEGER PRIMARY KEY AUTOINCREMENT, code_type TEXT NOT NULL DEFAULT 'balance', amount REAL NOT NULL DEFAULT 0,
  pay_amount_cny REAL NOT NULL DEFAULT 0, original_pay_amount_cny REAL NULL, label TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1, sort_order INTEGER NOT NULL DEFAULT 0, sub2api_group_id INTEGER NULL,
  validity_days INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE redeem_access_requests (
  id INTEGER PRIMARY KEY AUTOINCREMENT, requestor_upstream_user_id INTEGER NOT NULL, requestor_email TEXT NOT NULL,
  requestor_username TEXT NOT NULL, tier_id INTEGER NOT NULL DEFAULT 0, code_type TEXT NOT NULL DEFAULT 'balance',
  tier_label TEXT NOT NULL DEFAULT '', amount REAL NOT NULL DEFAULT 0, pay_amount_cny REAL NOT NULL DEFAULT 0,
  sub2api_group_id INTEGER NULL, sub2api_group_name TEXT NOT NULL DEFAULT '', sub2api_group_platform TEXT NOT NULL DEFAULT '',
  sub2api_daily_limit_usd REAL NULL, sub2api_weekly_limit_usd REAL NULL, sub2api_monthly_limit_usd REAL NULL,
  validity_days INTEGER NOT NULL DEFAULT 0, note TEXT NOT NULL DEFAULT '', fulfillment_mode TEXT NOT NULL DEFAULT 'direct_charge',
  fulfillment_result TEXT NOT NULL DEFAULT '', fulfilled_via TEXT NOT NULL DEFAULT '', fulfillment_error TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL, approval_token_hash TEXT NOT NULL, approval_token_expires_at TEXT NOT NULL,
  approved_at TEXT NULL, rejected_at TEXT NULL, consumed_at TEXT NULL, notification_status TEXT NOT NULL DEFAULT 'pending',
  notification_error TEXT NOT NULL DEFAULT '', notification_sent_at TEXT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
INSERT INTO redeem_tiers (code_type, amount, pay_amount_cny, label, created_at, updated_at)
VALUES ('balance', 77, 70, 'Legacy', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
INSERT INTO redeem_access_requests (
  requestor_upstream_user_id, requestor_email, requestor_username, tier_id, code_type, tier_label, amount,
  pay_amount_cny, status, approval_token_hash, approval_token_expires_at, created_at, updated_at
) VALUES (9, 'legacy@example.com', 'legacy', 1, 'balance', 'Legacy', 77, 70, 'pending', 'hash',
  '2026-01-02T00:00:00Z', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')
`)
	require.NoError(t, err)

	require.NoError(t, store.Migrate(ctx))

	var tierConcurrency, requestConcurrency int
	require.NoError(t, store.DB.QueryRowContext(ctx, `SELECT concurrency FROM redeem_tiers WHERE id = 1`).Scan(&tierConcurrency))
	require.NoError(t, store.DB.QueryRowContext(ctx, `SELECT concurrency FROM redeem_access_requests WHERE id = 1`).Scan(&requestConcurrency))
	require.Zero(t, tierConcurrency)
	require.Zero(t, requestConcurrency)
	var label string
	require.NoError(t, store.DB.QueryRowContext(ctx, `SELECT label FROM redeem_tiers WHERE id = 1`).Scan(&label))
	require.Equal(t, "Legacy", label)
	var userID int64
	require.NoError(t, store.DB.QueryRowContext(ctx, `SELECT requestor_upstream_user_id FROM redeem_access_requests WHERE id = 1`).Scan(&userID))
	require.Equal(t, int64(9), userID)
}

func TestOpenAndMigrateAddsOptionalOriginalPaidAmountColumns(t *testing.T) {
	store, err := Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.Migrate(context.Background()))

	for _, table := range []string{"redeem_tiers", "redeem_balance_tiers"} {
		rows, err := store.DB.QueryContext(context.Background(), `PRAGMA table_info(`+table+`)`)
		require.NoError(t, err)

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
			if name == "original_pay_amount_cny" {
				found = true
				require.Equal(t, "REAL", typeName)
				require.Zero(t, notNull)
			}
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
		require.True(t, found, "expected %s to include original_pay_amount_cny", table)
	}
}

func TestMigrateBackfillsDirectChargeRedeemCodes(t *testing.T) {
	store, err := Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	require.NoError(t, store.Migrate(ctx))
	now := "2026-07-07T12:00:00.000000000Z"
	_, err = store.DB.ExecContext(ctx, `
INSERT INTO redeem_access_requests (
  id, requestor_upstream_user_id, requestor_email, requestor_username, tier_id, code_type, tier_label,
  amount, pay_amount_cny, note, fulfillment_mode, fulfillment_result, fulfilled_via, fulfillment_error,
  status, approval_token_hash, approval_token_expires_at, approved_at, consumed_at,
  notification_status, notification_error, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, 42, 7, "alice@example.com", "alice", 1, "balance", "$120", 120.0, 120.0, "legacy direct", "direct_charge", "direct_charge_succeeded", "direct_charge", "", "consumed", "token-hash", now, now, now, "sent", "", now, now)
	require.NoError(t, err)

	require.NoError(t, store.Migrate(ctx))
	require.NoError(t, store.Migrate(ctx))

	var requestCount int
	require.NoError(t, store.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM redeem_requests WHERE access_request_id = 42`).Scan(&requestCount))
	require.Equal(t, 1, requestCount)

	var code string
	var status string
	var usedBy int64
	var usedAt string
	var value float64
	require.NoError(t, store.DB.QueryRowContext(ctx, `
SELECT c.code, c.status, c.used_by_upstream_user_id, c.used_at, c.value
FROM redeem_codes c
JOIN redeem_requests r ON r.id = c.request_id
WHERE r.access_request_id = 42
`).Scan(&code, &status, &usedBy, &usedAt, &value))
	require.Equal(t, "giftcode-access-42", code)
	require.Equal(t, "used", status)
	require.Equal(t, int64(7), usedBy)
	require.Equal(t, now, usedAt)
	require.Equal(t, 120.0, value)
}

func TestOpenUsesWALForFileBackedSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.sqlite")

	store, err := Open("sqlite", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	var mode string
	require.NoError(t, store.DB.QueryRowContext(context.Background(), `PRAGMA journal_mode`).Scan(&mode))
	require.Equal(t, "wal", mode)
}

func TestAppendSQLitePragmasKeepsExistingDistinctPragmas(t *testing.T) {
	dsn := appendSQLitePragmas("locked.sqlite?_pragma=busy_timeout(50)", []string{
		"_pragma=foreign_keys(1)",
		"_pragma=busy_timeout(5000)",
		"_pragma=journal_mode(WAL)",
	})

	require.Contains(t, dsn, "_pragma=busy_timeout(50)")
	require.NotContains(t, dsn, "_pragma=busy_timeout(5000)")
	require.Contains(t, dsn, "_pragma=foreign_keys(1)")
	require.Contains(t, dsn, "_pragma=journal_mode(WAL)")
}

func TestAppendSQLitePragmasDoesNotDuplicateExistingJournalMode(t *testing.T) {
	dsn := appendSQLitePragmas("app.sqlite?_pragma=journal_mode(WAL)", []string{
		"_pragma=journal_mode(WAL)",
	})

	require.Equal(t, "app.sqlite?_pragma=journal_mode(WAL)", dsn)
}
