package db

import (
	"context"
	"fmt"
)

const schema = `
CREATE TABLE IF NOT EXISTS upstream_users (
  upstream_user_id INTEGER PRIMARY KEY,
  email TEXT NOT NULL,
  username TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT '',
  profile_json TEXT NOT NULL DEFAULT '',
  last_seen_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  upstream_user_id INTEGER NOT NULL,
  access_token TEXT NOT NULL,
  refresh_token TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS redeem_access_requests (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  requestor_upstream_user_id INTEGER NOT NULL,
  requestor_email TEXT NOT NULL,
  requestor_username TEXT NOT NULL,
  tier_id INTEGER NOT NULL DEFAULT 0,
  code_type TEXT NOT NULL DEFAULT 'balance',
  tier_label TEXT NOT NULL DEFAULT '',
  amount REAL NOT NULL DEFAULT 0,
  pay_amount_cny REAL NOT NULL DEFAULT 0,
  sub2api_group_id INTEGER NULL,
  sub2api_group_name TEXT NOT NULL DEFAULT '',
  sub2api_group_platform TEXT NOT NULL DEFAULT '',
  sub2api_daily_limit_usd REAL NULL,
  sub2api_weekly_limit_usd REAL NULL,
  sub2api_monthly_limit_usd REAL NULL,
  validity_days INTEGER NOT NULL DEFAULT 0,
  concurrency INTEGER NOT NULL DEFAULT 0,
  reset_count INTEGER NOT NULL DEFAULT 0 CHECK (reset_count >= 0),
  note TEXT NOT NULL DEFAULT '',
  fulfillment_mode TEXT NOT NULL DEFAULT 'direct_charge',
  fulfillment_result TEXT NOT NULL DEFAULT '',
  fulfilled_via TEXT NOT NULL DEFAULT '',
  fulfillment_error TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  approval_token_hash TEXT NOT NULL,
  approval_token_expires_at TEXT NOT NULL,
  approved_at TEXT NULL,
  rejected_at TEXT NULL,
  consumed_at TEXT NULL,
  notification_status TEXT NOT NULL DEFAULT 'pending',
  notification_error TEXT NOT NULL DEFAULT '',
  notification_sent_at TEXT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_access_requests_user_status
  ON redeem_access_requests(requestor_upstream_user_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS redeem_requests (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  access_request_id INTEGER NOT NULL UNIQUE,
  requestor_upstream_user_id INTEGER NOT NULL,
  requestor_email TEXT NOT NULL,
  requestor_username TEXT NOT NULL,
  code_type TEXT NOT NULL,
  tier_id INTEGER NOT NULL,
  value REAL NOT NULL,
  sub2api_group_id INTEGER NULL,
  validity_days INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL,
  note TEXT NOT NULL DEFAULT '',
  upstream_code TEXT NOT NULL DEFAULT '',
  upstream_code_id INTEGER NULL,
  error_message TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_redeem_requests_user_created
  ON redeem_requests(requestor_upstream_user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS redeem_codes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  request_id INTEGER NOT NULL UNIQUE,
  code TEXT NOT NULL UNIQUE,
  code_type TEXT NOT NULL,
  value REAL NOT NULL,
  status TEXT NOT NULL,
  used_by_upstream_user_id INTEGER NULL,
  used_at TEXT NULL,
  expires_at TEXT NULL,
  sub2api_code_id INTEGER NULL,
  sub2api_group_id INTEGER NULL,
  validity_days INTEGER NOT NULL DEFAULT 0,
  last_synced_at TEXT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_redeem_codes_status
  ON redeem_codes(status, created_at DESC);

CREATE TABLE IF NOT EXISTS redeem_balance_tiers (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  amount REAL NOT NULL,
  pay_amount_cny REAL NOT NULL DEFAULT 0,
  original_pay_amount_cny REAL NULL,
  label TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS redeem_tiers (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  code_type TEXT NOT NULL DEFAULT 'balance',
  amount REAL NOT NULL DEFAULT 0,
  pay_amount_cny REAL NOT NULL DEFAULT 0,
  original_pay_amount_cny REAL NULL,
  label TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  sort_order INTEGER NOT NULL DEFAULT 0,
  sub2api_group_id INTEGER NULL,
  validity_days INTEGER NOT NULL DEFAULT 0,
  concurrency INTEGER NOT NULL DEFAULT 0,
  reset_count INTEGER NOT NULL DEFAULT 0 CHECK (reset_count >= 0),
  legacy_reset_backfill_eligible INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS subscription_reset_periods (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  access_request_id INTEGER NOT NULL UNIQUE,
  upstream_user_id INTEGER NOT NULL,
  tier_id INTEGER NOT NULL,
  sub2api_group_id INTEGER NOT NULL,
  upstream_subscription_id INTEGER NULL,
  validity_days INTEGER NOT NULL,
  reset_limit INTEGER NOT NULL DEFAULT 0 CHECK (reset_limit >= 0),
  reset_used INTEGER NOT NULL DEFAULT 0 CHECK (reset_used >= 0 AND reset_used <= reset_limit),
  fulfilled_at TEXT NOT NULL,
  fulfillment_order INTEGER NOT NULL DEFAULT 0,
  period_start TEXT NULL,
  period_end TEXT NULL,
  status TEXT NOT NULL CHECK (status IN ('pending_binding', 'scheduled', 'active', 'expired', 'inactive')),
  inferred_from_legacy INTEGER NOT NULL DEFAULT 0,
  migration_version INTEGER NOT NULL DEFAULT 0,
  legacy_reset_backfilled INTEGER NOT NULL DEFAULT 0,
  last_synced_at TEXT NULL,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_subscription_reset_periods_user_group
  ON subscription_reset_periods(upstream_user_id, sub2api_group_id, period_start, id);

CREATE INDEX IF NOT EXISTS idx_subscription_reset_periods_subscription
  ON subscription_reset_periods(upstream_subscription_id, status);

CREATE UNIQUE INDEX IF NOT EXISTS idx_subscription_reset_periods_one_active
  ON subscription_reset_periods(upstream_user_id, sub2api_group_id)
  WHERE status = 'active';

CREATE TABLE IF NOT EXISTS subscription_reset_attempts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  request_id TEXT NOT NULL UNIQUE,
  period_id INTEGER NOT NULL,
  upstream_user_id INTEGER NOT NULL,
  upstream_subscription_id INTEGER NOT NULL,
  reset_daily INTEGER NOT NULL DEFAULT 0,
  reset_weekly INTEGER NOT NULL DEFAULT 0,
  reset_monthly INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL CHECK (status IN ('reserved', 'succeeded', 'failed', 'uncertain')),
  before_snapshot_json TEXT NOT NULL DEFAULT '',
  after_snapshot_json TEXT NOT NULL DEFAULT '',
  upstream_status INTEGER NULL,
  response_status INTEGER NOT NULL DEFAULT 0,
  response_reason TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  resolution TEXT NOT NULL DEFAULT '',
  reserved_at TEXT NOT NULL,
  completed_at TEXT NULL,
  confirmed_at TEXT NULL,
  confirmed_by_user_id INTEGER NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_subscription_reset_attempts_blocking
  ON subscription_reset_attempts(period_id)
  WHERE status IN ('reserved', 'uncertain');

CREATE INDEX IF NOT EXISTS idx_subscription_reset_attempts_status
  ON subscription_reset_attempts(status, reserved_at);

CREATE TABLE IF NOT EXISTS subscription_reset_backfill_runs (
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

CREATE TABLE IF NOT EXISTS subscription_concurrency_grants (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  access_request_id INTEGER NOT NULL UNIQUE,
  upstream_user_id INTEGER NOT NULL,
  tier_id INTEGER NOT NULL,
  sub2api_group_id INTEGER NOT NULL,
  desired_concurrency INTEGER NOT NULL,
  upstream_subscription_id INTEGER NULL,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'active', 'inactive')),
  upstream_expires_at TEXT NULL,
  last_synced_at TEXT NULL,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_subscription_concurrency_grants_user_status
  ON subscription_concurrency_grants(upstream_user_id, status);

CREATE TABLE IF NOT EXISTS subscription_concurrency_user_states (
  upstream_user_id INTEGER PRIMARY KEY,
  last_applied_concurrency INTEGER NULL,
  manual_override INTEGER NOT NULL DEFAULT 0,
  manual_override_concurrency INTEGER NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sync_state (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS compensation_batches (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  batch_key TEXT NOT NULL UNIQUE,
  subscription_days INTEGER NOT NULL,
  balance_amount REAL NOT NULL,
  excluded_domains_json TEXT NOT NULL DEFAULT '[]',
  note TEXT NOT NULL DEFAULT '',
  operator_upstream_user_id INTEGER NOT NULL,
  operator_email TEXT NOT NULL DEFAULT '',
  operator_username TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'running',
  total_users INTEGER NOT NULL DEFAULT 0,
  excluded_users INTEGER NOT NULL DEFAULT 0,
  subscription_compensated_users INTEGER NOT NULL DEFAULT 0,
  balance_compensated_users INTEGER NOT NULL DEFAULT 0,
  skipped_zero_balance_users INTEGER NOT NULL DEFAULT 0,
  failed_users INTEGER NOT NULL DEFAULT 0,
  detail_count INTEGER NOT NULL DEFAULT 0,
  upstream_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  completed_at TEXT NULL
);

CREATE INDEX IF NOT EXISTS idx_compensation_batches_created
  ON compensation_batches(created_at DESC);

CREATE TABLE IF NOT EXISTS compensation_batch_details (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  batch_id INTEGER NOT NULL,
  detail_key TEXT NOT NULL UNIQUE,
  upstream_user_id INTEGER NOT NULL,
  user_email TEXT NOT NULL DEFAULT '',
  user_username TEXT NOT NULL DEFAULT '',
  user_balance REAL NOT NULL DEFAULT 0,
  excluded INTEGER NOT NULL DEFAULT 0,
  excluded_domain TEXT NOT NULL DEFAULT '',
  has_active_subscriptions INTEGER NOT NULL DEFAULT 0,
  active_subscription_count INTEGER NOT NULL DEFAULT 0,
  active_subscription_ids_json TEXT NOT NULL DEFAULT '[]',
  decision_type TEXT NOT NULL DEFAULT '',
  action_type TEXT NOT NULL DEFAULT '',
  subscription_days INTEGER NOT NULL DEFAULT 0,
  balance_amount REAL NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT '',
  result_reason TEXT NOT NULL DEFAULT '',
  upstream_reference_json TEXT NOT NULL DEFAULT '',
  remark_requested INTEGER NOT NULL DEFAULT 0,
  remark_applied INTEGER NOT NULL DEFAULT 0,
  remark_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_compensation_batch_details_batch
  ON compensation_batch_details(batch_id, id ASC);

CREATE INDEX IF NOT EXISTS idx_compensation_batch_details_user
  ON compensation_batch_details(upstream_user_id, created_at DESC);
`

