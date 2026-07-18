package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"sort"
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

type SubscriptionResetBonusSummary struct {
	ID             int64     `json:"id"`
	BatchID        int64     `json:"batch_id"`
	Note           string    `json:"note"`
	ResetLimit     int       `json:"reset_limit"`
	ResetUsed      int       `json:"reset_used"`
	ResetRemaining int       `json:"reset_remaining"`
	ExpiresAt      time.Time `json:"expires_at"`
	Status         string    `json:"status"`
}

type SubscriptionResetEntitlementSummary struct {
	Type      string    `json:"type"`
	ID        int64     `json:"id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type SubscriptionCard struct {
	ID                  int64                                `json:"id"`
	GroupID             int64                                `json:"group_id"`
	GroupName           string                               `json:"group_name"`
	GroupPlatform       string                               `json:"group_platform"`
	StartsAt            time.Time                            `json:"starts_at"`
	ExpiresAt           time.Time                            `json:"expires_at"`
	RemainingDays       int                                  `json:"remaining_days"`
	QuotaWindows        []SubscriptionQuotaWindow            `json:"quota_windows"`
	CurrentPeriod       *SubscriptionResetPeriodSummary      `json:"current_period,omitempty"`
	NextPeriod          *SubscriptionResetPeriodSummary      `json:"next_period,omitempty"`
	BaseResetLimit      int                                  `json:"base_reset_limit"`
	BaseResetUsed       int                                  `json:"base_reset_used"`
	BaseResetRemaining  int                                  `json:"base_reset_remaining"`
	BonusResetRemaining int                                  `json:"bonus_reset_remaining"`
	TotalResetRemaining int                                  `json:"total_reset_remaining"`
	BonusGrants         []SubscriptionResetBonusSummary      `json:"bonus_grants"`
	NextEntitlement     *SubscriptionResetEntitlementSummary `json:"next_entitlement,omitempty"`
	Unlimited           bool                                 `json:"unlimited"`
	ExternalPeriod      bool                                 `json:"external_period"`
	ZeroResetLimit      bool                                 `json:"zero_reset_limit"`
	OperationPending    bool                                 `json:"operation_pending"`
	CanReset            bool                                 `json:"can_reset"`
	DisabledReason      string                               `json:"disabled_reason,omitempty"`
}

type SubscriptionResetResult struct {
	Operation    models.SubscriptionResetAttempt `json:"operation"`
	Subscription *SubscriptionCard               `json:"subscription,omitempty"`
}

type subscriptionQuotaSnapshot struct {
	Windows []SubscriptionQuotaWindow `json:"windows"`
}

type SubscriptionResetAttemptAdminView struct {
	models.SubscriptionResetAttempt
	Username             string                              `json:"username"`
	Email                string                              `json:"email"`
	Period               *models.SubscriptionResetPeriod     `json:"period,omitempty"`
	BonusGrant           *models.SubscriptionResetBonusGrant `json:"bonus_grant,omitempty"`
	BeforeSnapshot       []SubscriptionQuotaWindow           `json:"before_snapshot"`
	AfterSnapshot        []SubscriptionQuotaWindow           `json:"after_snapshot"`
	CurrentSnapshot      []SubscriptionQuotaWindow           `json:"current_snapshot"`
	SnapshotError        string                              `json:"snapshot_error,omitempty"`
	CurrentSnapshotError string                              `json:"current_snapshot_error,omitempty"`
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
	card.ExternalPeriod = card.CurrentPeriod == nil && card.NextPeriod == nil
	if card.CurrentPeriod != nil && card.CurrentPeriod.Status == "active" {
		card.BaseResetLimit = card.CurrentPeriod.ResetLimit
		card.BaseResetUsed = card.CurrentPeriod.ResetUsed
		card.BaseResetRemaining = card.CurrentPeriod.ResetRemaining
	}
	card.BonusGrants, err = s.listSubscriptionResetBonusSummaries(ctx, subscription.UserID, subscription.GroupID, subscription.ID, now)
	if err != nil {
		return card, err
	}
	for _, grant := range card.BonusGrants {
		card.BonusResetRemaining += grant.ResetRemaining
	}
	card.TotalResetRemaining = card.BaseResetRemaining + card.BonusResetRemaining
	card.ZeroResetLimit = card.CurrentPeriod != nil && card.CurrentPeriod.ResetLimit <= 0 && card.BonusResetRemaining == 0
	card.NextEntitlement = nextSubscriptionResetEntitlement(card)
	var blocking int
	if err := s.db().QueryRowContext(ctx, `SELECT COUNT(1) FROM subscription_reset_attempts WHERE upstream_subscription_id = ? AND status IN ('reserved', 'uncertain')`, card.ID).Scan(&blocking); err != nil {
		return card, err
	}
	card.OperationPending = blocking > 0
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

func (s *Service) listSubscriptionResetBonusSummaries(ctx context.Context, userID, groupID, subscriptionID int64, now time.Time) ([]SubscriptionResetBonusSummary, error) {
	if _, err := s.db().ExecContext(ctx, `UPDATE subscription_reset_bonus_grants SET status = 'expired', updated_at = ? WHERE upstream_subscription_id = ? AND status IN ('active', 'exhausted') AND expires_at <= ?`, formatTime(now), subscriptionID, formatTime(now)); err != nil {
		return nil, err
	}
	rows, err := s.db().QueryContext(ctx, `
SELECT grant.id, grant.batch_id, COALESCE(batch.note, ''), grant.reset_limit, grant.reset_used,
       grant.expires_at, grant.status
FROM subscription_reset_bonus_grants grant
LEFT JOIN subscription_reset_bonus_batches batch ON batch.id = grant.batch_id
WHERE grant.upstream_user_id = ? AND grant.sub2api_group_id = ? AND grant.upstream_subscription_id = ?
  AND grant.status IN ('active', 'exhausted') AND grant.starts_at <= ? AND grant.expires_at > ?
ORDER BY grant.expires_at, grant.id
`, userID, groupID, subscriptionID, formatTime(now), formatTime(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SubscriptionResetBonusSummary{}
	for rows.Next() {
		var item SubscriptionResetBonusSummary
		var expiresAt string
		if err := rows.Scan(&item.ID, &item.BatchID, &item.Note, &item.ResetLimit, &item.ResetUsed, &expiresAt, &item.Status); err != nil {
			return nil, err
		}
		item.ResetRemaining = item.ResetLimit - item.ResetUsed
		if item.ResetRemaining < 0 {
			item.ResetRemaining = 0
		}
		if item.ExpiresAt, err = parseNonNullTime(expiresAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func nextSubscriptionResetEntitlement(card SubscriptionCard) *SubscriptionResetEntitlementSummary {
	candidates := make([]SubscriptionResetEntitlementSummary, 0, len(card.BonusGrants)+1)
	if card.CurrentPeriod != nil && card.CurrentPeriod.Status == "active" && card.CurrentPeriod.ResetRemaining > 0 && card.CurrentPeriod.PeriodEnd != nil {
		candidates = append(candidates, SubscriptionResetEntitlementSummary{Type: "base_period", ID: card.CurrentPeriod.ID, ExpiresAt: card.CurrentPeriod.PeriodEnd.UTC()})
	}
	for _, grant := range card.BonusGrants {
		if grant.ResetRemaining > 0 {
			candidates = append(candidates, SubscriptionResetEntitlementSummary{Type: "bonus_grant", ID: grant.ID, ExpiresAt: grant.ExpiresAt.UTC()})
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		if !candidates[i].ExpiresAt.Equal(candidates[j].ExpiresAt) {
			return candidates[i].ExpiresAt.Before(candidates[j].ExpiresAt)
		}
		if candidates[i].Type != candidates[j].Type {
			return candidates[i].Type == "bonus_grant"
		}
		return candidates[i].ID < candidates[j].ID
	})
	return &candidates[0]
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
	if card.OperationPending {
		return SubscriptionResetReasonOperationPending, nil
	}
	if card.TotalResetRemaining <= 0 && card.CurrentPeriod == nil {
		if len(card.BonusGrants) > 0 {
			return SubscriptionResetReasonResetExhausted, nil
		}
		if card.NextPeriod != nil {
			return SubscriptionResetReasonPeriodScheduled, nil
		}
		return SubscriptionResetReasonExternalPeriod, nil
	}
	if card.TotalResetRemaining <= 0 && card.CurrentPeriod != nil && card.CurrentPeriod.Status != "active" {
		return SubscriptionResetReasonSubscriptionInactive, nil
	}
	if card.TotalResetRemaining <= 0 && card.CurrentPeriod != nil && card.CurrentPeriod.ResetLimit <= 0 && len(card.BonusGrants) == 0 {
		return SubscriptionResetReasonZeroResetLimit, nil
	}
	if card.TotalResetRemaining <= 0 {
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
		return s.resetReplay(ctx, existing, userID, subscriptionID)
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
		return s.resetReplay(ctx, existing, userID, subscriptionID)
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
	if !card.CanReset {
		return nil, withStableReason(ErrConflict, card.DisabledReason)
	}
	group := subscription.Group
	if group == nil {
		return nil, withStableReason(ErrConflict, SubscriptionResetReasonUpstreamUnavailable)
	}
	before := subscriptionQuotaSnapshot{Windows: card.QuotaWindows}
	attempt, err := s.reserveSubscriptionResetAttempt(ctx, requestID, userID, subscriptionID, subscription.GroupID, group, before, s.now())
	if err != nil {
		if existing, findErr := s.getSubscriptionResetAttemptByRequestID(ctx, requestID); findErr == nil {
			return s.resetReplay(ctx, existing, userID, subscriptionID)
		}
		return nil, err
	}
	updated, resetErr := s.upstream.ResetSubscriptionQuota(ctx, subscriptionID, *group)
	now := s.now()
	if resetErr == nil {
		refreshedSubscription := mergeResetSubscription(*subscription, updated)
		after := subscriptionQuotaSnapshot{Windows: configuredQuotaWindows(refreshedSubscription, nil, group)}
		attempt, err = s.finishSubscriptionResetAttempt(ctx, attempt.ID, "succeeded", intPointer(http.StatusOK), 200, "", "", marshalJSON(after), false, now)
		if err != nil {
			return nil, err
		}
		refreshedCard, err := s.buildSubscriptionCard(ctx, refreshedSubscription, nil, nil, now)
		if err != nil {
			return nil, err
		}
		s.WakeSubscriptionResetReconcile()
		return &SubscriptionResetResult{Operation: *attempt, Subscription: &refreshedCard}, nil
	}
	if errors.Is(resetErr, sub2api.ErrUpstreamRejected) {
		attempt, err = s.finishSubscriptionResetAttempt(ctx, attempt.ID, "failed", upstreamStatusFromError(resetErr), 409, SubscriptionResetReasonUpstreamRejected, resetErr.Error(), "", true, now)
		if err != nil {
			return nil, err
		}
		s.WakeSubscriptionResetReconcile()
		return nil, withStableReason(ErrConflict, SubscriptionResetReasonUpstreamRejected)
	}
	attempt, err = s.finishSubscriptionResetAttempt(ctx, attempt.ID, "uncertain", nil, 202, "result_unknown", resetErr.Error(), "", false, now)
	if err != nil {
		return nil, err
	}
	pendingCard, err := s.buildSubscriptionCard(ctx, *subscription, progress, nil, now)
	if err != nil {
		return nil, err
	}
	s.WakeSubscriptionResetReconcile()
	return &SubscriptionResetResult{Operation: *attempt, Subscription: &pendingCard}, nil
}

func (s *Service) resetReplay(ctx context.Context, attempt *models.SubscriptionResetAttempt, userID, subscriptionID int64) (*SubscriptionResetResult, error) {
	if attempt.UpstreamUserID != userID || attempt.UpstreamSubscriptionID != subscriptionID {
		return nil, withStableReason(ErrConflict, SubscriptionResetReasonRequestIDConflict)
	}
	return &SubscriptionResetResult{Operation: *attempt, Subscription: s.subscriptionCardForAttempt(ctx, attempt)}, nil
}

func mergeResetSubscription(before sub2api.Subscription, updated *sub2api.Subscription) sub2api.Subscription {
	if updated == nil {
		return before
	}
	merged := before
	merged.DailyUsageUSD = updated.DailyUsageUSD
	merged.WeeklyUsageUSD = updated.WeeklyUsageUSD
	merged.MonthlyUsageUSD = updated.MonthlyUsageUSD
	merged.DailyWindowStart = updated.DailyWindowStart
	merged.WeeklyWindowStart = updated.WeeklyWindowStart
	merged.MonthlyWindowStart = updated.MonthlyWindowStart
	if !updated.StartsAt.IsZero() {
		merged.StartsAt = updated.StartsAt
	}
	if !updated.ExpiresAt.IsZero() {
		merged.ExpiresAt = updated.ExpiresAt
	}
	if updated.Status != "" {
		merged.Status = updated.Status
	}
	return merged
}

func intPointer(value int) *int { return &value }

func upstreamStatusFromError(err error) *int {
	var operationErr *sub2api.OperationError
	if errors.As(err, &operationErr) && operationErr.Status > 0 {
		return intPointer(operationErr.Status)
	}
	return nil
}

func (s *Service) subscriptionCardForAttempt(ctx context.Context, attempt *models.SubscriptionResetAttempt) *SubscriptionCard {
	if attempt == nil || s == nil || s.upstream == nil {
		return s.fallbackSubscriptionCardForAttempt(ctx, attempt)
	}
	subscription, err := s.upstream.GetSubscription(ctx, attempt.UpstreamSubscriptionID)
	if err != nil || subscription.UserID != attempt.UpstreamUserID {
		return s.fallbackSubscriptionCardForAttempt(ctx, attempt)
	}
	if subscription.Group == nil {
		groups, groupsErr := s.upstream.ListGroupsAll(ctx)
		if groupsErr != nil {
			return s.fallbackSubscriptionCardForAttempt(ctx, attempt)
		}
		for i := range groups {
			if groups[i].ID == subscription.GroupID {
				group := groups[i]
				subscription.Group = &group
				break
			}
		}
	}
	progress, progressErr := s.upstream.GetSubscriptionProgress(ctx, subscription.ID)
	card, buildErr := s.buildSubscriptionCard(ctx, *subscription, progress, progressErr, s.now())
	if buildErr != nil {
		return s.fallbackSubscriptionCardForAttempt(ctx, attempt)
	}
	return &card
}

func (s *Service) fallbackSubscriptionCardForAttempt(ctx context.Context, attempt *models.SubscriptionResetAttempt) *SubscriptionCard {
	if attempt == nil || s == nil || s.db() == nil {
		return nil
	}
	now := s.now()
	card := SubscriptionCard{
		ID:               attempt.UpstreamSubscriptionID,
		OperationPending: attempt.Status == "reserved" || attempt.Status == "uncertain",
		CanReset:         false,
		DisabledReason:   SubscriptionResetReasonUpstreamUnavailable,
	}
	if attempt.EntitlementType == "bonus_grant" {
		grant, err := s.getSubscriptionResetBonusGrantByID(ctx, attempt.EntitlementID)
		if err != nil || grant.UpstreamUserID != attempt.UpstreamUserID {
			return nil
		}
		card.GroupID = grant.Sub2APIGroupID
		card.StartsAt = grant.StartsAt
		card.ExpiresAt = grant.ExpiresAt
		if grant.ExpiresAt.After(now) {
			card.RemainingDays = int(math.Ceil(grant.ExpiresAt.Sub(now).Hours() / 24))
		}
		remaining := grant.ResetLimit - grant.ResetUsed
		if remaining < 0 {
			remaining = 0
		}
		card.BonusResetRemaining = remaining
		card.TotalResetRemaining = remaining
		card.ExternalPeriod = true
		card.BonusGrants = []SubscriptionResetBonusSummary{{ID: grant.ID, BatchID: grant.BatchID, ResetLimit: grant.ResetLimit, ResetUsed: grant.ResetUsed, ResetRemaining: remaining, ExpiresAt: grant.ExpiresAt, Status: grant.Status}}
	} else {
		if attempt.PeriodID == nil {
			return nil
		}
		period, err := s.getSubscriptionResetPeriodByID(ctx, *attempt.PeriodID)
		if err != nil || period.UpstreamUserID != attempt.UpstreamUserID {
			return nil
		}
		card.GroupID = period.Sub2APIGroupID
		if period.PeriodStart != nil {
			card.StartsAt = period.PeriodStart.UTC()
		}
		if period.PeriodEnd != nil {
			card.ExpiresAt = period.PeriodEnd.UTC()
			if period.PeriodEnd.After(now) {
				card.RemainingDays = int(math.Ceil(period.PeriodEnd.Sub(now).Hours() / 24))
			}
		}
		summary := summarizeResetPeriod(*period)
		if period.PeriodStart != nil && period.PeriodEnd != nil && !now.Before(*period.PeriodStart) && now.Before(*period.PeriodEnd) {
			card.CurrentPeriod = &summary
		} else if period.PeriodStart != nil && now.Before(*period.PeriodStart) {
			card.NextPeriod = &summary
		}
		card.ExternalPeriod = card.CurrentPeriod == nil && card.NextPeriod == nil
		card.ZeroResetLimit = card.CurrentPeriod != nil && card.CurrentPeriod.ResetLimit <= 0
		if card.CurrentPeriod != nil {
			card.BaseResetLimit = card.CurrentPeriod.ResetLimit
			card.BaseResetUsed = card.CurrentPeriod.ResetUsed
			card.BaseResetRemaining = card.CurrentPeriod.ResetRemaining
			card.TotalResetRemaining = card.BaseResetRemaining
		}
	}
	var snapshot subscriptionQuotaSnapshot
	snapshotJSON := attempt.AfterSnapshotJSON
	if strings.TrimSpace(snapshotJSON) == "" {
		snapshotJSON = attempt.BeforeSnapshotJSON
	}
	if json.Unmarshal([]byte(snapshotJSON), &snapshot) == nil {
		card.QuotaWindows = snapshot.Windows
	}
	if card.QuotaWindows == nil {
		card.QuotaWindows = []SubscriptionQuotaWindow{}
	}
	return &card
}

func subscriptionAuthorityError(err error) error {
	var operationErr *sub2api.OperationError
	if errors.As(err, &operationErr) && operationErr.Status == 404 {
		return ErrNotFound
	}
	return withStableReason(ErrUpstreamUnavailable, SubscriptionResetReasonUpstreamUnavailable)
}

func (s *Service) reserveSubscriptionResetAttempt(ctx context.Context, requestID string, userID, subscriptionID, groupID int64, group *sub2api.Group, before subscriptionQuotaSnapshot, now time.Time) (*models.SubscriptionResetAttempt, error) {
	tx, err := s.db().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var blocking int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM subscription_reset_attempts WHERE upstream_subscription_id = ? AND status IN ('reserved', 'uncertain')`, subscriptionID).Scan(&blocking); err != nil {
		return nil, err
	}
	if blocking > 0 {
		return nil, withStableReason(ErrConflict, SubscriptionResetReasonOperationPending)
	}
	type entitlement struct {
		typeName string
		id       int64
		periodID sql.NullInt64
	}
	var selected entitlement
	if err := tx.QueryRowContext(ctx, `
SELECT entitlement_type, entitlement_id, period_id FROM (
  SELECT 'base_period' AS entitlement_type, id AS entitlement_id, id AS period_id,
         period_end AS expires_at, 1 AS type_priority
  FROM subscription_reset_periods
  WHERE upstream_user_id = ? AND sub2api_group_id = ? AND upstream_subscription_id = ?
    AND status = 'active' AND legacy_ignored = 0
    AND period_start IS NOT NULL AND period_start <= ? AND period_end > ?
    AND reset_used < reset_limit
  UNION ALL
  SELECT 'bonus_grant' AS entitlement_type, id AS entitlement_id, NULL AS period_id,
         expires_at, 0 AS type_priority
  FROM subscription_reset_bonus_grants
  WHERE upstream_user_id = ? AND sub2api_group_id = ? AND upstream_subscription_id = ?
    AND status = 'active' AND starts_at <= ? AND expires_at > ?
    AND reset_used < reset_limit
)
ORDER BY expires_at, type_priority, entitlement_id
LIMIT 1
`, userID, groupID, subscriptionID, formatTime(now), formatTime(now), userID, groupID, subscriptionID, formatTime(now), formatTime(now)).Scan(&selected.typeName, &selected.id, &selected.periodID); errors.Is(err, sql.ErrNoRows) {
		return nil, withStableReason(ErrConflict, SubscriptionResetReasonResetExhausted)
	} else if err != nil {
		return nil, err
	}
	var result sql.Result
	switch selected.typeName {
	case "base_period":
		result, err = tx.ExecContext(ctx, `UPDATE subscription_reset_periods SET reset_used = reset_used + 1, updated_at = ? WHERE id = ? AND reset_used < reset_limit AND status = 'active' AND legacy_ignored = 0`, formatTime(now), selected.id)
	case "bonus_grant":
		result, err = tx.ExecContext(ctx, `UPDATE subscription_reset_bonus_grants SET reset_used = reset_used + 1, status = CASE WHEN reset_used + 1 >= reset_limit THEN 'exhausted' ELSE 'active' END, updated_at = ? WHERE id = ? AND reset_used < reset_limit AND status = 'active' AND expires_at > ?`, formatTime(now), selected.id, formatTime(now))
	default:
		return nil, errors.New("unknown reset entitlement type")
	}
	if err != nil {
		return nil, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if affected != 1 {
		return nil, withStableReason(ErrConflict, SubscriptionResetReasonResetExhausted)
	}
	var periodID any
	if selected.periodID.Valid {
		periodID = selected.periodID.Int64
	}
	result, err = tx.ExecContext(ctx, `
INSERT INTO subscription_reset_attempts (
  request_id, period_id, entitlement_type, entitlement_id, upstream_user_id, upstream_subscription_id,
  reset_daily, reset_weekly, reset_monthly, status, before_snapshot_json,
  response_status, reserved_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'reserved', ?, 202, ?, ?, ?)
`, requestID, periodID, selected.typeName, selected.id, userID, subscriptionID, boolInt(positiveAppLimit(group.DailyLimitUSD)), boolInt(positiveAppLimit(group.WeeklyLimitUSD)), boolInt(positiveAppLimit(group.MonthlyLimitUSD)), marshalJSON(before), formatTime(now), formatTime(now), formatTime(now))
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

func (s *Service) finishSubscriptionResetAttempt(ctx context.Context, attemptID int64, status string, upstreamStatus *int, responseStatus int, reason, errorMessage, afterSnapshot string, release bool, now time.Time) (*models.SubscriptionResetAttempt, error) {
	tx, err := s.db().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	attempt, err := getSubscriptionResetAttemptTx(ctx, tx, attemptID)
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE subscription_reset_attempts SET status = ?, upstream_status = ?, response_status = ?, response_reason = ?, error_message = ?, after_snapshot_json = ?, completed_at = ?, updated_at = ? WHERE id = ? AND status = 'reserved' AND resolution = ''`, status, upstreamStatus, responseStatus, reason, errorMessage, afterSnapshot, formatTime(now), formatTime(now), attemptID)
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
		if err := releaseSubscriptionResetEntitlementTx(ctx, tx, attempt, now); err != nil {
			return nil, err
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
SELECT a.upstream_user_id, COALESCE(p.sub2api_group_id, grant.sub2api_group_id)
FROM subscription_reset_attempts a
LEFT JOIN subscription_reset_periods p ON a.entitlement_type = 'base_period' AND p.id = a.entitlement_id
LEFT JOIN subscription_reset_bonus_grants grant ON a.entitlement_type = 'bonus_grant' AND grant.id = a.entitlement_id
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
		if err := releaseSubscriptionResetEntitlementTx(ctx, tx, attempt, now); err != nil {
			return nil, err
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

func releaseSubscriptionResetEntitlementTx(ctx context.Context, tx *sql.Tx, attempt *models.SubscriptionResetAttempt, now time.Time) error {
	if attempt == nil {
		return errors.New("reset attempt is missing")
	}
	var result sql.Result
	var err error
	switch attempt.EntitlementType {
	case "base_period":
		result, err = tx.ExecContext(ctx, `UPDATE subscription_reset_periods SET reset_used = reset_used - 1, updated_at = ? WHERE id = ? AND reset_used > 0`, formatTime(now), attempt.EntitlementID)
	case "bonus_grant":
		result, err = tx.ExecContext(ctx, `UPDATE subscription_reset_bonus_grants SET reset_used = reset_used - 1, status = CASE WHEN status IN ('revoked', 'expired') THEN status WHEN expires_at <= ? THEN 'expired' ELSE 'active' END, updated_at = ? WHERE id = ? AND reset_used > 0`, formatTime(now), formatTime(now), attempt.EntitlementID)
	default:
		return errors.New("unknown reset entitlement type")
	}
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errors.New("reserved reset count is missing")
	}
	return nil
}

func (s *Service) ListPendingSubscriptionResetAttempts(ctx context.Context) ([]SubscriptionResetAttemptAdminView, error) {
	rows, err := s.db().QueryContext(ctx, subscriptionResetAttemptSelectSQL()+` WHERE status IN ('reserved', 'uncertain') ORDER BY reserved_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var attempts []models.SubscriptionResetAttempt
	for rows.Next() {
		attempt, scanErr := scanSubscriptionResetAttempt(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		attempts = append(attempts, *attempt)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	out := make([]SubscriptionResetAttemptAdminView, 0, len(attempts))
	for i := range attempts {
		item, loadErr := s.subscriptionResetAttemptAdminView(ctx, attempts[i])
		if loadErr != nil {
			return nil, loadErr
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Service) subscriptionResetAttemptAdminView(ctx context.Context, attempt models.SubscriptionResetAttempt) (SubscriptionResetAttemptAdminView, error) {
	item := SubscriptionResetAttemptAdminView{SubscriptionResetAttempt: attempt}
	if err := s.db().QueryRowContext(ctx, `SELECT email, username FROM upstream_users WHERE upstream_user_id = ?`, attempt.UpstreamUserID).Scan(&item.Email, &item.Username); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return item, err
	}
	switch attempt.EntitlementType {
	case "base_period":
		if attempt.PeriodID == nil {
			return item, errors.New("base period reset attempt is missing period_id")
		}
		period, err := s.getSubscriptionResetPeriodByID(ctx, *attempt.PeriodID)
		if err != nil {
			return item, err
		}
		item.Period = period
	case "bonus_grant":
		grant, err := s.getSubscriptionResetBonusGrantByID(ctx, attempt.EntitlementID)
		if err != nil {
			return item, err
		}
		item.BonusGrant = grant
	default:
		return item, errors.New("unknown reset entitlement type")
	}
	var before subscriptionQuotaSnapshot
	if err := json.Unmarshal([]byte(attempt.BeforeSnapshotJSON), &before); err != nil {
		item.SnapshotError = "invalid_before_snapshot"
	} else {
		item.BeforeSnapshot = before.Windows
	}
	if strings.TrimSpace(attempt.AfterSnapshotJSON) != "" {
		var after subscriptionQuotaSnapshot
		if err := json.Unmarshal([]byte(attempt.AfterSnapshotJSON), &after); err != nil {
			item.SnapshotError = "invalid_after_snapshot"
		} else {
			item.AfterSnapshot = after.Windows
		}
	}
	if len(item.BeforeSnapshot) == 0 || s.upstream == nil {
		item.CurrentSnapshotError = SubscriptionResetReasonUpstreamUnavailable
		return item, nil
	}
	progress, progressErr := s.upstream.GetSubscriptionProgress(ctx, attempt.UpstreamSubscriptionID)
	if progressErr != nil {
		item.CurrentSnapshotError = SubscriptionResetReasonUpstreamUnavailable
		return item, nil
	}
	item.CurrentSnapshot = currentSnapshotFromProgress(item.BeforeSnapshot, progress)
	return item, nil
}

func currentSnapshotFromProgress(before []SubscriptionQuotaWindow, progress *sub2api.SubscriptionProgress) []SubscriptionQuotaWindow {
	out := make([]SubscriptionQuotaWindow, 0, len(before))
	for _, window := range before {
		var current *sub2api.UsageWindowProgress
		if progress != nil {
			switch window.Kind {
			case "daily":
				current = progress.Daily
			case "weekly":
				current = progress.Weekly
			case "monthly":
				current = progress.Monthly
			}
		}
		window.UsedUSD = 0
		window.RemainingUSD = window.LimitUSD
		window.WindowStart = nil
		window.ResetsAt = nil
		if current != nil {
			if current.LimitUSD > 0 {
				window.LimitUSD = current.LimitUSD
			}
			window.UsedUSD = current.UsedUSD
			window.RemainingUSD = current.RemainingUSD
			window.WindowStart = current.WindowStart
			window.ResetsAt = current.ResetsAt
		}
		out = append(out, window)
	}
	return out
}

func (s *Service) ListSubscriptionResetBackfillRuns(ctx context.Context) ([]models.SubscriptionResetBackfillRun, error) {
	rows, err := s.db().QueryContext(ctx, `SELECT id, tier_id, reset_limit, status, total_records, processed_records, granted_records, error_message, retry_count, last_error_at, triggered_at, started_at, completed_at, updated_at FROM subscription_reset_backfill_runs ORDER BY triggered_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.SubscriptionResetBackfillRun{}
	for rows.Next() {
		var item models.SubscriptionResetBackfillRun
		var triggeredAt, updatedAt string
		var lastErrorAt, startedAt, completedAt sql.NullString
		if err := rows.Scan(&item.ID, &item.TierID, &item.ResetLimit, &item.Status, &item.TotalRecords, &item.ProcessedRecords, &item.GrantedRecords, &item.ErrorMessage, &item.RetryCount, &lastErrorAt, &triggeredAt, &startedAt, &completedAt, &updatedAt); err != nil {
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
		if item.LastErrorAt, err = scanMaybeTime(lastErrorAt); err != nil {
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
	return `SELECT id, request_id, period_id, entitlement_type, entitlement_id, upstream_user_id, upstream_subscription_id, reset_daily, reset_weekly, reset_monthly, status, before_snapshot_json, after_snapshot_json, upstream_status, response_status, response_reason, error_message, resolution, reserved_at, completed_at, confirmed_at, confirmed_by_user_id, created_at, updated_at FROM subscription_reset_attempts`
}

func scanSubscriptionResetAttempt(scanner interface{ Scan(...any) error }) (*models.SubscriptionResetAttempt, error) {
	var out models.SubscriptionResetAttempt
	var resetDaily, resetWeekly, resetMonthly int
	var periodID sql.NullInt64
	var upstreamStatus sql.NullInt64
	var reservedAt, createdAt, updatedAt string
	var completedAt, confirmedAt sql.NullString
	var confirmedBy sql.NullInt64
	if err := scanner.Scan(&out.ID, &out.RequestID, &periodID, &out.EntitlementType, &out.EntitlementID, &out.UpstreamUserID, &out.UpstreamSubscriptionID, &resetDaily, &resetWeekly, &resetMonthly, &out.Status, &out.BeforeSnapshotJSON, &out.AfterSnapshotJSON, &upstreamStatus, &out.ResponseStatus, &out.ResponseReason, &out.ErrorMessage, &out.Resolution, &reservedAt, &completedAt, &confirmedAt, &confirmedBy, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	out.PeriodID = parseNullableInt64(periodID)
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
