package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"sub2api-giftcode/backend/internal/models"
	"sub2api-giftcode/backend/internal/sub2api"
)

const (
	SubscriptionResetReasonUnlimited            = "unlimited"
	SubscriptionResetReasonExternalPeriod       = "external_period"
	SubscriptionResetReasonPeriodScheduled      = "period_scheduled"
	SubscriptionResetReasonZeroResetLimit       = "zero_reset_limit"
	SubscriptionResetReasonResetExhausted       = "reset_exhausted"
	SubscriptionResetReasonNoUsage              = "no_usage"
	SubscriptionResetReasonOperationPending     = "operation_pending"
	SubscriptionResetReasonSubscriptionInactive = "subscription_inactive"
	SubscriptionResetReasonUpstreamUnavailable  = "upstream_unavailable"
	SubscriptionResetReasonUpstreamRejected     = "upstream_rejected"
	SubscriptionResetReasonRequestIDConflict    = "request_id_conflict"
	SubscriptionResetReasonResolutionConflict   = "resolution_conflict"
)

type SubscriptionQuotaWindow struct {
	Kind         string     `json:"kind"`
	LimitUSD     float64    `json:"limit_usd"`
	UsedUSD      float64    `json:"used_usd"`
	RemainingUSD float64    `json:"remaining_usd"`
	WindowStart  *time.Time `json:"window_start,omitempty"`
	ResetsAt     *time.Time `json:"resets_at,omitempty"`
}

type SubscriptionResetPeriodSummary struct {
	ID             int64      `json:"id"`
	PeriodStart    *time.Time `json:"period_start,omitempty"`
	PeriodEnd      *time.Time `json:"period_end,omitempty"`
	Status         string     `json:"status"`
	ResetLimit     int        `json:"reset_limit"`
	ResetUsed      int        `json:"reset_used"`
	ResetRemaining int        `json:"reset_remaining"`
}

type SubscriptionCard struct {
	ID             int64                           `json:"id"`
	GroupID        int64                           `json:"group_id"`
	GroupName      string                          `json:"group_name"`
	GroupPlatform  string                          `json:"group_platform"`
	StartsAt       time.Time                       `json:"starts_at"`
	ExpiresAt      time.Time                       `json:"expires_at"`
	RemainingDays  int                             `json:"remaining_days"`
	QuotaWindows   []SubscriptionQuotaWindow       `json:"quota_windows"`
	CurrentPeriod  *SubscriptionResetPeriodSummary `json:"current_period,omitempty"`
	NextPeriod     *SubscriptionResetPeriodSummary `json:"next_period,omitempty"`
	Unlimited      bool                            `json:"unlimited"`
	CanReset       bool                            `json:"can_reset"`
	DisabledReason string                          `json:"disabled_reason,omitempty"`
}

type SubscriptionResetResult struct {
	Operation    models.SubscriptionResetAttempt `json:"operation"`
	Subscription *SubscriptionCard               `json:"subscription,omitempty"`
}

type subscriptionQuotaSnapshot struct {
	Windows []SubscriptionQuotaWindow `json:"windows"`
}

func (s *Service) ListSubscriptions(ctx context.Context, userID int64) ([]SubscriptionCard, error) {
	if userID <= 0 {
		return nil, ErrBadRequest
	}
	if err := s.requireUpstreamClient(); err != nil {
		return nil, withStableReason(ErrUpstreamUnavailable, SubscriptionResetReasonUpstreamUnavailable)
	}
	subscriptions, err := s.upstream.ListActiveUserSubscriptions(ctx, userID)
	if err != nil {
		return nil, withStableReason(ErrUpstreamUnavailable, SubscriptionResetReasonUpstreamUnavailable)
	}
	cards := make([]SubscriptionCard, 0, len(subscriptions))
	groupsByID := map[int64]*sub2api.Group{}
	groupsLoaded := false
	for i := range subscriptions {
		subscription := subscriptions[i]
		if subscription.UserID != userID {
			continue
		}
		var metadataErr error
		if subscription.Group == nil {
			if !groupsLoaded {
				groupsLoaded = true
				groups, groupsErr := s.upstream.ListGroupsAll(ctx)
				if groupsErr != nil {
					metadataErr = groupsErr
				} else {
					for groupIndex := range groups {
						group := groups[groupIndex]
						groupsByID[group.ID] = &group
					}
				}
			}
			subscription.Group = groupsByID[subscription.GroupID]
			if subscription.Group == nil && metadataErr == nil {
				metadataErr = errors.New("subscription group metadata is unavailable")
			}
		}
		progress, progressErr := s.upstream.GetSubscriptionProgress(ctx, subscription.ID)
		if metadataErr != nil {
			progressErr = errors.Join(progressErr, metadataErr)
		}
		card, buildErr := s.buildSubscriptionCard(ctx, subscription, progress, progressErr, s.now())
		if buildErr != nil {
			return nil, buildErr
		}
		cards = append(cards, card)
	}
	return cards, nil
}

