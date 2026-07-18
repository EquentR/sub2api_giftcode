package app

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"sub2api-giftcode/backend/internal/models"
	"sub2api-giftcode/backend/internal/sub2api"
)

func (s *Service) ListBalanceTiers(ctx context.Context) ([]models.BalanceTier, error) {
	tiers, err := s.listRedeemTiersRaw(ctx, "balance", false)
	if err != nil {
		return nil, err
	}
	out := make([]models.BalanceTier, 0, len(tiers))
	for _, tier := range tiers {
		out = append(out, models.BalanceTier{
			ID:                   tier.ID,
			Amount:               tier.Amount,
			PayAmountCny:         tier.PayAmountCny,
			OriginalPayAmountCny: tier.OriginalPayAmountCny,
			Label:                tier.Label,
			Enabled:              tier.Enabled,
			SortOrder:            tier.SortOrder,
			CreatedAt:            tier.CreatedAt,
			UpdatedAt:            tier.UpdatedAt,
		})
	}
	return out, nil
}

func (s *Service) ReplaceBalanceTiers(ctx context.Context, tiers []models.BalanceTier) ([]models.BalanceTier, error) {
	redeemTiers := make([]models.RedeemTier, 0, len(tiers))
	for _, tier := range tiers {
		redeemTiers = append(redeemTiers, models.RedeemTier{
			ID:                   tier.ID,
			CodeType:             "balance",
			Amount:               tier.Amount,
			PayAmountCny:         tier.PayAmountCny,
			OriginalPayAmountCny: tier.OriginalPayAmountCny,
			Label:                tier.Label,
			Enabled:              tier.Enabled,
			SortOrder:            tier.SortOrder,
		})
	}
	updated, err := s.replaceRedeemTiers(ctx, redeemTiers, "balance")
	if err != nil {
		return nil, err
	}
	out := make([]models.BalanceTier, 0, len(updated))
	for _, tier := range updated {
		out = append(out, models.BalanceTier{
			ID:                   tier.ID,
			Amount:               tier.Amount,
			PayAmountCny:         tier.PayAmountCny,
			OriginalPayAmountCny: tier.OriginalPayAmountCny,
			Label:                tier.Label,
			Enabled:              tier.Enabled,
			SortOrder:            tier.SortOrder,
			CreatedAt:            tier.CreatedAt,
			UpdatedAt:            tier.UpdatedAt,
		})
	}
	return out, nil
}

func (s *Service) ListRedeemTiers(ctx context.Context, enabledOnly bool) ([]models.RedeemTier, error) {
	tiers, err := s.listRedeemTiersRaw(ctx, "", enabledOnly)
	if err != nil {
		return nil, err
	}
	s.enrichRedeemTiersWithGroups(ctx, tiers)
	return tiers, nil
}

func (s *Service) ReplaceRedeemTiers(ctx context.Context, tiers []models.RedeemTier) ([]models.RedeemTier, error) {
	return s.replaceRedeemTiers(ctx, tiers, "")
}

