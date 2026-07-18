package app

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const resetEntitlementQueryBatchSize = 900

type SubscriptionResetEntitlementAdminView struct {
	UpstreamUserID         int64     `json:"upstream_user_id"`
	Username               string    `json:"username"`
	Email                  string    `json:"email"`
	UpstreamSubscriptionID int64     `json:"upstream_subscription_id"`
	Sub2APIGroupID         int64     `json:"sub2api_group_id"`
	GroupName              string    `json:"group_name"`
	StartsAt               time.Time `json:"starts_at"`
	ExpiresAt              time.Time `json:"expires_at"`
	RemainingDays          int       `json:"remaining_days"`
	BaseResetLimit         int       `json:"base_reset_limit"`
	BaseResetUsed          int       `json:"base_reset_used"`
	BaseResetRemaining     int       `json:"base_reset_remaining"`
	BonusResetLimit        int       `json:"bonus_reset_limit"`
	BonusResetUsed         int       `json:"bonus_reset_used"`
	BonusResetRemaining    int       `json:"bonus_reset_remaining"`
	TotalResetRemaining    int       `json:"total_reset_remaining"`
}

func (s *Service) ListSubscriptionResetEntitlements(ctx context.Context) ([]SubscriptionResetEntitlementAdminView, error) {
	if err := s.requireUpstreamClient(); err != nil {
		return nil, withStableReason(ErrUpstreamUnavailable, SubscriptionResetReasonUpstreamUnavailable)
	}
	subscriptions, err := s.upstream.ListAllActiveSubscriptions(ctx)
	if err != nil {
		return nil, withStableReason(ErrUpstreamUnavailable, SubscriptionResetReasonUpstreamUnavailable)
	}

	now := s.now()
	items := make([]SubscriptionResetEntitlementAdminView, 0, len(subscriptions))
	indexBySubscriptionID := make(map[int64]int, len(subscriptions))
	for _, subscription := range subscriptions {
		if !activeSubscriptionAt(subscription, now) {
			continue
		}
		item := SubscriptionResetEntitlementAdminView{
			UpstreamUserID: subscription.UserID, UpstreamSubscriptionID: subscription.ID,
			Sub2APIGroupID: subscription.GroupID, StartsAt: subscription.StartsAt.UTC(), ExpiresAt: subscription.ExpiresAt.UTC(),
			RemainingDays: int(math.Ceil(subscription.ExpiresAt.Sub(now).Hours() / 24)),
		}
		if subscription.User != nil && subscription.User.ID == subscription.UserID {
			item.Username = subscription.User.Username
			item.Email = subscription.User.Email
		}
		if subscription.Group != nil && subscription.Group.ID == subscription.GroupID {
			item.GroupName = subscription.Group.Name
		}
		indexBySubscriptionID[subscription.ID] = len(items)
		items = append(items, item)
	}
	if len(items) == 0 {
		return items, nil
	}

	if err := s.loadSubscriptionResetBaseEntitlements(ctx, items, indexBySubscriptionID, now); err != nil {
		return nil, err
	}
	if err := s.loadSubscriptionResetBonusEntitlements(ctx, items, indexBySubscriptionID, now); err != nil {
		return nil, err
	}
	for i := range items {
		items[i].TotalResetRemaining = items[i].BaseResetRemaining + items[i].BonusResetRemaining
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpstreamUserID != items[j].UpstreamUserID {
			return items[i].UpstreamUserID < items[j].UpstreamUserID
		}
		if items[i].Sub2APIGroupID != items[j].Sub2APIGroupID {
			return items[i].Sub2APIGroupID < items[j].Sub2APIGroupID
		}
		return items[i].UpstreamSubscriptionID < items[j].UpstreamSubscriptionID
	})
	return items, nil
}

