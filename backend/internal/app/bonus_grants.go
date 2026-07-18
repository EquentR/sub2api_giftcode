package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"sub2api-giftcode/backend/internal/models"
	"sub2api-giftcode/backend/internal/sub2api"
)

const (
	SubscriptionResetBonusReasonUnlimitedGroup = "unlimited_group"
	SubscriptionResetBonusReasonPreviewInvalid = "preview_invalid"
	SubscriptionResetBonusReasonPreviewExpired = "preview_expired"
	SubscriptionResetBonusReasonPreviewStale   = "preview_stale"
	bonusPreviewTTL                            = 10 * time.Minute
)

type SubscriptionResetBonusPreviewInput struct {
	TargetScope     string  `json:"target_scope"`
	SelectedUserIDs []int64 `json:"selected_user_ids"`
	GroupIDs        []int64 `json:"group_ids"`
	ResetCount      int     `json:"reset_count"`
	Note            string  `json:"note"`
}

type SubscriptionResetBonusPreview struct {
	UserCount         int            `json:"user_count"`
	SubscriptionCount int            `json:"subscription_count"`
	GroupCounts       map[int64]int  `json:"group_counts"`
	MissingUserIDs    []int64        `json:"missing_user_ids"`
	SkippedCounts     map[string]int `json:"skipped_counts"`
	PreviewDigest     string         `json:"preview_digest"`
	PreviewToken      string         `json:"preview_token"`
	ExpiresAt         time.Time      `json:"expires_at"`
}

type subscriptionResetBonusCandidate struct {
	UpstreamUserID         int64     `json:"upstream_user_id"`
	Sub2APIGroupID         int64     `json:"sub2api_group_id"`
	UpstreamSubscriptionID int64     `json:"upstream_subscription_id"`
	StartsAt               time.Time `json:"starts_at"`
	ExpiresAt              time.Time `json:"expires_at"`
	Status                 string    `json:"status"`
	SnapshotJSON           string    `json:"snapshot_json"`
}

type subscriptionResetBonusPreviewClaims struct {
	Version         int                                `json:"version"`
	OperatorUserID  int64                              `json:"operator_user_id"`
	Input           SubscriptionResetBonusPreviewInput `json:"input"`
	Candidates      []subscriptionResetBonusCandidate  `json:"candidates"`
	CandidateDigest string                             `json:"candidate_digest"`
	IssuedAtUnix    int64                              `json:"issued_at_unix"`
	ExpiresAtUnix   int64                              `json:"expires_at_unix"`
}

func (s *Service) PreviewSubscriptionResetBonus(ctx context.Context, operator *SessionUser, input SubscriptionResetBonusPreviewInput) (*SubscriptionResetBonusPreview, error) {
	if err := s.validateBonusOperator(operator); err != nil {
		return nil, err
	}
	input = normalizeSubscriptionResetBonusInput(input)
	candidates, missing, skipped, err := s.collectSubscriptionResetBonusCandidates(ctx, input)
	if err != nil {
		return nil, err
	}
	now := s.now()
	expiresAt := now.Add(bonusPreviewTTL)
	digest := subscriptionResetBonusCandidateDigest(input, candidates)
	claims := subscriptionResetBonusPreviewClaims{
		Version: 1, OperatorUserID: operator.User.ID, Input: input, Candidates: candidates,
		CandidateDigest: digest, IssuedAtUnix: now.Unix(), ExpiresAtUnix: expiresAt.Unix(),
	}
	token, err := s.signSubscriptionResetBonusPreview(claims)
	if err != nil {
		return nil, err
	}
	groupCounts := make(map[int64]int)
	users := make(map[int64]struct{})
	for _, candidate := range candidates {
		groupCounts[candidate.Sub2APIGroupID]++
		users[candidate.UpstreamUserID] = struct{}{}
	}
	return &SubscriptionResetBonusPreview{
		UserCount: len(users), SubscriptionCount: len(candidates), GroupCounts: groupCounts,
		MissingUserIDs: missing, SkippedCounts: skipped, PreviewDigest: digest,
		PreviewToken: token, ExpiresAt: expiresAt,
	}, nil
}

