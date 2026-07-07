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
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sync_state (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
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
		"fulfillment_mode":          "TEXT NOT NULL DEFAULT 'direct_charge'",
		"fulfillment_result":        "TEXT NOT NULL DEFAULT ''",
		"fulfilled_via":             "TEXT NOT NULL DEFAULT ''",
		"fulfillment_error":         "TEXT NOT NULL DEFAULT ''",
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
  sub2api_group_id, validity_days, created_at, updated_at
)
SELECT id, 'balance', amount, pay_amount_cny, original_pay_amount_cny, label, enabled, sort_order,
       NULL, 0, created_at, updated_at
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
