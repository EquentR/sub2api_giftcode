package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"sub2api-giftcode/backend/internal/models"
	"sub2api-giftcode/backend/internal/sub2api"
)

type concurrencyGrantRecord struct {
	models.SubscriptionConcurrencyGrant
	FulfilledVia string
	CodeStatus   sql.NullString
	CodeUsedBy   sql.NullInt64
}

type concurrencyUserState struct {
	Exists                    bool
	LastAppliedConcurrency    sql.NullInt64
	ManualOverride            bool
	ManualOverrideConcurrency sql.NullInt64
}

const (
	subscriptionConcurrencyLastReconciliationAtKey = "subscription_concurrency_last_reconciliation_at"
	subscriptionConcurrencyLatestErrorKey          = "subscription_concurrency_latest_error"
	subscriptionConcurrencyLatestErrorAtKey        = "subscription_concurrency_latest_error_at"
	subscriptionConcurrencyControlBootstrappedKey  = "subscription_concurrency_control_bootstrapped"
)

// SubscriptionConcurrencyMonitorStatus describes the current local grant state
// and the live default concurrency configured by sub2api.
type SubscriptionConcurrencyMonitorStatus struct {
	DefaultConcurrency      int        `json:"default_concurrency"`
	DefaultConcurrencyError string     `json:"default_concurrency_error"`
	LastReconciliationAt    *time.Time `json:"last_reconciliation_at,omitempty"`
	ActiveGrants            int        `json:"active_grants"`
	PendingGrants           int        `json:"pending_grants"`
	InactiveGrants          int        `json:"inactive_grants"`
	ErrorGrants             int        `json:"error_grants"`
	ManualOverrideUsers     int        `json:"manual_override_users"`
	LatestError             string     `json:"latest_error"`
	LatestErrorAt           *time.Time `json:"latest_error_at,omitempty"`
}

// SubscriptionConcurrencyMonitorDetail is the latest local reconciliation snapshot for one managed user.
type SubscriptionConcurrencyMonitorDetail struct {
	UpstreamUserID     int64      `json:"upstream_user_id"`
	Username           string     `json:"username"`
	Email              string     `json:"email"`
	CurrentConcurrency *int       `json:"current_concurrency,omitempty"`
	TargetConcurrency  int        `json:"target_concurrency"`
	ManualOverride     bool       `json:"manual_override"`
	ActiveGrants       int        `json:"active_grants"`
	PendingGrants      int        `json:"pending_grants"`
	InactiveGrants     int        `json:"inactive_grants"`
	LastSyncedAt       *time.Time `json:"last_synced_at,omitempty"`
	LastError          string     `json:"last_error"`
}

// ReconcileSubscriptionConcurrency applies the current upstream entitlement state to every managed user.
func (s *Service) ReconcileSubscriptionConcurrency(ctx context.Context) (err error) {
	defer func() { err = errors.Join(err, s.recordSubscriptionConcurrencyReconciliation(err)) }()
	if err := s.requireUpstreamClient(); err != nil {
		return err
	}
	s.concurrencyMu.Lock()
	defer s.concurrencyMu.Unlock()
	repairErr := s.repairMissingSubscriptionConcurrencyGrants(ctx)
	bootstrapState, bootstrapErr := s.getSyncState(ctx, subscriptionConcurrencyControlBootstrappedKey)
	if errors.Is(bootstrapErr, sql.ErrNoRows) {
		bootstrapErr = nil
	}
	if bootstrapErr != nil {
		return errors.Join(repairErr, bootstrapErr)
	}
	protectUntracked := bootstrapState != "1"
	reconcileErr := s.reconcileSubscriptionConcurrency(ctx, 0, protectUntracked)
	if repairErr == nil && reconcileErr == nil && protectUntracked {
		reconcileErr = s.setSyncState(ctx, subscriptionConcurrencyControlBootstrappedKey, "1", s.now())
	}
	return errors.Join(repairErr, reconcileErr)
}

