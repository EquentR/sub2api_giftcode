package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"sub2api-giftcode/backend/internal/models"
	"sub2api-giftcode/backend/internal/sub2api"
)

const (
	compensationBatchStatusRunning               = "running"
	compensationBatchStatusCompleted             = "completed"
	compensationBatchStatusCompletedWithFailures = "completed_with_failures"
	compensationBatchStatusFailed                = "failed"
)

type CompensationBatchInput struct {
	SubscriptionDays int      `json:"subscription_days"`
	BalanceAmount    float64  `json:"balance_amount"`
	ExcludedDomains  []string `json:"excluded_domains"`
	Note             string   `json:"note"`
}

func (s *Service) RunCompensationBatch(ctx context.Context, operator *SessionUser, input CompensationBatchInput) (*models.CompensationBatch, error) {
	if err := s.requireUpstreamClient(); err != nil {
		return nil, err
	}
	if s.db() == nil {
		return nil, errors.New("database not configured")
	}
	if operator == nil || !operator.IsAdmin {
		return nil, ErrForbidden
	}
	input = normalizeCompensationBatchInput(input)
	if input.SubscriptionDays <= 0 || input.BalanceAmount <= 0 {
		return nil, ErrBadRequest
	}

	now := s.now()
	batchKey, err := newRandomToken(12)
	if err != nil {
		return nil, err
	}
	batch := models.CompensationBatch{
		BatchKey:               batchKey,
		SubscriptionDays:       input.SubscriptionDays,
		BalanceAmount:          input.BalanceAmount,
		ExcludedDomains:        input.ExcludedDomains,
		Note:                   input.Note,
		OperatorUpstreamUserID: operator.User.ID,
		OperatorEmail:          strings.TrimSpace(operator.User.Email),
		OperatorUsername:       strings.TrimSpace(operator.User.Username),
		Status:                 compensationBatchStatusRunning,
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	batchID, err := s.insertCompensationBatch(ctx, batch)
	if err != nil {
		return nil, err
	}
	batch.ID = batchID

	users, err := s.upstream.ListUsersAll(ctx)
	if err != nil {
		batch.Status = compensationBatchStatusFailed
		batch.UpstreamError = err.Error()
		completedAt := s.now()
		batch.CompletedAt = &completedAt
		batch.UpdatedAt = completedAt
		if updateErr := s.updateCompensationBatch(ctx, batch); updateErr != nil {
			return nil, updateErr
		}
		return s.GetCompensationBatch(ctx, batch.ID)
	}

	excludedDomains := make(map[string]struct{}, len(input.ExcludedDomains))
	for _, domain := range input.ExcludedDomains {
		excludedDomains[domain] = struct{}{}
	}

	batch.TotalUsers = len(users)
	for _, user := range users {
		detail := models.CompensationBatchDetail{
			BatchID:           batch.ID,
			DetailKey:         fmt.Sprintf("%s:%d", batch.BatchKey, user.ID),
			UpstreamUserID:    user.ID,
			UserEmail:         strings.TrimSpace(user.Email),
			UserUsername:      strings.TrimSpace(user.Username),
			UserBalance:       user.Balance,
			SubscriptionDays:  input.SubscriptionDays,
			BalanceAmount:     input.BalanceAmount,
			RemarkRequested:   input.Note != "",
			CreatedAt:         s.now(),
			UpdatedAt:         s.now(),
			UpstreamReferenceJSON: "{}",
		}

		if domain := emailDomain(user.Email); domain != "" {
			if _, excluded := excludedDomains[domain]; excluded {
				detail.Excluded = true
				detail.ExcludedDomain = domain
				detail.DecisionType = "excluded_domain"
				detail.ActionType = "skip"
				detail.Status = "skipped"
				detail.ResultReason = "excluded by email domain"
				batch.ExcludedUsers++
				if err := s.insertCompensationBatchDetail(ctx, detail); err != nil {
					return nil, err
				}
				batch.DetailCount++
				continue
			}
		}

		subscriptions, err := s.upstream.ListActiveUserSubscriptions(ctx, user.ID)
		if err != nil {
			detail.DecisionType = "active_subscription_lookup"
			detail.ActionType = "lookup"
			detail.Status = "failed"
			detail.ResultReason = err.Error()
			batch.FailedUsers++
			if err := s.insertCompensationBatchDetail(ctx, detail); err != nil {
				return nil, err
			}
			batch.DetailCount++
			continue
		}

		detail.HasActiveSubscriptions = len(subscriptions) > 0
		detail.ActiveSubscriptionCount = len(subscriptions)
		detail.ActiveSubscriptionIDs = make([]int64, 0, len(subscriptions))
		for _, subscription := range subscriptions {
			detail.ActiveSubscriptionIDs = append(detail.ActiveSubscriptionIDs, subscription.ID)
		}

		if len(subscriptions) > 0 {
			detail.DecisionType = "active_subscription"
			detail.ActionType = "subscription"
			if detail.RemarkRequested {
				detail.RemarkError = "upstream subscription extend endpoint does not support notes"
			}
			extendedIDs := make([]int64, 0, len(subscriptions))
			failedExtends := make([]map[string]any, 0)
			for _, subscription := range subscriptions {
				if _, err := s.upstream.ExtendSubscription(ctx, subscription.ID, input.SubscriptionDays); err != nil {
					failedExtends = append(failedExtends, map[string]any{
						"subscription_id": subscription.ID,
						"error":           err.Error(),
					})
					continue
				}
				extendedIDs = append(extendedIDs, subscription.ID)
			}
			detail.UpstreamReferenceJSON = marshalJSON(map[string]any{
				"subscription_ids": detail.ActiveSubscriptionIDs,
				"extended_ids":     extendedIDs,
				"failed_extensions": failedExtends,
			})
			if len(failedExtends) > 0 {
				detail.Status = "failed"
				detail.ResultReason = "one or more active subscriptions failed to extend"
				batch.FailedUsers++
			} else {
				detail.Status = "success"
				detail.ResultReason = fmt.Sprintf("extended %d active subscriptions", len(extendedIDs))
				batch.SubscriptionCompensatedUsers++
			}
			if err := s.insertCompensationBatchDetail(ctx, detail); err != nil {
				return nil, err
			}
			batch.DetailCount++
			continue
		}

		if user.Balance > 0 {
			detail.DecisionType = "positive_balance"
			detail.ActionType = "balance"
			updatedUser, remarkApplied, remarkError, err := s.addUserBalanceBestEffort(ctx, user.ID, input.BalanceAmount, input.Note)
			if err != nil {
				detail.Status = "failed"
				detail.ResultReason = err.Error()
				batch.FailedUsers++
			} else {
				detail.Status = "success"
				detail.ResultReason = "balance compensation applied"
				detail.RemarkApplied = remarkApplied
				detail.RemarkError = remarkError
				batch.BalanceCompensatedUsers++
				detail.UpstreamReferenceJSON = marshalJSON(map[string]any{
					"user_id":         user.ID,
					"updated_balance": updatedUser.Balance,
				})
			}
			if err := s.insertCompensationBatchDetail(ctx, detail); err != nil {
				return nil, err
			}
			batch.DetailCount++
			continue
		}

		detail.DecisionType = "non_positive_balance"
		detail.ActionType = "skip"
		detail.Status = "skipped"
		detail.ResultReason = "no active subscriptions and balance <= 0"
		batch.SkippedZeroBalanceUsers++
		if err := s.insertCompensationBatchDetail(ctx, detail); err != nil {
			return nil, err
		}
		batch.DetailCount++
	}

	batch.UpdatedAt = s.now()
	completedAt := batch.UpdatedAt
	batch.CompletedAt = &completedAt
	if batch.FailedUsers > 0 {
		batch.Status = compensationBatchStatusCompletedWithFailures
	} else {
		batch.Status = compensationBatchStatusCompleted
	}
	if err := s.updateCompensationBatch(ctx, batch); err != nil {
		return nil, err
	}
	return s.GetCompensationBatch(ctx, batch.ID)
}

func (s *Service) ListCompensationBatches(ctx context.Context) ([]models.CompensationBatch, error) {
	rows, err := s.db().QueryContext(ctx, `
SELECT id, batch_key, subscription_days, balance_amount, excluded_domains_json, note,
       operator_upstream_user_id, operator_email, operator_username, status,
       total_users, excluded_users, subscription_compensated_users, balance_compensated_users,
       skipped_zero_balance_users, failed_users, detail_count, upstream_error,
       created_at, updated_at, completed_at
FROM compensation_batches
ORDER BY created_at DESC, id DESC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.CompensationBatch, 0)
	for rows.Next() {
		item, err := scanCompensationBatchRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func (s *Service) GetCompensationBatch(ctx context.Context, batchID int64) (*models.CompensationBatch, error) {
	if batchID <= 0 {
		return nil, ErrBadRequest
	}
	row := s.db().QueryRowContext(ctx, `
SELECT id, batch_key, subscription_days, balance_amount, excluded_domains_json, note,
       operator_upstream_user_id, operator_email, operator_username, status,
       total_users, excluded_users, subscription_compensated_users, balance_compensated_users,
       skipped_zero_balance_users, failed_users, detail_count, upstream_error,
       created_at, updated_at, completed_at
FROM compensation_batches
WHERE id = ?
`, batchID)
	item, err := scanCompensationBatchRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return item, nil
}

func (s *Service) ListCompensationBatchDetails(ctx context.Context, batchID int64) ([]models.CompensationBatchDetail, error) {
	if batchID <= 0 {
		return nil, ErrBadRequest
	}
	if _, err := s.GetCompensationBatch(ctx, batchID); err != nil {
		return nil, err
	}
	rows, err := s.db().QueryContext(ctx, `
SELECT id, batch_id, detail_key, upstream_user_id, user_email, user_username, user_balance,
       excluded, excluded_domain, has_active_subscriptions, active_subscription_count,
       active_subscription_ids_json, decision_type, action_type, subscription_days,
       balance_amount, status, result_reason, upstream_reference_json,
       remark_requested, remark_applied, remark_error, created_at, updated_at
FROM compensation_batch_details
WHERE batch_id = ?
ORDER BY id ASC
`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.CompensationBatchDetail, 0)
	for rows.Next() {
		item, err := scanCompensationBatchDetailRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func (s *Service) addUserBalanceBestEffort(ctx context.Context, userID int64, amount float64, note string) (*sub2api.User, bool, string, error) {
	note = strings.TrimSpace(note)
	if note == "" {
		user, err := s.upstream.AddUserBalance(ctx, userID, amount, "")
		return user, false, "", err
	}
	user, err := s.upstream.AddUserBalance(ctx, userID, amount, note)
	if err == nil {
		return user, true, "", nil
	}
	var apiErr *sub2api.APIError
	if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 {
		fallbackUser, fallbackErr := s.upstream.AddUserBalance(ctx, userID, amount, "")
		if fallbackErr == nil {
			return fallbackUser, false, "upstream balance endpoint rejected notes, retried without notes", nil
		}
	}
	return nil, false, "", err
}

func (s *Service) insertCompensationBatch(ctx context.Context, batch models.CompensationBatch) (int64, error) {
	result, err := s.db().ExecContext(ctx, `
INSERT INTO compensation_batches (
  batch_key, subscription_days, balance_amount, excluded_domains_json, note,
  operator_upstream_user_id, operator_email, operator_username, status,
  total_users, excluded_users, subscription_compensated_users, balance_compensated_users,
  skipped_zero_balance_users, failed_users, detail_count, upstream_error,
  created_at, updated_at, completed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		batch.BatchKey,
		batch.SubscriptionDays,
		batch.BalanceAmount,
		marshalJSON(batch.ExcludedDomains),
		batch.Note,
		batch.OperatorUpstreamUserID,
		batch.OperatorEmail,
		batch.OperatorUsername,
		batch.Status,
		batch.TotalUsers,
		batch.ExcludedUsers,
		batch.SubscriptionCompensatedUsers,
		batch.BalanceCompensatedUsers,
		batch.SkippedZeroBalanceUsers,
		batch.FailedUsers,
		batch.DetailCount,
		batch.UpstreamError,
		formatTime(batch.CreatedAt),
		formatTime(batch.UpdatedAt),
		formatNullableTime(batch.CompletedAt),
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *Service) updateCompensationBatch(ctx context.Context, batch models.CompensationBatch) error {
	_, err := s.db().ExecContext(ctx, `
UPDATE compensation_batches
SET status = ?, total_users = ?, excluded_users = ?, subscription_compensated_users = ?,
    balance_compensated_users = ?, skipped_zero_balance_users = ?, failed_users = ?,
    detail_count = ?, upstream_error = ?, updated_at = ?, completed_at = ?
WHERE id = ?
`,
		batch.Status,
		batch.TotalUsers,
		batch.ExcludedUsers,
		batch.SubscriptionCompensatedUsers,
		batch.BalanceCompensatedUsers,
		batch.SkippedZeroBalanceUsers,
		batch.FailedUsers,
		batch.DetailCount,
		batch.UpstreamError,
		formatTime(batch.UpdatedAt),
		formatNullableTime(batch.CompletedAt),
		batch.ID,
	)
	return err
}

func (s *Service) insertCompensationBatchDetail(ctx context.Context, detail models.CompensationBatchDetail) error {
	_, err := s.db().ExecContext(ctx, `
INSERT INTO compensation_batch_details (
  batch_id, detail_key, upstream_user_id, user_email, user_username, user_balance,
  excluded, excluded_domain, has_active_subscriptions, active_subscription_count,
  active_subscription_ids_json, decision_type, action_type, subscription_days,
  balance_amount, status, result_reason, upstream_reference_json,
  remark_requested, remark_applied, remark_error, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		detail.BatchID,
		detail.DetailKey,
		detail.UpstreamUserID,
		detail.UserEmail,
		detail.UserUsername,
		detail.UserBalance,
		boolToInt(detail.Excluded),
		detail.ExcludedDomain,
		boolToInt(detail.HasActiveSubscriptions),
		detail.ActiveSubscriptionCount,
		marshalJSON(detail.ActiveSubscriptionIDs),
		detail.DecisionType,
		detail.ActionType,
		detail.SubscriptionDays,
		detail.BalanceAmount,
		detail.Status,
		detail.ResultReason,
		emptyJSONObject(detail.UpstreamReferenceJSON),
		boolToInt(detail.RemarkRequested),
		boolToInt(detail.RemarkApplied),
		detail.RemarkError,
		formatTime(detail.CreatedAt),
		formatTime(detail.UpdatedAt),
	)
	return err
}

func scanCompensationBatchRow(scanner interface {
	Scan(dest ...any) error
}) (*models.CompensationBatch, error) {
	var (
		out               models.CompensationBatch
		excludedDomains   string
		createdAt         string
		updatedAt         string
		completedAt       sql.NullString
	)
	if err := scanner.Scan(
		&out.ID,
		&out.BatchKey,
		&out.SubscriptionDays,
		&out.BalanceAmount,
		&excludedDomains,
		&out.Note,
		&out.OperatorUpstreamUserID,
		&out.OperatorEmail,
		&out.OperatorUsername,
		&out.Status,
		&out.TotalUsers,
		&out.ExcludedUsers,
		&out.SubscriptionCompensatedUsers,
		&out.BalanceCompensatedUsers,
		&out.SkippedZeroBalanceUsers,
		&out.FailedUsers,
		&out.DetailCount,
		&out.UpstreamError,
		&createdAt,
		&updatedAt,
		&completedAt,
	); err != nil {
		return nil, err
	}
	if strings.TrimSpace(excludedDomains) != "" {
		_ = json.Unmarshal([]byte(excludedDomains), &out.ExcludedDomains)
	}
	var err error
	if out.CreatedAt, err = parseNonNullTime(createdAt); err != nil {
		return nil, err
	}
	if out.UpdatedAt, err = parseNonNullTime(updatedAt); err != nil {
		return nil, err
	}
	if out.CompletedAt, err = scanMaybeTime(completedAt); err != nil {
		return nil, err
	}
	return &out, nil
}

func scanCompensationBatchDetailRow(scanner interface {
	Scan(dest ...any) error
}) (*models.CompensationBatchDetail, error) {
	var (
		out                   models.CompensationBatchDetail
		excluded              int
		hasActiveSubscriptions int
		activeSubscriptionIDs string
		remarkRequested       int
		remarkApplied         int
		createdAt             string
		updatedAt             string
	)
	if err := scanner.Scan(
		&out.ID,
		&out.BatchID,
		&out.DetailKey,
		&out.UpstreamUserID,
		&out.UserEmail,
		&out.UserUsername,
		&out.UserBalance,
		&excluded,
		&out.ExcludedDomain,
		&hasActiveSubscriptions,
		&out.ActiveSubscriptionCount,
		&activeSubscriptionIDs,
		&out.DecisionType,
		&out.ActionType,
		&out.SubscriptionDays,
		&out.BalanceAmount,
		&out.Status,
		&out.ResultReason,
		&out.UpstreamReferenceJSON,
		&remarkRequested,
		&remarkApplied,
		&out.RemarkError,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}
	out.Excluded = excluded != 0
	out.HasActiveSubscriptions = hasActiveSubscriptions != 0
	out.RemarkRequested = remarkRequested != 0
	out.RemarkApplied = remarkApplied != 0
	if strings.TrimSpace(activeSubscriptionIDs) != "" {
		_ = json.Unmarshal([]byte(activeSubscriptionIDs), &out.ActiveSubscriptionIDs)
	}
	var err error
	if out.CreatedAt, err = parseNonNullTime(createdAt); err != nil {
		return nil, err
	}
	if out.UpdatedAt, err = parseNonNullTime(updatedAt); err != nil {
		return nil, err
	}
	return &out, nil
}

func normalizeCompensationBatchInput(input CompensationBatchInput) CompensationBatchInput {
	input.Note = strings.TrimSpace(input.Note)
	input.ExcludedDomains = normalizeExcludedDomains(input.ExcludedDomains)
	return input
}

func normalizeExcludedDomains(domains []string) []string {
	seen := make(map[string]struct{}, len(domains))
	out := make([]string, 0, len(domains))
	for _, domain := range domains {
		domain = strings.ToLower(strings.TrimSpace(domain))
		domain = strings.TrimPrefix(domain, "@")
		if domain == "" {
			continue
		}
		if _, ok := seen[domain]; ok {
			continue
		}
		seen[domain] = struct{}{}
		out = append(out, domain)
	}
	return out
}

func emailDomain(email string) string {
	email = strings.TrimSpace(email)
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return strings.ToLower(email[at+1:])
}

func emptyJSONObject(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "{}"
	}
	return raw
}
