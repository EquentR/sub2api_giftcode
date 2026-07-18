package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"sub2api-giftcode/backend/internal/models"
	"sub2api-giftcode/backend/internal/sub2api"
)

const (
	subscriptionResetLastReconciliationAtKey = "subscription_reset_last_reconciliation_at"
	staleResetReservationAge                 = 2 * time.Minute
	ambiguousDirectResetBoundaryPrefix       = "ambiguous_direct_charge_boundary:"
)

type deliveredSubscriptionResetCandidate struct {
	AccessRequestID int64
	UpstreamUserID  int64
	TierID          int64
	GroupID         int64
	ValidityDays    int
	ResetLimit      int
	FulfilledAt     time.Time
}

type subscriptionResetBoundaryKey struct {
	UserID  int64
	GroupID int64
}

type subscriptionResetBoundaryLock struct {
	mu   sync.Mutex
	refs int
}

func (s *Service) lockSubscriptionResetBoundary(userID, groupID int64) func() {
	key := subscriptionResetBoundaryKey{UserID: userID, GroupID: groupID}
	s.resetBoundaryLocksMu.Lock()
	if s.resetBoundaryLocks == nil {
		s.resetBoundaryLocks = make(map[subscriptionResetBoundaryKey]*subscriptionResetBoundaryLock)
	}
	lock := s.resetBoundaryLocks[key]
	if lock == nil {
		lock = &subscriptionResetBoundaryLock{}
		s.resetBoundaryLocks[key] = lock
	}
	lock.refs++
	s.resetBoundaryLocksMu.Unlock()

	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		s.resetBoundaryLocksMu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(s.resetBoundaryLocks, key)
		}
		s.resetBoundaryLocksMu.Unlock()
	}
}

func matchingResetSubscription(subscriptions []sub2api.Subscription, userID, groupID int64) *sub2api.Subscription {
	for i := range subscriptions {
		if subscriptions[i].UserID == userID && subscriptions[i].GroupID == groupID && subscriptions[i].Status == "active" {
			return &subscriptions[i]
		}
	}
	return nil
}

func (s *Service) ReconcileSubscriptionResetPeriods(ctx context.Context) error {
	if s == nil || s.db() == nil {
		return errors.New("subscription reset service is not configured")
	}
	s.resetMu.Lock()
	defer s.resetMu.Unlock()

	now := s.now()
	if err := s.markStaleResetReservationsUncertain(ctx, now); err != nil {
		return err
	}
	repairErr := s.repairMissingSubscriptionResetPeriods(ctx, now)
	if err := s.requireUpstreamClient(); err != nil {
		return errors.Join(repairErr, err)
	}
	bonusErr := s.ReconcileSubscriptionResetBonusGrants(ctx)
	periods, err := s.listSubscriptionResetPeriods(ctx)
	if err != nil {
		return errors.Join(repairErr, bonusErr, err)
	}

	byGroup := make(map[subscriptionResetBoundaryKey][]*models.SubscriptionResetPeriod)
	for i := range periods {
		period := &periods[i]
		key := subscriptionResetBoundaryKey{UserID: period.UpstreamUserID, GroupID: period.Sub2APIGroupID}
		byGroup[key] = append(byGroup[key], period)
	}
	var reconcileErr error
	for key, groupPeriods := range byGroup {
		groupErr := func() error {
			unlock := s.lockSubscriptionResetBoundary(key.UserID, key.GroupID)
			defer unlock()
			subscriptions, listErr := s.upstream.ListActiveUserSubscriptions(ctx, key.UserID)
			if listErr != nil {
				_ = s.recordSubscriptionResetPeriodGroupError(ctx, key.UserID, key.GroupID, listErr.Error(), now)
				return listErr
			}
			subscription := matchingResetSubscription(subscriptions, key.UserID, key.GroupID)
			if err := s.reconcileSubscriptionResetPeriodGroup(ctx, groupPeriods, subscription, now); err != nil {
				return err
			}
			return s.reconcileUncertainSubscriptionResetAttempts(ctx, subscription, now)
		}()
		if groupErr != nil {
			reconcileErr = errors.Join(reconcileErr, fmt.Errorf("user %d group %d: %w", key.UserID, key.GroupID, groupErr))
		}
	}
	metadataErr := s.setSyncState(ctx, subscriptionResetLastReconciliationAtKey, formatTime(now), now)
	return errors.Join(repairErr, bonusErr, reconcileErr, metadataErr)
}

