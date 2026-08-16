package db

import (
	"context"
	"database/sql"
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

	var stateColumns int
	require.NoError(t, store.DB.QueryRowContext(context.Background(), `
SELECT COUNT(1) FROM pragma_table_info('subscription_concurrency_user_states')
WHERE name IN (
  'upstream_user_id', 'last_applied_concurrency', 'manual_override',
  'manual_override_concurrency', 'created_at', 'updated_at'
)
`).Scan(&stateColumns))
	require.Equal(t, 6, stateColumns)
}

func TestOpenAndMigrateAddsSubscriptionResetSchema(t *testing.T) {
	store, err := Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.Migrate(context.Background()))

	for _, table := range []string{"redeem_tiers", "redeem_access_requests"} {
		var count int
		require.NoError(t, store.DB.QueryRowContext(context.Background(), `
SELECT COUNT(1) FROM pragma_table_info(?)
WHERE name = 'reset_count' AND type = 'INTEGER' AND [notnull] = 1 AND dflt_value = '0'
`, table).Scan(&count))
		require.Equal(t, 1, count, "expected %s.reset_count", table)
	}

	expectedColumns := map[string][]string{
		"subscription_reset_periods": {
			"legacy_ignored", "legacy_ignored_at", "legacy_ignore_reason",
		},
		"subscription_reset_attempts": {
			"period_id", "entitlement_type", "entitlement_id", "upstream_subscription_id",
		},
		"subscription_reset_backfill_runs": {
			"superseded_at", "superseded_reason",
		},
		"subscription_reset_bonus_batches": {
			"batch_key", "target_scope", "selected_user_ids_json", "group_ids_json", "reset_count", "status",
		},
		"subscription_reset_bonus_batch_details": {
			"batch_id", "upstream_subscription_id", "status", "reason", "bonus_grant_id",
		},
		"subscription_reset_bonus_grants": {
			"batch_id", "batch_detail_id", "upstream_subscription_id", "reset_limit", "reset_used", "expires_at", "status",
		},
		"subscription_extension_events": {
			"event_key", "source_type", "upstream_subscription_id", "extension_days", "before_expires_at", "after_expires_at", "status", "resolution",
		},
	}
	for table, columns := range expectedColumns {
		for _, column := range columns {
			var count int
			require.NoError(t, store.DB.QueryRowContext(context.Background(), `SELECT COUNT(1) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&count))
			require.Equal(t, 1, count, "expected %s.%s", table, column)
		}
	}

	var periodNotNull int
	require.NoError(t, store.DB.QueryRowContext(context.Background(), `SELECT [notnull] FROM pragma_table_info('subscription_reset_attempts') WHERE name = 'period_id'`).Scan(&periodNotNull))
	require.Zero(t, periodNotNull)

	var blockingIndexSQL string
	require.NoError(t, store.DB.QueryRowContext(context.Background(), `
SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'idx_subscription_reset_attempts_blocking'
`).Scan(&blockingIndexSQL))
	require.Contains(t, blockingIndexSQL, "upstream_subscription_id")
	require.Contains(t, blockingIndexSQL, "WHERE status IN ('reserved', 'uncertain')")

	var activeIndexSQL string
	require.NoError(t, store.DB.QueryRowContext(context.Background(), `
SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'idx_subscription_reset_periods_one_active'
`).Scan(&activeIndexSQL))
	require.Contains(t, activeIndexSQL, "WHERE status = 'active'")
	require.Contains(t, activeIndexSQL, "legacy_ignored = 0")
}

func TestMigratePreservesAttemptsAndSupersedesLegacyResetBackfill(t *testing.T) {
	store, err := Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	_, err = store.DB.ExecContext(ctx, `
CREATE TABLE redeem_tiers (
  id INTEGER PRIMARY KEY AUTOINCREMENT, code_type TEXT NOT NULL DEFAULT 'balance', amount REAL NOT NULL DEFAULT 0,
  pay_amount_cny REAL NOT NULL DEFAULT 0, original_pay_amount_cny REAL NULL, label TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1, sort_order INTEGER NOT NULL DEFAULT 0, sub2api_group_id INTEGER NULL,
  validity_days INTEGER NOT NULL DEFAULT 0, concurrency INTEGER NOT NULL DEFAULT 0,
  reset_count INTEGER NOT NULL DEFAULT 0, legacy_reset_backfill_eligible INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE subscription_reset_periods (
  id INTEGER PRIMARY KEY AUTOINCREMENT, access_request_id INTEGER NOT NULL UNIQUE, upstream_user_id INTEGER NOT NULL,
  tier_id INTEGER NOT NULL, sub2api_group_id INTEGER NOT NULL, upstream_subscription_id INTEGER NULL,
  validity_days INTEGER NOT NULL, reset_limit INTEGER NOT NULL DEFAULT 0, reset_used INTEGER NOT NULL DEFAULT 0,
  fulfilled_at TEXT NOT NULL, fulfillment_order INTEGER NOT NULL DEFAULT 0, period_start TEXT NULL, period_end TEXT NULL,
  status TEXT NOT NULL, inferred_from_legacy INTEGER NOT NULL DEFAULT 0, migration_version INTEGER NOT NULL DEFAULT 0,
  legacy_reset_backfilled INTEGER NOT NULL DEFAULT 0, last_synced_at TEXT NULL, last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE subscription_reset_attempts (
  id INTEGER PRIMARY KEY AUTOINCREMENT, request_id TEXT NOT NULL UNIQUE, period_id INTEGER NOT NULL,
  upstream_user_id INTEGER NOT NULL, upstream_subscription_id INTEGER NOT NULL,
  reset_daily INTEGER NOT NULL DEFAULT 0, reset_weekly INTEGER NOT NULL DEFAULT 0, reset_monthly INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL, before_snapshot_json TEXT NOT NULL DEFAULT '', after_snapshot_json TEXT NOT NULL DEFAULT '',
  upstream_status INTEGER NULL, response_status INTEGER NOT NULL DEFAULT 0, response_reason TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '', resolution TEXT NOT NULL DEFAULT '', reserved_at TEXT NOT NULL,
  completed_at TEXT NULL, confirmed_at TEXT NULL, confirmed_by_user_id INTEGER NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE subscription_reset_backfill_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT, tier_id INTEGER NOT NULL UNIQUE, reset_limit INTEGER NOT NULL,
  status TEXT NOT NULL, total_records INTEGER NOT NULL DEFAULT 0, processed_records INTEGER NOT NULL DEFAULT 0,
  granted_records INTEGER NOT NULL DEFAULT 0, error_message TEXT NOT NULL DEFAULT '', retry_count INTEGER NOT NULL DEFAULT 0,
  last_error_at TEXT NULL, triggered_at TEXT NOT NULL, started_at TEXT NULL, completed_at TEXT NULL, updated_at TEXT NOT NULL
);
INSERT INTO redeem_tiers (id, code_type, label, reset_count, legacy_reset_backfill_eligible, created_at, updated_at)
VALUES (50, 'subscription', 'Legacy Std', 3, 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
INSERT INTO subscription_reset_periods (
  id, access_request_id, upstream_user_id, tier_id, sub2api_group_id, upstream_subscription_id,
  validity_days, reset_limit, reset_used, fulfilled_at, fulfillment_order, period_start, period_end,
  status, inferred_from_legacy, migration_version, legacy_reset_backfilled, created_at, updated_at
) VALUES
  (1, 101, 5, 50, 20, 17, 30, 3, 1, '2026-06-24T00:00:00Z', 101, '2026-06-24T00:00:00Z', '2026-07-24T00:00:00Z', 'active', 1, 1, 1, '2026-06-24T00:00:00Z', '2026-06-24T00:00:00Z'),
  (2, 102, 1, 50, 20, NULL, 30, 0, 0, '2026-06-28T00:00:00Z', 102, NULL, NULL, 'pending_binding', 1, 1, 0, '2026-06-28T00:00:00Z', '2026-06-28T00:00:00Z');
INSERT INTO subscription_reset_attempts (
  id, request_id, period_id, upstream_user_id, upstream_subscription_id, reset_daily, status,
  before_snapshot_json, response_status, reserved_at, created_at, updated_at
) VALUES (9, '123e4567-e89b-42d3-a456-426614174000', 1, 5, 17, 1, 'succeeded', '{}', 200,
  '2026-07-01T00:00:00Z', '2026-07-01T00:00:00Z', '2026-07-01T00:00:00Z');
INSERT INTO subscription_reset_backfill_runs (
  id, tier_id, reset_limit, status, total_records, processed_records, granted_records, error_message,
  retry_count, triggered_at, updated_at
) VALUES (7, 50, 3, 'failed', 4, 2, 2, 'awaiting reconciliation', 72,
  '2026-07-01T00:00:00Z', '2026-07-17T00:00:00Z');
`)
	require.NoError(t, err)

	require.NoError(t, store.Migrate(ctx))
	require.NoError(t, store.Migrate(ctx))

	var periodID sql.NullInt64
	var entitlementType string
	var entitlementID int64
	require.NoError(t, store.DB.QueryRowContext(ctx, `SELECT period_id, entitlement_type, entitlement_id FROM subscription_reset_attempts WHERE id = 9`).Scan(&periodID, &entitlementType, &entitlementID))
	require.True(t, periodID.Valid)
	require.Equal(t, int64(1), periodID.Int64)
	require.Equal(t, "base_period", entitlementType)
	require.Equal(t, int64(1), entitlementID)

	var status, supersededReason string
	var supersededAt sql.NullString
	require.NoError(t, store.DB.QueryRowContext(ctx, `SELECT status, superseded_at, superseded_reason FROM subscription_reset_backfill_runs WHERE id = 7`).Scan(&status, &supersededAt, &supersededReason))
	require.Equal(t, "superseded", status)
	require.True(t, supersededAt.Valid)
	require.NotEmpty(t, supersededReason)

	var grantedIgnored, missingIgnored int
	require.NoError(t, store.DB.QueryRowContext(ctx, `SELECT legacy_ignored FROM subscription_reset_periods WHERE id = 1`).Scan(&grantedIgnored))
	require.NoError(t, store.DB.QueryRowContext(ctx, `SELECT legacy_ignored FROM subscription_reset_periods WHERE id = 2`).Scan(&missingIgnored))
	require.Zero(t, grantedIgnored, "already granted reset counts must remain usable")
	require.Equal(t, 1, missingIgnored, "ungranted legacy periods must stop participating in reconciliation")
}

func TestMigrateLegacyIgnoredActivePeriodDoesNotBlockReplacement(t *testing.T) {
	store, err := Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	require.NoError(t, store.Migrate(ctx))

	_, err = store.DB.ExecContext(ctx, `
INSERT INTO subscription_reset_periods (
  access_request_id, upstream_user_id, tier_id, sub2api_group_id, upstream_subscription_id,
  validity_days, reset_limit, fulfilled_at, period_start, period_end, status,
  legacy_ignored, legacy_ignored_at, legacy_ignore_reason, created_at, updated_at
) VALUES (101, 1, 50, 20, 17, 30, 0, '2026-06-01T00:00:00Z', '2026-06-01T00:00:00Z',
  '2026-07-01T00:00:00Z', 'active', 1, '2026-07-18T00:00:00Z', 'disabled',
  '2026-06-01T00:00:00Z', '2026-07-18T00:00:00Z');
INSERT INTO subscription_reset_periods (
  access_request_id, upstream_user_id, tier_id, sub2api_group_id, upstream_subscription_id,
  validity_days, reset_limit, fulfilled_at, period_start, period_end, status, created_at, updated_at
) VALUES (102, 1, 51, 20, 21, 30, 3, '2026-07-17T00:00:00Z', '2026-07-17T00:00:00Z',
  '2026-08-16T00:00:00Z', 'active', '2026-07-17T00:00:00Z', '2026-07-17T00:00:00Z');
`)
	require.NoError(t, err)
}

func TestMigrateDoesNotIgnorePostBackfillPeriod(t *testing.T) {
	store, err := Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	require.NoError(t, store.Migrate(ctx))
	_, err = store.DB.ExecContext(ctx, `
INSERT INTO redeem_tiers (id, code_type, label, reset_count, legacy_reset_backfill_eligible, created_at, updated_at)
VALUES (50, 'subscription', 'Legacy Std', 3, 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
INSERT INTO subscription_reset_backfill_runs (
  tier_id, reset_limit, status, triggered_at, updated_at
) VALUES (50, 3, 'failed', '2026-07-01T00:00:00Z', '2026-07-01T00:00:00Z');
INSERT INTO subscription_reset_periods (
  access_request_id, upstream_user_id, tier_id, sub2api_group_id, validity_days, reset_limit,
  fulfilled_at, status, inferred_from_legacy, created_at, updated_at
) VALUES (201, 1, 50, 20, 30, 0, '2026-07-10T00:00:00Z', 'pending_binding', 0,
  '2026-07-10T00:00:00Z', '2026-07-10T00:00:00Z');
`)
	require.NoError(t, err)
	require.NoError(t, store.Migrate(ctx))

	var ignored int
	require.NoError(t, store.DB.QueryRowContext(ctx, `SELECT legacy_ignored FROM subscription_reset_periods WHERE access_request_id = 201`).Scan(&ignored))
	require.Zero(t, ignored)
}

func TestMigrateDuplicateUncertainAttemptsDoNotReleaseReservedCounts(t *testing.T) {
	store, err := Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	_, err = store.DB.ExecContext(ctx, `
CREATE TABLE subscription_reset_periods (
  id INTEGER PRIMARY KEY AUTOINCREMENT, access_request_id INTEGER NOT NULL UNIQUE, upstream_user_id INTEGER NOT NULL,
  tier_id INTEGER NOT NULL, sub2api_group_id INTEGER NOT NULL, upstream_subscription_id INTEGER NULL,
  validity_days INTEGER NOT NULL, reset_limit INTEGER NOT NULL DEFAULT 0, reset_used INTEGER NOT NULL DEFAULT 0,
  fulfilled_at TEXT NOT NULL, fulfillment_order INTEGER NOT NULL DEFAULT 0, period_start TEXT NULL, period_end TEXT NULL,
  status TEXT NOT NULL, inferred_from_legacy INTEGER NOT NULL DEFAULT 0, migration_version INTEGER NOT NULL DEFAULT 0,
  legacy_reset_backfilled INTEGER NOT NULL DEFAULT 0, last_synced_at TEXT NULL, last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE subscription_reset_attempts (
  id INTEGER PRIMARY KEY AUTOINCREMENT, request_id TEXT NOT NULL UNIQUE, period_id INTEGER NOT NULL,
  upstream_user_id INTEGER NOT NULL, upstream_subscription_id INTEGER NOT NULL,
  reset_daily INTEGER NOT NULL DEFAULT 0, reset_weekly INTEGER NOT NULL DEFAULT 0, reset_monthly INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL, before_snapshot_json TEXT NOT NULL DEFAULT '', after_snapshot_json TEXT NOT NULL DEFAULT '',
  upstream_status INTEGER NULL, response_status INTEGER NOT NULL DEFAULT 0, response_reason TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '', resolution TEXT NOT NULL DEFAULT '', reserved_at TEXT NOT NULL,
  completed_at TEXT NULL, confirmed_at TEXT NULL, confirmed_by_user_id INTEGER NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_subscription_reset_attempts_blocking
  ON subscription_reset_attempts(period_id) WHERE status IN ('reserved', 'uncertain');
INSERT INTO subscription_reset_periods (
  id, access_request_id, upstream_user_id, tier_id, sub2api_group_id, upstream_subscription_id,
  validity_days, reset_limit, reset_used, fulfilled_at, period_start, period_end, status, created_at, updated_at
) VALUES
  (1, 101, 1, 50, 20, 17, 30, 3, 1, '2026-06-01T00:00:00Z', '2026-06-01T00:00:00Z', '2026-07-01T00:00:00Z', 'expired', '2026-06-01T00:00:00Z', '2026-07-01T00:00:00Z'),
  (2, 102, 1, 50, 20, 17, 30, 3, 1, '2026-07-01T00:00:00Z', '2026-07-01T00:00:00Z', '2026-08-01T00:00:00Z', 'active', '2026-07-01T00:00:00Z', '2026-07-01T00:00:00Z');
INSERT INTO subscription_reset_attempts (
  id, request_id, period_id, upstream_user_id, upstream_subscription_id, status, reserved_at, created_at, updated_at
) VALUES
  (1, '11111111-1111-4111-8111-111111111111', 1, 1, 17, 'uncertain', '2026-07-01T00:00:00Z', '2026-07-01T00:00:00Z', '2026-07-01T00:00:00Z'),
  (2, '22222222-2222-4222-8222-222222222222', 2, 1, 17, 'uncertain', '2026-07-02T00:00:00Z', '2026-07-02T00:00:00Z', '2026-07-02T00:00:00Z');
`)
	require.NoError(t, err)
	require.NoError(t, store.Migrate(ctx))

	var blocking int
	require.NoError(t, store.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM subscription_reset_attempts WHERE status IN ('reserved', 'uncertain')`).Scan(&blocking))
	require.Equal(t, 1, blocking)
	var status, resolution, reason string
	require.NoError(t, store.DB.QueryRowContext(ctx, `SELECT status, resolution, response_reason FROM subscription_reset_attempts WHERE id = 2`).Scan(&status, &resolution, &reason))
	require.Equal(t, "succeeded", status)
	require.Equal(t, "consumed", resolution)
	require.Equal(t, "migration_duplicate_blocking_conservatively_consumed", reason)
	var resetUsed int
	require.NoError(t, store.DB.QueryRowContext(ctx, `SELECT reset_used FROM subscription_reset_periods WHERE id = 2`).Scan(&resetUsed))
	require.Equal(t, 1, resetUsed)
}

func TestMigrateAddsBackfillRetryAuditColumnsWithoutLosingRun(t *testing.T) {
	store, err := Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	_, err = store.DB.Exec(`
CREATE TABLE subscription_reset_backfill_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  tier_id INTEGER NOT NULL UNIQUE,
  reset_limit INTEGER NOT NULL CHECK (reset_limit > 0),
  status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'succeeded', 'failed')),
  total_records INTEGER NOT NULL DEFAULT 0,
  processed_records INTEGER NOT NULL DEFAULT 0,
  granted_records INTEGER NOT NULL DEFAULT 0,
  error_message TEXT NOT NULL DEFAULT '',
  triggered_at TEXT NOT NULL,
  started_at TEXT NULL,
  completed_at TEXT NULL,
  updated_at TEXT NOT NULL
);
INSERT INTO subscription_reset_backfill_runs (
  tier_id, reset_limit, status, error_message, triggered_at, updated_at
) VALUES (50, 2, 'failed', 'temporary failure', '2026-07-17T00:00:00Z', '2026-07-17T00:01:00Z');
`)
	require.NoError(t, err)

	require.NoError(t, store.Migrate(context.Background()))
	require.NoError(t, store.Migrate(context.Background()))

	var tierID, retryCount int
	var lastErrorAt any
	require.NoError(t, store.DB.QueryRow(`
SELECT tier_id, retry_count, last_error_at
FROM subscription_reset_backfill_runs
WHERE tier_id = 50
`).Scan(&tierID, &retryCount, &lastErrorAt))
	require.Equal(t, 50, tierID)
	require.Zero(t, retryCount)
	require.Nil(t, lastErrorAt)
}

func TestMigrateFreezesOnlyPreexistingSubscriptionTiersForLegacyResetBackfill(t *testing.T) {
	store, err := Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	_, err = store.DB.ExecContext(ctx, `
CREATE TABLE redeem_tiers (
  id INTEGER PRIMARY KEY AUTOINCREMENT, code_type TEXT NOT NULL DEFAULT 'balance', amount REAL NOT NULL DEFAULT 0,
  pay_amount_cny REAL NOT NULL DEFAULT 0, original_pay_amount_cny REAL NULL, label TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1, sort_order INTEGER NOT NULL DEFAULT 0, sub2api_group_id INTEGER NULL,
  validity_days INTEGER NOT NULL DEFAULT 0, concurrency INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
INSERT INTO redeem_tiers (code_type, amount, pay_amount_cny, label, sub2api_group_id, validity_days, concurrency, created_at, updated_at)
VALUES ('subscription', 0, 88, 'Legacy subscription', 7, 30, 10, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
`)
	require.NoError(t, err)

	require.NoError(t, store.Migrate(ctx))

	var legacyEligible int
	require.NoError(t, store.DB.QueryRowContext(ctx, `SELECT legacy_reset_backfill_eligible FROM redeem_tiers WHERE id = 1`).Scan(&legacyEligible))
	require.Equal(t, 1, legacyEligible)

	_, err = store.DB.ExecContext(ctx, `
INSERT INTO redeem_tiers (
  code_type, amount, pay_amount_cny, label, enabled, sort_order, sub2api_group_id, validity_days, concurrency,
  reset_count, created_at, updated_at
) VALUES ('subscription', 0, 99, 'New subscription', 1, 20, 8, 30, 10, 0, '2026-07-17T00:00:00Z', '2026-07-17T00:00:00Z')
`)
	require.NoError(t, err)
	require.NoError(t, store.Migrate(ctx))

	var newEligible int
	require.NoError(t, store.DB.QueryRowContext(ctx, `SELECT legacy_reset_backfill_eligible FROM redeem_tiers WHERE label = 'New subscription'`).Scan(&newEligible))
	require.Zero(t, newEligible)
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
	var autoCheckpoint int
	require.NoError(t, store.DB.QueryRowContext(context.Background(), `PRAGMA wal_autocheckpoint`).Scan(&autoCheckpoint))
	require.Equal(t, walAutoCheckpointPages, autoCheckpoint)
}

func TestCheckpointWALReturnsStats(t *testing.T) {
	store, err := Open("sqlite", filepath.Join(t.TempDir(), "checkpoint.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate(context.Background()))

	stats, err := store.CheckpointWAL(context.Background())

	require.NoError(t, err)
	require.Zero(t, stats.Busy)
	require.GreaterOrEqual(t, stats.LogFrames, stats.CheckpointedFrames)
	require.GreaterOrEqual(t, stats.WALSizeBytes, int64(0))
}

func TestAppendSQLitePragmasKeepsExistingDistinctPragmas(t *testing.T) {
	dsn := appendSQLitePragmas("locked.sqlite?_pragma=busy_timeout(50)", []string{
		"_pragma=foreign_keys(1)",
		"_pragma=busy_timeout(5000)",
		"_pragma=journal_mode(WAL)",
		"_pragma=wal_autocheckpoint(1000)",
	})

	require.Contains(t, dsn, "_pragma=busy_timeout(50)")
	require.NotContains(t, dsn, "_pragma=busy_timeout(5000)")
	require.Contains(t, dsn, "_pragma=foreign_keys(1)")
	require.Contains(t, dsn, "_pragma=journal_mode(WAL)")
	require.Contains(t, dsn, "_pragma=wal_autocheckpoint(1000)")
}

func TestAppendSQLitePragmasDoesNotDuplicateExistingJournalMode(t *testing.T) {
	dsn := appendSQLitePragmas("app.sqlite?_pragma=journal_mode(WAL)", []string{
		"_pragma=journal_mode(WAL)",
	})

	require.Equal(t, "app.sqlite?_pragma=journal_mode(WAL)", dsn)
}