func (s *Service) buildSubscriptionCard(ctx context.Context, subscription sub2api.Subscription, progress *sub2api.SubscriptionProgress, progressErr error, now time.Time) (SubscriptionCard, error) {
	card := SubscriptionCard{
		ID: subscription.ID, GroupID: subscription.GroupID, StartsAt: subscription.StartsAt.UTC(), ExpiresAt: subscription.ExpiresAt.UTC(),
	}
	if !subscription.ExpiresAt.IsZero() && subscription.ExpiresAt.After(now) {
		card.RemainingDays = int(math.Ceil(subscription.ExpiresAt.Sub(now).Hours() / 24))
	}
	group := subscription.Group
	if group != nil {
		card.GroupName = group.Name
		card.GroupPlatform = group.Platform
	}
	card.QuotaWindows = configuredQuotaWindows(subscription, progress, group)
	card.Unlimited = group != nil && len(card.QuotaWindows) == 0
	periods, err := s.listSubscriptionResetPeriodsForGroup(ctx, subscription.UserID, subscription.GroupID)
	if err != nil {
		return card, err
	}
	for i := range periods {
		period := periods[i]
		if period.UpstreamSubscriptionID == nil || *period.UpstreamSubscriptionID != subscription.ID {
			continue
		}
		summary := summarizeResetPeriod(period)
		if period.PeriodStart != nil && period.PeriodEnd != nil && !now.Before(*period.PeriodStart) && now.Before(*period.PeriodEnd) {
			card.CurrentPeriod = &summary
		} else if period.PeriodStart != nil && now.Before(*period.PeriodStart) && card.NextPeriod == nil {
			card.NextPeriod = &summary
		}
	}
	card.DisabledReason, err = s.subscriptionDisabledReason(ctx, subscription, card, progressErr, now)
	if err != nil {
		return card, err
	}
	card.CanReset = card.DisabledReason == ""
	return card, nil
}

func configuredQuotaWindows(subscription sub2api.Subscription, progress *sub2api.SubscriptionProgress, group *sub2api.Group) []SubscriptionQuotaWindow {
	if group == nil {
		return []SubscriptionQuotaWindow{}
	}
	out := make([]SubscriptionQuotaWindow, 0, 3)
	appendWindow := func(kind string, limit *float64, fallbackUsed float64, fallbackStart *time.Time, value *sub2api.UsageWindowProgress) {
		if limit == nil || *limit <= 0 {
			return
		}
		window := SubscriptionQuotaWindow{Kind: kind, LimitUSD: *limit, UsedUSD: fallbackUsed, WindowStart: fallbackStart}
		window.RemainingUSD = math.Max(0, window.LimitUSD-window.UsedUSD)
		if value != nil {
			window.LimitUSD = value.LimitUSD
			window.UsedUSD = value.UsedUSD
			window.RemainingUSD = value.RemainingUSD
			window.WindowStart = value.WindowStart
			window.ResetsAt = value.ResetsAt
		}
		out = append(out, window)
	}
	var daily, weekly, monthly *sub2api.UsageWindowProgress
	if progress != nil {
		daily, weekly, monthly = progress.Daily, progress.Weekly, progress.Monthly
	}
	appendWindow("daily", group.DailyLimitUSD, subscription.DailyUsageUSD, subscription.DailyWindowStart, daily)
	appendWindow("weekly", group.WeeklyLimitUSD, subscription.WeeklyUsageUSD, subscription.WeeklyWindowStart, weekly)
	appendWindow("monthly", group.MonthlyLimitUSD, subscription.MonthlyUsageUSD, subscription.MonthlyWindowStart, monthly)
	return out
}