// SubscriptionConcurrencyMonitorStatus returns local grant counts even when
// fetching the upstream default fails, so operators can distinguish an
// upstream settings outage from an empty monitor.
func (s *Service) SubscriptionConcurrencyMonitorStatus(ctx context.Context) (*SubscriptionConcurrencyMonitorStatus, error) {
	status := &SubscriptionConcurrencyMonitorStatus{}
	row := s.db().QueryRowContext(ctx, `
SELECT
  COALESCE(SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN status = 'inactive' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN status = 'error' OR TRIM(last_error) <> '' THEN 1 ELSE 0 END), 0)
FROM subscription_concurrency_grants`)
	if err := row.Scan(&status.ActiveGrants, &status.PendingGrants, &status.InactiveGrants, &status.ErrorGrants); err != nil {
		return nil, err
	}
	if err := s.db().QueryRowContext(ctx, `SELECT COUNT(1) FROM subscription_concurrency_user_states WHERE manual_override = 1`).Scan(&status.ManualOverrideUsers); err != nil {
		return nil, err
	}
	if err := s.loadSubscriptionConcurrencyRunMetadata(ctx, status); err != nil {
		return nil, err
	}
	if err := s.requireUpstreamClient(); err != nil {
		status.DefaultConcurrencyError = err.Error()
		return status, nil
	}
	defaultConcurrency, err := s.upstream.GetDefaultConcurrency(ctx)
	if err != nil {
		status.DefaultConcurrencyError = err.Error()
		return status, nil
	}
	status.DefaultConcurrency = defaultConcurrency
	return status, nil
}