func (s *Store) Migrate(ctx context.Context) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("db store is nil")
	}
	if _, err := s.DB.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	if err := s.ensureAccessRequestTierColumn(ctx); err != nil {
		return err
	}
	if err := s.ensureAccessRequestAmountColumns(ctx); err != nil {
		return err
	}
	if err := s.ensureBalanceTierPayAmountColumn(ctx); err != nil {
		return err
	}
	if err := s.ensureTierOriginalPayAmountColumns(ctx); err != nil {
		return err
	}
	if err := s.ensureRedeemTierConcurrencyColumn(ctx); err != nil {
		return err
	}
	if err := s.ensureSubscriptionResetColumns(ctx); err != nil {
		return err
	}
	if err := s.freezeLegacySubscriptionResetEligibility(ctx); err != nil {
		return err
	}
	if err := s.ensureAccessRequestSnapshotColumns(ctx); err != nil {
		return err
	}
	if err := s.ensureRedeemRequestSubscriptionColumns(ctx); err != nil {
		return err
	}
	if err := s.ensureRedeemCodeSubscriptionColumns(ctx); err != nil {
		return err
	}
	if err := s.seedBalanceTiers(ctx); err != nil {
		return err
	}
	if err := s.migrateBalanceTiersToRedeemTiers(ctx); err != nil {
		return err
	}
	return s.backfillDirectChargeRedeemCodes(ctx)
}

