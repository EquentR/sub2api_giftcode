package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"sub2api-giftcode/backend/internal/models"
	"sub2api-giftcode/backend/internal/sub2api"
)

func (s *Service) CreateRedeemRequest(ctx context.Context, sessionID string, tierID int64, note string) (*models.RedeemRequest, *models.RedeemCode, error) {
	sessionUser, err := s.CurrentSession(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	tier, err := s.getRedeemTierByID(ctx, tierID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	if !tier.Enabled {
		return nil, nil, ErrForbidden
	}
	accessReq, err := s.getApprovedAccessRequestByUser(ctx, sessionUser.User.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrForbidden
		}
		return nil, nil, err
	}
	redeemReq, err := s.getRedeemRequestByAccessRequestID(ctx, accessReq.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}
	if redeemReq != nil && redeemReq.Status == "issued" {
		existingCode, err := s.getRedeemCodeByRequestID(ctx, redeemReq.ID)
		if err == nil {
			return redeemReq, existingCode, nil
		}
	}
	return s.issueRedeemRequest(ctx, accessReq, tier, strings.TrimSpace(note))
}

func (s *Service) ListMyRedeemRequests(ctx context.Context, sessionID string) ([]models.RedeemRequest, error) {
	sessionUser, err := s.CurrentSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return s.listRedeemRequests(ctx, &sessionUser.User.ID)
}

func (s *Service) ListAllRedeemRequests(ctx context.Context) ([]models.RedeemRequest, error) {
	return s.listRedeemRequests(ctx, nil)
}

func (s *Service) ListMyRedeemCodes(ctx context.Context, sessionID string) ([]models.RedeemCode, error) {
	sessionUser, err := s.CurrentSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return s.listRedeemCodes(ctx, &sessionUser.User.ID)
}

func (s *Service) ListAllRedeemCodes(ctx context.Context) ([]models.RedeemCode, error) {
	return s.listRedeemCodes(ctx, nil)
}

func (s *Service) ListRedeemCodesForUser(ctx context.Context, userID int64) ([]models.RedeemCode, error) {
	return s.listRedeemCodes(ctx, &userID)
}

func (s *Service) ListRedeemRequestsForUser(ctx context.Context, userID int64) ([]models.RedeemRequest, error) {
	return s.listRedeemRequests(ctx, &userID)
}

func (s *Service) SyncRedeemCodes(ctx context.Context) (int, error) {
	if err := s.requireUpstreamClient(); err != nil {
		return 0, err
	}
	codes, err := s.listRedeemCodes(ctx, nil)
	if err != nil {
		return 0, err
	}
	updated := 0
	now := s.now()
	for _, local := range codes {
		remoteCodes, remoteErr := s.upstream.ListRedeemCodes(ctx, local.Code, 20)
		if remoteErr != nil {
			continue
		}
		var matched *sub2api.RedeemCode
		for i := range remoteCodes {
			if strings.EqualFold(remoteCodes[i].Code, local.Code) {
				matched = &remoteCodes[i]
				break
			}
		}
		if matched == nil {
			continue
		}
		if err := s.updateRedeemCodeFromUpstream(ctx, local.ID, matched, now); err != nil {
			return updated, err
		}
		updated++
	}
	_ = s.setSyncState(ctx, "redeem_codes_last_sync_at", formatTime(now), now)
	return updated, nil
}

func (s *Service) issueRedeemRequest(ctx context.Context, accessReq *models.AccessRequest, tier *models.RedeemTier, note string) (*models.RedeemRequest, *models.RedeemCode, error) {
	if accessReq == nil {
		return nil, nil, ErrBadRequest
	}
	if tier == nil {
		return nil, nil, ErrBadRequest
	}
	trimmedNote := strings.TrimSpace(note)
	if trimmedNote == "" {
		trimmedNote = strings.TrimSpace(accessReq.Note)
	}
	redeemReq, err := s.getRedeemRequestByAccessRequestID(ctx, accessReq.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}
	redeemTierID := tier.ID
	redeemValue := accessReq.Amount
	if redeemValue <= 0 {
		redeemValue = tier.Amount
	}
	codeType := normalizeCodeType(accessReq.CodeType)
	if codeType == "" {
		codeType = normalizeCodeType(tier.CodeType)
	}
	groupID := accessReq.Sub2APIGroupID
	validityDays := accessReq.ValidityDays
	if codeType == "subscription" {
		if groupID == nil {
			groupID = tier.Sub2APIGroupID
		}
		if validityDays <= 0 {
			validityDays = tier.ValidityDays
		}
		if groupID == nil || *groupID <= 0 || validityDays <= 0 {
			return nil, nil, ErrBadRequest
		}
		if _, err := s.subscriptionGroupByID(ctx, *groupID); err != nil {
			return nil, nil, err
		}
		redeemValue = 0
	}
	if redeemReq != nil {
		if redeemReq.TierID > 0 {
			redeemTierID = redeemReq.TierID
		}
		if redeemReq.Value > 0 || codeType == "subscription" {
			redeemValue = redeemReq.Value
		}
		if redeemReq.CodeType != "" {
			codeType = normalizeCodeType(redeemReq.CodeType)
		}
		if redeemReq.Sub2APIGroupID != nil {
			groupID = redeemReq.Sub2APIGroupID
		}
		if redeemReq.ValidityDays > 0 {
			validityDays = redeemReq.ValidityDays
		}
	}
	if redeemReq != nil && redeemReq.Status == "issued" {
		existingCode, err := s.getRedeemCodeByRequestID(ctx, redeemReq.ID)
		if err == nil {
			return redeemReq, existingCode, nil
		}
	}
	now := s.now()
	if redeemReq == nil {
		redeemReq = &models.RedeemRequest{
			AccessRequestID:         accessReq.ID,
			RequestorUpstreamUserID: accessReq.RequestorUpstreamUserID,
			RequestorEmail:          accessReq.RequestorEmail,
			RequestorUsername:       accessReq.RequestorUsername,
			CodeType:                codeType,
			TierID:                  redeemTierID,
			Value:                   redeemValue,
			Sub2APIGroupID:          groupID,
			ValidityDays:            validityDays,
			Status:                  "pending",
			Note:                    trimmedNote,
			CreatedAt:               now,
			UpdatedAt:               now,
		}
		id, err := s.insertRedeemRequest(ctx, redeemReq)
		if err != nil {
			return nil, nil, err
		}
		redeemReq.ID = id
	} else {
		redeemReq.RequestorUpstreamUserID = accessReq.RequestorUpstreamUserID
		redeemReq.RequestorEmail = accessReq.RequestorEmail
		redeemReq.RequestorUsername = accessReq.RequestorUsername
		redeemReq.CodeType = codeType
		redeemReq.TierID = redeemTierID
		redeemReq.Value = redeemValue
		redeemReq.Sub2APIGroupID = groupID
		redeemReq.ValidityDays = validityDays
		redeemReq.Status = "pending"
		redeemReq.Note = trimmedNote
		redeemReq.UpdatedAt = now
		if err := s.updateRedeemRequest(ctx, redeemReq); err != nil {
			return nil, nil, err
		}
	}

	if err := s.requireUpstreamClient(); err != nil {
		_ = s.updateRedeemRequestFailure(ctx, redeemReq.ID, err.Error(), now)
		return redeemReq, nil, err
	}
	idempotencyKey := redeemIssueIdempotencyKey(accessReq, redeemReq)
	generated, err := s.upstream.GenerateRedeemCodes(ctx, idempotencyKey, sub2api.GenerateRedeemCodesInput{
		Type:         codeType,
		Value:        redeemReq.Value,
		GroupID:      groupID,
		ValidityDays: validityDays,
	})
	if err != nil {
		_ = s.updateRedeemRequestFailure(ctx, redeemReq.ID, err.Error(), now)
		return redeemReq, nil, fmt.Errorf("%w: %w", ErrUpstreamFailed, err)
	}
	if len(generated) == 0 {
		_ = s.updateRedeemRequestFailure(ctx, redeemReq.ID, "empty upstream response", now)
		return redeemReq, nil, fmt.Errorf("%w: empty upstream response", ErrUpstreamFailed)
	}
	upstreamCode := generated[0]
	code := &models.RedeemCode{
		RequestID:            redeemReq.ID,
		Code:                 upstreamCode.Code,
		CodeType:             upstreamCode.Type,
		Value:                upstreamCode.Value,
		Status:               normalizeUpstreamCodeStatus(upstreamCode.Status),
		UsedByUpstreamUserID: upstreamCode.UsedBy,
		UsedAt:               upstreamCode.UsedAt,
		ExpiresAt:            upstreamCode.ExpiresAt,
		Sub2APICodeID:        &upstreamCode.ID,
		Sub2APIGroupID:       upstreamCode.GroupID,
		ValidityDays:         upstreamCode.ValidityDays,
		LastSyncedAt:         &now,
		CreatedAt:            now,
		UpdatedAt:            now,
	}

	if err := s.persistIssuedRedeemCode(ctx, redeemReq, code, accessReq.ID); err != nil {
		_ = s.updateRedeemRequestFailure(ctx, redeemReq.ID, err.Error(), now)
		return redeemReq, nil, err
	}
	redeemReq.Status = "issued"
	redeemReq.UpstreamCode = code.Code
	redeemReq.UpstreamCodeID = code.Sub2APICodeID
	redeemReq.ErrorMessage = ""
	redeemReq.UpdatedAt = now

	approvalTime := now
	accessReq.Status = "consumed"
	accessReq.ApprovedAt = &approvalTime
	accessReq.ConsumedAt = &approvalTime
	accessReq.UpdatedAt = approvalTime
	return redeemReq, code, nil
}

func redeemIssueIdempotencyKey(accessReq *models.AccessRequest, redeemReq *models.RedeemRequest) string {
	tokenHash := strings.TrimSpace(accessReq.ApprovalTokenHash)
	if tokenHash == "" {
		tokenHash = "legacy"
	}
	return fmt.Sprintf("giftcode-redeem-access-%d-request-%d-%s", accessReq.ID, redeemReq.ID, tokenHash)
}

func normalizeUpstreamCodeStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "used", "expired", "disabled":
		return strings.ToLower(strings.TrimSpace(status))
	default:
		return "unused"
	}
}

