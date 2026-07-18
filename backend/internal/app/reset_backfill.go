package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"sub2api-giftcode/backend/internal/models"
)

const legacyResetMigrationVersion = 1

type legacyResetBackfillRun struct {
	ID          int64
	TierID      int64
	ResetLimit  int
	TriggeredAt time.Time
}

func (s *Service) processLegacyResetBackfillRuns(
	ctx context.Context,
	now time.Time,
	confirmedGroups map[subscriptionResetBoundaryKey]bool,
	activeGroups map[subscriptionResetBoundaryKey]bool,
) error {
	runs, err := s.listPendingLegacyResetBackfillRuns(ctx)
	if err != nil {
		return err
	}
	var out error
	for _, run := range runs {
		if err := s.processLegacyResetBackfillRun(ctx, run, now, confirmedGroups, activeGroups); err != nil {
			_ = s.failLegacyResetBackfillRun(ctx, run.ID, err.Error(), now)
			out = errors.Join(out, fmt.Errorf("legacy reset backfill tier %d: %w", run.TierID, err))
		}
	}
	return out
}

func (s *Service) listPendingLegacyResetBackfillRuns(ctx context.Context) ([]legacyResetBackfillRun, error) {
	rows, err := s.db().QueryContext(ctx, `
SELECT id, tier_id, reset_limit, triggered_at
FROM subscription_reset_backfill_runs
WHERE status IN ('pending', 'running', 'failed')
ORDER BY id ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []legacyResetBackfillRun
	for rows.Next() {
		var run legacyResetBackfillRun
		var triggeredAt string
		if err := rows.Scan(&run.ID, &run.TierID, &run.ResetLimit, &triggeredAt); err != nil {
			return nil, err
		}
		run.TriggeredAt, err = parseNonNullTime(triggeredAt)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (s *Service) processLegacyResetBackfillRun(
	ctx context.Context,
	run legacyResetBackfillRun,
	now time.Time,
	confirmedGroups map[subscriptionResetBoundaryKey]bool,
	activeGroups map[subscriptionResetBoundaryKey]bool,
) error {
	if run.ID <= 0 || run.TierID <= 0 || run.ResetLimit <= 0 {
		return ErrBadRequest
	}
	if _, err := s.db().ExecContext(ctx, `
UPDATE subscription_reset_backfill_runs
SET status = 'running', started_at = COALESCE(started_at, ?), error_message = '', updated_at = ?
WHERE id = ? AND status IN ('pending', 'running', 'failed')
`, formatTime(now), formatTime(now), run.ID); err != nil {
		return err
	}

	var groupID int64
	if err := s.db().QueryRowContext(ctx, `
SELECT sub2api_group_id
FROM redeem_tiers
WHERE id = ? AND code_type = 'subscription' AND sub2api_group_id IS NOT NULL
`, run.TierID).Scan(&groupID); err != nil {
		return err
	}
	if _, err := s.db().ExecContext(ctx, `
UPDATE subscription_reset_periods
SET inferred_from_legacy = 1, migration_version = ?, updated_at = ?
WHERE sub2api_group_id = ? AND fulfilled_at <= ?
`, legacyResetMigrationVersion, formatTime(now), groupID, formatTime(run.TriggeredAt)); err != nil {
		return err
	}

	periods, err := s.listLegacyResetBackfillTargetPeriods(ctx, run)
	if err != nil {
		return err
	}
	for _, period := range periods {
		if period.LegacyResetBackfilled {
			continue
		}
		key := subscriptionResetBoundaryKey{UserID: period.UpstreamUserID, GroupID: period.Sub2APIGroupID}
		if !confirmedGroups[key] {
			continue
		}
		resolved, resolveErr := func() (bool, error) {
			unlock := s.lockSubscriptionResetBoundary(key.UserID, key.GroupID)
			defer unlock()
			fresh, err := s.getSubscriptionResetPeriodByID(ctx, period.ID)
			if err != nil {
				return false, err
			}
			if fresh.LegacyResetBackfilled {
				return true, nil
			}
			grant := false
			if activeGroups[key] {
				if fresh.PeriodStart == nil || fresh.PeriodEnd == nil {
					return false, nil
				}
				grant = fresh.PeriodEnd.After(now) && (fresh.Status == "active" || fresh.Status == "scheduled")
			}
			return true, s.resolveLegacyResetBackfillPeriod(ctx, fresh.ID, run.ResetLimit, grant, now)
		}()
		if resolveErr != nil {
			return resolveErr
		}
		if !resolved {
			continue
		}
	}

	total, processed, granted, err := s.legacyResetBackfillRunCounts(ctx, run)
	if err != nil {
		return err
	}
	if processed < total {
		message := fmt.Sprintf("%d historical periods await authoritative reconciliation", total-processed)
		_, err = s.db().ExecContext(ctx, `
UPDATE subscription_reset_backfill_runs
SET status = 'failed', total_records = ?, processed_records = ?, granted_records = ?,
    error_message = ?, completed_at = NULL, updated_at = ?
WHERE id = ?
`, total, processed, granted, message, formatTime(now), run.ID)
		return err
	}
	_, err = s.db().ExecContext(ctx, `
UPDATE subscription_reset_backfill_runs
SET status = 'succeeded', total_records = ?, processed_records = ?, granted_records = ?,
    error_message = '', completed_at = ?, updated_at = ?
WHERE id = ?
`, total, processed, granted, formatTime(now), formatTime(now), run.ID)
	return err
}

func (s *Service) getSubscriptionResetPeriodByID(ctx context.Context, periodID int64) (*models.SubscriptionResetPeriod, error) {
	row := s.db().QueryRowContext(ctx, subscriptionResetPeriodSelectSQL()+` WHERE id = ?`, periodID)
	return scanSubscriptionResetPeriod(row)
}

func (s *Service) listLegacyResetBackfillTargetPeriods(ctx context.Context, run legacyResetBackfillRun) ([]models.SubscriptionResetPeriod, error) {
	rows, err := s.db().QueryContext(ctx, subscriptionResetPeriodSelectSQL()+`
WHERE tier_id = ?
  AND fulfilled_at <= ?
  AND (reset_limit = 0 OR legacy_reset_backfilled = 1)
ORDER BY fulfilled_at ASC, access_request_id ASC
`, run.TierID, formatTime(run.TriggeredAt))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.SubscriptionResetPeriod
	for rows.Next() {
		period, err := scanSubscriptionResetPeriod(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *period)
	}
	return out, rows.Err()
}

func (s *Service) resolveLegacyResetBackfillPeriod(ctx context.Context, periodID int64, resetLimit int, grant bool, now time.Time) error {
	if grant {
		_, err := s.db().ExecContext(ctx, `
UPDATE subscription_reset_periods
SET reset_limit = ?, legacy_reset_backfilled = 1, inferred_from_legacy = 1,
    migration_version = ?, updated_at = ?
WHERE id = ? AND legacy_reset_backfilled = 0 AND reset_limit = 0
`, resetLimit, legacyResetMigrationVersion, formatTime(now), periodID)
		return err
	}
	_, err := s.db().ExecContext(ctx, `
UPDATE subscription_reset_periods
SET legacy_reset_backfilled = 1, inferred_from_legacy = 1,
    migration_version = ?, updated_at = ?
WHERE id = ? AND legacy_reset_backfilled = 0 AND reset_limit = 0
`, legacyResetMigrationVersion, formatTime(now), periodID)
	return err
}

func (s *Service) legacyResetBackfillRunCounts(ctx context.Context, run legacyResetBackfillRun) (total, processed, granted int, err error) {
	err = s.db().QueryRowContext(ctx, `
SELECT
  COUNT(1),
  COALESCE(SUM(CASE WHEN legacy_reset_backfilled = 1 THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN legacy_reset_backfilled = 1 AND reset_limit = ? THEN 1 ELSE 0 END), 0)
FROM subscription_reset_periods
WHERE tier_id = ?
  AND fulfilled_at <= ?
  AND (reset_limit = 0 OR legacy_reset_backfilled = 1)
`, run.ResetLimit, run.TierID, formatTime(run.TriggeredAt)).Scan(&total, &processed, &granted)
	return
}

func (s *Service) failLegacyResetBackfillRun(ctx context.Context, runID int64, message string, now time.Time) error {
	_, err := s.db().ExecContext(ctx, `
UPDATE subscription_reset_backfill_runs
SET status = 'failed', error_message = ?, completed_at = NULL, updated_at = ?
WHERE id = ? AND status <> 'succeeded'
`, message, formatTime(now), runID)
	return err
}