func (s *Service) CreateSubscriptionResetBonusBatch(ctx context.Context, operator *SessionUser, previewToken string) (*models.SubscriptionResetBonusBatch, error) {
	if err := s.validateBonusOperator(operator); err != nil {
		return nil, err
	}
	claims, err := s.verifySubscriptionResetBonusPreview(previewToken)
	if err != nil {
		return nil, err
	}
	if claims.OperatorUserID != operator.User.ID {
		return nil, ErrForbidden
	}
	batchKey := hashSubscriptionResetBonusToken(previewToken)
	if existing, loadErr := s.getSubscriptionResetBonusBatchByKey(ctx, batchKey); loadErr == nil {
		return existing, nil
	} else if !errors.Is(loadErr, sql.ErrNoRows) {
		return nil, loadErr
	}
	if !s.now().Before(time.Unix(claims.ExpiresAtUnix, 0).UTC()) {
		return nil, withStableReason(ErrConflict, SubscriptionResetBonusReasonPreviewExpired)
	}
	candidates, _, _, err := s.collectSubscriptionResetBonusCandidates(ctx, claims.Input)
	if err != nil {
		return nil, err
	}
	if subscriptionResetBonusCandidateDigest(claims.Input, candidates) != claims.CandidateDigest {
		return nil, withStableReason(ErrConflict, SubscriptionResetBonusReasonPreviewStale)
	}
	now := s.now()
	tx, err := s.db().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
INSERT INTO subscription_reset_bonus_batches (
  batch_key, target_scope, selected_user_ids_json, group_ids_json, reset_count, note,
  preview_digest, status, total_candidates, operator_upstream_user_id, operator_email,
  operator_username, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?, ?, ?, ?)
`, batchKey, claims.Input.TargetScope, marshalJSON(claims.Input.SelectedUserIDs), marshalJSON(claims.Input.GroupIDs),
		claims.Input.ResetCount, claims.Input.Note, claims.CandidateDigest, len(candidates), operator.User.ID,
		strings.TrimSpace(operator.User.Email), strings.TrimSpace(operator.User.Username), formatTime(now), formatTime(now))
	if err != nil {
		_ = tx.Rollback()
		if existing, loadErr := s.getSubscriptionResetBonusBatchByKey(ctx, batchKey); loadErr == nil {
			return existing, nil
		}
		return nil, err
	}
	batchID, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	for _, candidate := range candidates {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO subscription_reset_bonus_batch_details (
  batch_id, upstream_user_id, sub2api_group_id, upstream_subscription_id,
  subscription_starts_at, subscription_expires_at, subscription_status,
  subscription_snapshot_json, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?)
`, batchID, candidate.UpstreamUserID, candidate.Sub2APIGroupID, candidate.UpstreamSubscriptionID,
			formatTime(candidate.StartsAt), formatTime(candidate.ExpiresAt), candidate.Status,
			candidate.SnapshotJSON, formatTime(now), formatTime(now)); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.WakeSubscriptionResetBonusWorker()
	return s.GetSubscriptionResetBonusBatch(ctx, batchID)
}

