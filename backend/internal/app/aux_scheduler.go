package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"sub2api-giftcode/backend/internal/models"
	"sub2api-giftcode/backend/internal/sub2api"
)

const (
	AuxSchedulerStateIdle         = "idle"
	AuxSchedulerStateBackupActive = "backup_active"
)

type AuxSchedulerRuleInput struct {
	Name              string  `json:"name"`
	Enabled           bool    `json:"enabled"`
	PrimaryAccountIDs []int64 `json:"primary_account_ids"`
	BackupAccountIDs  []int64 `json:"backup_account_ids"`
}

type AuxSchedulerAccountInfo struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Status string `json:"status"`
}

type AuxSchedulerRuleView struct {
	models.AuxSchedulerRule
	PrimaryAccounts []AuxSchedulerAccountInfo `json:"primary_accounts"`
	BackupAccounts  []AuxSchedulerAccountInfo `json:"backup_accounts"`
	UpstreamError   string                    `json:"upstream_error,omitempty"`
}

func (s *Service) RunAuxSchedulerLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if err := s.ReconcileAuxScheduler(ctx); err != nil {
		log.Printf("aux scheduler reconcile failed: %v", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.ReconcileAuxScheduler(ctx); err != nil {
				log.Printf("aux scheduler reconcile failed: %v", err)
			}
		}
	}
}

