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
	AuxSchedulerStateIdle                     = "idle"
	AuxSchedulerStateBackupActive             = "backup_active"
	AuxSchedulerMigrationStatusNeedsMigration = "needs_migration"
)

const auxSchedulerRuleColumns = `
id, name, enabled, primary_account_ids_json, backup_account_ids_json,
model_names_json, lanes_json, maximum_auto_lane, migration_status, migration_source_json,
state, expected_open_through_lane, observed_open_through_lane, verified_open_through_lane,
target_open_through_lane, transition_status, transition_generation,
upgrade_evidence_json, missing_models_json,
recovery_candidate_lane, recovery_candidate_since,
last_observed_at, last_verified_at, blocked_reason, warnings,
activated_at, last_checked_at, last_error, created_at, updated_at`

type AuxSchedulerRuleInput struct {
	Name              string    `json:"name"`
	Enabled           bool      `json:"enabled"`
	ModelNames        []string  `json:"model_names"`
	Lanes             [][]int64 `json:"lanes"`
	MaximumAutoLane   int       `json:"maximum_auto_lane"`
	PrimaryAccountIDs []int64   `json:"primary_account_ids"`
	BackupAccountIDs  []int64   `json:"backup_account_ids"`
}

type AuxSchedulerAccountInfo struct {
	ID          int64  `json:"id"`
	Name        string `json:"name,omitempty"`
	Type        string `json:"type,omitempty"`
	Status      string `json:"status,omitempty"`
	Schedulable *bool  `json:"schedulable,omitempty"`
}

type AuxSchedulerLaneView struct {
	Number   int                       `json:"number"`
	Accounts []AuxSchedulerAccountInfo `json:"accounts"`
}

type AuxSchedulerRuleView struct {
	models.AuxSchedulerRule
	LaneAccounts    []AuxSchedulerLaneView    `json:"lane_accounts"`
	PrimaryAccounts []AuxSchedulerAccountInfo `json:"primary_accounts,omitempty"`
	BackupAccounts  []AuxSchedulerAccountInfo `json:"backup_accounts,omitempty"`
	UpstreamError   string                    `json:"upstream_error,omitempty"`
}