func (s *Service) ProcessSubscriptionResetBonusBatches(ctx context.Context) error {
	rows, err := s.db().QueryContext(ctx, `SELECT id FROM subscription_reset_bonus_batches WHERE status IN ('pending', 'running') ORDER BY id`)
	if err != nil {
		return err
	}
	var batchIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		batchIDs = append(batchIDs, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, batchID := range batchIDs {
		if err := s.processSubscriptionResetBonusBatch(ctx, batchID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) processSubscriptionResetBonusBatch(ctx context.Context, batchID int64) error {
	batch, err := s.GetSubscriptionResetBonusBatch(ctx, batchID)
	if err != nil {
		return err
	}
	now := s.now()
	if _, err := s.db().ExecContext(ctx, `UPDATE subscription_reset_bonus_batches SET status = 'running', started_at = COALESCE(started_at, ?), updated_at = ? WHERE id = ? AND status IN ('pending', 'running')`, formatTime(now), formatTime(now), batchID); err != nil {
		return err
	}
	details, err := s.listPendingSubscriptionResetBonusBatchDetails(ctx, batchID)
	if err != nil {
		return err
	}
	groups, err := s.upstream.ListGroupsAll(ctx)
	if err != nil {
		return err
	}
	groupByID := make(map[int64]sub2api.Group, len(groups))
	for _, group := range groups {
		groupByID[group.ID] = group
	}
	for _, detail := range details {
		subscription, loadErr := s.upstream.GetSubscription(ctx, detail.UpstreamSubscriptionID)
		if loadErr != nil {
			_, err = s.db().ExecContext(ctx, `UPDATE subscription_reset_bonus_batch_details SET status = 'failed', reason = 'upstream_unavailable', error_message = ?, updated_at = ? WHERE id = ? AND status = 'pending'`, loadErr.Error(), formatTime(s.now()), detail.ID)
			if err != nil {
				return err
			}
			continue
		}
		now = s.now()
		group, groupExists := groupByID[detail.Sub2APIGroupID]
		if !groupExists || !strings.EqualFold(strings.TrimSpace(group.Status), "active") || !groupHasQuota(group) {
			reason := "group_not_selected"
			if groupExists && !groupHasQuota(group) {
				reason = SubscriptionResetBonusReasonUnlimitedGroup
			}
			if _, err := s.db().ExecContext(ctx, `UPDATE subscription_reset_bonus_batch_details SET status = 'skipped', reason = ?, subscription_status = ?, subscription_snapshot_json = ?, updated_at = ? WHERE id = ? AND status = 'pending'`, reason, subscription.Status, marshalJSON(subscription), formatTime(now), detail.ID); err != nil {
				return err
			}
			continue
		}
		if subscription.UserID != detail.UpstreamUserID || subscription.GroupID != detail.Sub2APIGroupID || !activeSubscriptionAt(*subscription, now) {
			reason := "subscription_inactive"
			if subscription.UserID != detail.UpstreamUserID {
				reason = "user_not_found"
			} else if subscription.GroupID != detail.Sub2APIGroupID {
				reason = "group_not_selected"
			}
			if _, err := s.db().ExecContext(ctx, `UPDATE subscription_reset_bonus_batch_details SET status = 'skipped', reason = ?, subscription_status = ?, subscription_snapshot_json = ?, updated_at = ? WHERE id = ? AND status = 'pending'`, reason, subscription.Status, marshalJSON(subscription), formatTime(now), detail.ID); err != nil {
				return err
			}
			continue
		}
		if err := s.grantSubscriptionResetBonus(ctx, *batch, detail, *subscription, now); err != nil {
			return err
		}
	}
	return s.finishSubscriptionResetBonusBatch(ctx, batchID, s.now())
}

func (s *Service) grantSubscriptionResetBonus(ctx context.Context, batch models.SubscriptionResetBonusBatch, detail models.SubscriptionResetBonusBatchDetail, subscription sub2api.Subscription, now time.Time) error {
	tx, err := s.db().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
INSERT INTO subscription_reset_bonus_grants (
  batch_id, batch_detail_id, upstream_user_id, sub2api_group_id, upstream_subscription_id,
  reset_limit, reset_used, starts_at, expires_at, status, subscription_snapshot_json,
  last_synced_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?, 'active', ?, ?, ?, ?)
ON CONFLICT(batch_detail_id) DO NOTHING
`, batch.ID, detail.ID, subscription.UserID, subscription.GroupID, subscription.ID, batch.ResetCount,
		formatTime(subscription.StartsAt), formatTime(subscription.ExpiresAt), marshalJSON(subscription),
		formatTime(now), formatTime(now), formatTime(now))
	if err != nil {
		return err
	}
	grantID, err := result.LastInsertId()
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		if err := tx.QueryRowContext(ctx, `SELECT id FROM subscription_reset_bonus_grants WHERE batch_detail_id = ?`, detail.ID).Scan(&grantID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE subscription_reset_bonus_batch_details SET status = 'granted', reason = '', error_message = '', bonus_grant_id = ?, subscription_expires_at = ?, subscription_status = ?, subscription_snapshot_json = ?, updated_at = ? WHERE id = ? AND status = 'pending'`, grantID, formatTime(subscription.ExpiresAt), subscription.Status, marshalJSON(subscription), formatTime(now), detail.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) finishSubscriptionResetBonusBatch(ctx context.Context, batchID int64, now time.Time) error {
	var total, processed, granted, skipped, failed int
	if err := s.db().QueryRowContext(ctx, `
SELECT COUNT(1),
       COALESCE(SUM(CASE WHEN status <> 'pending' THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN status = 'granted' THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN status = 'skipped' THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0)
FROM subscription_reset_bonus_batch_details WHERE batch_id = ?
`, batchID).Scan(&total, &processed, &granted, &skipped, &failed); err != nil {
		return err
	}
	status := "completed"
	if failed > 0 || skipped > 0 {
		status = "completed_with_failures"
	}
	if processed < total {
		status = "running"
	}
	var completedAt any
	if status != "running" {
		completedAt = formatTime(now)
	}
	_, err := s.db().ExecContext(ctx, `UPDATE subscription_reset_bonus_batches SET status = ?, total_candidates = ?, processed_candidates = ?, granted_subscriptions = ?, skipped_subscriptions = ?, failed_subscriptions = ?, completed_at = ?, updated_at = ? WHERE id = ?`, status, total, processed, granted, skipped, failed, completedAt, formatTime(now), batchID)
	return err
}

func (s *Service) ListSubscriptionResetBonusBatches(ctx context.Context) ([]models.SubscriptionResetBonusBatch, error) {
	rows, err := s.db().QueryContext(ctx, subscriptionResetBonusBatchSelectSQL()+` ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.SubscriptionResetBonusBatch{}
	for rows.Next() {
		item, err := scanSubscriptionResetBonusBatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func (s *Service) GetSubscriptionResetBonusBatch(ctx context.Context, id int64) (*models.SubscriptionResetBonusBatch, error) {
	item, err := scanSubscriptionResetBonusBatch(s.db().QueryRowContext(ctx, subscriptionResetBonusBatchSelectSQL()+` WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return item, err
}

func (s *Service) getSubscriptionResetBonusBatchByKey(ctx context.Context, key string) (*models.SubscriptionResetBonusBatch, error) {
	return scanSubscriptionResetBonusBatch(s.db().QueryRowContext(ctx, subscriptionResetBonusBatchSelectSQL()+` WHERE batch_key = ?`, key))
}

func (s *Service) ListSubscriptionResetBonusBatchDetails(ctx context.Context, batchID int64) ([]models.SubscriptionResetBonusBatchDetail, error) {
	if _, err := s.GetSubscriptionResetBonusBatch(ctx, batchID); err != nil {
		return nil, err
	}
	rows, err := s.db().QueryContext(ctx, subscriptionResetBonusDetailSelectSQL()+` WHERE batch_id = ? ORDER BY id`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.SubscriptionResetBonusBatchDetail{}
	for rows.Next() {
		item, err := scanSubscriptionResetBonusBatchDetail(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func (s *Service) listPendingSubscriptionResetBonusBatchDetails(ctx context.Context, batchID int64) ([]models.SubscriptionResetBonusBatchDetail, error) {
	rows, err := s.db().QueryContext(ctx, subscriptionResetBonusDetailSelectSQL()+` WHERE batch_id = ? AND status = 'pending' ORDER BY id`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.SubscriptionResetBonusBatchDetail{}
	for rows.Next() {
		item, err := scanSubscriptionResetBonusBatchDetail(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func (s *Service) WakeSubscriptionResetBonusWorker() {
	if s == nil || s.bonusWake == nil {
		return
	}
	select {
	case s.bonusWake <- struct{}{}:
	default:
	}
}

func (s *Service) RunSubscriptionResetBonusLoop(ctx context.Context, interval time.Duration) {
	run := func() { _ = s.ProcessSubscriptionResetBonusBatches(ctx) }
	run()
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		case <-s.bonusWake:
			run()
		}
	}
}

func (s *Service) collectSubscriptionResetBonusCandidates(ctx context.Context, input SubscriptionResetBonusPreviewInput) ([]subscriptionResetBonusCandidate, []int64, map[string]int, error) {
	if err := s.requireUpstreamClient(); err != nil {
		return nil, nil, nil, err
	}
	if input.ResetCount <= 0 || len(input.GroupIDs) == 0 || (input.TargetScope != "all" && input.TargetScope != "selected") || (input.TargetScope == "selected" && len(input.SelectedUserIDs) == 0) {
		return nil, nil, nil, ErrBadRequest
	}
	groups, err := s.upstream.ListGroupsAll(ctx)
	if err != nil {
		return nil, nil, nil, withStableReason(ErrUpstreamUnavailable, SubscriptionResetReasonUpstreamUnavailable)
	}
	groupByID := make(map[int64]sub2api.Group, len(groups))
	for _, group := range groups {
		groupByID[group.ID] = group
	}
	selectedGroups := make(map[int64]struct{}, len(input.GroupIDs))
	for _, id := range input.GroupIDs {
		group, ok := groupByID[id]
		if !ok || !strings.EqualFold(strings.TrimSpace(group.Status), "active") {
			return nil, nil, nil, ErrBadRequest
		}
		if !groupHasQuota(group) {
			return nil, nil, nil, withStableReason(ErrBadRequest, SubscriptionResetBonusReasonUnlimitedGroup)
		}
		selectedGroups[id] = struct{}{}
	}
	var users []sub2api.User
	if input.TargetScope == "all" {
		users, err = s.upstream.ListUsersAll(ctx)
		if err != nil {
			return nil, nil, nil, withStableReason(ErrUpstreamUnavailable, SubscriptionResetReasonUpstreamUnavailable)
		}
	} else {
		for _, userID := range input.SelectedUserIDs {
			user, loadErr := s.upstream.GetUser(ctx, userID)
			if loadErr != nil || user == nil || user.ID != userID {
				return nil, nil, nil, ErrBadRequest
			}
			users = append(users, *user)
		}
	}
	now := s.now()
	candidates := make([]subscriptionResetBonusCandidate, 0)
	missing := make([]int64, 0)
	skipped := map[string]int{}
	for _, user := range users {
		subscriptions, loadErr := s.upstream.ListActiveUserSubscriptions(ctx, user.ID)
		if loadErr != nil {
			return nil, nil, nil, withStableReason(ErrUpstreamUnavailable, SubscriptionResetReasonUpstreamUnavailable)
		}
		matched := 0
		for _, subscription := range subscriptions {
			if _, ok := selectedGroups[subscription.GroupID]; !ok {
				skipped["group_not_selected"]++
				continue
			}
			if !activeSubscriptionAt(subscription, now) {
				skipped["subscription_inactive"]++
				continue
			}
			matched++
			candidates = append(candidates, subscriptionResetBonusCandidate{
				UpstreamUserID: user.ID, Sub2APIGroupID: subscription.GroupID,
				UpstreamSubscriptionID: subscription.ID, StartsAt: subscription.StartsAt.UTC(),
				ExpiresAt: subscription.ExpiresAt.UTC(), Status: subscription.Status,
				SnapshotJSON: marshalJSON(subscription),
			})
		}
		if matched == 0 && input.TargetScope == "selected" {
			missing = append(missing, user.ID)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].UpstreamUserID != candidates[j].UpstreamUserID {
			return candidates[i].UpstreamUserID < candidates[j].UpstreamUserID
		}
		if candidates[i].Sub2APIGroupID != candidates[j].Sub2APIGroupID {
			return candidates[i].Sub2APIGroupID < candidates[j].Sub2APIGroupID
		}
		return candidates[i].UpstreamSubscriptionID < candidates[j].UpstreamSubscriptionID
	})
	return candidates, missing, skipped, nil
}

func (s *Service) validateBonusOperator(operator *SessionUser) error {
	if operator == nil || !operator.IsAdmin || operator.User.ID <= 0 {
		return ErrForbidden
	}
	if s == nil || s.db() == nil {
		return errors.New("database not configured")
	}
	return nil
}

func normalizeSubscriptionResetBonusInput(input SubscriptionResetBonusPreviewInput) SubscriptionResetBonusPreviewInput {
	input.TargetScope = strings.ToLower(strings.TrimSpace(input.TargetScope))
	input.Note = strings.TrimSpace(input.Note)
	input.SelectedUserIDs = normalizePositiveInt64s(input.SelectedUserIDs)
	input.GroupIDs = normalizePositiveInt64s(input.GroupIDs)
	if input.TargetScope == "all" {
		input.SelectedUserIDs = []int64{}
	}
	return input
}

func normalizePositiveInt64s(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	out := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func groupHasQuota(group sub2api.Group) bool {
	return positiveAppLimit(group.DailyLimitUSD) || positiveAppLimit(group.WeeklyLimitUSD) || positiveAppLimit(group.MonthlyLimitUSD)
}

func activeSubscriptionAt(subscription sub2api.Subscription, now time.Time) bool {
	return subscription.ID > 0 && subscription.UserID > 0 && subscription.GroupID > 0 &&
		strings.EqualFold(strings.TrimSpace(subscription.Status), "active") &&
		!now.Before(subscription.StartsAt) && now.Before(subscription.ExpiresAt)
}

func subscriptionResetBonusCandidateDigest(input SubscriptionResetBonusPreviewInput, candidates []subscriptionResetBonusCandidate) string {
	payload, _ := json.Marshal(struct {
		Input      SubscriptionResetBonusPreviewInput `json:"input"`
		Candidates []subscriptionResetBonusCandidate  `json:"candidates"`
	}{input, candidates})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func (s *Service) signSubscriptionResetBonusPreview(claims subscriptionResetBonusPreviewClaims) (string, error) {
	secret := []byte(strings.TrimSpace(s.cfg.Session.CookieSecret))
	if len(secret) == 0 {
		return "", errors.New("session cookie secret is required for bonus preview")
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *Service) verifySubscriptionResetBonusPreview(token string) (*subscriptionResetBonusPreviewClaims, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 2 || s == nil || s.cfg == nil {
		return nil, withStableReason(ErrBadRequest, SubscriptionResetBonusReasonPreviewInvalid)
	}
	secret := []byte(strings.TrimSpace(s.cfg.Session.CookieSecret))
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(secret) == 0 {
		return nil, withStableReason(ErrBadRequest, SubscriptionResetBonusReasonPreviewInvalid)
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return nil, withStableReason(ErrBadRequest, SubscriptionResetBonusReasonPreviewInvalid)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, withStableReason(ErrBadRequest, SubscriptionResetBonusReasonPreviewInvalid)
	}
	var claims subscriptionResetBonusPreviewClaims
	if json.Unmarshal(payload, &claims) != nil || claims.Version != 1 || claims.OperatorUserID <= 0 || claims.ExpiresAtUnix <= claims.IssuedAtUnix {
		return nil, withStableReason(ErrBadRequest, SubscriptionResetBonusReasonPreviewInvalid)
	}
	return &claims, nil
}

func hashSubscriptionResetBonusToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func subscriptionResetBonusBatchSelectSQL() string {
	return `SELECT id, batch_key, target_scope, selected_user_ids_json, group_ids_json, reset_count, note,
       preview_digest, status, total_candidates, processed_candidates, granted_subscriptions,
       skipped_subscriptions, failed_subscriptions, operator_upstream_user_id, operator_email,
       operator_username, error_message, created_at, started_at, completed_at, updated_at
FROM subscription_reset_bonus_batches`
}

func scanSubscriptionResetBonusBatch(scanner interface{ Scan(...any) error }) (*models.SubscriptionResetBonusBatch, error) {
	var out models.SubscriptionResetBonusBatch
	var usersJSON, groupsJSON, createdAt, updatedAt string
	var startedAt, completedAt sql.NullString
	if err := scanner.Scan(&out.ID, &out.BatchKey, &out.TargetScope, &usersJSON, &groupsJSON, &out.ResetCount,
		&out.Note, &out.PreviewDigest, &out.Status, &out.TotalCandidates, &out.ProcessedCandidates,
		&out.GrantedSubscriptions, &out.SkippedSubscriptions, &out.FailedSubscriptions,
		&out.OperatorUpstreamUserID, &out.OperatorEmail, &out.OperatorUsername, &out.ErrorMessage,
		&createdAt, &startedAt, &completedAt, &updatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(usersJSON), &out.SelectedUserIDs); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(groupsJSON), &out.GroupIDs); err != nil {
		return nil, err
	}
	var err error
	if out.CreatedAt, err = parseNonNullTime(createdAt); err != nil {
		return nil, err
	}
	if out.UpdatedAt, err = parseNonNullTime(updatedAt); err != nil {
		return nil, err
	}
	if out.StartedAt, err = scanMaybeTime(startedAt); err != nil {
		return nil, err
	}
	if out.CompletedAt, err = scanMaybeTime(completedAt); err != nil {
		return nil, err
	}
	return &out, nil
}

func subscriptionResetBonusDetailSelectSQL() string {
	return `SELECT id, batch_id, upstream_user_id, sub2api_group_id, upstream_subscription_id,
       subscription_starts_at, subscription_expires_at, subscription_status, subscription_snapshot_json,
       status, reason, error_message, bonus_grant_id, created_at, updated_at
FROM subscription_reset_bonus_batch_details`
}

func scanSubscriptionResetBonusBatchDetail(scanner interface{ Scan(...any) error }) (*models.SubscriptionResetBonusBatchDetail, error) {
	var out models.SubscriptionResetBonusBatchDetail
	var startsAt, expiresAt, createdAt, updatedAt string
	var grantID sql.NullInt64
	if err := scanner.Scan(&out.ID, &out.BatchID, &out.UpstreamUserID, &out.Sub2APIGroupID,
		&out.UpstreamSubscriptionID, &startsAt, &expiresAt, &out.SubscriptionStatus,
		&out.SubscriptionSnapshotJSON, &out.Status, &out.Reason, &out.ErrorMessage,
		&grantID, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	out.BonusGrantID = parseNullableInt64(grantID)
	var err error
	if out.SubscriptionStartsAt, err = parseNonNullTime(startsAt); err != nil {
		return nil, err
	}
	if out.SubscriptionExpiresAt, err = parseNonNullTime(expiresAt); err != nil {
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