func summarizeResetPeriod(period models.SubscriptionResetPeriod) SubscriptionResetPeriodSummary {
	remaining := period.ResetLimit - period.ResetUsed
	if remaining < 0 {
		remaining = 0
	}
	return SubscriptionResetPeriodSummary{ID: period.ID, PeriodStart: period.PeriodStart, PeriodEnd: period.PeriodEnd, Status: period.Status, ResetLimit: period.ResetLimit, ResetUsed: period.ResetUsed, ResetRemaining: remaining}
}

func (s *Service) subscriptionDisabledReason(ctx context.Context, subscription sub2api.Subscription, card SubscriptionCard, progressErr error, now time.Time) (string, error) {
	if subscription.Group == nil {
		return SubscriptionResetReasonUpstreamUnavailable, nil
	}
	if card.Unlimited {
		return SubscriptionResetReasonUnlimited, nil
	}
	if subscription.Status != "active" || subscription.ExpiresAt.IsZero() || !now.Before(subscription.ExpiresAt) {
		return SubscriptionResetReasonSubscriptionInactive, nil
	}
	if progressErr != nil {
		return SubscriptionResetReasonUpstreamUnavailable, nil
	}
	if card.CurrentPeriod == nil {
		if card.NextPeriod != nil {
			return SubscriptionResetReasonPeriodScheduled, nil
		}
		return SubscriptionResetReasonExternalPeriod, nil
	}
	var blocking int
	if err := s.db().QueryRowContext(ctx, `SELECT COUNT(1) FROM subscription_reset_attempts WHERE period_id = ? AND status IN ('reserved', 'uncertain')`, card.CurrentPeriod.ID).Scan(&blocking); err != nil {
		return "", err
	}
	if blocking > 0 {
		return SubscriptionResetReasonOperationPending, nil
	}
	if card.CurrentPeriod.Status != "active" {
		return SubscriptionResetReasonSubscriptionInactive, nil
	}
	if card.CurrentPeriod.ResetLimit <= 0 {
		return SubscriptionResetReasonZeroResetLimit, nil
	}
	if card.CurrentPeriod.ResetRemaining <= 0 {
		return SubscriptionResetReasonResetExhausted, nil
	}
	for _, window := range card.QuotaWindows {
		if window.UsedUSD > 0 {
			return "", nil
		}
	}
	return SubscriptionResetReasonNoUsage, nil
}

