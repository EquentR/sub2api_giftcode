.headers on
.mode column

PRAGMA quick_check;
PRAGMA foreign_key_check;

SELECT 'reset_periods_total' AS metric, COUNT(*) AS value
FROM subscription_reset_periods
UNION ALL
SELECT 'reset_periods_with_positive_limit', COUNT(*)
FROM subscription_reset_periods WHERE reset_limit > 0
UNION ALL
SELECT 'legacy_backfilled_periods', COUNT(*)
FROM subscription_reset_periods WHERE legacy_reset_backfilled = 1;

SELECT status, COUNT(*) AS runs
FROM subscription_reset_backfill_runs
GROUP BY status
ORDER BY status;

SELECT upstream_subscription_id, COUNT(*) AS blocking_operations
FROM subscription_reset_attempts
WHERE status IN ('reserved', 'uncertain')
GROUP BY upstream_subscription_id
HAVING COUNT(*) > 1;

SELECT id, tier_id, reset_limit, status, processed_records, granted_records,
       triggered_at, updated_at
FROM subscription_reset_backfill_runs
WHERE status IN ('pending', 'running', 'failed')
ORDER BY id;