// SubscriptionConcurrencyMonitorDetails aggregates the latest persisted monitor state without per-user upstream reads.
func (s *Service) SubscriptionConcurrencyMonitorDetails(ctx context.Context) ([]SubscriptionConcurrencyMonitorDetail, error) {
	rows, err := s.db().QueryContext(ctx, `
WITH grant_stats AS (
  SELECT
    upstream_user_id,
    SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END) AS active_grants,
    SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END) AS pending_grants,
    SUM(CASE WHEN status = 'inactive' THEN 1 ELSE 0 END) AS inactive_grants,
    MAX(CASE WHEN status = 'active' THEN desired_concurrency ELSE 0 END) AS active_target,
    COALESCE(MAX(last_synced_at), '') AS last_synced_at
  FROM subscription_concurrency_grants
  GROUP BY upstream_user_id
)
SELECT
  gs.upstream_user_id,
  COALESCE((
    SELECT ar.requestor_username FROM redeem_access_requests ar
    WHERE ar.requestor_upstream_user_id = gs.upstream_user_id
    ORDER BY ar.created_at DESC, ar.id DESC LIMIT 1
  ), ''),
  COALESCE((
    SELECT ar.requestor_email FROM redeem_access_requests ar
    WHERE ar.requestor_upstream_user_id = gs.upstream_user_id
    ORDER BY ar.created_at DESC, ar.id DESC LIMIT 1
  ), ''),
  CASE WHEN COALESCE(us.manual_override, 0) = 1
    THEN us.manual_override_concurrency ELSE us.last_applied_concurrency END,
  COALESCE(us.manual_override, 0),
  gs.active_grants,
  gs.pending_grants,
  gs.inactive_grants,
  gs.active_target,
  gs.last_synced_at,
  COALESCE((
    SELECT g.last_error FROM subscription_concurrency_grants g
    WHERE g.upstream_user_id = gs.upstream_user_id AND TRIM(g.last_error) <> ''
    ORDER BY g.updated_at DESC, g.id DESC LIMIT 1
  ), '')
FROM grant_stats gs
LEFT JOIN subscription_concurrency_user_states us ON us.upstream_user_id = gs.upstream_user_id
ORDER BY gs.upstream_user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	details := make([]SubscriptionConcurrencyMonitorDetail, 0)
	needsDefaultConcurrency := false
	for rows.Next() {
		var detail SubscriptionConcurrencyMonitorDetail
		var current sql.NullInt64
		var manualOverride int
		var activeTarget int
		var lastSyncedAt string
		if err := rows.Scan(
			&detail.UpstreamUserID,
			&detail.Username,
			&detail.Email,
			&current,
			&manualOverride,
			&detail.ActiveGrants,
			&detail.PendingGrants,
			&detail.InactiveGrants,
			&activeTarget,
			&lastSyncedAt,
			&detail.LastError,
		); err != nil {
			return nil, err
		}
		if current.Valid {
			value := int(current.Int64)
			detail.CurrentConcurrency = &value
		}
		detail.ManualOverride = manualOverride != 0
		detail.TargetConcurrency = activeTarget
		if detail.TargetConcurrency <= 0 {
			needsDefaultConcurrency = true
		}
		detail.LastSyncedAt, err = parseOptionalTime(lastSyncedAt)
		if err != nil {
			return nil, err
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if needsDefaultConcurrency {
		if err := s.requireUpstreamClient(); err != nil {
			return nil, err
		}
		defaultConcurrency, err := s.upstream.GetDefaultConcurrency(ctx)
		if err != nil {
			return nil, err
		}
		for i := range details {
			if details[i].TargetConcurrency <= 0 {
				details[i].TargetConcurrency = defaultConcurrency
			}
		}
	}
	return details, nil
}

func (s *Service) recordSubscriptionConcurrencyReconciliation(runErr error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	now := s.now()
	tx, err := s.db().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin subscription concurrency run metadata: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	latestError := ""
	latestErrorAt := ""
	if runErr != nil {
		latestError = runErr.Error()
		latestErrorAt = formatTime(now)
	}
	for _, state := range []struct{ key, value string }{
		{subscriptionConcurrencyLastReconciliationAtKey, formatTime(now)},
		{subscriptionConcurrencyLatestErrorKey, latestError},
		{subscriptionConcurrencyLatestErrorAtKey, latestErrorAt},
	} {
		if err := upsertSyncStateTx(ctx, tx, state.key, state.value, now); err != nil {
			return fmt.Errorf("record subscription concurrency run metadata: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit subscription concurrency run metadata: %w", err)
	}
	return nil
}

func (s *Service) loadSubscriptionConcurrencyRunMetadata(ctx context.Context, status *SubscriptionConcurrencyMonitorStatus) error {
	row := s.db().QueryRowContext(ctx, `
SELECT
  COALESCE((SELECT value FROM sync_state WHERE key = ?), ''),
  COALESCE((SELECT value FROM sync_state WHERE key = ?), ''),
  COALESCE((SELECT value FROM sync_state WHERE key = ?), '')`,
		subscriptionConcurrencyLastReconciliationAtKey,
		subscriptionConcurrencyLatestErrorKey,
		subscriptionConcurrencyLatestErrorAtKey,
	)
	var lastReconciliationAt, latestErrorAt string
	if err := row.Scan(&lastReconciliationAt, &status.LatestError, &latestErrorAt); err != nil {
		return err
	}
	var err error
	if status.LastReconciliationAt, err = parseOptionalTime(lastReconciliationAt); err != nil {
		return err
	}
	status.LatestErrorAt, err = parseOptionalTime(latestErrorAt)
	return err
}

func parseOptionalTime(raw string) (*time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	return parseTime(raw)
}

func (s *Service) repairMissingSubscriptionConcurrencyGrants(ctx context.Context) error {
	requests, err := s.listAccessRequests(ctx, nil)
	if err != nil {
		return err
	}
	var errs []error
	for i := range requests {
		request := &requests[i]
		if request.Status != "consumed" || normalizeCodeType(request.CodeType) != "subscription" {
			continue
		}
		var exists int
		if err := s.db().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM subscription_concurrency_grants WHERE access_request_id = ?)`, request.ID).Scan(&exists); err != nil {
			errs = append(errs, fmt.Errorf("inspect request %d grant: %w", request.ID, err))
			continue
		}
		if exists != 0 {
			continue
		}
		if err := s.ensureSubscriptionConcurrencyGrant(ctx, request); err != nil {
			errs = append(errs, fmt.Errorf("repair request %d grant: %w", request.ID, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Service) reconcileSubscriptionConcurrencyForUser(ctx context.Context, userID int64) error {
	if err := s.requireUpstreamClient(); err != nil {
		return err
	}
	s.concurrencyMu.Lock()
	defer s.concurrencyMu.Unlock()
	bootstrapState, err := s.getSyncState(ctx, subscriptionConcurrencyControlBootstrappedKey)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return err
	}
	return s.reconcileSubscriptionConcurrency(ctx, userID, bootstrapState != "1")
}

func (s *Service) reconcileSubscriptionConcurrency(ctx context.Context, onlyUserID int64, protectUntracked bool) error {
	userIDs, err := s.listConcurrencyGrantUserIDs(ctx, onlyUserID)
	if err != nil {
		return err
	}
	var errs []error
	for _, userID := range userIDs {
		if err := s.reconcileSubscriptionConcurrencyUser(ctx, userID, protectUntracked); err != nil {
			errs = append(errs, fmt.Errorf("user %d: %w", userID, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Service) reconcileSubscriptionConcurrencyUser(ctx context.Context, userID int64, protectUntracked bool) error {
	grants, err := s.listConcurrencyGrantRecords(ctx, userID)
	if err != nil {
		return err
	}
	if len(grants) == 0 {
		return nil
	}
	user, err := s.upstream.GetUser(ctx, userID)
	if err != nil {
		s.recordConcurrencyGrantError(ctx, userID, err.Error())
		return err
	}
	subscriptions, err := s.upstream.ListActiveUserSubscriptions(ctx, userID)
	if err != nil {
		s.recordConcurrencyGrantError(ctx, userID, err.Error())
		return err
	}
	now := s.now()
	activeDesired := 0
	for i := range grants {
		grant := &grants[i]
		match := matchingSubscription(*grant, subscriptions, now)
		status := "inactive"
		subscriptionID := grant.UpstreamSubscriptionID
		var expiresAt *time.Time
		if grantAwaitingRedeem(*grant) {
			status = "pending"
		} else if grantMayActivate(*grant) && match != nil {
			status = "active"
			id := match.ID
			subscriptionID = &id
			expires := match.ExpiresAt.UTC()
			expiresAt = &expires
			if grant.DesiredConcurrency > activeDesired {
				activeDesired = grant.DesiredConcurrency
			}
		}
		if err := s.updateConcurrencyGrantObservation(ctx, grant.ID, status, subscriptionID, expiresAt, now, ""); err != nil {
			return err
		}
	}
	target := activeDesired
	if target == 0 {
		target, err = s.upstream.GetDefaultConcurrency(ctx)
		if err != nil {
			s.recordConcurrencyGrantError(ctx, userID, err.Error())
			return err
		}
	}
	return s.reconcileConcurrencyUserTarget(ctx, userID, user.Concurrency, target, protectUntracked)
}

func (s *Service) reconcileConcurrencyUserTarget(ctx context.Context, userID int64, current, target int, protectUntracked bool) error {
	state, err := s.loadConcurrencyUserState(ctx, userID)
	if err != nil {
		return err
	}
	if !state.Exists {
		if protectUntracked && current != target {
			return s.saveConcurrencyUserState(ctx, userID, sql.NullInt64{}, true, sql.NullInt64{Int64: int64(current), Valid: true})
		}
		if current == target {
			return s.saveManagedConcurrencyUserState(ctx, userID, target)
		}
		return s.applyManagedConcurrencyTarget(ctx, userID, target)
	}
	if state.ManualOverride {
		if current == target {
			return s.saveManagedConcurrencyUserState(ctx, userID, target)
		}
		return s.saveConcurrencyUserState(ctx, userID, state.LastAppliedConcurrency, true, sql.NullInt64{Int64: int64(current), Valid: true})
	}
	if current == target {
		return s.saveManagedConcurrencyUserState(ctx, userID, target)
	}
	if !state.LastAppliedConcurrency.Valid || current != int(state.LastAppliedConcurrency.Int64) {
		return s.saveConcurrencyUserState(ctx, userID, state.LastAppliedConcurrency, true, sql.NullInt64{Int64: int64(current), Valid: true})
	}
	return s.applyManagedConcurrencyTarget(ctx, userID, target)
}

func (s *Service) applyManagedConcurrencyTarget(ctx context.Context, userID int64, target int) error {
	if _, err := s.upstream.UpdateUserConcurrency(ctx, userID, target); err != nil {
		s.recordConcurrencyGrantError(ctx, userID, err.Error())
		return err
	}
	return s.saveManagedConcurrencyUserState(ctx, userID, target)
}

func (s *Service) loadConcurrencyUserState(ctx context.Context, userID int64) (concurrencyUserState, error) {
	var state concurrencyUserState
	var manualOverride int
	err := s.db().QueryRowContext(ctx, `
SELECT last_applied_concurrency, manual_override, manual_override_concurrency
FROM subscription_concurrency_user_states WHERE upstream_user_id = ?`, userID).Scan(
		&state.LastAppliedConcurrency, &manualOverride, &state.ManualOverrideConcurrency,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	state.Exists = true
	state.ManualOverride = manualOverride != 0
	return state, nil
}

func (s *Service) saveManagedConcurrencyUserState(ctx context.Context, userID int64, concurrency int) error {
	return s.saveConcurrencyUserState(ctx, userID, sql.NullInt64{Int64: int64(concurrency), Valid: true}, false, sql.NullInt64{})
}

func (s *Service) saveConcurrencyUserState(ctx context.Context, userID int64, lastApplied sql.NullInt64, manualOverride bool, overrideConcurrency sql.NullInt64) error {
	now := s.now()
	_, err := s.db().ExecContext(ctx, `
INSERT INTO subscription_concurrency_user_states (
  upstream_user_id, last_applied_concurrency, manual_override, manual_override_concurrency, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(upstream_user_id) DO UPDATE SET
  last_applied_concurrency = excluded.last_applied_concurrency,
  manual_override = excluded.manual_override,
  manual_override_concurrency = excluded.manual_override_concurrency,
  updated_at = excluded.updated_at
`, userID, nullableInt64Value(lastApplied), boolToInt(manualOverride), nullableInt64Value(overrideConcurrency), formatTime(now), formatTime(now))
	return err
}

func nullableInt64Value(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}

func matchingSubscription(grant concurrencyGrantRecord, subscriptions []sub2api.Subscription, now time.Time) *sub2api.Subscription {
	if grant.UpstreamSubscriptionID != nil {
		for _, subscription := range subscriptions {
			if subscription.ID == *grant.UpstreamSubscriptionID && subscription.UserID == grant.UpstreamUserID && subscription.GroupID == grant.Sub2APIGroupID && strings.EqualFold(subscription.Status, "active") && subscription.ExpiresAt.After(now) {
				copy := subscription
				return &copy
			}
		}
		return nil
	}
	candidates := make([]sub2api.Subscription, 0)
	for _, subscription := range subscriptions {
		if subscription.UserID != grant.UpstreamUserID || subscription.GroupID != grant.Sub2APIGroupID || !strings.EqualFold(subscription.Status, "active") || !subscription.ExpiresAt.After(now) {
			continue
		}
		candidates = append(candidates, subscription)
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].ExpiresAt.Equal(candidates[j].ExpiresAt) {
			return candidates[i].ID > candidates[j].ID
		}
		return candidates[i].ExpiresAt.After(candidates[j].ExpiresAt)
	})
	return &candidates[0]
}

func grantMayActivate(grant concurrencyGrantRecord) bool {
	switch grant.FulfilledVia {
	case fulfilledViaRedeemCode, fulfilledViaRedeemCodeFallback:
		return grant.CodeStatus.Valid && grant.CodeStatus.String == "used" && grant.CodeUsedBy.Valid && grant.CodeUsedBy.Int64 == grant.UpstreamUserID
	default:
		return true
	}
}

func grantAwaitingRedeem(grant concurrencyGrantRecord) bool {
	switch grant.FulfilledVia {
	case fulfilledViaRedeemCode, fulfilledViaRedeemCodeFallback:
		return !(grant.CodeStatus.Valid && grant.CodeStatus.String == "used" && grant.CodeUsedBy.Valid)
	default:
		return false
	}
}

func (s *Service) ensureSubscriptionConcurrencyGrant(ctx context.Context, request *models.AccessRequest) error {
	if request == nil || normalizeCodeType(request.CodeType) != "subscription" || request.Sub2APIGroupID == nil || *request.Sub2APIGroupID <= 0 {
		return nil
	}
	desired := request.Concurrency
	if desired <= 0 && request.TierID > 0 {
		if tier, err := s.getRedeemTierByID(ctx, request.TierID); err == nil {
			desired = tier.Concurrency
		}
	}
	if desired <= 0 {
		err := s.db().QueryRowContext(ctx, `
SELECT concurrency FROM redeem_tiers
WHERE code_type = 'subscription' AND sub2api_group_id = ? AND concurrency > 0
ORDER BY enabled DESC, sort_order ASC, id ASC
LIMIT 1`, *request.Sub2APIGroupID).Scan(&desired)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("look up subscription concurrency for group %d: %w", *request.Sub2APIGroupID, err)
		}
	}
	if desired <= 0 {
		return fmt.Errorf("subscription request %d has no concurrency", request.ID)
	}
	now := s.now()
	_, err := s.db().ExecContext(ctx, `
INSERT INTO subscription_concurrency_grants (
  access_request_id, upstream_user_id, tier_id, sub2api_group_id, desired_concurrency, status,
  created_at, updated_at
) VALUES (?, ?, ?, ?, ?, 'pending', ?, ?)
ON CONFLICT(access_request_id) DO NOTHING
`, request.ID, request.RequestorUpstreamUserID, request.TierID, *request.Sub2APIGroupID, desired, formatTime(now), formatTime(now))
	return err
}

func (s *Service) listConcurrencyGrantUserIDs(ctx context.Context, onlyUserID int64) ([]int64, error) {
	query := `SELECT DISTINCT upstream_user_id FROM subscription_concurrency_grants`
	args := []any{}
	if onlyUserID > 0 {
		query += ` WHERE upstream_user_id = ?`
		args = append(args, onlyUserID)
	}
	rows, err := s.db().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Service) listConcurrencyGrantRecords(ctx context.Context, userID int64) ([]concurrencyGrantRecord, error) {
	rows, err := s.db().QueryContext(ctx, `
SELECT g.id, g.access_request_id, g.upstream_user_id, g.tier_id, g.sub2api_group_id, g.desired_concurrency,
       g.upstream_subscription_id, g.status, g.upstream_expires_at, g.last_synced_at, g.last_error, g.created_at, g.updated_at,
       ar.fulfilled_via, c.status, c.used_by_upstream_user_id
FROM subscription_concurrency_grants g
JOIN redeem_access_requests ar ON ar.id = g.access_request_id
LEFT JOIN redeem_requests rr ON rr.access_request_id = ar.id
LEFT JOIN redeem_codes c ON c.request_id = rr.id
WHERE g.upstream_user_id = ?
ORDER BY g.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []concurrencyGrantRecord{}
	for rows.Next() {
		var record concurrencyGrantRecord
		var subID sql.NullInt64
		var expiresAt, syncedAt sql.NullString
		var createdAt, updatedAt string
		if err := rows.Scan(&record.ID, &record.AccessRequestID, &record.UpstreamUserID, &record.TierID, &record.Sub2APIGroupID, &record.DesiredConcurrency, &subID, &record.Status, &expiresAt, &syncedAt, &record.LastError, &createdAt, &updatedAt, &record.FulfilledVia, &record.CodeStatus, &record.CodeUsedBy); err != nil {
			return nil, err
		}
		var err error
		record.UpstreamSubscriptionID = parseNullableInt64(subID)
		if record.UpstreamExpiresAt, err = scanMaybeTime(expiresAt); err != nil {
			return nil, err
		}
		if record.LastSyncedAt, err = scanMaybeTime(syncedAt); err != nil {
			return nil, err
		}
		if record.CreatedAt, err = parseNonNullTime(createdAt); err != nil {
			return nil, err
		}
		if record.UpdatedAt, err = parseNonNullTime(updatedAt); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (s *Service) updateConcurrencyGrantObservation(ctx context.Context, id int64, status string, subscriptionID *int64, expiresAt *time.Time, now time.Time, lastError string) error {
	_, err := s.db().ExecContext(ctx, `
UPDATE subscription_concurrency_grants
SET status = ?, upstream_subscription_id = ?, upstream_expires_at = ?, last_synced_at = ?, last_error = ?, updated_at = ?
WHERE id = ?`, status, subscriptionID, formatNullableTime(expiresAt), formatTime(now), lastError, formatTime(now), id)
	return err
}

func (s *Service) recordConcurrencyGrantError(ctx context.Context, userID int64, message string) {
	now := s.now()
	_, _ = s.db().ExecContext(ctx, `UPDATE subscription_concurrency_grants SET last_error = ?, updated_at = ? WHERE upstream_user_id = ?`, strings.TrimSpace(message), formatTime(now), userID)
}