func (s *Service) RunSubscriptionResetLoop(ctx context.Context, interval time.Duration) {
	run := func() {
		if err := s.ReconcileSubscriptionResetPeriods(ctx); err != nil && ctx.Err() == nil {
			log.Printf("reconcile subscription reset periods failed: %v", err)
		}
	}
	run()
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		case <-s.resetWake:
			run()
		}
	}
}

func (s *Service) WakeSubscriptionResetReconcile() {
	if s == nil || s.resetWake == nil {
		return
	}
	select {
	case s.resetWake <- struct{}{}:
	default:
	}
}

func (s *Service) markStaleResetReservationsUncertain(ctx context.Context, now time.Time) error {
	cutoff := now.Add(-staleResetReservationAge)
	rows, err := s.db().QueryContext(ctx, `
SELECT a.id, a.upstream_user_id, COALESCE(p.sub2api_group_id, grant.sub2api_group_id)
FROM subscription_reset_attempts a
LEFT JOIN subscription_reset_periods p ON a.entitlement_type = 'base_period' AND p.id = a.entitlement_id
LEFT JOIN subscription_reset_bonus_grants grant ON a.entitlement_type = 'bonus_grant' AND grant.id = a.entitlement_id
WHERE a.status = 'reserved' AND a.reserved_at < ?
ORDER BY a.id
`, formatTime(cutoff))
	if err != nil {
		return err
	}
	type staleReservation struct{ id, userID, groupID int64 }
	var stale []staleReservation
	for rows.Next() {
		var item staleReservation
		if err := rows.Scan(&item.id, &item.userID, &item.groupID); err != nil {
			_ = rows.Close()
			return err
		}
		stale = append(stale, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range stale {
		unlock := s.lockSubscriptionResetBoundary(item.userID, item.groupID)
		_, updateErr := s.db().ExecContext(ctx, `
UPDATE subscription_reset_attempts
SET status = 'uncertain', response_reason = 'reservation_timeout', completed_at = ?, updated_at = ?
WHERE id = ? AND status = 'reserved' AND reserved_at < ?
`, formatTime(now), formatTime(now), item.id, formatTime(cutoff))
		unlock()
		if updateErr != nil {
			return updateErr
		}
	}
	return nil
}

func (s *Service) repairMissingSubscriptionResetPeriods(ctx context.Context, now time.Time) error {
	rows, err := s.db().QueryContext(ctx, `
SELECT
  ar.id, ar.requestor_upstream_user_id, ar.tier_id, ar.sub2api_group_id,
  ar.validity_days, ar.reset_count,
  CASE
    WHEN ar.fulfilled_via = 'direct_charge' AND ar.fulfillment_result = 'direct_charge_succeeded'
      THEN COALESCE(ar.consumed_at, ar.updated_at)
    ELSE rc.used_at
  END AS fulfilled_at
FROM redeem_access_requests ar
LEFT JOIN redeem_requests rr ON rr.access_request_id = ar.id
LEFT JOIN redeem_codes rc ON rc.request_id = rr.id
WHERE ar.code_type = 'subscription'
  AND ar.status = 'consumed'
  AND ar.sub2api_group_id IS NOT NULL
  AND ar.validity_days > 0
  AND NOT EXISTS (
    SELECT 1 FROM subscription_reset_periods p WHERE p.access_request_id = ar.id
  )
  AND (
    (ar.fulfilled_via = 'direct_charge' AND ar.fulfillment_result = 'direct_charge_succeeded')
    OR (
      rc.status = 'used'
      AND rc.used_at IS NOT NULL
      AND rc.used_by_upstream_user_id = ar.requestor_upstream_user_id
    )
  )
ORDER BY fulfilled_at ASC, ar.id ASC
`)
	if err != nil {
		return err
	}
	var candidates []deliveredSubscriptionResetCandidate
	for rows.Next() {
		var candidate deliveredSubscriptionResetCandidate
		var fulfilledAt string
		if err := rows.Scan(
			&candidate.AccessRequestID,
			&candidate.UpstreamUserID,
			&candidate.TierID,
			&candidate.GroupID,
			&candidate.ValidityDays,
			&candidate.ResetLimit,
			&fulfilledAt,
		); err != nil {
			_ = rows.Close()
			return err
		}
		candidate.FulfilledAt, err = parseNonNullTime(fulfilledAt)
		if err != nil {
			_ = rows.Close()
			return err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, candidate := range candidates {
		if _, err := s.ensureSubscriptionResetPeriod(ctx, candidate, now); err != nil {
			return fmt.Errorf("repair access request %d reset period: %w", candidate.AccessRequestID, err)
		}
	}
	return nil
}

func (s *Service) ensureSubscriptionResetPeriod(ctx context.Context, candidate deliveredSubscriptionResetCandidate, now time.Time) (int64, error) {
	if candidate.AccessRequestID <= 0 || candidate.UpstreamUserID <= 0 || candidate.GroupID <= 0 || candidate.ValidityDays <= 0 || candidate.ResetLimit < 0 {
		return 0, ErrBadRequest
	}
	_, err := s.db().ExecContext(ctx, `
INSERT INTO subscription_reset_periods (
  access_request_id, upstream_user_id, tier_id, sub2api_group_id, validity_days,
  reset_limit, reset_used, fulfilled_at, fulfillment_order, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?, 'pending_binding', ?, ?)
ON CONFLICT(access_request_id) DO NOTHING
`,
		candidate.AccessRequestID,
		candidate.UpstreamUserID,
		candidate.TierID,
		candidate.GroupID,
		candidate.ValidityDays,
		candidate.ResetLimit,
		formatTime(candidate.FulfilledAt),
		candidate.AccessRequestID,
		formatTime(now),
		formatTime(now),
	)
	if err != nil {
		return 0, err
	}
	var id int64
	if err := s.db().QueryRowContext(ctx, `SELECT id FROM subscription_reset_periods WHERE access_request_id = ?`, candidate.AccessRequestID).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// recordDirectSubscriptionResetPeriodLocked requires resetMu to cover the
// direct-charge before/after observations and this write as one ordered unit.
func (s *Service) recordDirectSubscriptionResetPeriodLocked(
	ctx context.Context,
	req *models.AccessRequest,
	beforeKnown bool,
	before *sub2api.Subscription,
	after *sub2api.Subscription,
	fulfilledAt time.Time,
) error {
	if req == nil || req.Sub2APIGroupID == nil {
		return ErrBadRequest
	}
	recordedAt := s.now()
	periodID, err := s.ensureSubscriptionResetPeriod(ctx, deliveredSubscriptionResetCandidate{
		AccessRequestID: req.ID,
		UpstreamUserID:  req.RequestorUpstreamUserID,
		TierID:          req.TierID,
		GroupID:         *req.Sub2APIGroupID,
		ValidityDays:    req.ValidityDays,
		ResetLimit:      req.ResetCount,
		FulfilledAt:     fulfilledAt,
	}, recordedAt)
	if err != nil {
		return err
	}
	if after != nil {
		_, _ = s.db().ExecContext(ctx, `
UPDATE subscription_reset_periods
SET upstream_subscription_id = ?, last_synced_at = ?, updated_at = ?
WHERE id = ?
`, after.ID, formatTime(recordedAt), formatTime(recordedAt), periodID)
	}
	if !beforeKnown || after == nil {
		return nil
	}
	var start, end time.Time
	if before != nil {
		start = before.ExpiresAt.UTC()
		end = start.Add(time.Duration(req.ValidityDays) * 24 * time.Hour)
		if !after.ExpiresAt.Equal(end) {
			message := fmt.Sprintf("%s expected %s, observed %s", ambiguousDirectResetBoundaryPrefix, formatTime(end), formatTime(after.ExpiresAt))
			return s.recordSubscriptionResetPeriodDiagnostic(ctx, periodID, message, recordedAt)
		}
	} else {
		start = after.StartsAt.UTC()
		end = after.ExpiresAt.UTC()
		expectedEnd := start.Add(time.Duration(req.ValidityDays) * 24 * time.Hour)
		if !end.Equal(expectedEnd) {
			message := fmt.Sprintf("%s expected duration through %s, observed %s", ambiguousDirectResetBoundaryPrefix, formatTime(expectedEnd), formatTime(end))
			return s.recordSubscriptionResetPeriodDiagnostic(ctx, periodID, message, recordedAt)
		}
	}
	if start.IsZero() || !start.Before(end) {
		return s.recordSubscriptionResetPeriodDiagnostic(ctx, periodID, ambiguousDirectResetBoundaryPrefix+" invalid direct-charge subscription boundary", recordedAt)
	}
	if err := s.assignSubscriptionResetPeriodBoundary(ctx, periodID, after.ID, start, end); err != nil {
		return err
	}
	return s.updateSubscriptionResetPeriodStatuses(ctx, req.RequestorUpstreamUserID, *req.Sub2APIGroupID, after, recordedAt)
}

func (s *Service) reconcileSubscriptionResetPeriodGroup(ctx context.Context, periods []*models.SubscriptionResetPeriod, subscription *sub2api.Subscription, now time.Time) error {
	if len(periods) == 0 {
		return nil
	}
	sort.Slice(periods, func(i, j int) bool {
		if periods[i].FulfilledAt.Equal(periods[j].FulfilledAt) {
			return periods[i].AccessRequestID < periods[j].AccessRequestID
		}
		return periods[i].FulfilledAt.Before(periods[j].FulfilledAt)
	})
	userID := periods[0].UpstreamUserID
	groupID := periods[0].Sub2APIGroupID
	if subscription == nil {
		return s.updateSubscriptionResetPeriodStatuses(ctx, userID, groupID, nil, now)
	}
	for _, period := range periods {
		if period.UpstreamSubscriptionID == nil {
			_, err := s.db().ExecContext(ctx, `
UPDATE subscription_reset_periods
SET upstream_subscription_id = ?, last_synced_at = ?, updated_at = ?
WHERE id = ? AND upstream_subscription_id IS NULL
`, subscription.ID, formatTime(now), formatTime(now), period.ID)
			if err != nil {
				return err
			}
			id := subscription.ID
			period.UpstreamSubscriptionID = &id
		}
	}
	for _, period := range periods {
		if (period.PeriodStart == nil || period.PeriodEnd == nil) && strings.HasPrefix(period.LastError, ambiguousDirectResetBoundaryPrefix) {
			return s.updateSubscriptionResetPeriodStatuses(ctx, userID, groupID, subscription, now)
		}
	}
	for blockStart := 0; blockStart < len(periods); {
		if periods[blockStart].PeriodStart != nil && periods[blockStart].PeriodEnd != nil {
			blockStart++
			continue
		}
		blockEnd := blockStart
		totalDuration := time.Duration(0)
		for blockEnd < len(periods) && (periods[blockEnd].PeriodStart == nil || periods[blockEnd].PeriodEnd == nil) {
			totalDuration += time.Duration(periods[blockEnd].ValidityDays) * 24 * time.Hour
			blockEnd++
		}
		anchorEnd := subscription.ExpiresAt.UTC()
		if blockEnd < len(periods) {
			anchorEnd = periods[blockEnd].PeriodStart.UTC()
		}
		cursor := anchorEnd.Add(-totalDuration)
		var lowerBound *time.Time
		if blockStart > 0 && periods[blockStart-1].PeriodEnd != nil {
			bound := periods[blockStart-1].PeriodEnd.UTC()
			lowerBound = &bound
		} else if !subscription.StartsAt.IsZero() {
			bound := subscription.StartsAt.UTC()
			lowerBound = &bound
		}
		if lowerBound != nil && cursor.Before(*lowerBound) {
			message := fmt.Sprintf("inferred reset period block overlaps established boundary ending at %s", formatTime(*lowerBound))
			for i := blockStart; i < blockEnd; i++ {
				_ = s.recordSubscriptionResetPeriodDiagnostic(ctx, periods[i].ID, message, now)
			}
			return errors.New(message)
		}
		for i := blockStart; i < blockEnd; i++ {
			period := periods[i]
			end := cursor.Add(time.Duration(period.ValidityDays) * 24 * time.Hour)
			if err := s.assignSubscriptionResetPeriodBoundary(ctx, period.ID, subscription.ID, cursor, end); err != nil {
				return err
			}
			periodStart := cursor
			periodEnd := end
			period.PeriodStart = &periodStart
			period.PeriodEnd = &periodEnd
			cursor = end
		}
		blockStart = blockEnd
	}
	return s.updateSubscriptionResetPeriodStatuses(ctx, userID, groupID, subscription, now)
}

func (s *Service) assignSubscriptionResetPeriodBoundary(ctx context.Context, periodID, subscriptionID int64, start, end time.Time) error {
	start = start.UTC()
	end = end.UTC()
	if periodID <= 0 || subscriptionID <= 0 || start.IsZero() || !start.Before(end) {
		return ErrBadRequest
	}
	var userID, groupID int64
	var currentStart, currentEnd sql.NullString
	if err := s.db().QueryRowContext(ctx, `
SELECT upstream_user_id, sub2api_group_id, period_start, period_end
FROM subscription_reset_periods WHERE id = ?
`, periodID).Scan(&userID, &groupID, &currentStart, &currentEnd); err != nil {
		return err
	}
	if currentStart.Valid || currentEnd.Valid {
		if currentStart.Valid && currentEnd.Valid && currentStart.String == formatTime(start) && currentEnd.String == formatTime(end) {
			return nil
		}
		return errors.New("subscription reset period boundary is already established")
	}
	var overlapCount int
	if err := s.db().QueryRowContext(ctx, `
SELECT COUNT(1)
FROM subscription_reset_periods
WHERE id <> ?
  AND legacy_ignored = 0
  AND upstream_user_id = ?
  AND sub2api_group_id = ?
  AND period_start IS NOT NULL
  AND period_end IS NOT NULL
  AND period_start < ?
  AND period_end > ?
`, periodID, userID, groupID, formatTime(end), formatTime(start)).Scan(&overlapCount); err != nil {
		return err
	}
	if overlapCount > 0 {
		message := fmt.Sprintf("inferred reset period overlap [%s, %s)", formatTime(start), formatTime(end))
		_ = s.recordSubscriptionResetPeriodDiagnostic(ctx, periodID, message, s.now())
		return errors.New(message)
	}
	result, err := s.db().ExecContext(ctx, `
UPDATE subscription_reset_periods
SET upstream_subscription_id = ?, period_start = ?, period_end = ?, last_error = '', updated_at = ?
WHERE id = ? AND period_start IS NULL AND period_end IS NULL
`, subscriptionID, formatTime(start), formatTime(end), formatTime(s.now()), periodID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errors.New("subscription reset period boundary was not assigned")
	}
	return nil
}

func (s *Service) updateSubscriptionResetPeriodStatuses(ctx context.Context, userID, groupID int64, subscription *sub2api.Subscription, now time.Time) error {
	periods, err := s.listSubscriptionResetPeriodsForGroup(ctx, userID, groupID)
	if err != nil {
		return err
	}
	for _, period := range periods {
		status := period.Status
		lastError := ""
		switch {
		case period.PeriodStart == nil || period.PeriodEnd == nil:
			status = "pending_binding"
			lastError = period.LastError
			if subscription == nil {
				lastError = "active upstream subscription not found"
			}
		case !now.Before(*period.PeriodEnd):
			status = "expired"
		case subscription == nil:
			status = "inactive"
			lastError = "active upstream subscription not found"
		case subscription.ExpiresAt.Before(*period.PeriodEnd):
			status = "inactive"
			lastError = "upstream subscription expires before local period"
		case now.Before(*period.PeriodStart):
			status = "scheduled"
		default:
			status = "active"
		}
		_, err := s.db().ExecContext(ctx, `
UPDATE subscription_reset_periods
SET status = ?, last_synced_at = ?, last_error = ?, updated_at = ?
WHERE id = ?
`, status, formatTime(now), lastError, formatTime(now), period.ID)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) recordSubscriptionResetPeriodDiagnostic(ctx context.Context, periodID int64, message string, now time.Time) error {
	_, err := s.db().ExecContext(ctx, `
UPDATE subscription_reset_periods
SET last_error = ?, last_synced_at = ?, updated_at = ?
WHERE id = ?
`, message, formatTime(now), formatTime(now), periodID)
	return err
}

func (s *Service) recordSubscriptionResetPeriodGroupError(ctx context.Context, userID, groupID int64, message string, now time.Time) error {
	_, err := s.db().ExecContext(ctx, `
UPDATE subscription_reset_periods
SET last_error = ?, last_synced_at = ?, updated_at = ?
WHERE upstream_user_id = ? AND sub2api_group_id = ? AND status <> 'expired'
  AND legacy_ignored = 0
`, message, formatTime(now), formatTime(now), userID, groupID)
	return err
}

func (s *Service) listSubscriptionResetPeriods(ctx context.Context) ([]models.SubscriptionResetPeriod, error) {
	rows, err := s.db().QueryContext(ctx, subscriptionResetPeriodSelectSQL()+` WHERE legacy_ignored = 0 ORDER BY upstream_user_id, sub2api_group_id, fulfilled_at, access_request_id`)
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

func (s *Service) listSubscriptionResetPeriodsForGroup(ctx context.Context, userID, groupID int64) ([]models.SubscriptionResetPeriod, error) {
	rows, err := s.db().QueryContext(ctx, subscriptionResetPeriodSelectSQL()+`
WHERE upstream_user_id = ? AND sub2api_group_id = ? AND legacy_ignored = 0
ORDER BY fulfilled_at, access_request_id
`, userID, groupID)
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

func subscriptionResetPeriodSelectSQL() string {
	return `
SELECT id, access_request_id, upstream_user_id, tier_id, sub2api_group_id, upstream_subscription_id,
       validity_days, reset_limit, reset_used, fulfilled_at, fulfillment_order, period_start, period_end,
       status, inferred_from_legacy, migration_version, legacy_reset_backfilled,
       legacy_ignored, legacy_ignored_at, legacy_ignore_reason, last_synced_at,
       last_error, created_at, updated_at
FROM subscription_reset_periods
`
}

func scanSubscriptionResetPeriod(scanner interface{ Scan(...any) error }) (*models.SubscriptionResetPeriod, error) {
	var out models.SubscriptionResetPeriod
	var subscriptionID sql.NullInt64
	var fulfilledAt, createdAt, updatedAt string
	var periodStart, periodEnd, legacyIgnoredAt, lastSyncedAt sql.NullString
	var inferredFromLegacy, legacyResetBackfilled, legacyIgnored int
	if err := scanner.Scan(
		&out.ID,
		&out.AccessRequestID,
		&out.UpstreamUserID,
		&out.TierID,
		&out.Sub2APIGroupID,
		&subscriptionID,
		&out.ValidityDays,
		&out.ResetLimit,
		&out.ResetUsed,
		&fulfilledAt,
		&out.FulfillmentOrder,
		&periodStart,
		&periodEnd,
		&out.Status,
		&inferredFromLegacy,
		&out.MigrationVersion,
		&legacyResetBackfilled,
		&legacyIgnored,
		&legacyIgnoredAt,
		&out.LegacyIgnoreReason,
		&lastSyncedAt,
		&out.LastError,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}
	var err error
	out.UpstreamSubscriptionID = parseNullableInt64(subscriptionID)
	out.InferredFromLegacy = inferredFromLegacy != 0
	out.LegacyResetBackfilled = legacyResetBackfilled != 0
	out.LegacyIgnored = legacyIgnored != 0
	if out.FulfilledAt, err = parseNonNullTime(fulfilledAt); err != nil {
		return nil, err
	}
	if out.PeriodStart, err = scanMaybeTime(periodStart); err != nil {
		return nil, err
	}
	if out.PeriodEnd, err = scanMaybeTime(periodEnd); err != nil {
		return nil, err
	}
	if out.LastSyncedAt, err = scanMaybeTime(lastSyncedAt); err != nil {
		return nil, err
	}
	if out.LegacyIgnoredAt, err = scanMaybeTime(legacyIgnoredAt); err != nil {
		return nil, err
	}
	if out.CreatedAt, err = parseNonNullTime(createdAt); err != nil {
		return nil, err
	}
	if out.UpdatedAt, err = parseNonNullTime(updatedAt); err != nil {
		return nil, err
	}
	return &out, nil
}