func (s *Service) RunAuxSchedulerLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if err := s.ReconcileAuxScheduler(ctx); err != nil {
		log.Printf("辅助调度器扫描失败: %v", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.ReconcileAuxScheduler(ctx); err != nil {
				log.Printf("辅助调度器扫描失败: %v", err)
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
		if !rules[i].Enabled || rules[i].MigrationStatus == AuxSchedulerMigrationStatusNeedsMigration {
			continue
		}
		if err := s.reconcileAuxSchedulerRule(ctx, &rules[i]); err != nil {
			errs = append(errs, fmt.Errorf("辅助调度器规则 %d: %w", rules[i].ID, err))
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

	config := normalizeAuxSchedulerConfig(input)
	warnings, err := s.validateAuxSchedulerInput(ctx, input, config, 0)
	if err != nil {
		return nil, err
	}
	now := s.now()
	tx, err := s.db().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if config.Enabled {
		if err := s.assertAuxSchedulerOwnershipInTx(ctx, tx, config.Lanes, 0); err != nil {
			return nil, err
		}
	}
	modelJSON, err := json.Marshal(config.ModelNames)
	if err != nil {
		return nil, err
	}
	lanesJSON, err := json.Marshal(config.Lanes)
	if err != nil {
		return nil, err
	}
	primaryJSON, err := json.Marshal(config.PrimaryAccountIDs)
	if err != nil {
		return nil, err
	}
	backupJSON, err := json.Marshal(config.BackupAccountIDs)
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `
INSERT INTO aux_scheduler_rules (
  name, enabled, primary_account_ids_json, backup_account_ids_json,
  model_names_json, lanes_json, maximum_auto_lane, migration_status, migration_source_json,
  state, expected_open_through_lane, observed_open_through_lane, verified_open_through_lane,
  target_open_through_lane, transition_status, transition_generation,
  upgrade_evidence_json, missing_models_json,
  created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, '', '', ?, 1, 1, 1, 1, 'stable', 0, '{}', '[]', ?, ?)
`, strings.TrimSpace(config.Name), boolToInt(config.Enabled), string(primaryJSON), string(backupJSON),
		string(modelJSON), string(lanesJSON), config.MaximumAutoLane,
		AuxSchedulerStateIdle, formatTime(now), formatTime(now))
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	if len(warnings) > 0 {
		_, err = tx.ExecContext(ctx, `
UPDATE aux_scheduler_rules
SET last_error = ?, updated_at = ?
WHERE id = ?
`, strings.Join(warnings, "; "), formatTime(now), id)
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
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
	config := normalizeAuxSchedulerConfig(input)
	warnings, err := s.validateAuxSchedulerInput(ctx, input, config, id)
	if err != nil {
		return nil, err
	}

	state := existing.State
	activatedAt := existing.ActivatedAt
	if existing.State == AuxSchedulerStateBackupActive && existing.MigrationStatus != AuxSchedulerMigrationStatusNeedsMigration && !config.NewShape {
		nextBackupIDs := deduplicateInt64s(input.BackupAccountIDs)
		if input.Enabled {
			removed := differenceInt64s(existing.BackupAccountIDs, nextBackupIDs)
			if err := s.deactivateAuxSchedulerBackupIDs(ctx, removed); err != nil {
				return nil, err
			}
			added := differenceInt64s(nextBackupIDs, existing.BackupAccountIDs)
			activationWarnings, err := s.activateAuxSchedulerBackupIDs(ctx, added)
			if err != nil {
				return nil, err
			}
			warnings = append(warnings, activationWarnings...)
		} else {
			if err := s.deactivateAuxSchedulerBackupIDs(ctx, existing.BackupAccountIDs); err != nil {
				return nil, err
			}
			state = AuxSchedulerStateIdle
			activatedAt = nil
		}
	}
	now := s.now()
	tx, err := s.db().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if config.Enabled {
		if err := s.assertAuxSchedulerOwnershipInTx(ctx, tx, config.Lanes, id); err != nil {
			return nil, err
		}
	}
	migrationStatus := existing.MigrationStatus
	modelJSON, err := json.Marshal(config.ModelNames)
	if err != nil {
		return nil, err
	}
	lanesJSON, err := json.Marshal(config.Lanes)
	if err != nil {
		return nil, err
	}
	primaryJSON, err := json.Marshal(config.PrimaryAccountIDs)
	if err != nil {
		return nil, err
	}
	backupJSON, err := json.Marshal(config.BackupAccountIDs)
	if err != nil {
		return nil, err
	}
	sourceJSON := ""
	if !config.NewShape {
		if existing.MigrationSource != nil {
			raw, marshalErr := json.Marshal(existing.MigrationSource)
			if marshalErr != nil {
				return nil, marshalErr
			}
			sourceJSON = string(raw)
		}
	} else {
		migrationStatus = ""
	}
	_, err = tx.ExecContext(ctx, `
UPDATE aux_scheduler_rules
SET name = ?, enabled = ?, primary_account_ids_json = ?, backup_account_ids_json = ?,
    model_names_json = ?, lanes_json = ?, maximum_auto_lane = ?,
    migration_status = ?, migration_source_json = ?,
    state = ?, activated_at = ?, last_error = ?, updated_at = ?
WHERE id = ?
`, strings.TrimSpace(config.Name), boolToInt(config.Enabled), string(primaryJSON), string(backupJSON),
		string(modelJSON), string(lanesJSON), config.MaximumAutoLane,
		migrationStatus, sourceJSON,
		state, formatNullableTime(activatedAt), strings.Join(warnings, "; "), formatTime(now), id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
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
	if existing.State == AuxSchedulerStateBackupActive && existing.MigrationStatus != AuxSchedulerMigrationStatusNeedsMigration {
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
	if rule.Enabled && rule.MigrationStatus != AuxSchedulerMigrationStatusNeedsMigration {
		if err := s.reconcileAuxSchedulerRule(ctx, rule); err != nil {
			return nil, err
		}
	}
	return s.getAuxSchedulerRuleView(ctx, id)
}

func (s *Service) listAuxSchedulerRulesRaw(ctx context.Context) ([]models.AuxSchedulerRule, error) {
	rows, err := s.db().QueryContext(ctx, `
SELECT `+auxSchedulerRuleColumns+`
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
SELECT `+auxSchedulerRuleColumns+`
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
		view.LaneAccounts = auxSchedulerLaneViews(rule, accounts)
		view.PrimaryAccounts = auxAccountInfos(rule.PrimaryAccountIDs, accounts)
		view.BackupAccounts = auxAccountInfos(rule.BackupAccountIDs, accounts)
		views = append(views, view)
	}
	return views, nil
}

func auxSchedulerLaneViews(rule models.AuxSchedulerRule, accounts map[int64]sub2api.Account) []AuxSchedulerLaneView {
	lanes := rule.Lanes
	if len(lanes) == 0 {
		lanes = [][]int64{rule.PrimaryAccountIDs, rule.BackupAccountIDs}
	}
	out := make([]AuxSchedulerLaneView, 0, len(lanes))
	for index, ids := range lanes {
		out = append(out, AuxSchedulerLaneView{
			Number:   index + 1,
			Accounts: auxAccountInfos(ids, accounts),
		})
	}
	return out
}

func auxAccountInfos(ids []int64, accounts map[int64]sub2api.Account) []AuxSchedulerAccountInfo {
	out := make([]AuxSchedulerAccountInfo, 0, len(ids))
	for _, id := range ids {
		info := AuxSchedulerAccountInfo{ID: id}
		if account, ok := accounts[id]; ok {
			info.Name = account.Name
			info.Type = account.Type
			info.Status = account.Status
			value := account.Schedulable
			info.Schedulable = &value
		}
		out = append(out, info)
	}
	return out
}

func scanAuxSchedulerRule(row interface{ Scan(dest ...any) error }) (models.AuxSchedulerRule, error) {
	var rule models.AuxSchedulerRule
	var enabled int
	var primaryJSON, backupJSON string
	var modelNamesJSON, lanesJSON, migrationSourceJSON string
	var upgradeEvidenceJSON, missingModelsJSON string
	var recoveryCandidateLane sql.NullInt64
	var recoveryCandidateSince, lastObservedAt, lastVerifiedAt sql.NullString
	var createdAt, updatedAt string
	var activatedAt, lastCheckedAt sql.NullString
	if err := row.Scan(
		&rule.ID, &rule.Name, &enabled, &primaryJSON, &backupJSON,
		&modelNamesJSON, &lanesJSON, &rule.MaximumAutoLane, &rule.MigrationStatus, &migrationSourceJSON,
		&rule.State, &rule.ExpectedOpenThroughLane, &rule.ObservedOpenThroughLane, &rule.VerifiedOpenThroughLane,
		&rule.TargetOpenThroughLane, &rule.TransitionStatus, &rule.TransitionGeneration,
		&upgradeEvidenceJSON, &missingModelsJSON,
		&recoveryCandidateLane, &recoveryCandidateSince,
		&lastObservedAt, &lastVerifiedAt, &rule.BlockedReason, &rule.Warnings,
		&activatedAt, &lastCheckedAt, &rule.LastError, &createdAt, &updatedAt,
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
	if rule.ModelNames, err = parseStringJSON(modelNamesJSON); err != nil {
		return rule, err
	}
	if rule.Lanes, err = parseInt64SlicesJSON(lanesJSON); err != nil {
		return rule, err
	}
	if rule.MigrationSource, err = parseAuxMigrationSourceJSON(migrationSourceJSON); err != nil {
		return rule, err
	}
	if rule.UpgradeEvidence, err = parseAnyJSONMap(upgradeEvidenceJSON); err != nil {
		return rule, err
	}
	if rule.MissingModels, err = parseStringJSON(missingModelsJSON); err != nil {
		return rule, err
	}
	if recoveryCandidateLane.Valid {
		value := int(recoveryCandidateLane.Int64)
		rule.RecoveryCandidateLane = &value
	}
	if rule.RecoveryCandidateSince, err = parseTime(recoveryCandidateSince.String); err != nil {
		return rule, err
	}
	if rule.LastObservedAt, err = parseTime(lastObservedAt.String); err != nil {
		return rule, err
	}
	if rule.LastVerifiedAt, err = parseTime(lastVerifiedAt.String); err != nil {
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

func parseInt64SlicesJSON(raw string) ([][]int64, error) {
	var out [][]int64
	if strings.TrimSpace(raw) == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func parseStringJSON(raw string) ([]string, error) {
	var out []string
	if strings.TrimSpace(raw) == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func parseAnyJSONMap(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func parseAuxMigrationSourceJSON(raw string) (*models.AuxSchedulerMigrationSource, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var out models.AuxSchedulerMigrationSource
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type auxSchedulerConfig struct {
	NewShape          bool
	Name              string
	Enabled           bool
	ModelNames        []string
	Lanes             [][]int64
	MaximumAutoLane   int
	PrimaryAccountIDs []int64
	BackupAccountIDs  []int64
}

func normalizeAuxSchedulerConfig(input AuxSchedulerRuleInput) auxSchedulerConfig {
	config := auxSchedulerConfig{
		Name:              strings.TrimSpace(input.Name),
		Enabled:           input.Enabled,
		ModelNames:        normalizeAuxModelNames(input.ModelNames),
		Lanes:             input.Lanes,
		MaximumAutoLane:   input.MaximumAutoLane,
		PrimaryAccountIDs: input.PrimaryAccountIDs,
		BackupAccountIDs:  input.BackupAccountIDs,
	}
	if len(input.Lanes) > 0 {
		config.NewShape = true
		if len(input.Lanes) >= 2 {
			config.PrimaryAccountIDs = input.Lanes[0]
			config.BackupAccountIDs = input.Lanes[1]
		}
		if config.MaximumAutoLane <= 0 {
			config.MaximumAutoLane = len(input.Lanes)
		}
	}
	return config
}

func normalizeAuxModelNames(models []string) []string {
	seen := make(map[string]struct{}, len(models))
	out := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	return out
}

func (s *Service) validateAuxSchedulerInput(ctx context.Context, input AuxSchedulerRuleInput, config auxSchedulerConfig, excludeRuleID int64) ([]string, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, fmt.Errorf("%w: 规则名称不能为空", ErrBadRequest)
	}
	if err := s.requireUpstreamClient(); err != nil {
		return nil, err
	}
	accounts, err := s.ListOpenAIAccounts(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]sub2api.Account, len(accounts))
	for i := range accounts {
		byID[accounts[i].ID] = accounts[i]
	}
	if !config.NewShape {
		return s.validateLegacyAuxSchedulerInput(ctx, input, config, byID, excludeRuleID)
	}
	return s.validateLaneAuxSchedulerInput(ctx, config, byID, excludeRuleID)
}

func (s *Service) validateLegacyAuxSchedulerInput(ctx context.Context, input AuxSchedulerRuleInput, config auxSchedulerConfig, byID map[int64]sub2api.Account, excludeRuleID int64) ([]string, error) {
	primary := deduplicateInt64s(config.PrimaryAccountIDs)
	backup := deduplicateInt64s(config.BackupAccountIDs)
	if len(primary) == 0 || len(backup) == 0 {
		return nil, fmt.Errorf("%w: 必须同时配置主力账号和备用账号", ErrBadRequest)
	}
	owners, err := s.auxSchedulerAccountOwners(ctx, excludeRuleID)
	if err != nil {
		return nil, err
	}
	primarySet := make(map[int64]struct{}, len(primary))
	warnings := make([]string, 0, len(backup))
	for _, id := range primary {
		primarySet[id] = struct{}{}
		if err := validateAuxSchedulerAccount(id, byID); err != nil {
			return nil, err
		}
		if owner := owners.backup[id]; owner != "" {
			return nil, fmt.Errorf("%w: 账号 %s 已在规则 %s 中作为备用账号使用", ErrBadRequest, auxAccountLabel(byID[id].Name, id), owner)
		}
	}
	for _, id := range backup {
		if _, ok := primarySet[id]; ok {
			return nil, fmt.Errorf("%w: 账号 %s 不能同时作为主力账号和备用账号", ErrBadRequest, auxAccountLabel(byID[id].Name, id))
		}
		if owner := owners.backup[id]; owner != "" {
			return nil, fmt.Errorf("%w: 备用账号 %s 已被规则 %s 使用", ErrBadRequest, auxAccountLabel(byID[id].Name, id), owner)
		}
		if owner := owners.primary[id]; owner != "" {
			return nil, fmt.Errorf("%w: 账号 %s 已在规则 %s 中作为主力账号使用", ErrBadRequest, auxAccountLabel(byID[id].Name, id), owner)
		}
		warning, err := validateAuxSchedulerBackupAccount(id, byID)
		if err != nil {
			return nil, err
		}
		if warning != "" {
			warnings = append(warnings, warning)
		}
	}
	return warnings, nil
}

func (s *Service) validateLaneAuxSchedulerInput(ctx context.Context, config auxSchedulerConfig, byID map[int64]sub2api.Account, excludeRuleID int64) ([]string, error) {
	if len(config.ModelNames) == 0 {
		return nil, fmt.Errorf("%w: 必须配置非空模型集合", ErrBadRequest)
	}
	lanes, err := validateAuxSchedulerLanes(config.Lanes)
	if err != nil {
		return nil, err
	}
	if config.MaximumAutoLane < 1 || config.MaximumAutoLane > len(lanes) {
		return nil, fmt.Errorf("%w: maximum_auto_lane 必须在 1 到 %d 之间", ErrBadRequest, len(lanes))
	}
	supported := make(map[string]struct{})
	for _, lane := range lanes {
		for _, id := range lane {
			if err := validateAuxSchedulerAccount(id, byID); err != nil {
				return nil, err
			}
			for _, model := range auxAccountSupportedModels(byID[id]) {
				supported[model] = struct{}{}
			}
		}
	}
	for _, model := range config.ModelNames {
		if _, ok := supported[model]; !ok {
			return nil, fmt.Errorf("%w: 模型 %q 在规则账号中没有被支持", ErrBadRequest, model)
		}
	}
	if config.Enabled {
		if owner, err := s.auxSchedulerLaneOwner(ctx, lanes, excludeRuleID); err != nil {
			return nil, err
		} else if owner != "" {
			return nil, fmt.Errorf("%w: 账号已在规则 %s 中使用", ErrBadRequest, owner)
		}
	}
	return nil, nil
}

func validateAuxSchedulerLanes(lanes [][]int64) ([][]int64, error) {
	if len(lanes) < 2 {
		return nil, fmt.Errorf("%w: 至少需要两个泳道", ErrBadRequest)
	}
	seen := make(map[int64]struct{})
	out := make([][]int64, 0, len(lanes))
	for _, lane := range lanes {
		if len(lane) == 0 {
			return nil, fmt.Errorf("%w: 泳道不能为空", ErrBadRequest)
		}
		cleaned := make([]int64, 0, len(lane))
		for _, id := range lane {
			if id <= 0 {
				return nil, fmt.Errorf("%w: 泳道账号 ID 必须为正数", ErrBadRequest)
			}
			if _, ok := seen[id]; ok {
				return nil, fmt.Errorf("%w: 同一账号不能在多个泳道中重复", ErrBadRequest)
			}
			seen[id] = struct{}{}
			cleaned = append(cleaned, id)
		}
		out = append(out, cleaned)
	}
	return out, nil
}

func auxAccountSupportedModels(account sub2api.Account) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)
	credentials := account.Credentials
	if raw, ok := credentials["upstream_supported_models"].([]any); ok {
		for _, item := range raw {
			model, ok := item.(string)
			model = strings.TrimSpace(model)
			if ok && model != "" {
				if _, exists := seen[model]; !exists {
					seen[model] = struct{}{}
					out = append(out, model)
				}
			}
		}
	}
	if mapping, ok := credentials["model_mapping"].(map[string]any); ok {
		for model := range mapping {
			model = strings.TrimSpace(model)
			if model != "" {
				if _, exists := seen[model]; !exists {
					seen[model] = struct{}{}
					out = append(out, model)
				}
			}
		}
	}
	return out
}

type auxSchedulerOwnerMaps struct {
	primary map[int64]string
	backup  map[int64]string
}

func (s *Service) auxSchedulerAccountOwners(ctx context.Context, excludeRuleID int64) (*auxSchedulerOwnerMaps, error) {
	rules, err := s.listAuxSchedulerRulesRaw(ctx)
	if err != nil {
		return nil, err
	}
	owners := &auxSchedulerOwnerMaps{
		primary: make(map[int64]string),
		backup:  make(map[int64]string),
	}
	for i := range rules {
		if rules[i].ID == excludeRuleID || !rules[i].Enabled || rules[i].MigrationStatus == AuxSchedulerMigrationStatusNeedsMigration {
			continue
		}
		lanes := rules[i].Lanes
		if len(lanes) == 0 {
			lanes = [][]int64{rules[i].PrimaryAccountIDs, rules[i].BackupAccountIDs}
		}
		for index, lane := range lanes {
			target := owners.primary
			if index > 0 {
				target = owners.backup
			}
			for _, id := range lane {
				target[id] = rules[i].Name
			}
		}
	}
	return owners, nil
}

func (s *Service) auxSchedulerLaneOwner(ctx context.Context, lanes [][]int64, excludeRuleID int64) (string, error) {
	rules, err := s.listAuxSchedulerRulesRaw(ctx)
	if err != nil {
		return "", err
	}
	for i := range rules {
		if rules[i].ID == excludeRuleID || !rules[i].Enabled || rules[i].MigrationStatus == AuxSchedulerMigrationStatusNeedsMigration {
			continue
		}
		existingLanes := rules[i].Lanes
		if len(existingLanes) == 0 {
			existingLanes = [][]int64{rules[i].PrimaryAccountIDs, rules[i].BackupAccountIDs}
		}
		if anyAuxLanesOverlap(lanes, existingLanes) {
			return rules[i].Name, nil
		}
	}
	return "", nil
}

func anyAuxLanesOverlap(a, b [][]int64) bool {
	seen := make(map[int64]struct{})
	for _, lane := range a {
		for _, id := range lane {
			seen[id] = struct{}{}
		}
	}
	for _, lane := range b {
		for _, id := range lane {
			if _, ok := seen[id]; ok {
				return true
			}
		}
	}
	return false
}

func (s *Service) assertAuxSchedulerOwnershipInTx(ctx context.Context, tx *sql.Tx, lanes [][]int64, excludeRuleID int64) error {
	rows, err := tx.QueryContext(ctx, `
SELECT id, name, enabled, migration_status, lanes_json, primary_account_ids_json, backup_account_ids_json
FROM aux_scheduler_rules
WHERE id <> ?
`, excludeRuleID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type ownedRule struct {
		name  string
		lanes [][]int64
	}
	rules := make([]ownedRule, 0)
	for rows.Next() {
		var id int64
		var name, migrationStatus, lanesJSON, primaryJSON, backupJSON string
		var enabled int
		if err := rows.Scan(&id, &name, &enabled, &migrationStatus, &lanesJSON, &primaryJSON, &backupJSON); err != nil {
			return err
		}
		if enabled == 0 || migrationStatus == AuxSchedulerMigrationStatusNeedsMigration {
			continue
		}
		parsedLanes, err := parseInt64SlicesJSON(lanesJSON)
		if err != nil {
			return err
		}
		if len(parsedLanes) == 0 {
			primary, err := parseInt64JSON(primaryJSON)
			if err != nil {
				return err
			}
			backup, err := parseInt64JSON(backupJSON)
			if err != nil {
				return err
			}
			parsedLanes = [][]int64{primary, backup}
		}
		rules = append(rules, ownedRule{name: name, lanes: parsedLanes})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, rule := range rules {
		if anyAuxLanesOverlap(lanes, rule.lanes) {
			return fmt.Errorf("%w: 账号已在规则 %s 中使用", ErrBadRequest, rule.name)
		}
	}
	return nil
}

func validateAuxSchedulerAccount(id int64, accounts map[int64]sub2api.Account) error {
	account, ok := accounts[id]
	if !ok {
		return fmt.Errorf("%w: OpenAI 账号 #%d 不存在", ErrBadRequest, id)
	}
	accountType := strings.ToLower(strings.TrimSpace(account.Type))
	if accountType != "oauth" && accountType != "apikey" {
		return fmt.Errorf("%w: 账号 %s 的类型 %q 不受支持，仅支持 oauth 和 apikey", ErrBadRequest, auxAccountLabel(account.Name, id), account.Type)
	}
	return nil
}

func validateAuxSchedulerBackupAccount(id int64, accounts map[int64]sub2api.Account) (string, error) {
	if err := validateAuxSchedulerAccount(id, accounts); err != nil {
		return "", err
	}
	account := accounts[id]
	status := strings.ToLower(strings.TrimSpace(account.Status))
	if status == "error" {
		return fmt.Sprintf("备用账号 %s 当前状态为 error，恢复为 active 前不会启用", auxAccountLabel(account.Name, id)), nil
	}
	if status != "active" {
		return "", fmt.Errorf("%w: 备用账号 %s 当前状态必须为 active 或 error，实际状态为 %q", ErrBadRequest, auxAccountLabel(account.Name, id), account.Status)
	}
	return "", nil
}

func auxAccountLabel(name string, id int64) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Sprintf("#%d", id)
	}
	return fmt.Sprintf("%s (#%d)", name, id)
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
			return s.recordAuxSchedulerError(ctx, rule.ID, errors.New("备用启用时间缺失"))
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
		return s.setAuxSchedulerState(ctx, rule.ID, AuxSchedulerStateIdle, nil, "")
	default:
		unavailable, err := s.anyAuxPrimaryUnavailable(ctx, rule.PrimaryAccountIDs)
		if err != nil {
			return s.recordAuxSchedulerError(ctx, rule.ID, err)
		}
		if !unavailable {
			return s.recordAuxSchedulerChecked(ctx, rule.ID)
		}
		warnings, err := s.activateAuxSchedulerBackups(ctx, *rule)
		if err != nil {
			return s.recordAuxSchedulerError(ctx, rule.ID, err)
		}
		now := s.now()
		return s.setAuxSchedulerState(ctx, rule.ID, AuxSchedulerStateBackupActive, &now, strings.Join(warnings, "; "))
	}
}

func (s *Service) anyAuxPrimaryUnavailable(ctx context.Context, accountIDs []int64) (bool, error) {
	now := s.now()
	for _, id := range accountIDs {
		account, err := s.upstream.GetAccount(ctx, id)
		if err != nil {
			return false, fmt.Errorf("加载主力账号 #%d 失败: %w", id, err)
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
			return false, fmt.Errorf("加载主力账号 #%d 失败: %w", id, err)
		}
		if account.LastUsedAt != nil && account.LastUsedAt.After(since) {
			return true, nil
		}
		hasLog, err := s.upstream.HasOpenAIUsageLogAfter(ctx, id, since)
		if err != nil {
			return false, fmt.Errorf("加载账号 #%d 的调用记录失败: %w", id, err)
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

func (s *Service) activateAuxSchedulerBackups(ctx context.Context, rule models.AuxSchedulerRule) ([]string, error) {
	return s.activateAuxSchedulerBackupIDs(ctx, rule.BackupAccountIDs)
}

func (s *Service) activateAuxSchedulerBackupIDs(ctx context.Context, accountIDs []int64) ([]string, error) {
	var warnings []string
	var failures []string
	activated := 0
	for _, id := range accountIDs {
		account, err := s.upstream.GetAccount(ctx, id)
		if err != nil {
			failures = append(failures, fmt.Sprintf("账号 #%d 加载失败: %v", id, err))
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(account.Status), "active") {
			warnings = append(warnings, fmt.Sprintf("备用账号 %s 当前状态为 %s，跳过启用", auxAccountLabel(account.Name, id), account.Status))
			continue
		}
		if _, err := s.upstream.SetOpenAIAccountSchedulable(ctx, id, true); err != nil {
			failures = append(failures, fmt.Sprintf("账号 %s 切换调度失败: %v", auxAccountLabel(account.Name, id), err))
			continue
		}
		activated++
	}
	if activated == 0 {
		messages := append([]string{}, warnings...)
		messages = append(messages, failures...)
		if len(messages) == 0 {
			messages = append(messages, "没有可用的备用账号")
		}
		return warnings, fmt.Errorf("启用备用账号失败: %s", strings.Join(messages, "; "))
	}
	warnings = append(warnings, failures...)
	return warnings, nil
}

func (s *Service) deactivateAuxSchedulerBackups(ctx context.Context, rule models.AuxSchedulerRule) error {
	return s.deactivateAuxSchedulerBackupIDs(ctx, rule.BackupAccountIDs)
}

func (s *Service) deactivateAuxSchedulerBackupIDs(ctx context.Context, accountIDs []int64) error {
	var failures []string
	for _, id := range accountIDs {
		account, accountErr := s.upstream.GetAccount(ctx, id)
		if _, err := s.upstream.SetOpenAIAccountSchedulable(ctx, id, false); err != nil {
			label := fmt.Sprintf("#%d", id)
			if accountErr == nil {
				label = auxAccountLabel(account.Name, id)
			}
			failures = append(failures, fmt.Sprintf("账号 %s 切换调度失败: %v", label, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("关闭备用账号失败: %s", strings.Join(failures, "; "))
	}
	return nil
}

func (s *Service) setAuxSchedulerState(ctx context.Context, id int64, state string, activatedAt *time.Time, lastError string) error {
	now := s.now()
	_, err := s.db().ExecContext(ctx, `
UPDATE aux_scheduler_rules
SET state = ?, activated_at = ?, last_checked_at = ?, last_error = ?, updated_at = ?
WHERE id = ?
`, state, formatNullableTime(activatedAt), formatTime(now), lastError, formatTime(now), id)
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