func (s *Store) ensureSubscriptionResetColumns(ctx context.Context) error {
	if err := s.ensureColumns(ctx, "redeem_tiers", map[string]string{
		"reset_count":                    "INTEGER NOT NULL DEFAULT 0 CHECK (reset_count >= 0)",
		"legacy_reset_backfill_eligible": "INTEGER NOT NULL DEFAULT 0",
	}); err != nil {
		return err
	}
	return s.ensureColumns(ctx, "redeem_access_requests", map[string]string{
		"reset_count": "INTEGER NOT NULL DEFAULT 0 CHECK (reset_count >= 0)",
	})
}

func (s *Store) freezeLegacySubscriptionResetEligibility(ctx context.Context) error {
	now := s.NowUTC().Format(timeLayout)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
INSERT INTO sync_state (key, value, updated_at)
VALUES ('subscription_reset_legacy_eligibility_initialized', '1', ?)
ON CONFLICT(key) DO NOTHING
`, now)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 1 {
		if _, err := tx.ExecContext(ctx, `
UPDATE redeem_tiers
SET legacy_reset_backfill_eligible = 1
WHERE code_type = 'subscription'
`); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ensureAccessRequestSnapshotColumns(ctx context.Context) error {
	return s.ensureColumns(ctx, "redeem_access_requests", map[string]string{
		"code_type":                 "TEXT NOT NULL DEFAULT 'balance'",
		"tier_label":                "TEXT NOT NULL DEFAULT ''",
		"sub2api_group_id":          "INTEGER NULL",
		"sub2api_group_name":        "TEXT NOT NULL DEFAULT ''",
		"sub2api_group_platform":    "TEXT NOT NULL DEFAULT ''",
		"sub2api_daily_limit_usd":   "REAL NULL",
		"sub2api_weekly_limit_usd":  "REAL NULL",
		"sub2api_monthly_limit_usd": "REAL NULL",
		"validity_days":             "INTEGER NOT NULL DEFAULT 0",
		"concurrency":               "INTEGER NOT NULL DEFAULT 0",
		"fulfillment_mode":          "TEXT NOT NULL DEFAULT 'direct_charge'",
		"fulfillment_result":        "TEXT NOT NULL DEFAULT ''",
		"fulfilled_via":             "TEXT NOT NULL DEFAULT ''",
		"fulfillment_error":         "TEXT NOT NULL DEFAULT ''",
	})
}

func (s *Store) ensureRedeemTierConcurrencyColumn(ctx context.Context) error {
	return s.ensureColumns(ctx, "redeem_tiers", map[string]string{
		"concurrency": "INTEGER NOT NULL DEFAULT 0",
	})
}

func (s *Store) ensureRedeemRequestSubscriptionColumns(ctx context.Context) error {
	return s.ensureColumns(ctx, "redeem_requests", map[string]string{
		"sub2api_group_id": "INTEGER NULL",
		"validity_days":    "INTEGER NOT NULL DEFAULT 0",
	})
}

func (s *Store) ensureRedeemCodeSubscriptionColumns(ctx context.Context) error {
	return s.ensureColumns(ctx, "redeem_codes", map[string]string{
		"sub2api_group_id": "INTEGER NULL",
		"validity_days":    "INTEGER NOT NULL DEFAULT 0",
	})
}

func (s *Store) ensureTierOriginalPayAmountColumns(ctx context.Context) error {
	if err := s.ensureColumns(ctx, "redeem_tiers", map[string]string{
		"original_pay_amount_cny": "REAL NULL",
	}); err != nil {
		return err
	}
	return s.ensureColumns(ctx, "redeem_balance_tiers", map[string]string{
		"original_pay_amount_cny": "REAL NULL",
	})
}

func (s *Store) ensureColumns(ctx context.Context, table string, columns map[string]string) error {
	rows, err := s.DB.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return err
	}
	found := map[string]bool{}
	for rows.Next() {
		var (
			cid          int
			name         string
			typeName     string
			notNull      int
			defaultValue any
			pk           int
		)
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &defaultValue, &pk); err != nil {
			_ = rows.Close()
			return err
		}
		found[name] = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for name, spec := range columns {
		if found[name] {
			continue
		}
		if _, err := s.DB.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+name+` `+spec); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureAccessRequestTierColumn(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `PRAGMA table_info(redeem_access_requests)`)
	if err != nil {
		return err
	}

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
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == "tier_id" {
			found = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = s.DB.ExecContext(ctx, `ALTER TABLE redeem_access_requests ADD COLUMN tier_id INTEGER NOT NULL DEFAULT 0`)
	return err
}

func (s *Store) ensureAccessRequestAmountColumns(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `PRAGMA table_info(redeem_access_requests)`)
	if err != nil {
		return err
	}

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
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		switch name {
		case "amount":
			foundAmount = true
		case "pay_amount_cny":
			foundPayAmount = true
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !foundAmount {
		if _, err := s.DB.ExecContext(ctx, `ALTER TABLE redeem_access_requests ADD COLUMN amount REAL NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	if !foundPayAmount {
		if _, err := s.DB.ExecContext(ctx, `ALTER TABLE redeem_access_requests ADD COLUMN pay_amount_cny REAL NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	_, err = s.DB.ExecContext(ctx, `
UPDATE redeem_access_requests
SET amount = COALESCE((SELECT amount FROM redeem_balance_tiers t WHERE t.id = redeem_access_requests.tier_id LIMIT 1), amount),
    pay_amount_cny = COALESCE((SELECT pay_amount_cny FROM redeem_balance_tiers t WHERE t.id = redeem_access_requests.tier_id LIMIT 1), pay_amount_cny)
WHERE tier_id > 0 AND (amount = 0 OR pay_amount_cny = 0)
`)
	return err
}

func (s *Store) ensureBalanceTierPayAmountColumn(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `PRAGMA table_info(redeem_balance_tiers)`)
	if err != nil {
		return err
	}

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
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == "pay_amount_cny" {
			found = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !found {
		if _, err := s.DB.ExecContext(ctx, `ALTER TABLE redeem_balance_tiers ADD COLUMN pay_amount_cny REAL NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE redeem_balance_tiers SET pay_amount_cny = amount WHERE pay_amount_cny = 0 AND amount > 0`)
	return err
}

func (s *Store) seedBalanceTiers(ctx context.Context) error {
	var count int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM redeem_balance_tiers`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	now := s.NowUTC().Format(timeLayout)
	_, err := s.DB.ExecContext(ctx, `
INSERT INTO redeem_balance_tiers (amount, pay_amount_cny, label, enabled, sort_order, created_at, updated_at)
VALUES
  (120, 120, '$120', 1, 10, ?, ?),
  (240, 240, '$240', 1, 20, ?, ?)
`, now, now, now, now)
	return err
}

func (s *Store) migrateBalanceTiersToRedeemTiers(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `
INSERT INTO redeem_tiers (
  id, code_type, amount, pay_amount_cny, original_pay_amount_cny, label, enabled, sort_order,
  sub2api_group_id, validity_days, concurrency, created_at, updated_at
)
SELECT id, 'balance', amount, pay_amount_cny, original_pay_amount_cny, label, enabled, sort_order,
       NULL, 0, 0, created_at, updated_at
FROM redeem_balance_tiers bt
WHERE NOT EXISTS (SELECT 1 FROM redeem_tiers rt WHERE rt.id = bt.id)
`)
	return err
}

func (s *Store) backfillDirectChargeRedeemCodes(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	rows, err := tx.QueryContext(ctx, `
SELECT id, requestor_upstream_user_id, requestor_email, requestor_username, tier_id, code_type,
       amount, sub2api_group_id, validity_days, note, consumed_at, created_at, updated_at
FROM redeem_access_requests ar
WHERE ar.status = 'consumed'
  AND ar.fulfillment_mode = 'direct_charge'
  AND ar.fulfillment_result = 'direct_charge_succeeded'
  AND ar.fulfilled_via = 'direct_charge'
  AND NOT EXISTS (
    SELECT 1 FROM redeem_requests rr WHERE rr.access_request_id = ar.id
  )
ORDER BY ar.id ASC
`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type rowData struct {
		id                      int64
		requestorUpstreamUserID int64
		requestorEmail          string
		requestorUsername       string
		tierID                  int64
		codeType                string
		amount                  float64
		sub2apiGroupID          any
		validityDays            int
		note                    string
		consumedAt              string
		createdAt               string
		updatedAt               string
	}
	items := make([]rowData, 0)
	for rows.Next() {
		var item rowData
		if err := rows.Scan(
			&item.id,
			&item.requestorUpstreamUserID,
			&item.requestorEmail,
			&item.requestorUsername,
			&item.tierID,
			&item.codeType,
			&item.amount,
			&item.sub2apiGroupID,
			&item.validityDays,
			&item.note,
			&item.consumedAt,
			&item.createdAt,
			&item.updatedAt,
		); err != nil {
			return err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range items {
		timestamp := item.consumedAt
		if timestamp == "" {
			timestamp = item.updatedAt
		}
		if timestamp == "" {
			timestamp = item.createdAt
		}
		result, err := tx.ExecContext(ctx, `
INSERT INTO redeem_requests (
  access_request_id, requestor_upstream_user_id, requestor_email, requestor_username,
  code_type, tier_id, value, sub2api_group_id, validity_days, status, note, upstream_code,
  upstream_code_id, error_message, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'issued', ?, ?, NULL, '', ?, ?)
ON CONFLICT(access_request_id) DO NOTHING
`,
			item.id,
			item.requestorUpstreamUserID,
			item.requestorEmail,
			item.requestorUsername,
			normalizeMigrationCodeType(item.codeType),
			item.tierID,
			item.amount,
			item.sub2apiGroupID,
			item.validityDays,
			item.note,
			directChargeMigrationCode(item.id),
			timestamp,
			timestamp,
		)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			continue
		}
		redeemRequestID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
INSERT INTO redeem_codes (
  request_id, code, code_type, value, status, used_by_upstream_user_id, used_at,
  expires_at, sub2api_code_id, sub2api_group_id, validity_days, last_synced_at, created_at, updated_at
) VALUES (?, ?, ?, ?, 'used', ?, ?, NULL, NULL, ?, ?, ?, ?, ?)
ON CONFLICT(request_id) DO NOTHING
`,
			redeemRequestID,
			directChargeMigrationCode(item.id),
			normalizeMigrationCodeType(item.codeType),
			item.amount,
			item.requestorUpstreamUserID,
			timestamp,
			item.sub2apiGroupID,
			item.validityDays,
			timestamp,
			timestamp,
			timestamp,
		)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func directChargeMigrationCode(accessRequestID int64) string {
	return fmt.Sprintf("giftcode-access-%d", accessRequestID)
}

func normalizeMigrationCodeType(codeType string) string {
	if codeType == "subscription" {
		return "subscription"
	}
	return "balance"
}

const timeLayout = "2006-01-02T15:04:05.000000000Z07:00"
