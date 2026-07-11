package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"sub2api-giftcode/backend/internal/models"
)

const (
	fulfillmentModeDirectCharge      = "direct_charge"
	fulfillmentModeRedeemCode        = "redeem_code"
	fulfillmentResultDirectSucceeded = "direct_charge_succeeded"
	fulfillmentResultCodeIssued      = "redeem_code_issued"
	fulfilledViaDirectCharge         = "direct_charge"
	fulfilledViaRedeemCode           = "redeem_code"
	fulfilledViaRedeemCodeFallback   = "redeem_code_fallback"
)

func normalizeFulfillmentMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", fulfillmentModeDirectCharge:
		return fulfillmentModeDirectCharge
	case fulfillmentModeRedeemCode:
		return fulfillmentModeRedeemCode
	default:
		return ""
	}
}

func (s *Service) CreateAccessRequest(ctx context.Context, sessionID string, tierID int64, note string, fulfillmentMode string) (*models.AccessRequest, error) {
	sessionUser, err := s.CurrentSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if err := s.requireMailer(); err != nil {
		return nil, err
	}
	tier, err := s.getRedeemTierByID(ctx, tierID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if !tier.Enabled {
		return nil, ErrForbidden
	}
	if tier.CodeType == "subscription" {
		if tier.Sub2APIGroupID == nil {
			return nil, ErrBadRequest
		}
		group, err := s.subscriptionGroupByID(ctx, *tier.Sub2APIGroupID)
		if err != nil {
			return nil, err
		}
		tier.Sub2APIGroupName = group.Name
		tier.Sub2APIGroupPlatform = group.Platform
		tier.Sub2APIDailyLimitUSD = group.DailyLimitUSD
		tier.Sub2APIWeeklyLimitUSD = group.WeeklyLimitUSD
		tier.Sub2APIMonthlyLimitUSD = group.MonthlyLimitUSD
	}
	if existing, err := s.getOpenAccessRequestByUser(ctx, sessionUser.User.ID); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	} else if existing != nil {
		return nil, ErrConflict
	}
	normalizedMode := normalizeFulfillmentMode(fulfillmentMode)
	if normalizedMode == "" {
		return nil, ErrBadRequest
	}
	now := s.now()
	token, err := newRandomToken(32)
	if err != nil {
		return nil, err
	}
	req := models.AccessRequest{
		RequestorUpstreamUserID: sessionUser.User.ID,
		RequestorEmail:          sessionUser.User.Email,
		RequestorUsername:       sessionUser.User.Username,
		TierID:                  tier.ID,
		CodeType:                tier.CodeType,
		TierLabel:               tier.Label,
		Amount:                  tier.Amount,
		PayAmountCny:            tier.PayAmountCny,
		Sub2APIGroupID:          tier.Sub2APIGroupID,
		Sub2APIGroupName:        tier.Sub2APIGroupName,
		Sub2APIGroupPlatform:    tier.Sub2APIGroupPlatform,
		Sub2APIDailyLimitUSD:    tier.Sub2APIDailyLimitUSD,
		Sub2APIWeeklyLimitUSD:   tier.Sub2APIWeeklyLimitUSD,
		Sub2APIMonthlyLimitUSD:  tier.Sub2APIMonthlyLimitUSD,
		ValidityDays:            tier.ValidityDays,
		Concurrency:             tier.Concurrency,
		Note:                    strings.TrimSpace(note),
		FulfillmentMode:         normalizedMode,
		FulfillmentResult:       "",
		FulfilledVia:            "",
		FulfillmentError:        "",
		Status:                  "pending",
		ApprovalTokenHash:       hashToken(token),
		ApprovalTokenExpiresAt:  now.Add(s.approvalTTL()),
		NotificationStatus:      "pending",
		NotificationError:       "",
		CreatedAt:               now,
		UpdatedAt:               now,
	}
	id, err := s.insertAccessRequest(ctx, &req)
	if err != nil {
		return nil, err
	}
	req.ID = id
	approvalURL := s.approvalConfirmURL(token)
	branding := defaultSiteBranding()
	if current, brandErr := s.GetSiteBranding(ctx); brandErr == nil && current != nil {
		branding = current
	}
	subjectPrefix := effectiveMailSubjectPrefix(branding.Title, branding.MailSubjectPrefix)
	subject, body := s.mailer.ApprovalEmail(branding.Title, subjectPrefix, req.ID, req.RequestorUsername, req.RequestorEmail, displayTierLabel(&req), req.Amount, req.PayAmountCny, req.Note, approvalURL)
	if err := s.mailer.SendApprovalEmail(ctx, s.cfg.Mail.AdminToAddress, subject, body); err != nil {
		req.NotificationStatus = "failed"
		req.NotificationError = err.Error()
		now := s.now()
		req.NotificationSentAt = nil
		_ = s.updateAccessRequestNotification(ctx, req.ID, req.NotificationStatus, req.NotificationError, nil, now)
		return &req, nil
	}
	now = s.now()
	req.NotificationStatus = "sent"
	req.NotificationError = ""
	req.NotificationSentAt = &now
	if err := s.updateAccessRequestNotification(ctx, req.ID, req.NotificationStatus, req.NotificationError, req.NotificationSentAt, now); err != nil {
		return nil, err
	}
	return &req, nil
}

