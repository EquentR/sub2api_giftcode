package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
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
  legacy_ignored INTEGER NOT NULL DEFAULT 0,
  legacy_ignored_at TEXT NULL,
  legacy_ignore_reason TEXT NOT NULL DEFAULT '',
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
  period_id INTEGER NULL,
  entitlement_type TEXT NOT NULL CHECK (entitlement_type IN ('base_period', 'bonus_grant')),
  entitlement_id INTEGER NOT NULL,
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
  ON subscription_reset_attempts(upstream_subscription_id)
  WHERE status IN ('reserved', 'uncertain');

CREATE INDEX IF NOT EXISTS idx_subscription_reset_attempts_status
  ON subscription_reset_attempts(status, reserved_at);

CREATE TABLE IF NOT EXISTS subscription_reset_backfill_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  tier_id INTEGER NOT NULL UNIQUE,
  reset_limit INTEGER NOT NULL CHECK (reset_limit > 0),
  status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'superseded')),
  total_records INTEGER NOT NULL DEFAULT 0,
  processed_records INTEGER NOT NULL DEFAULT 0,
  granted_records INTEGER NOT NULL DEFAULT 0,
  error_message TEXT NOT NULL DEFAULT '',
  retry_count INTEGER NOT NULL DEFAULT 0 CHECK (retry_count >= 0),
  last_error_at TEXT NULL,
  triggered_at TEXT NOT NULL,
  started_at TEXT NULL,
  completed_at TEXT NULL,
  superseded_at TEXT NULL,
  superseded_reason TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS subscription_reset_bonus_batches (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  batch_key TEXT NOT NULL UNIQUE,
  target_scope TEXT NOT NULL CHECK (target_scope IN ('all', 'selected')),
  selected_user_ids_json TEXT NOT NULL DEFAULT '[]',
  group_ids_json TEXT NOT NULL DEFAULT '[]',
  reset_count INTEGER NOT NULL CHECK (reset_count > 0),
  note TEXT NOT NULL DEFAULT '',
  preview_digest TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'completed', 'completed_with_failures', 'failed')),
  total_candidates INTEGER NOT NULL DEFAULT 0,
  processed_candidates INTEGER NOT NULL DEFAULT 0,
  granted_subscriptions INTEGER NOT NULL DEFAULT 0,
  skipped_subscriptions INTEGER NOT NULL DEFAULT 0,
  failed_subscriptions INTEGER NOT NULL DEFAULT 0,
  operator_upstream_user_id INTEGER NOT NULL,
  operator_email TEXT NOT NULL DEFAULT '',
  operator_username TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  started_at TEXT NULL,
  completed_at TEXT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_subscription_reset_bonus_batches_status
  ON subscription_reset_bonus_batches(status, created_at, id);

CREATE TABLE IF NOT EXISTS subscription_reset_bonus_batch_details (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  batch_id INTEGER NOT NULL,
  upstream_user_id INTEGER NOT NULL,
  sub2api_group_id INTEGER NOT NULL,
  upstream_subscription_id INTEGER NOT NULL,
  subscription_starts_at TEXT NOT NULL,
  subscription_expires_at TEXT NOT NULL,
  subscription_status TEXT NOT NULL,
  subscription_snapshot_json TEXT NOT NULL DEFAULT '{}',
  status TEXT NOT NULL CHECK (status IN ('pending', 'granted', 'skipped', 'failed')),
  reason TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  bonus_grant_id INTEGER NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(batch_id, upstream_subscription_id)
);

CREATE INDEX IF NOT EXISTS idx_subscription_reset_bonus_details_batch
  ON subscription_reset_bonus_batch_details(batch_id, status, id);

CREATE TABLE IF NOT EXISTS subscription_reset_bonus_grants (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  batch_id INTEGER NOT NULL,
  batch_detail_id INTEGER NOT NULL UNIQUE,
  upstream_user_id INTEGER NOT NULL,
  sub2api_group_id INTEGER NOT NULL,
  upstream_subscription_id INTEGER NOT NULL,
  reset_limit INTEGER NOT NULL CHECK (reset_limit > 0),
  reset_used INTEGER NOT NULL DEFAULT 0 CHECK (reset_used >= 0 AND reset_used <= reset_limit),
  starts_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('active', 'exhausted', 'expired', 'revoked')),
  subscription_snapshot_json TEXT NOT NULL DEFAULT '{}',
  last_synced_at TEXT NULL,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(batch_id, upstream_subscription_id),
  CHECK (starts_at < expires_at)
);

