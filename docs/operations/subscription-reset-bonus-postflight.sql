.headers on
.mode column

PRAGMA quick_check;
PRAGMA foreign_key_check;

SELECT name
FROM sqlite_master
WHERE type = 'table'
  AND name IN (
    'subscription_reset_bonus_batches',
    'subscription_reset_bonus_batch_details',
    'subscription_reset_bonus_grants',
    'subscription_extension_events'
  )
ORDER BY name;

SELECT 'old_backfill_not_superseded' AS check_name, COUNT(*) AS failures
FROM subscription_reset_backfill_runs
WHERE status IN ('pending', 'running', 'failed')
UNION ALL
SELECT 'ignored_positive_period', COUNT(*)
FROM subscription_reset_periods
WHERE legacy_ignored = 1 AND reset_limit > 0
UNION ALL
SELECT 'ignored_already_granted_period', COUNT(*)
FROM subscription_reset_periods
WHERE legacy_ignored = 1 AND legacy_reset_backfilled = 1
UNION ALL
SELECT 'duplicate_blocking_attempt', COUNT(*)
FROM (
  SELECT upstream_subscription_id
  FROM subscription_reset_attempts
  WHERE status IN ('reserved', 'uncertain')
  GROUP BY upstream_subscription_id
  HAVING COUNT(*) > 1
)
UNION ALL
SELECT 'invalid_base_entitlement', COUNT(*)
FROM subscription_reset_attempts
WHERE entitlement_type = 'base_period'
  AND (period_id IS NULL OR entitlement_id <> period_id)
UNION ALL
SELECT 'invalid_bonus_entitlement', COUNT(*)
FROM subscription_reset_attempts
WHERE entitlement_type = 'bonus_grant'
  AND (period_id IS NOT NULL OR entitlement_id <= 0);

SELECT status, COUNT(*) AS runs
FROM subscription_reset_backfill_runs
GROUP BY status
ORDER BY status;

SELECT status, resolution, COUNT(*) AS events
FROM subscription_extension_events
GROUP BY status, resolution
ORDER BY status, resolution;

SELECT status, COUNT(*) AS grants, COALESCE(SUM(reset_limit - reset_used), 0) AS remaining_resets
FROM subscription_reset_bonus_grants
GROUP BY status
ORDER BY status;

SELECT COUNT(*) AS marketing_batches_created_by_migration
FROM subscription_reset_bonus_batches;