func (s *Service) ResetSubscriptionQuota(ctx context.Context, userID, subscriptionID int64, requestID string) (*SubscriptionResetResult, error) {
	requestID = strings.TrimSpace(requestID)
	parsed, err := uuid.Parse(requestID)
	if err != nil || parsed.String() != requestID {
		return nil, ErrBadRequest
	}
	if existing, findErr := s.getSubscriptionResetAttemptByRequestID(ctx, requestID); findErr == nil {
		return resetReplay(existing, userID, subscriptionID)
	} else if !errors.Is(findErr, sql.ErrNoRows) {
		return nil, findErr
	}
	if userID <= 0 || subscriptionID <= 0 {
		return nil, ErrBadRequest
	}
	if err := s.requireUpstreamClient(); err != nil {
		return nil, withStableReason(ErrUpstreamUnavailable, SubscriptionResetReasonUpstreamUnavailable)
	}
	subscription, err := s.upstream.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, subscriptionAuthorityError(err)
	}
	if subscription.UserID != userID {
		return nil, ErrNotFound
	}
	unlock := s.lockSubscriptionResetBoundary(userID, subscription.GroupID)
	defer unlock()
	if existing, findErr := s.getSubscriptionResetAttemptByRequestID(ctx, requestID); findErr == nil {
		return resetReplay(existing, userID, subscriptionID)
	} else if !errors.Is(findErr, sql.ErrNoRows) {
		return nil, findErr
	}
	subscription, err = s.upstream.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, subscriptionAuthorityError(err)
	}
	if subscription.UserID != userID {
		return nil, ErrNotFound
	}
	if subscription.Group == nil {
		groups, groupsErr := s.upstream.ListGroupsAll(ctx)
		if groupsErr != nil {
			return nil, withStableReason(ErrUpstreamUnavailable, SubscriptionResetReasonUpstreamUnavailable)
		}
		for groupIndex := range groups {
			if groups[groupIndex].ID == subscription.GroupID {
				group := groups[groupIndex]
				subscription.Group = &group
				break
			}
		}
		if subscription.Group == nil {
			return nil, withStableReason(ErrUpstreamUnavailable, SubscriptionResetReasonUpstreamUnavailable)
		}
	}
	progress, err := s.upstream.GetSubscriptionProgress(ctx, subscriptionID)
	if err != nil {
		return nil, withStableReason(ErrConflict, SubscriptionResetReasonUpstreamUnavailable)
	}
	card, err := s.buildSubscriptionCard(ctx, *subscription, progress, nil, s.now())
	if err != nil {
		return nil, err
	}
	if !card.CanReset || card.CurrentPeriod == nil {
		return nil, withStableReason(ErrConflict, card.DisabledReason)
	}
	group := subscription.Group
	if group == nil {
		return nil, withStableReason(ErrConflict, SubscriptionResetReasonUpstreamUnavailable)
	}
	before := subscriptionQuotaSnapshot{Windows: card.QuotaWindows}
	attempt, err := s.reserveSubscriptionResetAttempt(ctx, requestID, userID, subscriptionID, card.CurrentPeriod.ID, group, before, s.now())
	if err != nil {
		if existing, findErr := s.getSubscriptionResetAttemptByRequestID(ctx, requestID); findErr == nil {
			return resetReplay(existing, userID, subscriptionID)
		}
		return nil, err
	}
	updated, resetErr := s.upstream.ResetSubscriptionQuota(ctx, subscriptionID, *group)
	now := s.now()
	if resetErr == nil {
		after := subscriptionQuotaSnapshot{Windows: configuredQuotaWindows(*updated, nil, group)}
		attempt, err = s.finishSubscriptionResetAttempt(ctx, attempt.ID, "succeeded", 200, "", "", marshalJSON(after), false, now)
		if err != nil {
			return nil, err
		}
		s.WakeSubscriptionResetReconcile()
		return &SubscriptionResetResult{Operation: *attempt}, nil
	}
	if errors.Is(resetErr, sub2api.ErrUpstreamRejected) {
		attempt, err = s.finishSubscriptionResetAttempt(ctx, attempt.ID, "failed", 409, SubscriptionResetReasonUpstreamRejected, resetErr.Error(), "", true, now)
		if err != nil {
			return nil, err
		}
		s.WakeSubscriptionResetReconcile()
		return nil, withStableReason(ErrConflict, SubscriptionResetReasonUpstreamRejected)
	}
	attempt, err = s.finishSubscriptionResetAttempt(ctx, attempt.ID, "uncertain", 202, "result_unknown", resetErr.Error(), "", false, now)
	if err != nil {
		return nil, err
	}
	s.WakeSubscriptionResetReconcile()
	return &SubscriptionResetResult{Operation: *attempt}, nil
}

func resetReplay(attempt *models.SubscriptionResetAttempt, userID, subscriptionID int64) (*SubscriptionResetResult, error) {
	if attempt.UpstreamUserID != userID || attempt.UpstreamSubscriptionID != subscriptionID {
		return nil, withStableReason(ErrConflict, SubscriptionResetReasonRequestIDConflict)
	}
	return &SubscriptionResetResult{Operation: *attempt}, nil
}

func subscriptionAuthorityError(err error) error {
	var operationErr *sub2api.OperationError
	if errors.As(err, &operationErr) && operationErr.Status == 404 {
		return ErrNotFound
	}
	return withStableReason(ErrUpstreamUnavailable, SubscriptionResetReasonUpstreamUnavailable)
}