func (s *Service) ReconcileAuxScheduler(ctx context.Context) error {
	if err := s.requireUpstreamClient(); err != nil {
		return err
	}
	s.auxMu.Lock()
	defer s.auxMu.Unlock()

	rules, err := s.listAuxSchedulerRulesRaw(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for i := range rules {
		if !rules[i].Enabled {
			continue
		}
		if err := s.reconcileAuxSchedulerRule(ctx, &rules[i]); err != nil {
			errs = append(errs, fmt.Errorf("aux scheduler rule %d: %w", rules[i].ID, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Service) ListAuxSchedulerRules(ctx context.Context) ([]AuxSchedulerRuleView, error) {
	rules, err := s.listAuxSchedulerRulesRaw(ctx)
	if err != nil {
		return nil, err
	}
	return s.enrichAuxSchedulerRules(ctx, rules)
}

func (s *Service) CreateAuxSchedulerRule(ctx context.Context, input AuxSchedulerRuleInput) (*AuxSchedulerRuleView, error) {
	s.auxMu.Lock()
	defer s.auxMu.Unlock()

	if err := s.validateAuxSchedulerInput(ctx, input); err != nil {
		return nil, err
	}
	now := s.now()
	primaryJSON, err := json.Marshal(deduplicateInt64s(input.PrimaryAccountIDs))
	if err != nil {
		return nil, err
	}
	backupJSON, err := json.Marshal(deduplicateInt64s(input.BackupAccountIDs))
	if err != nil {
		return nil, err
	}
	result, err := s.db().ExecContext(ctx, `
INSERT INTO aux_scheduler_rules (
  name, enabled, primary_account_ids_json, backup_account_ids_json,
  state, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?)
`, strings.TrimSpace(input.Name), boolToInt(input.Enabled), string(primaryJSON), string(backupJSON),
		AuxSchedulerStateIdle, formatTime(now), formatTime(now))
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.getAuxSchedulerRuleView(ctx, id)
}

func (s *Service) UpdateAuxSchedulerRule(ctx context.Context, id int64, input AuxSchedulerRuleInput) (*AuxSchedulerRuleView, error) {
	s.auxMu.Lock()
	defer s.auxMu.Unlock()

	existing, err := s.getAuxSchedulerRuleRaw(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.validateAuxSchedulerInput(ctx, input); err != nil {
		return nil, err
	}

	state := existing.State
	activatedAt := existing.ActivatedAt
	if existing.State == AuxSchedulerStateBackupActive {
		nextBackupIDs := deduplicateInt64s(input.BackupAccountIDs)
		if input.Enabled {
			removed := differenceInt64s(existing.BackupAccountIDs, nextBackupIDs)
			if err := s.deactivateAuxSchedulerBackupIDs(ctx, removed); err != nil {
				return nil, err
			}
			added := differenceInt64s(nextBackupIDs, existing.BackupAccountIDs)
			if err := s.activateAuxSchedulerBackupIDs(ctx, added); err != nil {
				return nil, err
			}
		} else {
			if err := s.deactivateAuxSchedulerBackupIDs(ctx, existing.BackupAccountIDs); err != nil {
				return nil, err
			}
			state = AuxSchedulerStateIdle
			activatedAt = nil
		}
	}
	primaryJSON, err := json.Marshal(deduplicateInt64s(input.PrimaryAccountIDs))
	if err != nil {
		return nil, err
	}
	backupJSON, err := json.Marshal(deduplicateInt64s(input.BackupAccountIDs))
	if err != nil {
		return nil, err
	}
	_, err = s.db().ExecContext(ctx, `
UPDATE aux_scheduler_rules
SET name = ?, enabled = ?, primary_account_ids_json = ?, backup_account_ids_json = ?,
    state = ?, activated_at = ?, last_error = '', updated_at = ?
WHERE id = ?
`, strings.TrimSpace(input.Name), boolToInt(input.Enabled), string(primaryJSON), string(backupJSON),
		state, formatNullableTime(activatedAt), formatTime(s.now()), id)
	if err != nil {
		return nil, err
	}
	return s.getAuxSchedulerRuleView(ctx, id)
}

func (s *Service) DeleteAuxSchedulerRule(ctx context.Context, id int64) error {
	s.auxMu.Lock()
	defer s.auxMu.Unlock()

	existing, err := s.getAuxSchedulerRuleRaw(ctx, id)
	if err != nil {
		return err
	}
	if existing.State == AuxSchedulerStateBackupActive {
		if err := s.deactivateAuxSchedulerBackups(ctx, *existing); err != nil {
			return err
		}
	}
	result, err := s.db().ExecContext(ctx, `DELETE FROM aux_scheduler_rules WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) CheckAuxSchedulerRule(ctx context.Context, id int64) (*AuxSchedulerRuleView, error) {
	s.auxMu.Lock()
	defer s.auxMu.Unlock()

	rule, err := s.getAuxSchedulerRuleRaw(ctx, id)
	if err != nil {
		return nil, err
	}
	if rule.Enabled {
		if err := s.reconcileAuxSchedulerRule(ctx, rule); err != nil {
			return nil, err
		}
	}
	return s.getAuxSchedulerRuleView(ctx, id)
}

func (s *Service) listAuxSchedulerRulesRaw(ctx context.Context) ([]models.AuxSchedulerRule, error) {
	rows, err := s.db().QueryContext(ctx, `
SELECT id, name, enabled, primary_account_ids_json, backup_account_ids_json,
       state, activated_at, last_checked_at, last_error, created_at, updated_at
FROM aux_scheduler_rules
ORDER BY id
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.AuxSchedulerRule, 0)
	for rows.Next() {
		rule, err := scanAuxSchedulerRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) getAuxSchedulerRuleRaw(ctx context.Context, id int64) (*models.AuxSchedulerRule, error) {
	row := s.db().QueryRowContext(ctx, `
SELECT id, name, enabled, primary_account_ids_json, backup_account_ids_json,
       state, activated_at, last_checked_at, last_error, created_at, updated_at
FROM aux_scheduler_rules
WHERE id = ?
`, id)
	rule, err := scanAuxSchedulerRule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

func (s *Service) getAuxSchedulerRuleView(ctx context.Context, id int64) (*AuxSchedulerRuleView, error) {
	rule, err := s.getAuxSchedulerRuleRaw(ctx, id)
	if err != nil {
		return nil, err
	}
	views, err := s.enrichAuxSchedulerRules(ctx, []models.AuxSchedulerRule{*rule})
	if err != nil {
		return nil, err
	}
	return &views[0], nil
}

func (s *Service) enrichAuxSchedulerRules(ctx context.Context, rules []models.AuxSchedulerRule) ([]AuxSchedulerRuleView, error) {
	accounts := map[int64]sub2api.Account{}
	upstreamError := ""
	upstreamAccounts, err := s.ListOpenAIAccounts(ctx)
	if err != nil {
		upstreamError = err.Error()
	} else {
		for i := range upstreamAccounts {
			accounts[upstreamAccounts[i].ID] = upstreamAccounts[i]
		}
	}

	views := make([]AuxSchedulerRuleView, 0, len(rules))
	for _, rule := range rules {
		view := AuxSchedulerRuleView{AuxSchedulerRule: rule, UpstreamError: upstreamError}
		view.PrimaryAccounts = auxAccountInfos(rule.PrimaryAccountIDs, accounts)
		view.BackupAccounts = auxAccountInfos(rule.BackupAccountIDs, accounts)
		views = append(views, view)
	}
	return views, nil
}

func auxAccountInfos(ids []int64, accounts map[int64]sub2api.Account) []AuxSchedulerAccountInfo {
	out := make([]AuxSchedulerAccountInfo, 0, len(ids))
	for _, id := range ids {
		info := AuxSchedulerAccountInfo{ID: id}
		if account, ok := accounts[id]; ok {
			info.Name = account.Name
			info.Type = account.Type
			info.Status = account.Status
		}
		out = append(out, info)
	}
	return out
}

func scanAuxSchedulerRule(row interface{ Scan(dest ...any) error }) (models.AuxSchedulerRule, error) {
	var rule models.AuxSchedulerRule
	var enabled int
	var primaryJSON, backupJSON, createdAt, updatedAt string
	var activatedAt, lastCheckedAt sql.NullString
	if err := row.Scan(
		&rule.ID, &rule.Name, &enabled, &primaryJSON, &backupJSON,
		&rule.State, &activatedAt, &lastCheckedAt, &rule.LastError, &createdAt, &updatedAt,
	); err != nil {
		return rule, err
	}
	rule.Enabled = enabled != 0
	var err error
	if rule.PrimaryAccountIDs, err = parseInt64JSON(primaryJSON); err != nil {
		return rule, err
	}
	if rule.BackupAccountIDs, err = parseInt64JSON(backupJSON); err != nil {
		return rule, err
	}
	if activatedAt.Valid {
		if rule.ActivatedAt, err = parseTime(activatedAt.String); err != nil {
			return rule, err
		}
	}
	if lastCheckedAt.Valid {
		if rule.LastCheckedAt, err = parseTime(lastCheckedAt.String); err != nil {
			return rule, err
		}
	}
	if rule.CreatedAt, err = parseNonNullTime(createdAt); err != nil {
		return rule, err
	}
	if rule.UpdatedAt, err = parseNonNullTime(updatedAt); err != nil {
		return rule, err
	}
	return rule, nil
}

func parseInt64JSON(raw string) ([]int64, error) {
	var out []int64
	if strings.TrimSpace(raw) == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) validateAuxSchedulerInput(ctx context.Context, input AuxSchedulerRuleInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return fmt.Errorf("%w: rule name is required", ErrBadRequest)
	}
	if len(input.PrimaryAccountIDs) == 0 || len(input.BackupAccountIDs) == 0 {
		return fmt.Errorf("%w: primary and backup accounts are required", ErrBadRequest)
	}
	if err := s.requireUpstreamClient(); err != nil {
		return err
	}
	accounts, err := s.ListOpenAIAccounts(ctx)
	if err != nil {
		return err
	}
	byID := make(map[int64]sub2api.Account, len(accounts))
	for i := range accounts {
		byID[accounts[i].ID] = accounts[i]
	}
	primary := deduplicateInt64s(input.PrimaryAccountIDs)
	backup := deduplicateInt64s(input.BackupAccountIDs)
	if len(primary) == 0 || len(backup) == 0 {
		return fmt.Errorf("%w: primary and backup accounts are required", ErrBadRequest)
	}
	primarySet := make(map[int64]struct{}, len(primary))
	for _, id := range primary {
		primarySet[id] = struct{}{}
		if err := validateAuxSchedulerAccount(id, byID); err != nil {
			return err
		}
	}
	for _, id := range backup {
		if _, ok := primarySet[id]; ok {
			return fmt.Errorf("%w: account %d cannot be both primary and backup", ErrBadRequest, id)
		}
		if err := validateAuxSchedulerAccount(id, byID); err != nil {
			return err
		}
	}
	return nil
}

func validateAuxSchedulerAccount(id int64, accounts map[int64]sub2api.Account) error {
	account, ok := accounts[id]
	if !ok {
		return fmt.Errorf("%w: OpenAI account %d not found", ErrBadRequest, id)
	}
	accountType := strings.ToLower(strings.TrimSpace(account.Type))
	if accountType != "oauth" && accountType != "apikey" {
		return fmt.Errorf("%w: account %d type %q is not supported, only oauth and apikey", ErrBadRequest, id, account.Type)
	}
	return nil
}

func deduplicateInt64s(values []int64) []int64 {
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
	return out
}

func differenceInt64s(base, exclude []int64) []int64 {
	excluded := make(map[int64]struct{}, len(exclude))
	for _, value := range exclude {
		excluded[value] = struct{}{}
	}
	out := make([]int64, 0, len(base))
	for _, value := range base {
		if _, ok := excluded[value]; !ok {
			out = append(out, value)
		}
	}
	return out
}

func (s *Service) reconcileAuxSchedulerRule(ctx context.Context, rule *models.AuxSchedulerRule) error {
	switch rule.State {
	case AuxSchedulerStateBackupActive:
		unavailable, err := s.anyAuxPrimaryUnavailable(ctx, rule.PrimaryAccountIDs)
		if err != nil {
			return s.recordAuxSchedulerError(ctx, rule.ID, err)
		}
		if unavailable {
			return s.recordAuxSchedulerChecked(ctx, rule.ID)
		}
		if rule.ActivatedAt == nil {
			return s.recordAuxSchedulerError(ctx, rule.ID, errors.New("backup activation timestamp is missing"))
		}
		hasSuccess, err := s.anyAuxPrimarySucceededSince(ctx, rule.PrimaryAccountIDs, *rule.ActivatedAt)
		if err != nil {
			return s.recordAuxSchedulerError(ctx, rule.ID, err)
		}
		if !hasSuccess {
			return s.recordAuxSchedulerChecked(ctx, rule.ID)
		}
		if err := s.deactivateAuxSchedulerBackups(ctx, *rule); err != nil {
			return s.recordAuxSchedulerError(ctx, rule.ID, err)
		}
		return s.setAuxSchedulerState(ctx, rule.ID, AuxSchedulerStateIdle, nil)
	default:
		unavailable, err := s.anyAuxPrimaryUnavailable(ctx, rule.PrimaryAccountIDs)
		if err != nil {
			return s.recordAuxSchedulerError(ctx, rule.ID, err)
		}
		if !unavailable {
			return s.recordAuxSchedulerChecked(ctx, rule.ID)
		}
		if err := s.activateAuxSchedulerBackups(ctx, *rule); err != nil {
			return s.recordAuxSchedulerError(ctx, rule.ID, err)
		}
		now := s.now()
		return s.setAuxSchedulerState(ctx, rule.ID, AuxSchedulerStateBackupActive, &now)
	}
}

func (s *Service) anyAuxPrimaryUnavailable(ctx context.Context, accountIDs []int64) (bool, error) {
	now := s.now()
	for _, id := range accountIDs {
		account, err := s.upstream.GetAccount(ctx, id)
		if err != nil {
			return false, fmt.Errorf("load primary account %d: %w", id, err)
		}
		if accountHasAuxTemporaryUnavailability(*account, now) {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) anyAuxPrimarySucceededSince(ctx context.Context, accountIDs []int64, since time.Time) (bool, error) {
	for _, id := range accountIDs {
		account, err := s.upstream.GetAccount(ctx, id)
		if err != nil {
			return false, fmt.Errorf("load primary account %d: %w", id, err)
		}
		if account.LastUsedAt != nil && account.LastUsedAt.After(since) {
			return true, nil
		}
		hasLog, err := s.upstream.HasOpenAIUsageLogAfter(ctx, id, since)
		if err != nil {
			return false, fmt.Errorf("load usage logs for account %d: %w", id, err)
		}
		if hasLog {
			return true, nil
		}
	}
	return false, nil
}

func accountHasAuxTemporaryUnavailability(account sub2api.Account, now time.Time) bool {
	if !strings.EqualFold(strings.TrimSpace(account.Status), "active") {
		return true
	}
	for _, until := range []*time.Time{account.TempUnschedulableUntil, account.RateLimitResetAt, account.OverloadUntil} {
		if until != nil && now.Before(*until) {
			return true
		}
	}
	rawLimits, ok := account.Extra["model_rate_limits"].(map[string]any)
	if !ok {
		return false
	}
	for _, rawEntry := range rawLimits {
		resetAt := parseModelRateLimitResetAt(rawEntry)
		if resetAt != nil && now.Before(*resetAt) {
			return true
		}
	}
	return false
}

func parseModelRateLimitResetAt(raw any) *time.Time {
	switch value := raw.(type) {
	case string:
		parsed, _ := parseTime(value)
		return parsed
	case map[string]any:
		resetAt, _ := value["rate_limit_reset_at"].(string)
		parsed, _ := parseTime(resetAt)
		return parsed
	case map[string]string:
		parsed, _ := parseTime(value["rate_limit_reset_at"])
		return parsed
	default:
		return nil
	}
}

func (s *Service) activateAuxSchedulerBackups(ctx context.Context, rule models.AuxSchedulerRule) error {
	return s.activateAuxSchedulerBackupIDs(ctx, rule.BackupAccountIDs)
}

func (s *Service) activateAuxSchedulerBackupIDs(ctx context.Context, accountIDs []int64) error {
	var failures []string
	for _, id := range accountIDs {
		if _, err := s.upstream.UpdateOpenAIAccountStatus(ctx, id, "active"); err != nil {
			failures = append(failures, fmt.Sprintf("account %d: %v", id, err))
			continue
		}
		if _, err := s.upstream.SetOpenAIAccountSchedulable(ctx, id, true); err != nil {
			failures = append(failures, fmt.Sprintf("account %d schedulable: %v", id, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("activate backup accounts failed: %s", strings.Join(failures, "; "))
	}
	return nil
}

func (s *Service) deactivateAuxSchedulerBackups(ctx context.Context, rule models.AuxSchedulerRule) error {
	return s.deactivateAuxSchedulerBackupIDs(ctx, rule.BackupAccountIDs)
}

func (s *Service) deactivateAuxSchedulerBackupIDs(ctx context.Context, accountIDs []int64) error {
	var failures []string
	for _, id := range accountIDs {
		if _, err := s.upstream.UpdateOpenAIAccountStatus(ctx, id, "inactive"); err != nil {
			failures = append(failures, fmt.Sprintf("account %d: %v", id, err))
			continue
		}
		if _, err := s.upstream.SetOpenAIAccountSchedulable(ctx, id, false); err != nil {
			failures = append(failures, fmt.Sprintf("account %d schedulable: %v", id, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("deactivate backup accounts failed: %s", strings.Join(failures, "; "))
	}
	return nil
}

func (s *Service) setAuxSchedulerState(ctx context.Context, id int64, state string, activatedAt *time.Time) error {
	now := s.now()
	_, err := s.db().ExecContext(ctx, `
UPDATE aux_scheduler_rules
SET state = ?, activated_at = ?, last_checked_at = ?, last_error = '', updated_at = ?
WHERE id = ?
`, state, formatNullableTime(activatedAt), formatTime(now), formatTime(now), id)
	return err
}

func (s *Service) recordAuxSchedulerChecked(ctx context.Context, id int64) error {
	now := s.now()
	_, err := s.db().ExecContext(ctx, `
UPDATE aux_scheduler_rules
SET last_checked_at = ?, updated_at = ?
WHERE id = ?
`, formatTime(now), formatTime(now), id)
	return err
}

func (s *Service) recordAuxSchedulerError(ctx context.Context, id int64, err error) error {
	now := s.now()
	message := ""
	if err != nil {
		message = err.Error()
	}
	_, updateErr := s.db().ExecContext(ctx, `
UPDATE aux_scheduler_rules
SET last_error = ?, last_checked_at = ?, updated_at = ?
WHERE id = ?
`, message, formatTime(now), formatTime(now), id)
	if updateErr != nil {
		return errors.Join(err, updateErr)
	}
	return err
}