func (s *Service) getRedeemRequestByAccessRequestID(ctx context.Context, accessRequestID int64) (*models.RedeemRequest, error) {
	row := s.db().QueryRowContext(ctx, `
SELECT id, access_request_id, requestor_upstream_user_id, requestor_email, requestor_username,
       code_type, tier_id, value, sub2api_group_id, validity_days, status, note, upstream_code, upstream_code_id, error_message,
       created_at, updated_at
FROM redeem_requests
WHERE access_request_id = ?
LIMIT 1
`, accessRequestID)
	return scanRedeemRequestRow(row)
}

func (s *Service) getRedeemCodeByRequestID(ctx context.Context, requestID int64) (*models.RedeemCode, error) {
	row := s.db().QueryRowContext(ctx, `
SELECT id, request_id, code, code_type, value, status, used_by_upstream_user_id, used_at,
       expires_at, sub2api_code_id, sub2api_group_id, validity_days, last_synced_at, created_at, updated_at
FROM redeem_codes
WHERE request_id = ?
LIMIT 1
`, requestID)
	return scanRedeemCodeRow(row)
}

func (s *Service) insertRedeemRequest(ctx context.Context, req *models.RedeemRequest) (int64, error) {
	res, err := s.db().ExecContext(ctx, `
INSERT INTO redeem_requests (
  access_request_id, requestor_upstream_user_id, requestor_email, requestor_username,
  code_type, tier_id, value, sub2api_group_id, validity_days, status, note, upstream_code, upstream_code_id,
  error_message, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		req.AccessRequestID,
		req.RequestorUpstreamUserID,
		req.RequestorEmail,
		req.RequestorUsername,
		req.CodeType,
		req.TierID,
		req.Value,
		req.Sub2APIGroupID,
		req.ValidityDays,
		req.Status,
		req.Note,
		req.UpstreamCode,
		req.UpstreamCodeID,
		req.ErrorMessage,
		formatTime(req.CreatedAt),
		formatTime(req.UpdatedAt),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Service) updateRedeemRequest(ctx context.Context, req *models.RedeemRequest) error {
	_, err := s.db().ExecContext(ctx, `
UPDATE redeem_requests
SET requestor_upstream_user_id = ?, requestor_email = ?, requestor_username = ?,
    code_type = ?, tier_id = ?, value = ?, sub2api_group_id = ?, validity_days = ?, status = ?, note = ?, upstream_code = ?,
    upstream_code_id = ?, error_message = ?, updated_at = ?
WHERE id = ?
`,
		req.RequestorUpstreamUserID,
		req.RequestorEmail,
		req.RequestorUsername,
		req.CodeType,
		req.TierID,
		req.Value,
		req.Sub2APIGroupID,
		req.ValidityDays,
		req.Status,
		req.Note,
		req.UpstreamCode,
		req.UpstreamCodeID,
		req.ErrorMessage,
		formatTime(req.UpdatedAt),
		req.ID,
	)
	return err
}

func (s *Service) updateRedeemRequestFailure(ctx context.Context, id int64, message string, updatedAt time.Time) error {
	_, err := s.db().ExecContext(ctx, `
UPDATE redeem_requests
SET status = 'failed', error_message = ?, updated_at = ?
WHERE id = ?
`, message, formatTime(updatedAt), id)
	return err
}

func (s *Service) persistIssuedRedeemCode(ctx context.Context, redeemReq *models.RedeemRequest, code *models.RedeemCode, accessRequestID int64) error {
	tx, err := s.db().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO redeem_codes (
  request_id, code, code_type, value, status, used_by_upstream_user_id, used_at,
  expires_at, sub2api_code_id, sub2api_group_id, validity_days, last_synced_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(request_id) DO UPDATE SET
  code = excluded.code,
  code_type = excluded.code_type,
  value = excluded.value,
  status = excluded.status,
  used_by_upstream_user_id = excluded.used_by_upstream_user_id,
  used_at = excluded.used_at,
  expires_at = excluded.expires_at,
  sub2api_code_id = excluded.sub2api_code_id,
  sub2api_group_id = excluded.sub2api_group_id,
  validity_days = excluded.validity_days,
  last_synced_at = excluded.last_synced_at,
  updated_at = excluded.updated_at
`,
		code.RequestID,
		code.Code,
		code.CodeType,
		code.Value,
		code.Status,
		code.UsedByUpstreamUserID,
		formatNullableTime(code.UsedAt),
		formatNullableTime(code.ExpiresAt),
		code.Sub2APICodeID,
		code.Sub2APIGroupID,
		code.ValidityDays,
		formatNullableTime(code.LastSyncedAt),
		formatTime(code.CreatedAt),
		formatTime(code.UpdatedAt),
	); err != nil {
		return rollback(err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE redeem_requests
SET status = 'issued', upstream_code = ?, upstream_code_id = ?, error_message = '', updated_at = ?
WHERE id = ?
`, code.Code, code.Sub2APICodeID, formatTime(code.UpdatedAt), redeemReq.ID); err != nil {
		return rollback(err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE redeem_access_requests
SET status = 'consumed', approved_at = ?, consumed_at = ?, updated_at = ?
WHERE id = ?
`, formatTime(code.UpdatedAt), formatTime(code.UpdatedAt), formatTime(code.UpdatedAt), accessRequestID); err != nil {
		return rollback(err)
	}
	return tx.Commit()
}

func (s *Service) listRedeemRequests(ctx context.Context, userID *int64) ([]models.RedeemRequest, error) {
	query := `
SELECT id, access_request_id, requestor_upstream_user_id, requestor_email, requestor_username,
       code_type, tier_id, value, sub2api_group_id, validity_days, status, note, upstream_code, upstream_code_id, error_message,
       created_at, updated_at
FROM redeem_requests
`
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
	out := make([]models.RedeemRequest, 0)
	for rows.Next() {
		item, err := scanRedeemRequestRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func (s *Service) listRedeemCodes(ctx context.Context, userID *int64) ([]models.RedeemCode, error) {
	query := `
SELECT c.id, c.request_id, c.code, c.code_type, c.value, c.status, c.used_by_upstream_user_id,
       c.used_at, c.expires_at, c.sub2api_code_id, c.sub2api_group_id, c.validity_days, c.last_synced_at, c.created_at, c.updated_at
FROM redeem_codes c
JOIN redeem_requests r ON r.id = c.request_id
`
	args := make([]any, 0, 1)
	if userID != nil {
		query += `WHERE r.requestor_upstream_user_id = ? `
		args = append(args, *userID)
	}
	query += `ORDER BY c.created_at DESC`
	rows, err := s.db().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.RedeemCode, 0)
	for rows.Next() {
		item, err := scanRedeemCodeRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func (s *Service) updateRedeemCodeFromUpstream(ctx context.Context, id int64, remote *sub2api.RedeemCode, syncedAt time.Time) error {
	_, err := s.db().ExecContext(ctx, `
UPDATE redeem_codes
SET code_type = ?, value = ?, status = ?, used_by_upstream_user_id = ?, used_at = ?, expires_at = ?,
    sub2api_code_id = ?, sub2api_group_id = ?, validity_days = ?, last_synced_at = ?, updated_at = ?
WHERE id = ?
`,
		remote.Type,
		remote.Value,
		normalizeUpstreamCodeStatus(remote.Status),
		remote.UsedBy,
		formatNullableTime(remote.UsedAt),
		formatNullableTime(remote.ExpiresAt),
		remote.ID,
		remote.GroupID,
		remote.ValidityDays,
		formatTime(syncedAt),
		formatTime(syncedAt),
		id,
	)
	return err
}

func scanRedeemRequestRow(scanner interface {
	Scan(dest ...any) error
}) (*models.RedeemRequest, error) {
	var out models.RedeemRequest
	var upstreamCodeID sql.NullInt64
	var groupID sql.NullInt64
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(
		&out.ID,
		&out.AccessRequestID,
		&out.RequestorUpstreamUserID,
		&out.RequestorEmail,
		&out.RequestorUsername,
		&out.CodeType,
		&out.TierID,
		&out.Value,
		&groupID,
		&out.ValidityDays,
		&out.Status,
		&out.Note,
		&out.UpstreamCode,
		&upstreamCodeID,
		&out.ErrorMessage,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}
	out.CodeType = normalizeCodeType(out.CodeType)
	out.UpstreamCodeID = parseNullableInt64(upstreamCodeID)
	out.Sub2APIGroupID = parseNullableInt64(groupID)
	var err error
	if out.CreatedAt, err = parseNonNullTime(createdAt); err != nil {
		return nil, err
	}
	if out.UpdatedAt, err = parseNonNullTime(updatedAt); err != nil {
		return nil, err
	}
	return &out, nil
}

func scanRedeemCodeRow(scanner interface {
	Scan(dest ...any) error
}) (*models.RedeemCode, error) {
	var out models.RedeemCode
	var usedBy sql.NullInt64
	var usedAt sql.NullString
	var expiresAt sql.NullString
	var sub2apiCodeID sql.NullInt64
	var sub2apiGroupID sql.NullInt64
	var lastSyncedAt sql.NullString
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(
		&out.ID,
		&out.RequestID,
		&out.Code,
		&out.CodeType,
		&out.Value,
		&out.Status,
		&usedBy,
		&usedAt,
		&expiresAt,
		&sub2apiCodeID,
		&sub2apiGroupID,
		&out.ValidityDays,
		&lastSyncedAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}
	out.CodeType = normalizeCodeType(out.CodeType)
	out.UsedByUpstreamUserID = parseNullableInt64(usedBy)
	var err error
	if out.UsedAt, err = scanMaybeTime(usedAt); err != nil {
		return nil, err
	}
	if out.ExpiresAt, err = scanMaybeTime(expiresAt); err != nil {
		return nil, err
	}
	out.Sub2APICodeID = parseNullableInt64(sub2apiCodeID)
	out.Sub2APIGroupID = parseNullableInt64(sub2apiGroupID)
	if out.LastSyncedAt, err = scanMaybeTime(lastSyncedAt); err != nil {
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
