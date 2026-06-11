package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"sub2api-giftcode/backend/internal/models"
)

func (s *Service) ListBalanceTiers(ctx context.Context) ([]models.BalanceTier, error) {
	rows, err := s.db().QueryContext(ctx, `
SELECT id, amount, pay_amount_cny, label, enabled, sort_order, created_at, updated_at
FROM redeem_balance_tiers
ORDER BY sort_order ASC, id ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.BalanceTier, 0)
	for rows.Next() {
		tier, err := scanBalanceTierRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *tier)
	}
	return out, rows.Err()
}

func (s *Service) ReplaceBalanceTiers(ctx context.Context, tiers []models.BalanceTier) ([]models.BalanceTier, error) {
	if len(tiers) == 0 {
		return nil, ErrBadRequest
	}
	now := s.now()
	tx, err := s.db().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}
	kept := map[int64]struct{}{}
	out := make([]models.BalanceTier, 0, len(tiers))
	for _, tier := range tiers {
		if tier.Amount <= 0 {
			return nil, ErrBadRequest
		}
		if tier.PayAmountCny <= 0 {
			return nil, ErrBadRequest
		}
		if strings.TrimSpace(tier.Label) == "" {
			tier.Label = fmt.Sprintf("$%.0f", tier.Amount)
		}
		tier.Enabled = tier.Enabled
		tier.UpdatedAt = now
		if tier.ID > 0 {
			tier.CreatedAt = now
			if _, err := tx.ExecContext(ctx, `
INSERT INTO redeem_balance_tiers (id, amount, pay_amount_cny, label, enabled, sort_order, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  amount = excluded.amount,
  pay_amount_cny = excluded.pay_amount_cny,
  label = excluded.label,
  enabled = excluded.enabled,
  sort_order = excluded.sort_order,
  updated_at = excluded.updated_at
`, tier.ID, tier.Amount, tier.PayAmountCny, tier.Label, boolToInt(tier.Enabled), tier.SortOrder, formatTime(tier.CreatedAt), formatTime(tier.UpdatedAt)); err != nil {
				return nil, rollback(err)
			}
			kept[tier.ID] = struct{}{}
			out = append(out, tier)
			continue
		}
		tier.CreatedAt = now
		res, err := tx.ExecContext(ctx, `
INSERT INTO redeem_balance_tiers (amount, pay_amount_cny, label, enabled, sort_order, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
`, tier.Amount, tier.PayAmountCny, tier.Label, boolToInt(tier.Enabled), tier.SortOrder, formatTime(tier.CreatedAt), formatTime(tier.UpdatedAt))
		if err != nil {
			return nil, rollback(err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, rollback(err)
		}
		tier.ID = id
		kept[id] = struct{}{}
		out = append(out, tier)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM redeem_balance_tiers`)
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
	for _, id := range allIDs {
		if _, ok := kept[id]; ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE redeem_balance_tiers SET enabled = 0, updated_at = ? WHERE id = ?`, formatTime(now), id); err != nil {
			return nil, rollback(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) getBalanceTierByID(ctx context.Context, id int64) (*models.BalanceTier, error) {
	row := s.db().QueryRowContext(ctx, `
SELECT id, amount, pay_amount_cny, label, enabled, sort_order, created_at, updated_at
FROM redeem_balance_tiers
WHERE id = ?
`, id)
	return scanBalanceTierRow(row)
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
  (SELECT COUNT(1) FROM redeem_requests) AS redeem_requests,
  (SELECT COUNT(1) FROM redeem_codes) AS redeem_codes_total,
  (SELECT COUNT(1) FROM redeem_codes WHERE status = 'unused') AS redeem_codes_unused,
  (SELECT COUNT(1) FROM redeem_codes WHERE status = 'used') AS redeem_codes_used,
  (SELECT COUNT(1) FROM redeem_balance_tiers WHERE enabled = 1) AS active_tiers,
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
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(&out.ID, &out.Amount, &out.PayAmountCny, &out.Label, &enabled, &out.SortOrder, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	out.Enabled = enabled != 0
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