func (s *Service) replaceRedeemTiers(ctx context.Context, tiers []models.RedeemTier, scopeCodeType string) ([]models.RedeemTier, error) {
	if len(tiers) == 0 {
		return nil, ErrBadRequest
	}
	if err := s.validateRedeemTiers(ctx, tiers); err != nil {
		return nil, err
	}
	now := s.now()
	tx, err := s.db().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}
	kept := map[int64]struct{}{}
	out := make([]models.RedeemTier, 0, len(tiers))
	for _, tier := range tiers {
		tier = normalizeRedeemTier(tier)
		tier.Enabled = tier.Enabled
		tier.UpdatedAt = now
		if tier.ID > 0 {
			tier.CreatedAt = now
			if _, err := tx.ExecContext(ctx, `
INSERT INTO redeem_tiers (
  id, code_type, amount, pay_amount_cny, original_pay_amount_cny, label, enabled, sort_order,
  sub2api_group_id, validity_days, concurrency, reset_count, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  code_type = excluded.code_type,
  amount = excluded.amount,
  pay_amount_cny = excluded.pay_amount_cny,
  original_pay_amount_cny = excluded.original_pay_amount_cny,
  label = excluded.label,
  enabled = excluded.enabled,
  sort_order = excluded.sort_order,
  sub2api_group_id = excluded.sub2api_group_id,
  validity_days = excluded.validity_days,
  concurrency = excluded.concurrency,
  reset_count = excluded.reset_count,
  updated_at = excluded.updated_at
`, tier.ID, tier.CodeType, tier.Amount, tier.PayAmountCny, tier.OriginalPayAmountCny, tier.Label, boolToInt(tier.Enabled), tier.SortOrder, tier.Sub2APIGroupID, tier.ValidityDays, tier.Concurrency, tier.ResetCount, formatTime(tier.CreatedAt), formatTime(tier.UpdatedAt)); err != nil {
				return nil, rollback(err)
			}
			if err := upsertBalanceTierMirror(ctx, tx, tier); err != nil {
				return nil, rollback(err)
			}
			kept[tier.ID] = struct{}{}
			out = append(out, tier)
			continue
		}
		tier.CreatedAt = now
		res, err := tx.ExecContext(ctx, `
INSERT INTO redeem_tiers (
  code_type, amount, pay_amount_cny, original_pay_amount_cny, label, enabled, sort_order,
  sub2api_group_id, validity_days, concurrency, reset_count, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, tier.CodeType, tier.Amount, tier.PayAmountCny, tier.OriginalPayAmountCny, tier.Label, boolToInt(tier.Enabled), tier.SortOrder, tier.Sub2APIGroupID, tier.ValidityDays, tier.Concurrency, tier.ResetCount, formatTime(tier.CreatedAt), formatTime(tier.UpdatedAt))
		if err != nil {
			return nil, rollback(err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, rollback(err)
		}
		tier.ID = id
		if err := upsertBalanceTierMirror(ctx, tx, tier); err != nil {
			return nil, rollback(err)
		}
		kept[id] = struct{}{}
		out = append(out, tier)
	}
	query := `SELECT id FROM redeem_tiers`
	args := []any{}
	if strings.TrimSpace(scopeCodeType) != "" {
		query += ` WHERE code_type = ?`
		args = append(args, strings.TrimSpace(scopeCodeType))
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, rollback(err)
	}
	defer rows.Close()
	var allIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, rollback(err)
		}
		allIDs = append(allIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, rollback(err)
	}
	if err := rows.Close(); err != nil {
		return nil, rollback(err)
	}
	for _, id := range allIDs {
		if _, ok := kept[id]; ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE redeem_tiers SET enabled = 0, updated_at = ? WHERE id = ?`, formatTime(now), id); err != nil {
			return nil, rollback(err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE redeem_balance_tiers SET enabled = 0, updated_at = ? WHERE id = ?`, formatTime(now), id); err != nil {
			return nil, rollback(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.WakeSubscriptionResetReconcile()
	return out, nil
}

func upsertBalanceTierMirror(ctx context.Context, tx *sql.Tx, tier models.RedeemTier) error {
	if tier.CodeType != "balance" {
		if tier.ID <= 0 {
			return nil
		}
		_, err := tx.ExecContext(ctx, `UPDATE redeem_balance_tiers SET enabled = 0, updated_at = ? WHERE id = ?`, formatTime(tier.UpdatedAt), tier.ID)
		return err
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO redeem_balance_tiers (id, amount, pay_amount_cny, original_pay_amount_cny, label, enabled, sort_order, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  amount = excluded.amount,
  pay_amount_cny = excluded.pay_amount_cny,
  original_pay_amount_cny = excluded.original_pay_amount_cny,
  label = excluded.label,
  enabled = excluded.enabled,
  sort_order = excluded.sort_order,
  updated_at = excluded.updated_at
`, tier.ID, tier.Amount, tier.PayAmountCny, tier.OriginalPayAmountCny, tier.Label, boolToInt(tier.Enabled), tier.SortOrder, formatTime(tier.CreatedAt), formatTime(tier.UpdatedAt))
	return err
}

func (s *Service) getRedeemTierByID(ctx context.Context, id int64) (*models.RedeemTier, error) {
	row := s.db().QueryRowContext(ctx, `
SELECT id, code_type, amount, pay_amount_cny, original_pay_amount_cny, label, enabled, sort_order,
       sub2api_group_id, validity_days, concurrency, reset_count, legacy_reset_backfill_eligible,
       created_at, updated_at
FROM redeem_tiers
WHERE id = ?
`, id)
	return scanRedeemTierRow(row)
}

func (s *Service) getBalanceTierByID(ctx context.Context, id int64) (*models.BalanceTier, error) {
	tier, err := s.getRedeemTierByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if tier.CodeType != "balance" {
		return nil, sql.ErrNoRows
	}
	return &models.BalanceTier{
		ID:                   tier.ID,
		Amount:               tier.Amount,
		PayAmountCny:         tier.PayAmountCny,
		OriginalPayAmountCny: tier.OriginalPayAmountCny,
		Label:                tier.Label,
		Enabled:              tier.Enabled,
		SortOrder:            tier.SortOrder,
		CreatedAt:            tier.CreatedAt,
		UpdatedAt:            tier.UpdatedAt,
	}, nil
}

func (s *Service) listRedeemTiersRaw(ctx context.Context, codeType string, enabledOnly bool) ([]models.RedeemTier, error) {
	query := `
SELECT id, code_type, amount, pay_amount_cny, original_pay_amount_cny, label, enabled, sort_order,
       sub2api_group_id, validity_days, concurrency, reset_count, legacy_reset_backfill_eligible,
       created_at, updated_at
FROM redeem_tiers
`
	args := make([]any, 0, 2)
	clauses := make([]string, 0, 2)
	if strings.TrimSpace(codeType) != "" {
		clauses = append(clauses, "code_type = ?")
		args = append(args, strings.TrimSpace(codeType))
	}
	if enabledOnly {
		clauses = append(clauses, "enabled = 1")
	}
	if len(clauses) > 0 {
		query += "WHERE " + strings.Join(clauses, " AND ") + " "
	}
	query += "ORDER BY sort_order ASC, id ASC"
	rows, err := s.db().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.RedeemTier, 0)
	for rows.Next() {
		tier, err := scanRedeemTierRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *tier)
	}
	return out, rows.Err()
}