func (s *Service) ConfirmAccessRequest(ctx context.Context, token string) (*models.AccessRequest, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrBadRequest
	}
	req, err := s.PreviewAccessRequestByToken(ctx, token)
	if err != nil {
		return req, err
	}
	approvedReq, _, err := s.ApproveAccessRequestByID(ctx, req.ID)
	if err != nil {
		return req, err
	}
	return approvedReq, nil
}

func (s *Service) PreviewAccessRequestByToken(ctx context.Context, token string) (*models.AccessRequest, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrBadRequest
	}
	req, err := s.getAccessRequestByTokenHash(ctx, hashToken(token))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	now := s.now()
	if req.Status == "pending" && now.After(req.ApprovalTokenExpiresAt) {
		if markErr := s.markAccessRequestExpired(ctx, req.ID, now); markErr != nil {
			return req, markErr
		}
		req.Status = "expired"
		req.UpdatedAt = now
		return req, ErrConflict
	}
	return req, nil
}

func (s *Service) RejectAccessRequestByID(ctx context.Context, id int64) (*models.AccessRequest, error) {
	req, err := s.getAccessRequestByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if req.Status != "pending" {
		return nil, ErrConflict
	}
	now := s.now()
	req.Status = "rejected"
	req.RejectedAt = &now
	req.UpdatedAt = now
	if err := s.markAccessRequestRejected(ctx, req.ID, now); err != nil {
		return nil, err
	}
	return req, nil
}

