package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"sub2api-giftcode/backend/internal/models"
	"sub2api-giftcode/backend/internal/sub2api"
)

type subscriptionExtensionEventState struct {
	ID         int64
	Status     string
	Resolution string
}

func (s *Service) extendCompensatedSubscription(ctx context.Context, batchID int64, batchKey string, userID int64, subscription sub2api.Subscription, days int) (*sub2api.Subscription, error) {
	eventKey := compensationSubscriptionExtensionIdempotencyKey(batchKey, userID, subscription.ID)
	now := s.now()
	result, err := s.db().ExecContext(ctx, `
INSERT INTO subscription_extension_events (
  event_key, source_type, compensation_batch_id, upstream_user_id, sub2api_group_id,
  upstream_subscription_id, extension_days, before_expires_at, status, reserved_at,
  created_at, updated_at
) VALUES (?, 'compensation', ?, ?, ?, ?, ?, ?, 'reserved', ?, ?, ?)
ON CONFLICT(event_key) DO NOTHING
`, eventKey, batchID, userID, subscription.GroupID, subscription.ID, days,
		formatTime(subscription.ExpiresAt), formatTime(now), formatTime(now), formatTime(now))
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	var state subscriptionExtensionEventState
	if err := s.db().QueryRowContext(ctx, `SELECT id, status, resolution FROM subscription_extension_events WHERE event_key = ?`, eventKey).Scan(&state.ID, &state.Status, &state.Resolution); err != nil {
		return nil, err
	}
	if affected == 0 {
		if state.Status == "succeeded" && state.Resolution == "applied" {
			return &subscription, nil
		}
		return nil, sub2api.ErrResultUnknown
	}

	updated, extendErr := s.upstream.ExtendSubscription(ctx, eventKey, subscription.ID, days)
	if extendErr != nil {
		if errors.Is(extendErr, sub2api.ErrUpstreamRejected) {
			_, err = s.db().ExecContext(ctx, `UPDATE subscription_extension_events SET status = 'failed', resolution = 'released', error_message = ?, completed_at = ?, updated_at = ? WHERE id = ? AND status = 'reserved' AND resolution = ''`, extendErr.Error(), formatTime(now), formatTime(now), state.ID)
			if err != nil {
				return nil, err
			}
			return nil, extendErr
		}
		_, err = s.db().ExecContext(ctx, `UPDATE subscription_extension_events SET status = 'uncertain', error_message = ?, completed_at = ?, updated_at = ? WHERE id = ? AND status = 'reserved' AND resolution = ''`, extendErr.Error(), formatTime(now), formatTime(now), state.ID)
		if err != nil {
			return nil, err
		}
		return nil, extendErr
	}
	minimumExpiry := subscription.ExpiresAt.Add(time.Duration(days) * 24 * time.Hour)
	if updated == nil || updated.ExpiresAt.Before(minimumExpiry) {
		message := "upstream extension result did not prove the requested expiry"
		_, err = s.db().ExecContext(ctx, `UPDATE subscription_extension_events SET status = 'uncertain', after_expires_at = ?, error_message = ?, completed_at = ?, updated_at = ? WHERE id = ? AND status = 'reserved' AND resolution = ''`, formatNullableSubscriptionExpiry(updated), message, formatTime(now), formatTime(now), state.ID)
		if err != nil {
			return nil, err
		}
		return nil, sub2api.ErrResultUnknown
	}
	if err := s.applySubscriptionExtensionEvent(ctx, state.ID, &updated.ExpiresAt, 0, now); err != nil {
		_, _ = s.db().ExecContext(ctx, `UPDATE subscription_extension_events SET status = 'uncertain', error_message = ?, completed_at = ?, updated_at = ? WHERE id = ? AND status = 'reserved' AND resolution = ''`, err.Error(), formatTime(now), formatTime(now), state.ID)
		return nil, err
	}
	return updated, nil
}