func (s *Service) validateRedeemTiers(ctx context.Context, tiers []models.RedeemTier) error {
	groupMap := map[int64]sub2api.Group{}
	needsGroups := false
	for _, tier := range tiers {
		if normalizeCodeType(tier.CodeType) == "subscription" {
			needsGroups = true
			break
		}
	}
	if needsGroups && s.upstream != nil {
		groups, err := s.ListSubscriptionGroups(ctx)
		if err != nil {
			return err
		}
		for _, group := range groups {
			groupMap[group.ID] = group
		}
	}
	groupConcurrency := make(map[int64]int)
	for i := range tiers {
		if tiers[i].ResetCount < 0 {
			return ErrBadRequest
		}
		tier := normalizeRedeemTier(tiers[i])
		if tier.CodeType == "subscription" && tier.Sub2APIGroupID != nil {
			if group, ok := groupMap[*tier.Sub2APIGroupID]; ok &&
				group.DailyLimitUSD == nil && group.WeeklyLimitUSD == nil && group.MonthlyLimitUSD == nil {
				tier.ResetCount = 0
			}
		}
		tiers[i] = tier
		switch tier.CodeType {
		case "balance":
			if tier.Amount <= 0 || tier.PayAmountCny <= 0 {
				return ErrBadRequest
			}
		case "subscription":
			if tier.PayAmountCny <= 0 || tier.Sub2APIGroupID == nil || *tier.Sub2APIGroupID <= 0 || tier.ValidityDays <= 0 || tier.Concurrency <= 0 {
				return ErrBadRequest
			}
			if concurrency, ok := groupConcurrency[*tier.Sub2APIGroupID]; ok && concurrency != tier.Concurrency {
				return &TierConcurrencyConflictError{
					GroupID:                *tier.Sub2APIGroupID,
					ExistingConcurrency:    concurrency,
					ConflictingConcurrency: tier.Concurrency,
				}
			}
			groupConcurrency[*tier.Sub2APIGroupID] = tier.Concurrency
			if needsGroups && s.upstream != nil {
				if _, ok := groupMap[*tier.Sub2APIGroupID]; !ok {
					return ErrNotFound
				}
			}
		default:
			return ErrBadRequest
		}
	}
	return nil
}