CREATE INDEX IF NOT EXISTS idx_subscription_reset_bonus_grants_subscription
  ON subscription_reset_bonus_grants(upstream_subscription_id, status, expires_at, id);

CREATE TABLE IF NOT EXISTS subscription_extension_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  event_key TEXT NOT NULL UNIQUE,
  source_type TEXT NOT NULL CHECK (source_type IN ('compensation', 'legacy_compensation')),
  compensation_batch_id INTEGER NULL,
  compensation_detail_id INTEGER NULL,
  upstream_user_id INTEGER NOT NULL,
  sub2api_group_id INTEGER NOT NULL,
  upstream_subscription_id INTEGER NOT NULL,
  extension_days INTEGER NOT NULL CHECK (extension_days > 0),
  before_expires_at TEXT NULL,
  after_expires_at TEXT NULL,
  status TEXT NOT NULL CHECK (status IN ('reserved', 'succeeded', 'failed', 'uncertain')),
  resolution TEXT NOT NULL DEFAULT '' CHECK (resolution IN ('', 'applied', 'released')),
  applied_base_periods INTEGER NOT NULL DEFAULT 0,
  applied_bonus_grants INTEGER NOT NULL DEFAULT 0,
  inferred_from_legacy INTEGER NOT NULL DEFAULT 0,
  migration_version INTEGER NOT NULL DEFAULT 0,
  error_message TEXT NOT NULL DEFAULT '',
  reserved_at TEXT NOT NULL,
  completed_at TEXT NULL,
  confirmed_at TEXT NULL,
  confirmed_by_user_id INTEGER NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(compensation_detail_id, upstream_subscription_id)
);

CREATE INDEX IF NOT EXISTS idx_subscription_extension_events_status
  ON subscription_extension_events(status, resolution, reserved_at, id);

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