func formatNullableSubscriptionExpiry(subscription *sub2api.Subscription) any {
	if subscription == nil || subscription.ExpiresAt.IsZero() {
		return nil
	}
	return formatTime(subscription.ExpiresAt)
}

func (s *Service) applySubscriptionExtensionEvent(ctx context.Context, eventID int64, afterExpiresAt *time.Time, operatorUserID int64, now time.Time) error {
	tx, err := s.db().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var subscriptionID int64
	var days int
	var status, resolution, eventCreatedRaw, sourceType string
	if err := tx.QueryRowContext(ctx, `SELECT upstream_subscription_id, extension_days, status, resolution, created_at, source_type FROM subscription_extension_events WHERE id = ?`, eventID).Scan(&subscriptionID, &days, &status, &resolution, &eventCreatedRaw, &sourceType); err != nil {
		return err
	}
	eventCreatedAt, err := parseNonNullTime(eventCreatedRaw)
	if err != nil {
		return err
	}
	if resolution == "applied" {
		return tx.Commit()
	}
	if resolution != "" || (status != "reserved" && status != "uncertain" && status != "succeeded") {
		return withStableReason(ErrConflict, SubscriptionResetReasonResolutionConflict)
	}
	delta := time.Duration(days) * 24 * time.Hour
	periodRows, err := tx.QueryContext(ctx, `
SELECT id, period_start, period_end
FROM subscription_reset_periods
WHERE upstream_subscription_id = ? AND legacy_ignored = 0
  AND (? = 'legacy_compensation' OR created_at <= ?)
  AND period_start IS NOT NULL AND period_end IS NOT NULL AND period_end > ?
ORDER BY period_start, id
`, subscriptionID, sourceType, formatTime(eventCreatedAt), formatTime(eventCreatedAt))
	if err != nil {
		return err
	}
	type periodShift struct {
		id         int64
		start, end time.Time
	}
	var periods []periodShift
	for periodRows.Next() {
		var item periodShift
		var startRaw, endRaw string
		if err := periodRows.Scan(&item.id, &startRaw, &endRaw); err != nil {
			_ = periodRows.Close()
			return err
		}
		start, parseErr := parseNonNullTime(startRaw)
		if parseErr != nil {
			_ = periodRows.Close()
			return parseErr
		}
		end, parseErr := parseNonNullTime(endRaw)
		if parseErr != nil {
			_ = periodRows.Close()
			return parseErr
		}
		item.start, item.end = start, end
		periods = append(periods, item)
	}
	if err := periodRows.Close(); err != nil {
		return err
	}
	baseCount := 0
	for _, period := range periods {
		start, end := period.start, period.end.Add(delta)
		if period.start.After(eventCreatedAt) {
			start = start.Add(delta)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE subscription_reset_periods SET period_start = ?, period_end = ?, updated_at = ? WHERE id = ?`, formatTime(start), formatTime(end), formatTime(now), period.id); err != nil {
			return err
		}
		baseCount++
	}

	grantRows, err := tx.QueryContext(ctx, `SELECT id, expires_at, status, reset_limit, reset_used FROM subscription_reset_bonus_grants WHERE upstream_subscription_id = ? AND status IN ('active', 'exhausted', 'expired') AND expires_at > ? AND created_at <= ? ORDER BY id`, subscriptionID, formatTime(eventCreatedAt), formatTime(eventCreatedAt))
	if err != nil {
		return err
	}
	type grantShift struct {
		id      int64
		expires time.Time
		status  string
		limit   int
		used    int
	}
	var grants []grantShift
	for grantRows.Next() {
		var item grantShift
		var expiresRaw string
		if err := grantRows.Scan(&item.id, &expiresRaw, &item.status, &item.limit, &item.used); err != nil {
			_ = grantRows.Close()
			return err
		}
		if item.expires, err = parseNonNullTime(expiresRaw); err != nil {
			_ = grantRows.Close()
			return err
		}
		grants = append(grants, item)
	}
	if err := grantRows.Close(); err != nil {
		return err
	}
	for _, grant := range grants {
		newExpiry := grant.expires.Add(delta)
		newStatus := grant.status
		if sourceType != "legacy_compensation" && grant.status == "expired" && newExpiry.After(now) {
			newStatus = "active"
			if grant.used >= grant.limit {
				newStatus = "exhausted"
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE subscription_reset_bonus_grants SET expires_at = ?, status = ?, updated_at = ? WHERE id = ?`, formatTime(newExpiry), newStatus, formatTime(now), grant.id); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE subscription_extension_events
SET status = 'succeeded', resolution = 'applied', after_expires_at = ?,
    applied_base_periods = ?, applied_bonus_grants = ?, error_message = '',
    completed_at = COALESCE(completed_at, ?), confirmed_at = ?, confirmed_by_user_id = ?, updated_at = ?
WHERE id = ? AND resolution = ''
`, formatNullableTime(afterExpiresAt), baseCount, len(grants), formatTime(now), formatTime(now), nullablePositiveInt64(operatorUserID), formatTime(now), eventID); err != nil {
		return err
	}
	return tx.Commit()
}

func nullablePositiveInt64(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func (s *Service) attachCompensationDetailToExtensionEvents(ctx context.Context, batchID, detailID, userID int64) error {
	if detailID <= 0 {
		return fmt.Errorf("compensation detail id must be positive")
	}
	_, err := s.db().ExecContext(ctx, `UPDATE subscription_extension_events SET compensation_detail_id = ?, updated_at = ? WHERE compensation_batch_id = ? AND upstream_user_id = ? AND compensation_detail_id IS NULL`, detailID, formatTime(s.now()), batchID, userID)
	return err
}

func (s *Service) compensationDetailID(ctx context.Context, batchID int64, detailKey string) (int64, error) {
	var id int64
	err := s.db().QueryRowContext(ctx, `SELECT id FROM compensation_batch_details WHERE batch_id = ? AND detail_key = ?`, batchID, detailKey).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

func (s *Service) ResolveSubscriptionExtensionEvent(ctx context.Context, eventID, operatorUserID int64, resolution string) (*models.SubscriptionExtensionEvent, error) {
	if eventID <= 0 || operatorUserID <= 0 || (resolution != "applied" && resolution != "released") {
		return nil, ErrBadRequest
	}
	event, err := s.GetSubscriptionExtensionEvent(ctx, eventID)
	if err != nil {
		return nil, err
	}
	if event.Resolution != "" {
		if event.Resolution != resolution {
			return nil, withStableReason(ErrConflict, SubscriptionResetReasonResolutionConflict)
		}
		return event, nil
	}
	if event.Status != "uncertain" && event.Status != "reserved" {
		return nil, withStableReason(ErrConflict, SubscriptionResetReasonResolutionConflict)
	}
	now := s.now()
	if resolution == "released" {
		result, err := s.db().ExecContext(ctx, `UPDATE subscription_extension_events SET status = 'failed', resolution = 'released', completed_at = COALESCE(completed_at, ?), confirmed_at = ?, confirmed_by_user_id = ?, updated_at = ? WHERE id = ? AND resolution = '' AND status IN ('reserved', 'uncertain')`, formatTime(now), formatTime(now), operatorUserID, formatTime(now), eventID)
		if err != nil {
			return nil, err
		}
		if affected, err := result.RowsAffected(); err != nil || affected != 1 {
			if err != nil {
				return nil, err
			}
			return s.GetSubscriptionExtensionEvent(ctx, eventID)
		}
		return s.GetSubscriptionExtensionEvent(ctx, eventID)
	}
	if event.BeforeExpiresAt == nil {
		return nil, withStableReason(ErrConflict, SubscriptionResetReasonResolutionConflict)
	}
	after := event.BeforeExpiresAt.Add(time.Duration(event.ExtensionDays) * 24 * time.Hour)
	if event.AfterExpiresAt != nil && event.AfterExpiresAt.After(after) {
		after = *event.AfterExpiresAt
	}
	if err := s.applySubscriptionExtensionEvent(ctx, eventID, &after, operatorUserID, now); err != nil {
		return nil, err
	}
	return s.GetSubscriptionExtensionEvent(ctx, eventID)
}

func (s *Service) ListSubscriptionExtensionEvents(ctx context.Context) ([]models.SubscriptionExtensionEvent, error) {
	rows, err := s.db().QueryContext(ctx, subscriptionExtensionEventSelectSQL()+` ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.SubscriptionExtensionEvent{}
	for rows.Next() {
		item, err := scanSubscriptionExtensionEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func (s *Service) GetSubscriptionExtensionEvent(ctx context.Context, eventID int64) (*models.SubscriptionExtensionEvent, error) {
	item, err := scanSubscriptionExtensionEvent(s.db().QueryRowContext(ctx, subscriptionExtensionEventSelectSQL()+` WHERE id = ?`, eventID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return item, err
}

func subscriptionExtensionEventSelectSQL() string {
	return `SELECT id, event_key, source_type, compensation_batch_id, compensation_detail_id,
       upstream_user_id, sub2api_group_id, upstream_subscription_id, extension_days,
       before_expires_at, after_expires_at, status, resolution, applied_base_periods,
       applied_bonus_grants, inferred_from_legacy, migration_version, error_message,
       reserved_at, completed_at, confirmed_at, confirmed_by_user_id, created_at, updated_at
FROM subscription_extension_events`
}

func scanSubscriptionExtensionEvent(scanner interface{ Scan(...any) error }) (*models.SubscriptionExtensionEvent, error) {
	var out models.SubscriptionExtensionEvent
	var batchID, detailID, confirmedBy sql.NullInt64
	var inferred int
	var before, after, completed, confirmed sql.NullString
	var reserved, created, updated string
	if err := scanner.Scan(&out.ID, &out.EventKey, &out.SourceType, &batchID, &detailID,
		&out.UpstreamUserID, &out.Sub2APIGroupID, &out.UpstreamSubscriptionID, &out.ExtensionDays,
		&before, &after, &out.Status, &out.Resolution, &out.AppliedBasePeriods,
		&out.AppliedBonusGrants, &inferred, &out.MigrationVersion, &out.ErrorMessage,
		&reserved, &completed, &confirmed, &confirmedBy, &created, &updated); err != nil {
		return nil, err
	}
	out.CompensationBatchID = parseNullableInt64(batchID)
	out.CompensationDetailID = parseNullableInt64(detailID)
	out.ConfirmedByUserID = parseNullableInt64(confirmedBy)
	out.InferredFromLegacy = inferred != 0
	var err error
	if out.BeforeExpiresAt, err = scanMaybeTime(before); err != nil {
		return nil, err
	}
	if out.AfterExpiresAt, err = scanMaybeTime(after); err != nil {
		return nil, err
	}
	if out.ReservedAt, err = parseNonNullTime(reserved); err != nil {
		return nil, err
	}
	if out.CompletedAt, err = scanMaybeTime(completed); err != nil {
		return nil, err
	}
	if out.ConfirmedAt, err = scanMaybeTime(confirmed); err != nil {
		return nil, err
	}
	if out.CreatedAt, err = parseNonNullTime(created); err != nil {
		return nil, err
	}
	if out.UpdatedAt, err = parseNonNullTime(updated); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *Service) MigrateLegacySubscriptionExtensionEvents(ctx context.Context) error {
	rows, err := s.db().QueryContext(ctx, `
SELECT detail.id, detail.batch_id, detail.upstream_user_id, detail.subscription_days,
       detail.upstream_reference_json, detail.created_at
FROM compensation_batch_details detail
WHERE detail.action_type = 'subscription' AND detail.subscription_days > 0
ORDER BY detail.id
`)
	if err != nil {
		return err
	}
	type legacyDetail struct {
		id, batchID, userID int64
		days                int
		referenceJSON       string
		createdRaw          string
	}
	var details []legacyDetail
	for rows.Next() {
		var detail legacyDetail
		if err := rows.Scan(&detail.id, &detail.batchID, &detail.userID, &detail.days, &detail.referenceJSON, &detail.createdRaw); err != nil {
			_ = rows.Close()
			return err
		}
		details = append(details, detail)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, detail := range details {
		var reference struct {
			ExtendedIDs []int64 `json:"extended_ids"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(detail.referenceJSON)), &reference); err != nil {
			return fmt.Errorf("decode legacy compensation detail %d: %w", detail.id, err)
		}
		eventAt, err := parseNonNullTime(detail.createdRaw)
		if err != nil {
			return err
		}
		for _, subscriptionID := range normalizePositiveInt64s(reference.ExtendedIDs) {
			var existingCount int
			if err := s.db().QueryRowContext(ctx, `SELECT COUNT(1) FROM subscription_extension_events WHERE compensation_detail_id = ? AND upstream_subscription_id = ?`, detail.id, subscriptionID).Scan(&existingCount); err != nil {
				return err
			}
			if existingCount > 0 {
				continue
			}
			var groupID int64
			if err := s.db().QueryRowContext(ctx, `
SELECT COALESCE(
  (SELECT sub2api_group_id FROM subscription_reset_periods WHERE upstream_subscription_id = ? AND upstream_user_id = ? ORDER BY id DESC LIMIT 1),
  (SELECT sub2api_group_id FROM subscription_reset_bonus_grants WHERE upstream_subscription_id = ? AND upstream_user_id = ? ORDER BY id DESC LIMIT 1),
  0
)
`, subscriptionID, detail.userID, subscriptionID, detail.userID).Scan(&groupID); err != nil {
				return err
			}
			eventKey := fmt.Sprintf("legacy-compensation-batch-%d-detail-%d-subscription-%d", detail.batchID, detail.id, subscriptionID)
			result, err := s.db().ExecContext(ctx, `
INSERT INTO subscription_extension_events (
  event_key, source_type, compensation_batch_id, compensation_detail_id, upstream_user_id,
  sub2api_group_id, upstream_subscription_id, extension_days, status, resolution,
  inferred_from_legacy, migration_version, reserved_at, created_at, updated_at
) VALUES (?, 'legacy_compensation', ?, ?, ?, ?, ?, ?, 'succeeded', '', 1, 1, ?, ?, ?)
ON CONFLICT(event_key) DO NOTHING
`, eventKey, detail.batchID, detail.id, detail.userID, groupID, subscriptionID, detail.days,
				formatTime(eventAt), formatTime(eventAt), formatTime(eventAt))
			if err != nil {
				return err
			}
			var eventID int64
			if err := s.db().QueryRowContext(ctx, `SELECT id FROM subscription_extension_events WHERE event_key = ?`, eventKey).Scan(&eventID); err != nil {
				return err
			}
			if affected, err := result.RowsAffected(); err != nil {
				return err
			} else if affected == 0 {
				event, err := s.GetSubscriptionExtensionEvent(ctx, eventID)
				if err != nil {
					return err
				}
				if event.Resolution == "applied" {
					continue
				}
			}
			if err := s.applySubscriptionExtensionEvent(ctx, eventID, nil, 0, eventAt); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) RecoverStaleSubscriptionExtensionEvents(ctx context.Context) error {
	now := s.now()
	cutoff := now.Add(-2 * time.Minute)
	_, err := s.db().ExecContext(ctx, `
UPDATE subscription_extension_events
SET status = 'uncertain', error_message = CASE WHEN error_message = '' THEN 'extension operation interrupted before result was recorded' ELSE error_message END,
    completed_at = COALESCE(completed_at, ?), updated_at = ?
WHERE status = 'reserved' AND resolution = '' AND reserved_at < ?
`, formatTime(now), formatTime(now), formatTime(cutoff))
	return err
}