func normalizeRedeemTier(tier models.RedeemTier) models.RedeemTier {
	tier.CodeType = normalizeCodeType(tier.CodeType)
	if tier.OriginalPayAmountCny != nil && *tier.OriginalPayAmountCny <= 0 {
		tier.OriginalPayAmountCny = nil
	}
	if tier.CodeType == "balance" {
		tier.Sub2APIGroupID = nil
		tier.ValidityDays = 0
		tier.Concurrency = 0
		tier.ResetCount = 0
		if strings.TrimSpace(tier.Label) == "" {
			tier.Label = fmt.Sprintf("$%.0f", tier.Amount)
		}
	} else if tier.CodeType == "subscription" {
		tier.Amount = 0
		if strings.TrimSpace(tier.Label) == "" {
			tier.Label = fmt.Sprintf("Subscription %d days", tier.ValidityDays)
		}
	}
	tier.Label = strings.TrimSpace(tier.Label)
	return tier
}

func normalizeCodeType(codeType string) string {
	switch strings.ToLower(strings.TrimSpace(codeType)) {
	case "subscription":
		return "subscription"
	default:
		return "balance"
	}
}

func (s *Service) ListSubscriptionGroups(ctx context.Context) ([]sub2api.Group, error) {
	if err := s.requireUpstreamClient(); err != nil {
		return nil, err
	}
	groups, err := s.upstream.ListGroupsAll(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]sub2api.Group, 0, len(groups))
	for _, group := range groups {
		if strings.EqualFold(strings.TrimSpace(group.SubscriptionType), "subscription") {
			out = append(out, group)
		}
	}
	return out, nil
}

func (s *Service) ListOpenAIAccounts(ctx context.Context) ([]sub2api.Account, error) {
	if err := s.requireUpstreamClient(); err != nil {
		return nil, err
	}
	accounts, err := s.upstream.ListOpenAIAccounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]sub2api.Account, 0, len(accounts))
	for _, account := range accounts {
		if strings.EqualFold(strings.TrimSpace(account.Platform), "openai") {
			out = append(out, account)
		}
	}
	return out, nil
}

func (s *Service) UpdateOpenAIAccountUserAgent(ctx context.Context, accountID int64, userAgent string) (*sub2api.Account, error) {
	if accountID <= 0 {
		return nil, ErrBadRequest
	}
	if err := s.requireUpstreamClient(); err != nil {
		return nil, err
	}
	return s.upstream.UpdateOpenAIAccountUserAgent(ctx, accountID, strings.TrimSpace(userAgent))
}

func (s *Service) subscriptionGroupByID(ctx context.Context, groupID int64) (*sub2api.Group, error) {
	groups, err := s.ListSubscriptionGroups(ctx)
	if err != nil {
		return nil, err
	}
	for i := range groups {
		if groups[i].ID == groupID {
			return &groups[i], nil
		}
	}
	return nil, ErrNotFound
}

func (s *Service) enrichRedeemTiersWithGroups(ctx context.Context, tiers []models.RedeemTier) {
	needsGroups := false
	for i := range tiers {
		if tiers[i].CodeType == "balance" {
			tiers[i].UpstreamAvailable = true
			continue
		}
		if tiers[i].CodeType == "subscription" {
			needsGroups = true
		}
	}
	if !needsGroups {
		return
	}
	groups, err := s.ListSubscriptionGroups(ctx)
	if err != nil {
		for i := range tiers {
			if tiers[i].CodeType == "subscription" {
				tiers[i].UpstreamAvailable = false
				tiers[i].UpstreamError = err.Error()
			}
		}
		return
	}
	groupByID := make(map[int64]sub2api.Group, len(groups))
	for _, group := range groups {
		groupByID[group.ID] = group
	}
	for i := range tiers {
		if tiers[i].CodeType != "subscription" || tiers[i].Sub2APIGroupID == nil {
			continue
		}
		group, ok := groupByID[*tiers[i].Sub2APIGroupID]
		if !ok {
			tiers[i].UpstreamAvailable = false
			tiers[i].UpstreamError = "subscription group not found"
			continue
		}
		tiers[i].Sub2APIGroupName = group.Name
		tiers[i].Sub2APIGroupPlatform = group.Platform
		tiers[i].Sub2APIDailyLimitUSD = group.DailyLimitUSD
		tiers[i].Sub2APIWeeklyLimitUSD = group.WeeklyLimitUSD
		tiers[i].Sub2APIMonthlyLimitUSD = group.MonthlyLimitUSD
		tiers[i].UpstreamAvailable = true
		tiers[i].UpstreamError = ""
	}
}

