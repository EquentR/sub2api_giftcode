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
  amount REAL NOT NULL DEFAULT 0,
  pay_amount_cny REAL NOT NULL DEFAULT 0,
  note TEXT NOT NULL DEFAULT '',
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
  label TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  sort_order INTEGER NOT NULL DEFAULT 0,
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
	return s.seedBalanceTiers(ctx)
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

const timeLayout = "2006-01-02T15:04:05.000000000Z07:00"