func (s *Service) reserveSubscriptionResetAttempt(ctx context.Context, requestID string, userID, subscriptionID, periodID int64, group *sub2api.Group, before subscriptionQuotaSnapshot, now time.Time) (*models.SubscriptionResetAttempt, error) {
	tx, err := s.db().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE subscription_reset_periods SET reset_used = reset_used + 1, updated_at = ? WHERE id = ? AND reset_used < reset_limit AND status = 'active'`, formatTime(now), periodID)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected != 1 {
		return nil, withStableReason(ErrConflict, SubscriptionResetReasonResetExhausted)
	}
	result, err = tx.ExecContext(ctx, `
INSERT INTO subscription_reset_attempts (
  request_id, period_id, upstream_user_id, upstream_subscription_id,
  reset_daily, reset_weekly, reset_monthly, status, before_snapshot_json,
  response_status, reserved_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, 'reserved', ?, 202, ?, ?, ?)
`, requestID, periodID, userID, subscriptionID, boolInt(positiveAppLimit(group.DailyLimitUSD)), boolInt(positiveAppLimit(group.WeeklyLimitUSD)), boolInt(positiveAppLimit(group.MonthlyLimitUSD)), marshalJSON(before), formatTime(now), formatTime(now), formatTime(now))
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	attempt, err := getSubscriptionResetAttemptTx(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return attempt, nil
}

func (s *Service) finishSubscriptionResetAttempt(ctx context.Context, attemptID int64, status string, responseStatus int, reason, errorMessage, afterSnapshot string, release bool, now time.Time) (*models.SubscriptionResetAttempt, error) {
	tx, err := s.db().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	attempt, err := getSubscriptionResetAttemptTx(ctx, tx, attemptID)
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE subscription_reset_attempts SET status = ?, response_status = ?, response_reason = ?, error_message = ?, after_snapshot_json = ?, completed_at = ?, updated_at = ? WHERE id = ? AND status = 'reserved' AND resolution = ''`, status, responseStatus, reason, errorMessage, afterSnapshot, formatTime(now), formatTime(now), attemptID)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected != 1 {
		current, loadErr := getSubscriptionResetAttemptTx(ctx, tx, attemptID)
		if loadErr != nil {
			return nil, loadErr
		}
		return current, tx.Commit()
	}
	if release {
		result, err := tx.ExecContext(ctx, `UPDATE subscription_reset_periods SET reset_used = reset_used - 1, updated_at = ? WHERE id = ? AND reset_used > 0`, formatTime(now), attempt.PeriodID)
		if err != nil {
			return nil, err
		}
		if affected, err := result.RowsAffected(); err != nil || affected != 1 {
			if err != nil {
				return nil, err
			}
			return nil, errors.New("reserved reset count is missing")
		}
	}
	attempt, err = getSubscriptionResetAttemptTx(ctx, tx, attemptID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return attempt, nil
}