func (s *Service) ListUsers(ctx context.Context) ([]UserSummary, error) {
	rows, err := s.db().QueryContext(ctx, `
SELECT
  u.upstream_user_id,
  u.email,
  u.username,
  u.role,
  u.status,
  u.profile_json,
  u.last_seen_at,
  u.created_at,
  u.updated_at,
  (SELECT COUNT(1) FROM redeem_access_requests r WHERE r.requestor_upstream_user_id = u.upstream_user_id) AS access_request_count,
  (SELECT COUNT(1) FROM redeem_requests rr WHERE rr.requestor_upstream_user_id = u.upstream_user_id) AS redeem_request_count,
  (SELECT COUNT(1)
     FROM redeem_codes c
     JOIN redeem_requests rr ON rr.id = c.request_id
    WHERE rr.requestor_upstream_user_id = u.upstream_user_id) AS redeem_code_count,
  (SELECT COUNT(1)
     FROM redeem_codes c
     JOIN redeem_requests rr ON rr.id = c.request_id
    WHERE rr.requestor_upstream_user_id = u.upstream_user_id AND c.status = 'used') AS used_code_count,
  (SELECT COUNT(1)
     FROM redeem_codes c
     JOIN redeem_requests rr ON rr.id = c.request_id
    WHERE rr.requestor_upstream_user_id = u.upstream_user_id AND c.status = 'unused') AS unused_code_count,
  COALESCE((SELECT MAX(created_at) FROM redeem_access_requests r WHERE r.requestor_upstream_user_id = u.upstream_user_id), '') AS latest_request_at,
  COALESCE((SELECT MAX(c.created_at)
     FROM redeem_codes c
     JOIN redeem_requests rr ON rr.id = c.request_id
    WHERE rr.requestor_upstream_user_id = u.upstream_user_id), '') AS latest_code_at
FROM upstream_users u
ORDER BY u.updated_at DESC, u.upstream_user_id DESC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]UserSummary, 0)
	for rows.Next() {
		item, err := scanUserSummaryRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func (s *Service) Stats(ctx context.Context) (*DashboardStats, error) {
	row := s.db().QueryRowContext(ctx, `
SELECT
  (SELECT COUNT(1) FROM upstream_users) AS total_users,
  (SELECT COUNT(1) FROM redeem_access_requests WHERE status = 'pending') AS pending_access_requests,
  (SELECT COUNT(1) FROM redeem_access_requests WHERE status = 'approved') AS approved_access_requests,
  (SELECT COUNT(1) FROM redeem_access_requests WHERE status = 'rejected') AS rejected_access_requests,
  (SELECT COUNT(1) FROM redeem_access_requests WHERE status = 'consumed') AS consumed_access_requests,
  (SELECT COUNT(1) FROM redeem_access_requests WHERE fulfillment_result = 'direct_charge_succeeded') AS direct_charge_access_requests,
  (SELECT COUNT(1) FROM redeem_requests) AS redeem_requests,
  (SELECT COUNT(1) FROM redeem_codes) AS redeem_codes_total,
  (SELECT COUNT(1) FROM redeem_codes WHERE status = 'unused') AS redeem_codes_unused,
  (SELECT COUNT(1) FROM redeem_codes WHERE status = 'used') AS redeem_codes_used,
  (SELECT COUNT(1) FROM redeem_tiers WHERE enabled = 1) AS active_tiers,
  COALESCE((SELECT value FROM sync_state WHERE key = 'redeem_codes_last_sync_at' LIMIT 1), '') AS last_sync_at
`)
	var out DashboardStats
	var lastSync string
	if err := row.Scan(
		&out.TotalUsers,
		&out.PendingAccessRequests,
		&out.ApprovedAccessRequests,
		&out.RejectedAccessRequests,
		&out.ConsumedAccessRequests,
		&out.DirectChargeAccessRequests,
		&out.RedeemRequests,
		&out.RedeemCodesTotal,
		&out.RedeemCodesUnused,
		&out.RedeemCodesUsed,
		&out.ActiveTiers,
		&lastSync,
	); err != nil {
		return nil, err
	}
	if lastSync != "" {
		t, err := parseTime(lastSync)
		if err != nil {
			return nil, err
		}
		out.LastSyncAt = t
	}
	return &out, nil
}

func (s *Service) getSyncState(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db().QueryRowContext(ctx, `SELECT value FROM sync_state WHERE key = ?`, key).Scan(&value)
	if err != nil {
		return "", err
	}
	return value, nil
}

func (s *Service) setSyncState(ctx context.Context, key, value string, updatedAt time.Time) error {
	_, err := s.db().ExecContext(ctx, `
INSERT INTO sync_state (key, value, updated_at) VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
`, key, value, formatTime(updatedAt))
	return err
}

func scanBalanceTierRow(scanner interface {
	Scan(dest ...any) error
}) (*models.BalanceTier, error) {
	var out models.BalanceTier
	var enabled int
	var originalPayAmount sql.NullFloat64
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(&out.ID, &out.Amount, &out.PayAmountCny, &originalPayAmount, &out.Label, &enabled, &out.SortOrder, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	out.Enabled = enabled != 0
	out.OriginalPayAmountCny = parseNullableFloat64(originalPayAmount)
	var err error
	if out.CreatedAt, err = parseNonNullTime(createdAt); err != nil {
		return nil, err
	}
	if out.UpdatedAt, err = parseNonNullTime(updatedAt); err != nil {
		return nil, err
	}
	return &out, nil
}

func scanRedeemTierRow(scanner interface {
	Scan(dest ...any) error
}) (*models.RedeemTier, error) {
	var out models.RedeemTier
	var enabled int
	var groupID sql.NullInt64
	var originalPayAmount sql.NullFloat64
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(
		&out.ID,
		&out.CodeType,
		&out.Amount,
		&out.PayAmountCny,
		&originalPayAmount,
		&out.Label,
		&enabled,
		&out.SortOrder,
		&groupID,
		&out.ValidityDays,
		&out.Concurrency,
		&out.ResetCount,
		&out.LegacyResetBackfillEligible,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}
	out.CodeType = normalizeCodeType(out.CodeType)
	out.Enabled = enabled != 0
	out.Sub2APIGroupID = parseNullableInt64(groupID)
	out.OriginalPayAmountCny = parseNullableFloat64(originalPayAmount)
	var err error
	if out.CreatedAt, err = parseNonNullTime(createdAt); err != nil {
		return nil, err
	}
	if out.UpdatedAt, err = parseNonNullTime(updatedAt); err != nil {
		return nil, err
	}
	return &out, nil
}

func scanUserSummaryRow(scanner interface {
	Scan(dest ...any) error
}) (*UserSummary, error) {
	var out UserSummary
	var profileJSON string
	var lastSeenAt string
	var createdAt string
	var updatedAt string
	var latestRequestAt string
	var latestCodeAt string
	if err := scanner.Scan(
		&out.UpstreamUserID,
		&out.Email,
		&out.Username,
		&out.Role,
		&out.Status,
		&profileJSON,
		&lastSeenAt,
		&createdAt,
		&updatedAt,
		&out.AccessRequestCount,
		&out.RedeemRequestCount,
		&out.RedeemCodeCount,
		&out.UsedCodeCount,
		&out.UnusedCodeCount,
		&latestRequestAt,
		&latestCodeAt,
	); err != nil {
		return nil, err
	}
	out.ProfileJSON = profileJSON
	var err error
	if out.LastSeenAt, err = parseNonNullTime(lastSeenAt); err != nil {
		return nil, err
	}
	if out.CreatedAt, err = parseNonNullTime(createdAt); err != nil {
		return nil, err
	}
	if out.UpdatedAt, err = parseNonNullTime(updatedAt); err != nil {
		return nil, err
	}
	if latestRequestAt != "" {
		out.LatestRequestAt, err = parseTime(latestRequestAt)
		if err != nil {
			return nil, err
		}
	}
	if latestCodeAt != "" {
		out.LatestCodeAt, err = parseTime(latestCodeAt)
		if err != nil {
			return nil, err
		}
	}
	return &out, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