CREATE TABLE IF NOT EXISTS aux_scheduler_rules (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  primary_account_ids_json TEXT NOT NULL DEFAULT '[]',
  backup_account_ids_json TEXT NOT NULL DEFAULT '[]',
  model_names_json TEXT NOT NULL DEFAULT '[]',
  lanes_json TEXT NOT NULL DEFAULT '[]',
  maximum_auto_lane INTEGER NOT NULL DEFAULT 2,
  migration_status TEXT NOT NULL DEFAULT '',
  migration_source_json TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'idle' CHECK (state IN ('idle', 'backup_active')),
  expected_open_through_lane INTEGER NOT NULL DEFAULT 1,
  observed_open_through_lane INTEGER NOT NULL DEFAULT 1,
  verified_open_through_lane INTEGER NOT NULL DEFAULT 1,
  target_open_through_lane INTEGER NOT NULL DEFAULT 1,
  transition_status TEXT NOT NULL DEFAULT 'stable',
  transition_generation INTEGER NOT NULL DEFAULT 0,
  upgrade_evidence_json TEXT NOT NULL DEFAULT '{}',
  missing_models_json TEXT NOT NULL DEFAULT '[]',
  recovery_candidate_lane INTEGER NULL,
  recovery_candidate_since TEXT NULL,
  last_observed_at TEXT NULL,
  last_verified_at TEXT NULL,
  blocked_reason TEXT NOT NULL DEFAULT '',
  warnings TEXT NOT NULL DEFAULT '',
  activated_at TEXT NULL,
  last_checked_at TEXT NULL,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_aux_scheduler_rules_enabled
  ON aux_scheduler_rules(enabled, id);
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
	if err := s.migrateSubscriptionResetEntitlements(ctx); err != nil {
		return err
	}
	if err := s.freezeLegacySubscriptionResetEligibility(ctx); err != nil {
		return err
	}
	if err := s.supersedeLegacySubscriptionResetBackfill(ctx); err != nil {
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
	if err := s.ensureAuxSchedulerLaneColumns(ctx); err != nil {
		return err
	}
	if err := s.migrateAuxSchedulerLegacyLanes(ctx); err != nil {
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
	if err := s.ensureColumns(ctx, "redeem_access_requests", map[string]string{
		"reset_count": "INTEGER NOT NULL DEFAULT 0 CHECK (reset_count >= 0)",
	}); err != nil {
		return err
	}
	if err := s.ensureColumns(ctx, "subscription_reset_periods", map[string]string{
		"legacy_ignored":       "INTEGER NOT NULL DEFAULT 0",
		"legacy_ignored_at":    "TEXT NULL",
		"legacy_ignore_reason": "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	return s.ensureColumns(ctx, "subscription_reset_backfill_runs", map[string]string{
		"retry_count":   "INTEGER NOT NULL DEFAULT 0 CHECK (retry_count >= 0)",
		"last_error_at": "TEXT NULL",
	})
}

func (s *Store) migrateSubscriptionResetEntitlements(ctx context.Context) error {
	attemptColumns, err := s.tableColumns(ctx, "subscription_reset_attempts")
	if err != nil {
		return err
	}
	period, hasPeriod := attemptColumns["period_id"]
	_, hasEntitlementType := attemptColumns["entitlement_type"]
	_, hasEntitlementID := attemptColumns["entitlement_id"]
	if !hasPeriod || !hasEntitlementType || !hasEntitlementID || period.NotNull {
		if err := s.rebuildSubscriptionResetAttempts(ctx, hasEntitlementType && hasEntitlementID); err != nil {
			return err
		}
	}

	var backfillSQL string
	if err := s.DB.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'subscription_reset_backfill_runs'`).Scan(&backfillSQL); err != nil {
		return err
	}
	backfillColumns, err := s.tableColumns(ctx, "subscription_reset_backfill_runs")
	if err != nil {
		return err
	}
	_, hasSupersededAt := backfillColumns["superseded_at"]
	_, hasSupersededReason := backfillColumns["superseded_reason"]
	if !hasSupersededAt || !hasSupersededReason || !strings.Contains(backfillSQL, "'superseded'") {
		if err := s.rebuildSubscriptionResetBackfillRuns(ctx); err != nil {
			return err
		}
	}
	if _, err := s.DB.ExecContext(ctx, `DROP INDEX IF EXISTS idx_subscription_reset_periods_one_active`); err != nil {
		return err
	}
	if _, err := s.DB.ExecContext(ctx, `CREATE UNIQUE INDEX idx_subscription_reset_periods_one_active
  ON subscription_reset_periods(upstream_user_id, sub2api_group_id)
  WHERE status = 'active' AND legacy_ignored = 0`); err != nil {
		return err
	}
	return nil
}

type sqliteColumnInfo struct {
	NotNull bool
}

func (s *Store) tableColumns(ctx context.Context, table string) (map[string]sqliteColumnInfo, error) {
	rows, err := s.DB.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]sqliteColumnInfo{}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, typeName string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		out[name] = sqliteColumnInfo{NotNull: notNull != 0}
	}
	return out, rows.Err()
}

func (s *Store) rebuildSubscriptionResetAttempts(ctx context.Context, alreadyHasEntitlement bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, statement := range []string{
		`DROP INDEX IF EXISTS idx_subscription_reset_attempts_blocking`,
		`DROP INDEX IF EXISTS idx_subscription_reset_attempts_status`,
		`ALTER TABLE subscription_reset_attempts RENAME TO subscription_reset_attempts_legacy`,
		`CREATE TABLE subscription_reset_attempts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  request_id TEXT NOT NULL UNIQUE,
  period_id INTEGER NULL,
  entitlement_type TEXT NOT NULL CHECK (entitlement_type IN ('base_period', 'bonus_grant')),
  entitlement_id INTEGER NOT NULL,
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
)`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	entitlementSelect := `'base_period', period_id`
	if alreadyHasEntitlement {
		entitlementSelect = `entitlement_type, entitlement_id`
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO subscription_reset_attempts (
  id, request_id, period_id, entitlement_type, entitlement_id, upstream_user_id, upstream_subscription_id,
  reset_daily, reset_weekly, reset_monthly, status, before_snapshot_json, after_snapshot_json,
  upstream_status, response_status, response_reason, error_message, resolution, reserved_at,
  completed_at, confirmed_at, confirmed_by_user_id, created_at, updated_at
)
SELECT id, request_id, period_id, `+entitlementSelect+`, upstream_user_id, upstream_subscription_id,
       reset_daily, reset_weekly, reset_monthly, status, before_snapshot_json, after_snapshot_json,
       upstream_status, response_status, response_reason, error_message, resolution, reserved_at,
       completed_at, confirmed_at, confirmed_by_user_id, created_at, updated_at
FROM subscription_reset_attempts_legacy
`); err != nil {
		return err
	}
	duplicateBlocking := `status IN ('reserved', 'uncertain') AND EXISTS (
  SELECT 1 FROM subscription_reset_attempts earlier
  WHERE earlier.upstream_subscription_id = subscription_reset_attempts.upstream_subscription_id
    AND earlier.status IN ('reserved', 'uncertain')
    AND (earlier.reserved_at < subscription_reset_attempts.reserved_at
      OR (earlier.reserved_at = subscription_reset_attempts.reserved_at AND earlier.id < subscription_reset_attempts.id))
)`
	if _, err := tx.ExecContext(ctx, `
UPDATE subscription_reset_attempts
SET status = 'succeeded', resolution = 'consumed', response_status = 200,
    response_reason = 'migration_duplicate_blocking_conservatively_consumed',
    error_message = 'duplicate blocking operation conservatively consumed during entitlement migration',
    completed_at = COALESCE(completed_at, updated_at),
    confirmed_at = COALESCE(confirmed_at, updated_at)
WHERE `+duplicateBlocking); err != nil {
		return err
	}
	for _, statement := range []string{
		`DROP TABLE subscription_reset_attempts_legacy`,
		`CREATE UNIQUE INDEX idx_subscription_reset_attempts_blocking ON subscription_reset_attempts(upstream_subscription_id) WHERE status IN ('reserved', 'uncertain')`,
		`CREATE INDEX idx_subscription_reset_attempts_status ON subscription_reset_attempts(status, reserved_at)`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) rebuildSubscriptionResetBackfillRuns(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, statement := range []string{
		`ALTER TABLE subscription_reset_backfill_runs RENAME TO subscription_reset_backfill_runs_legacy`,
		`CREATE TABLE subscription_reset_backfill_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  tier_id INTEGER NOT NULL UNIQUE,
  reset_limit INTEGER NOT NULL CHECK (reset_limit > 0),
  status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'superseded')),
  total_records INTEGER NOT NULL DEFAULT 0,
  processed_records INTEGER NOT NULL DEFAULT 0,
  granted_records INTEGER NOT NULL DEFAULT 0,
  error_message TEXT NOT NULL DEFAULT '',
  retry_count INTEGER NOT NULL DEFAULT 0 CHECK (retry_count >= 0),
  last_error_at TEXT NULL,
  triggered_at TEXT NOT NULL,
  started_at TEXT NULL,
  completed_at TEXT NULL,
  superseded_at TEXT NULL,
  superseded_reason TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
)`,
		`INSERT INTO subscription_reset_backfill_runs (
  id, tier_id, reset_limit, status, total_records, processed_records, granted_records,
  error_message, retry_count, last_error_at, triggered_at, started_at, completed_at, updated_at
)
SELECT id, tier_id, reset_limit, status, total_records, processed_records, granted_records,
       error_message, retry_count, last_error_at, triggered_at, started_at, completed_at, updated_at
FROM subscription_reset_backfill_runs_legacy`,
		`DROP TABLE subscription_reset_backfill_runs_legacy`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) supersedeLegacySubscriptionResetBackfill(ctx context.Context) error {
	now := s.NowUTC().Format(timeLayout)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	const reason = "legacy automatic reset backfill disabled; use bonus grants"
	if _, err := tx.ExecContext(ctx, `
UPDATE subscription_reset_periods
SET legacy_ignored = 1, legacy_ignored_at = ?, legacy_ignore_reason = ?, updated_at = ?
WHERE legacy_ignored = 0
  AND reset_limit = 0
  AND legacy_reset_backfilled = 0
  AND EXISTS (
    SELECT 1 FROM subscription_reset_backfill_runs backfill
    WHERE backfill.tier_id = subscription_reset_periods.tier_id
      AND subscription_reset_periods.fulfilled_at <= backfill.triggered_at
  )
`, now, reason, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE subscription_reset_backfill_runs
SET status = 'superseded', superseded_at = COALESCE(superseded_at, ?),
    superseded_reason = CASE WHEN superseded_reason = '' THEN ? ELSE superseded_reason END,
    completed_at = COALESCE(completed_at, ?), updated_at = ?
WHERE status IN ('pending', 'running', 'failed')
`, now, reason, now, now); err != nil {
		return err
	}
	return tx.Commit()
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

func (s *Store) ensureAuxSchedulerLaneColumns(ctx context.Context) error {
	return s.ensureColumns(ctx, "aux_scheduler_rules", map[string]string{
		"model_names_json":           "TEXT NOT NULL DEFAULT '[]'",
		"lanes_json":                 "TEXT NOT NULL DEFAULT '[]'",
		"maximum_auto_lane":          "INTEGER NOT NULL DEFAULT 2",
		"migration_status":           "TEXT NOT NULL DEFAULT ''",
		"migration_source_json":      "TEXT NOT NULL DEFAULT ''",
		"expected_open_through_lane": "INTEGER NOT NULL DEFAULT 1",
		"observed_open_through_lane": "INTEGER NOT NULL DEFAULT 1",
		"verified_open_through_lane": "INTEGER NOT NULL DEFAULT 1",
		"target_open_through_lane":   "INTEGER NOT NULL DEFAULT 1",
		"transition_status":          "TEXT NOT NULL DEFAULT 'stable'",
		"transition_generation":      "INTEGER NOT NULL DEFAULT 0",
		"upgrade_evidence_json":      "TEXT NOT NULL DEFAULT '{}'",
		"missing_models_json":        "TEXT NOT NULL DEFAULT '[]'",
		"recovery_candidate_lane":    "INTEGER NULL",
		"recovery_candidate_since":   "TEXT NULL",
		"last_observed_at":           "TEXT NULL",
		"last_verified_at":           "TEXT NULL",
		"blocked_reason":             "TEXT NOT NULL DEFAULT ''",
		"warnings":                   "TEXT NOT NULL DEFAULT ''",
	})
}

func (s *Store) migrateAuxSchedulerLegacyLanes(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `
SELECT id, state, activated_at, primary_account_ids_json, backup_account_ids_json
FROM aux_scheduler_rules
WHERE lanes_json = '[]'
`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type legacyRule struct {
		id          int64
		state       string
		activatedAt sql.NullString
		primaryJSON string
		backupJSON  string
	}
	items := make([]legacyRule, 0)
	for rows.Next() {
		var item legacyRule
		if err := rows.Scan(&item.id, &item.state, &item.activatedAt, &item.primaryJSON, &item.backupJSON); err != nil {
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
		var primaryIDs []int64
		var backupIDs []int64
		if err := json.Unmarshal([]byte(item.primaryJSON), &primaryIDs); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(item.backupJSON), &backupIDs); err != nil {
			return err
		}
		lanesJSON, err := json.Marshal([][]int64{primaryIDs, backupIDs})
		if err != nil {
			return err
		}
		source := map[string]any{
			"legacy_state":               item.state,
			"legacy_primary_account_ids": primaryIDs,
			"legacy_backup_account_ids":  backupIDs,
		}
		if item.activatedAt.Valid {
			source["legacy_activated_at"] = item.activatedAt.String
		}
		sourceJSON, err := json.Marshal(source)
		if err != nil {
			return err
		}
		if _, err := s.DB.ExecContext(ctx, `
UPDATE aux_scheduler_rules
SET model_names_json = '[]',
    lanes_json = ?,
    maximum_auto_lane = 2,
    migration_status = 'needs_migration',
		migration_source_json = ?,
	    state = 'idle',
	    activated_at = NULL,
	    expected_open_through_lane = 1,
    observed_open_through_lane = 1,
    verified_open_through_lane = 1,
    target_open_through_lane = 1,
    transition_status = 'stable',
    transition_generation = 0,
    upgrade_evidence_json = '{}',
    missing_models_json = '[]',
    recovery_candidate_lane = NULL,
    recovery_candidate_since = NULL,
    last_observed_at = NULL,
    last_verified_at = NULL,
    blocked_reason = '',
    warnings = ''
WHERE id = ? AND lanes_json = '[]'
`, string(lanesJSON), string(sourceJSON), item.id); err != nil {
			return err
		}
	}
	return nil
}

const timeLayout = "2006-01-02T15:04:05.000000000Z07:00"