func (s *Service) ResolveSubscriptionResetAttempt(ctx context.Context, attemptID, operatorUserID int64, resolution string) (*models.SubscriptionResetAttempt, error) {
	if attemptID <= 0 || operatorUserID <= 0 || (resolution != "consumed" && resolution != "released") {
		return nil, ErrBadRequest
	}
	var upstreamUserID, groupID int64
	if err := s.db().QueryRowContext(ctx, `
SELECT a.upstream_user_id, p.sub2api_group_id
FROM subscription_reset_attempts a
JOIN subscription_reset_periods p ON p.id = a.period_id
WHERE a.id = ?
`, attemptID).Scan(&upstreamUserID, &groupID); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	unlock := s.lockSubscriptionResetBoundary(upstreamUserID, groupID)
	defer unlock()
	now := s.now()
	tx, err := s.db().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	attempt, err := getSubscriptionResetAttemptTx(ctx, tx, attemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if attempt.Resolution != "" {
		if attempt.Resolution != resolution {
			return nil, withStableReason(ErrConflict, SubscriptionResetReasonResolutionConflict)
		}
		return attempt, nil
	}
	if attempt.Status != "uncertain" {
		return nil, withStableReason(ErrConflict, SubscriptionResetReasonResolutionConflict)
	}
	status := "succeeded"
	responseStatus := 200
	if resolution == "released" {
		status = "failed"
		responseStatus = 409
		result, updateErr := tx.ExecContext(ctx, `UPDATE subscription_reset_periods SET reset_used = reset_used - 1, updated_at = ? WHERE id = ? AND reset_used > 0`, formatTime(now), attempt.PeriodID)
		if updateErr != nil {
			return nil, updateErr
		}
		if affected, updateErr := result.RowsAffected(); updateErr != nil || affected != 1 {
			if updateErr != nil {
				return nil, updateErr
			}
			return nil, errors.New("reserved reset count is missing")
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE subscription_reset_attempts SET status = ?, response_status = ?, resolution = ?, confirmed_at = ?, confirmed_by_user_id = ?, completed_at = ?, updated_at = ? WHERE id = ? AND resolution = ''`, status, responseStatus, resolution, formatTime(now), operatorUserID, formatTime(now), formatTime(now), attemptID)
	if err != nil {
		return nil, err
	}
	attempt, err = getSubscriptionResetAttemptTx(ctx, tx, attemptID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return attempt, nil
}

func (s *Service) ListPendingSubscriptionResetAttempts(ctx context.Context) ([]models.SubscriptionResetAttempt, error) {
	rows, err := s.db().QueryContext(ctx, subscriptionResetAttemptSelectSQL()+` WHERE status IN ('reserved', 'uncertain') ORDER BY reserved_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.SubscriptionResetAttempt{}
	for rows.Next() {
		attempt, scanErr := scanSubscriptionResetAttempt(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *attempt)
	}
	return out, rows.Err()
}

func (s *Service) ListSubscriptionResetBackfillRuns(ctx context.Context) ([]models.SubscriptionResetBackfillRun, error) {
	rows, err := s.db().QueryContext(ctx, `SELECT id, tier_id, reset_limit, status, total_records, processed_records, granted_records, error_message, triggered_at, started_at, completed_at, updated_at FROM subscription_reset_backfill_runs ORDER BY triggered_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.SubscriptionResetBackfillRun{}
	for rows.Next() {
		var item models.SubscriptionResetBackfillRun
		var triggeredAt, updatedAt string
		var startedAt, completedAt sql.NullString
		if err := rows.Scan(&item.ID, &item.TierID, &item.ResetLimit, &item.Status, &item.TotalRecords, &item.ProcessedRecords, &item.GrantedRecords, &item.ErrorMessage, &triggeredAt, &startedAt, &completedAt, &updatedAt); err != nil {
			return nil, err
		}
		var err error
		if item.TriggeredAt, err = parseNonNullTime(triggeredAt); err != nil {
			return nil, err
		}
		if item.UpdatedAt, err = parseNonNullTime(updatedAt); err != nil {
			return nil, err
		}
		if item.StartedAt, err = scanMaybeTime(startedAt); err != nil {
			return nil, err
		}
		if item.CompletedAt, err = scanMaybeTime(completedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func positiveAppLimit(limit *float64) bool { return limit != nil && *limit > 0 }

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (s *Service) getSubscriptionResetAttemptByRequestID(ctx context.Context, requestID string) (*models.SubscriptionResetAttempt, error) {
	return scanSubscriptionResetAttempt(s.db().QueryRowContext(ctx, subscriptionResetAttemptSelectSQL()+` WHERE request_id = ?`, requestID))
}

func getSubscriptionResetAttemptTx(ctx context.Context, tx *sql.Tx, id int64) (*models.SubscriptionResetAttempt, error) {
	return scanSubscriptionResetAttempt(tx.QueryRowContext(ctx, subscriptionResetAttemptSelectSQL()+` WHERE id = ?`, id))
}

func subscriptionResetAttemptSelectSQL() string {
	return `SELECT id, request_id, period_id, upstream_user_id, upstream_subscription_id, reset_daily, reset_weekly, reset_monthly, status, before_snapshot_json, after_snapshot_json, upstream_status, response_status, response_reason, error_message, resolution, reserved_at, completed_at, confirmed_at, confirmed_by_user_id, created_at, updated_at FROM subscription_reset_attempts`
}

func scanSubscriptionResetAttempt(scanner interface{ Scan(...any) error }) (*models.SubscriptionResetAttempt, error) {
	var out models.SubscriptionResetAttempt
	var resetDaily, resetWeekly, resetMonthly int
	var upstreamStatus sql.NullInt64
	var reservedAt, createdAt, updatedAt string
	var completedAt, confirmedAt sql.NullString
	var confirmedBy sql.NullInt64
	if err := scanner.Scan(&out.ID, &out.RequestID, &out.PeriodID, &out.UpstreamUserID, &out.UpstreamSubscriptionID, &resetDaily, &resetWeekly, &resetMonthly, &out.Status, &out.BeforeSnapshotJSON, &out.AfterSnapshotJSON, &upstreamStatus, &out.ResponseStatus, &out.ResponseReason, &out.ErrorMessage, &out.Resolution, &reservedAt, &completedAt, &confirmedAt, &confirmedBy, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	out.ResetDaily, out.ResetWeekly, out.ResetMonthly = resetDaily != 0, resetWeekly != 0, resetMonthly != 0
	if upstreamStatus.Valid {
		value := int(upstreamStatus.Int64)
		out.UpstreamStatus = &value
	}
	out.ConfirmedByUserID = parseNullableInt64(confirmedBy)
	var err error
	if out.ReservedAt, err = parseNonNullTime(reservedAt); err != nil {
		return nil, err
	}
	if out.CompletedAt, err = scanMaybeTime(completedAt); err != nil {
		return nil, err
	}
	if out.ConfirmedAt, err = scanMaybeTime(confirmedAt); err != nil {
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

func (s *Service) reconcileUncertainSubscriptionResetAttempts(ctx context.Context, subscription *sub2api.Subscription, now time.Time) error {
	if subscription == nil {
		return nil
	}
	rows, err := s.db().QueryContext(ctx, subscriptionResetAttemptSelectSQL()+` WHERE upstream_user_id = ? AND upstream_subscription_id = ? AND status = 'uncertain'`, subscription.UserID, subscription.ID)
	if err != nil {
		return err
	}
	var attempts []models.SubscriptionResetAttempt
	for rows.Next() {
		attempt, scanErr := scanSubscriptionResetAttempt(rows)
		if scanErr != nil {
			_ = rows.Close()
			return scanErr
		}
		attempts = append(attempts, *attempt)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for i := range attempts {
		attempt := attempts[i]
		var before subscriptionQuotaSnapshot
		if err := json.Unmarshal([]byte(attempt.BeforeSnapshotJSON), &before); err != nil {
			continue
		}
		if !allResetWindowStartsChanged(attempt, before, *subscription) {
			continue
		}
		after := currentAttemptSnapshot(attempt, before, *subscription)
		_, err := s.db().ExecContext(ctx, `UPDATE subscription_reset_attempts SET status = 'succeeded', response_status = 200, response_reason = 'window_start_changed', after_snapshot_json = ?, completed_at = ?, confirmed_at = ?, updated_at = ? WHERE id = ? AND status = 'uncertain'`, marshalJSON(after), formatTime(now), formatTime(now), formatTime(now), attempt.ID)
		if err != nil {
			return err
		}
	}
	return nil
}

func allResetWindowStartsChanged(attempt models.SubscriptionResetAttempt, before subscriptionQuotaSnapshot, subscription sub2api.Subscription) bool {
	starts := map[string]*time.Time{"daily": subscription.DailyWindowStart, "weekly": subscription.WeeklyWindowStart, "monthly": subscription.MonthlyWindowStart}
	targets := map[string]bool{"daily": attempt.ResetDaily, "weekly": attempt.ResetWeekly, "monthly": attempt.ResetMonthly}
	beforeByKind := make(map[string]*time.Time, len(before.Windows))
	for _, window := range before.Windows {
		beforeByKind[window.Kind] = window.WindowStart
	}
	for kind, target := range targets {
		if !target {
			continue
		}
		oldStart, ok := beforeByKind[kind]
		newStart := starts[kind]
		if !ok || oldStart == nil || newStart == nil || oldStart.Equal(*newStart) {
			return false
		}
	}
	return attempt.ResetDaily || attempt.ResetWeekly || attempt.ResetMonthly
}

func currentAttemptSnapshot(attempt models.SubscriptionResetAttempt, before subscriptionQuotaSnapshot, subscription sub2api.Subscription) subscriptionQuotaSnapshot {
	out := subscriptionQuotaSnapshot{Windows: make([]SubscriptionQuotaWindow, 0, len(before.Windows))}
	for _, window := range before.Windows {
		if (window.Kind == "daily" && attempt.ResetDaily) || (window.Kind == "weekly" && attempt.ResetWeekly) || (window.Kind == "monthly" && attempt.ResetMonthly) {
			switch window.Kind {
			case "daily":
				window.UsedUSD, window.WindowStart = subscription.DailyUsageUSD, subscription.DailyWindowStart
			case "weekly":
				window.UsedUSD, window.WindowStart = subscription.WeeklyUsageUSD, subscription.WeeklyWindowStart
			case "monthly":
				window.UsedUSD, window.WindowStart = subscription.MonthlyUsageUSD, subscription.MonthlyWindowStart
			}
			window.RemainingUSD = math.Max(0, window.LimitUSD-window.UsedUSD)
		}
		out.Windows = append(out.Windows, window)
	}
	return out
}