func (s *Service) ApproveAccessRequestByID(ctx context.Context, id int64) (*models.AccessRequest, *models.RedeemCode, error) {
	req, err := s.getAccessRequestByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	if req.Status == "consumed" {
		if normalizeCodeType(req.CodeType) == "subscription" {
			_ = s.ensureSubscriptionConcurrencyGrant(ctx, req)
			if req.FulfilledVia == fulfilledViaDirectCharge && req.FulfillmentResult == fulfillmentResultDirectSucceeded {
				_ = s.reconcileSubscriptionConcurrencyForUser(ctx, req.RequestorUpstreamUserID)
			}
		}
		if req.FulfilledVia == fulfilledViaDirectCharge && req.FulfillmentResult == fulfillmentResultDirectSucceeded {
			redeemReq, redeemErr := s.getRedeemRequestByAccessRequestID(ctx, req.ID)
			if redeemErr == nil && redeemReq != nil && redeemReq.Status == "issued" {
				code, codeErr := s.getRedeemCodeByRequestID(ctx, redeemReq.ID)
				if codeErr == nil {
					return req, code, nil
				}
			}
			return req, nil, nil
		}
		redeemReq, redeemErr := s.getRedeemRequestByAccessRequestID(ctx, req.ID)
		if redeemErr == nil && redeemReq != nil && redeemReq.Status == "issued" {
			code, codeErr := s.getRedeemCodeByRequestID(ctx, redeemReq.ID)
			if codeErr == nil {
				return req, code, nil
			}
		}
		return nil, nil, ErrConflict
	}
	if req.Status != "pending" && req.Status != "approved" {
		return nil, nil, ErrConflict
	}
	tier, err := s.getRedeemTierByID(ctx, req.TierID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	if !tier.Enabled {
		return nil, nil, ErrForbidden
	}
	switch normalizeFulfillmentMode(req.FulfillmentMode) {
	case fulfillmentModeRedeemCode:
		_, code, issueErr := s.issueRedeemRequest(ctx, req, tier, req.Note, fulfilledViaRedeemCode, "")
		if issueErr != nil {
			if code != nil {
				return req, code, issueErr
			}
			return nil, nil, issueErr
		}
		return req, code, nil
	case fulfillmentModeDirectCharge:
		if normalizeCodeType(req.CodeType) == "subscription" {
			if req.Sub2APIGroupID == nil || *req.Sub2APIGroupID <= 0 || req.ValidityDays <= 0 {
				return nil, nil, ErrBadRequest
			}
		}
		if code, err := s.issueDirectCharge(ctx, req, tier); err == nil {
			if normalizeCodeType(req.CodeType) == "subscription" {
				_ = s.ensureSubscriptionConcurrencyGrant(ctx, req)
				_ = s.reconcileSubscriptionConcurrencyForUser(ctx, req.RequestorUpstreamUserID)
			}
			return req, code, nil
		} else if errors.Is(err, ErrBadRequest) {
			return nil, nil, err
		} else if errors.Is(err, ErrConflict) {
			return nil, nil, err
		} else {
			fallbackError := err.Error()
			if updateErr := s.updateAccessRequestFulfillmentAttempt(ctx, req.ID, fallbackError, s.now()); updateErr != nil {
				return nil, nil, updateErr
			}
			req.FulfillmentError = fallbackError
			_, code, issueErr := s.issueRedeemRequest(ctx, req, tier, req.Note, fulfilledViaRedeemCodeFallback, fallbackError)
			if issueErr != nil {
				if code != nil {
					return req, code, issueErr
				}
				return nil, nil, issueErr
			}
			return req, code, nil
		}
	default:
		return nil, nil, ErrBadRequest
	}
}

func (s *Service) ListMyAccessRequests(ctx context.Context, sessionID string) ([]models.AccessRequest, error) {
	sessionUser, err := s.CurrentSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return s.listAccessRequests(ctx, &sessionUser.User.ID)
}

func (s *Service) ListAllAccessRequests(ctx context.Context) ([]models.AccessRequest, error) {
	return s.listAccessRequests(ctx, nil)
}

func (s *Service) ListAccessRequestsForUser(ctx context.Context, userID int64) ([]models.AccessRequest, error) {
	return s.listAccessRequests(ctx, &userID)
}

func (s *Service) GetAccessRequestByID(ctx context.Context, id int64) (*models.AccessRequest, error) {
	req, err := s.getAccessRequestByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return req, nil
}

func (s *Service) getOpenAccessRequestByUser(ctx context.Context, upstreamUserID int64) (*models.AccessRequest, error) {
	rows, err := s.listAccessRequests(ctx, &upstreamUserID)
	if err != nil {
		return nil, err
	}
	for _, req := range rows {
		if (req.Status == "pending" || req.Status == "approved") && req.ConsumedAt == nil {
			copy := req
			return &copy, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (s *Service) getApprovedAccessRequestByUser(ctx context.Context, upstreamUserID int64) (*models.AccessRequest, error) {
	rows, err := s.listAccessRequests(ctx, &upstreamUserID)
	if err != nil {
		return nil, err
	}
	for _, req := range rows {
		if req.Status == "approved" && req.ConsumedAt == nil {
			copy := req
			return &copy, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (s *Service) insertAccessRequest(ctx context.Context, req *models.AccessRequest) (int64, error) {
	res, err := s.db().ExecContext(ctx, `
INSERT INTO redeem_access_requests (
  requestor_upstream_user_id, requestor_email, requestor_username, tier_id, code_type, tier_label,
  amount, pay_amount_cny, sub2api_group_id, sub2api_group_name, sub2api_group_platform,
  sub2api_daily_limit_usd, sub2api_weekly_limit_usd, sub2api_monthly_limit_usd, validity_days,
  concurrency,
  note, fulfillment_mode, fulfillment_result, fulfilled_via, fulfillment_error, status, approval_token_hash,
  approval_token_expires_at, notification_status, notification_error, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		req.RequestorUpstreamUserID,
		req.RequestorEmail,
		req.RequestorUsername,
		req.TierID,
		req.CodeType,
		req.TierLabel,
		req.Amount,
		req.PayAmountCny,
		req.Sub2APIGroupID,
		req.Sub2APIGroupName,
		req.Sub2APIGroupPlatform,
		req.Sub2APIDailyLimitUSD,
		req.Sub2APIWeeklyLimitUSD,
		req.Sub2APIMonthlyLimitUSD,
		req.ValidityDays,
		req.Concurrency,
		req.Note,
		req.FulfillmentMode,
		req.FulfillmentResult,
		req.FulfilledVia,
		req.FulfillmentError,
		req.Status,
		req.ApprovalTokenHash,
		formatTime(req.ApprovalTokenExpiresAt),
		req.NotificationStatus,
		req.NotificationError,
		formatTime(req.CreatedAt),
		formatTime(req.UpdatedAt),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Service) updateAccessRequestNotification(ctx context.Context, id int64, status, errText string, sentAt *time.Time, updatedAt time.Time) error {
	_, err := s.db().ExecContext(ctx, `
UPDATE redeem_access_requests
SET notification_status = ?, notification_error = ?, notification_sent_at = ?, updated_at = ?
WHERE id = ?
`,
		status,
		errText,
		formatNullableTime(sentAt),
		formatTime(updatedAt),
		id,
	)
	return err
}

func (s *Service) markAccessRequestApproved(ctx context.Context, id int64, approvedAt time.Time) error {
	_, err := s.db().ExecContext(ctx, `
UPDATE redeem_access_requests
SET status = 'approved', approved_at = ?, updated_at = ?
WHERE id = ?
`, formatTime(approvedAt), formatTime(approvedAt), id)
	return err
}

func (s *Service) markAccessRequestRejected(ctx context.Context, id int64, rejectedAt time.Time) error {
	_, err := s.db().ExecContext(ctx, `
UPDATE redeem_access_requests
SET status = 'rejected', rejected_at = ?, updated_at = ?
WHERE id = ?
`, formatTime(rejectedAt), formatTime(rejectedAt), id)
	return err
}

func (s *Service) markAccessRequestExpired(ctx context.Context, id int64, expiredAt time.Time) error {
	_, err := s.db().ExecContext(ctx, `
UPDATE redeem_access_requests
SET status = 'expired', updated_at = ?
WHERE id = ?
`, formatTime(expiredAt), id)
	return err
}

func (s *Service) markAccessRequestConsumed(ctx context.Context, id int64, consumedAt time.Time) error {
	_, err := s.db().ExecContext(ctx, `
UPDATE redeem_access_requests
SET status = 'consumed', consumed_at = ?, updated_at = ?
WHERE id = ?
`, formatTime(consumedAt), formatTime(consumedAt), id)
	return err
}

func (s *Service) updateAccessRequestFulfillmentAttempt(ctx context.Context, id int64, fulfillmentError string, updatedAt time.Time) error {
	_, err := s.db().ExecContext(ctx, `
UPDATE redeem_access_requests
SET fulfillment_error = ?, updated_at = ?
WHERE id = ?
`, strings.TrimSpace(fulfillmentError), formatTime(updatedAt), id)
	return err
}

func (s *Service) getAccessRequestByTokenHash(ctx context.Context, tokenHash string) (*models.AccessRequest, error) {
	return s.getAccessRequestByField(ctx, "approval_token_hash", tokenHash)
}

func (s *Service) getAccessRequestByID(ctx context.Context, id int64) (*models.AccessRequest, error) {
	row := s.db().QueryRowContext(ctx, accessRequestSelectSQL()+`
WHERE id = ?
`, id)
	return scanAccessRequestRow(row)
}

func (s *Service) getAccessRequestByField(ctx context.Context, field string, value string) (*models.AccessRequest, error) {
	switch field {
	case "approval_token_hash":
		row := s.db().QueryRowContext(ctx, accessRequestSelectSQL()+`
WHERE approval_token_hash = ?
LIMIT 1
`, value)
		return scanAccessRequestRow(row)
	default:
		return nil, ErrBadRequest
	}
}

func (s *Service) listAccessRequests(ctx context.Context, userID *int64) ([]models.AccessRequest, error) {
	query := accessRequestSelectSQL()
	args := make([]any, 0, 1)
	if userID != nil {
		query += `WHERE requestor_upstream_user_id = ? `
		args = append(args, *userID)
	}
	query += `ORDER BY created_at DESC`
	rows, err := s.db().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.AccessRequest, 0)
	for rows.Next() {
		req, err := scanAccessRequestRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *req)
	}
	return out, rows.Err()
}

func accessRequestSelectSQL() string {
	return `
SELECT id, requestor_upstream_user_id, requestor_email, requestor_username, tier_id, code_type, tier_label,
       amount, pay_amount_cny, sub2api_group_id, sub2api_group_name, sub2api_group_platform,
       sub2api_daily_limit_usd, sub2api_weekly_limit_usd, sub2api_monthly_limit_usd, validity_days,
       concurrency,
       note, fulfillment_mode, fulfillment_result, fulfilled_via, fulfillment_error, status, approval_token_hash,
       approval_token_expires_at, approved_at, rejected_at, consumed_at, notification_status, notification_error,
       notification_sent_at, created_at, updated_at
FROM redeem_access_requests
`
}

func scanAccessRequestRow(scanner interface {
	Scan(dest ...any) error
}) (*models.AccessRequest, error) {
	var out models.AccessRequest
	var approvedAt sql.NullString
	var rejectedAt sql.NullString
	var consumedAt sql.NullString
	var notificationSentAt sql.NullString
	var approvalTokenExpiresAt string
	var sub2apiGroupID sql.NullInt64
	var sub2apiDailyLimit sql.NullFloat64
	var sub2apiWeeklyLimit sql.NullFloat64
	var sub2apiMonthlyLimit sql.NullFloat64
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(
		&out.ID,
		&out.RequestorUpstreamUserID,
		&out.RequestorEmail,
		&out.RequestorUsername,
		&out.TierID,
		&out.CodeType,
		&out.TierLabel,
		&out.Amount,
		&out.PayAmountCny,
		&sub2apiGroupID,
		&out.Sub2APIGroupName,
		&out.Sub2APIGroupPlatform,
		&sub2apiDailyLimit,
		&sub2apiWeeklyLimit,
		&sub2apiMonthlyLimit,
		&out.ValidityDays,
		&out.Concurrency,
		&out.Note,
		&out.FulfillmentMode,
		&out.FulfillmentResult,
		&out.FulfilledVia,
		&out.FulfillmentError,
		&out.Status,
		&out.ApprovalTokenHash,
		&approvalTokenExpiresAt,
		&approvedAt,
		&rejectedAt,
		&consumedAt,
		&out.NotificationStatus,
		&out.NotificationError,
		&notificationSentAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}
	out.CodeType = normalizeCodeType(out.CodeType)
	out.FulfillmentMode = normalizeFulfillmentMode(out.FulfillmentMode)
	out.Sub2APIGroupID = parseNullableInt64(sub2apiGroupID)
	if sub2apiDailyLimit.Valid {
		v := sub2apiDailyLimit.Float64
		out.Sub2APIDailyLimitUSD = &v
	}
	if sub2apiWeeklyLimit.Valid {
		v := sub2apiWeeklyLimit.Float64
		out.Sub2APIWeeklyLimitUSD = &v
	}
	if sub2apiMonthlyLimit.Valid {
		v := sub2apiMonthlyLimit.Float64
		out.Sub2APIMonthlyLimitUSD = &v
	}
	var err error
	if out.ApprovalTokenExpiresAt, err = parseNonNullTime(approvalTokenExpiresAt); err != nil {
		return nil, err
	}
	if out.ApprovedAt, err = scanMaybeTime(approvedAt); err != nil {
		return nil, err
	}
	if out.RejectedAt, err = scanMaybeTime(rejectedAt); err != nil {
		return nil, err
	}
	if out.ConsumedAt, err = scanMaybeTime(consumedAt); err != nil {
		return nil, err
	}
	if out.NotificationSentAt, err = scanMaybeTime(notificationSentAt); err != nil {
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

func scanAccessRequestRows(rows *sql.Rows) (*models.AccessRequest, error) {
	return scanAccessRequestRow(rows)
}

func displayTierLabel(req *models.AccessRequest) string {
	if req == nil {
		return ""
	}
	if strings.TrimSpace(req.TierLabel) != "" {
		return req.TierLabel
	}
	if req.CodeType == "subscription" && strings.TrimSpace(req.Sub2APIGroupName) != "" {
		return fmt.Sprintf("%s / %d days", req.Sub2APIGroupName, req.ValidityDays)
	}
	return fmt.Sprintf("%.0f USD", req.Amount)
}