func (s *Service) loadSubscriptionResetBaseEntitlements(ctx context.Context, items []SubscriptionResetEntitlementAdminView, indexes map[int64]int, now time.Time) error {
	seen := make(map[int64]struct{})
	for start := 0; start < len(items); start += resetEntitlementQueryBatchSize {
		end := minInt(start+resetEntitlementQueryBatchSize, len(items))
		if err := s.loadSubscriptionResetBaseEntitlementBatch(ctx, items[start:end], items, indexes, seen, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) loadSubscriptionResetBaseEntitlementBatch(ctx context.Context, queryItems, items []SubscriptionResetEntitlementAdminView, indexes map[int64]int, seen map[int64]struct{}, now time.Time) error {
	args, placeholders := resetEntitlementQueryArgs(queryItems)
	args = append(args, formatTime(now), formatTime(now))
	rows, err := s.db().QueryContext(ctx, fmt.Sprintf(`
SELECT upstream_subscription_id, upstream_user_id, sub2api_group_id, reset_limit, reset_used
FROM subscription_reset_periods
WHERE upstream_subscription_id IN (%s)
  AND status = 'active' AND legacy_ignored = 0
  AND period_start IS NOT NULL AND period_end IS NOT NULL
  AND period_start <= ? AND period_end > ?
ORDER BY upstream_subscription_id, id
`, placeholders), args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var subscriptionID, userID, groupID int64
		var limit, used int
		if err := rows.Scan(&subscriptionID, &userID, &groupID, &limit, &used); err != nil {
			return err
		}
		index, ok := indexes[subscriptionID]
		if !ok || items[index].UpstreamUserID != userID || items[index].Sub2APIGroupID != groupID {
			continue
		}
		if _, duplicate := seen[subscriptionID]; duplicate {
			return fmt.Errorf("multiple active reset periods for subscription %d", subscriptionID)
		}
		seen[subscriptionID] = struct{}{}
		items[index].BaseResetLimit = limit
		items[index].BaseResetUsed = used
		items[index].BaseResetRemaining = maxInt(limit-used, 0)
	}
	return rows.Err()
}

func (s *Service) loadSubscriptionResetBonusEntitlements(ctx context.Context, items []SubscriptionResetEntitlementAdminView, indexes map[int64]int, now time.Time) error {
	for start := 0; start < len(items); start += resetEntitlementQueryBatchSize {
		end := minInt(start+resetEntitlementQueryBatchSize, len(items))
		if err := s.loadSubscriptionResetBonusEntitlementBatch(ctx, items[start:end], items, indexes, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) loadSubscriptionResetBonusEntitlementBatch(ctx context.Context, queryItems, items []SubscriptionResetEntitlementAdminView, indexes map[int64]int, now time.Time) error {
	args, placeholders := resetEntitlementQueryArgs(queryItems)
	args = append(args, formatTime(now), formatTime(now))
	rows, err := s.db().QueryContext(ctx, fmt.Sprintf(`
SELECT upstream_subscription_id, upstream_user_id, sub2api_group_id, reset_limit, reset_used
FROM subscription_reset_bonus_grants
WHERE upstream_subscription_id IN (%s)
  AND status IN ('active', 'exhausted')
  AND starts_at <= ? AND expires_at > ?
ORDER BY upstream_subscription_id, id
`, placeholders), args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var subscriptionID, userID, groupID int64
		var limit, used int
		if err := rows.Scan(&subscriptionID, &userID, &groupID, &limit, &used); err != nil {
			return err
		}
		index, ok := indexes[subscriptionID]
		if !ok || items[index].UpstreamUserID != userID || items[index].Sub2APIGroupID != groupID {
			continue
		}
		items[index].BonusResetLimit += limit
		items[index].BonusResetUsed += used
		items[index].BonusResetRemaining += maxInt(limit-used, 0)
	}
	return rows.Err()
}

func resetEntitlementQueryArgs(items []SubscriptionResetEntitlementAdminView) ([]any, string) {
	args := make([]any, 0, len(items)+2)
	for _, item := range items {
		args = append(args, item.UpstreamSubscriptionID)
	}
	return args, strings.TrimSuffix(strings.Repeat("?,", len(items)), ",")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
