export interface UserProfile {
  id: number
  email: string
  username: string
  role: string
  status: string
  balance: number
  created_at: string
  updated_at: string
  deleted_at?: string | null
}

export interface AuthState {
  user: UserProfile
  is_admin: boolean
  session_expires_at: string
  session_token?: string
}

export interface AccessRequest {
  id: number
  requestor_upstream_user_id: number
  requestor_email: string
  requestor_username: string
  tier_id: number
  code_type: string
  tier_label: string
  amount: number
  pay_amount_cny: number
  sub2api_group_id?: number | null
  sub2api_group_name: string
  sub2api_group_platform: string
  sub2api_daily_limit_usd?: number | null
  sub2api_weekly_limit_usd?: number | null
  sub2api_monthly_limit_usd?: number | null
  validity_days: number
  concurrency: number
  reset_count: number
  note: string
  fulfillment_mode: string
  fulfillment_result: string
  fulfilled_via: string
  fulfillment_error: string
  status: string
  approval_token_hash: string
  approval_token_expires_at: string
  approved_at?: string | null
  rejected_at?: string | null
  consumed_at?: string | null
  notification_status: string
  notification_error: string
  notification_sent_at?: string | null
  created_at: string
  updated_at: string
}

export interface RedeemRequest {
  id: number
  access_request_id: number
  requestor_upstream_user_id: number
  requestor_email: string
  requestor_username: string
  code_type: string
  tier_id: number
  value: number
  sub2api_group_id?: number | null
  validity_days: number
  status: string
  note: string
  upstream_code: string
  upstream_code_id?: number | null
  error_message: string
  created_at: string
  updated_at: string
}

export interface RedeemCode {
  id: number
  request_id: number
  code: string
  code_type: string
  value: number
  status: string
  used_by_upstream_user_id?: number | null
  used_at?: string | null
  expires_at?: string | null
  sub2api_code_id?: number | null
  sub2api_group_id?: number | null
  validity_days: number
  last_synced_at?: string | null
  created_at: string
  updated_at: string
}

export interface RedeemTier {
  id: number
  code_type?: string
  amount: number
  pay_amount_cny: number
  original_pay_amount_cny?: number | null
  label: string
  enabled: boolean
  sort_order: number
  sub2api_group_id?: number | null
  sub2api_group_name?: string
  sub2api_group_platform?: string
  sub2api_daily_limit_usd?: number | null
  sub2api_weekly_limit_usd?: number | null
  sub2api_monthly_limit_usd?: number | null
  validity_days?: number
  concurrency: number
  reset_count: number
  upstream_available?: boolean
  upstream_error?: string
  created_at: string
  updated_at: string
}

export type BalanceTier = RedeemTier

export interface SubscriptionGroup {
  id: number
  name: string
  description: string
  platform: string
  rate_multiplier: number
  status: string
  subscription_type: string
  daily_limit_usd?: number | null
  weekly_limit_usd?: number | null
  monthly_limit_usd?: number | null
  created_at: string
  updated_at: string
}

export interface OpenAIAccount {
  id: number
  name: string
  platform: string
  type: string
  status: string
  credentials?: Record<string, unknown> | null
  created_at?: string
  updated_at?: string
}

export interface AuxSchedulerAccountInfo {
  id: number
  name?: string
  type?: string
  status?: string
  schedulable?: boolean
}

export interface AuxSchedulerLaneView {
  number: number
  accounts: AuxSchedulerAccountInfo[]
}

export interface AuxSchedulerMigrationSource {
  legacy_state: string
  legacy_primary_account_ids: number[]
  legacy_backup_account_ids: number[]
  legacy_activated_at?: string | null
}

export interface AuxSchedulerRule {
  id: number
  name: string
  enabled: boolean
  model_names: string[]
  lanes: number[][]
  maximum_auto_lane: number
  migration_status: '' | 'needs_migration'
  migration_source?: AuxSchedulerMigrationSource | null
  state: 'idle' | 'backup_active'
  expected_open_through_lane: number
  observed_open_through_lane: number
  verified_open_through_lane: number
  target_open_through_lane: number
  transition_status: string
  transition_generation: number
  upgrade_evidence?: Record<string, unknown> | null
  missing_models?: string[]
  recovery_candidate_lane?: number | null
  recovery_candidate_since?: string | null
  last_observed_at?: string | null
  last_verified_at?: string | null
  blocked_reason?: string
  warnings?: string
  activated_at?: string | null
  last_checked_at?: string | null
  last_error: string
  created_at: string
  updated_at: string
  lane_accounts: AuxSchedulerLaneView[]
  upstream_error?: string
}

export interface UserSummary {
  upstream_user_id: number
  email: string
  username: string
  role: string
  status: string
  profile_json: string
  last_seen_at: string
  created_at: string
  updated_at: string
  access_request_count: number
  redeem_request_count: number
  redeem_code_count: number
  used_code_count: number
  unused_code_count: number
  latest_request_at?: string | null
  latest_code_at?: string | null
}

export interface DashboardStats {
  total_users: number
  pending_access_requests: number
  approved_access_requests: number
  rejected_access_requests: number
  consumed_access_requests: number
  direct_charge_access_requests: number
  redeem_requests: number
  redeem_codes_total: number
  redeem_codes_unused: number
  redeem_codes_used: number
  active_tiers: number
  last_sync_at?: string | null
}

export interface SubscriptionConcurrencyMonitorStatus {
  default_concurrency: number
  default_concurrency_error: string
  last_reconciliation_at?: string | null
  active_grants: number
  pending_grants: number
  inactive_grants: number
  error_grants: number
  manual_override_users: number
  latest_error: string
  latest_error_at?: string | null
}

export type SubscriptionQuotaKind = 'daily' | 'weekly' | 'monthly'

export interface SubscriptionQuotaWindow {
  kind: SubscriptionQuotaKind
  limit_usd: number
  used_usd: number
  remaining_usd: number
  window_start?: string | null
  resets_at?: string | null
}

export interface SubscriptionResetPeriodSummary {
  id: number
  period_start?: string | null
  period_end?: string | null
  status: 'pending_binding' | 'scheduled' | 'active' | 'expired' | 'inactive'
  reset_limit: number
  reset_used: number
  reset_remaining: number
}

export interface SubscriptionResetBonusSummary {
  id: number
  batch_id: number
  note: string
  reset_limit: number
  reset_used: number
  reset_remaining: number
  expires_at: string
  status: string
}

export interface SubscriptionResetEntitlementSummary {
  type: 'base_period' | 'bonus_grant'
  id: number
  expires_at: string
}

export interface SubscriptionResetEntitlementAdminView {
  upstream_user_id: number
  username: string
  email: string
  upstream_subscription_id: number
  sub2api_group_id: number
  group_name: string
  starts_at: string
  expires_at: string
  remaining_days: number
  base_reset_limit: number
  base_reset_used: number
  base_reset_remaining: number
  bonus_reset_limit: number
  bonus_reset_used: number
  bonus_reset_remaining: number
  total_reset_remaining: number
}

export interface SubscriptionCard {
  id: number
  group_id: number
  group_name: string
  group_platform: string
  starts_at: string
  expires_at: string
  remaining_days: number
  quota_windows: SubscriptionQuotaWindow[]
  current_period?: SubscriptionResetPeriodSummary | null
  next_period?: SubscriptionResetPeriodSummary | null
  base_reset_limit: number
  base_reset_used: number
  base_reset_remaining: number
  bonus_reset_remaining: number
  total_reset_remaining: number
  bonus_grants: SubscriptionResetBonusSummary[]
  next_entitlement?: SubscriptionResetEntitlementSummary | null
  unlimited: boolean
  external_period: boolean
  zero_reset_limit: boolean
  operation_pending: boolean
  can_reset: boolean
  disabled_reason?: string
}

export interface SubscriptionResetAttempt {
  id: number
  request_id: string
  period_id?: number | null
  entitlement_type: 'base_period' | 'bonus_grant'
  entitlement_id: number
  upstream_user_id: number
  upstream_subscription_id: number
  reset_daily: boolean
  reset_weekly: boolean
  reset_monthly: boolean
  status: 'reserved' | 'succeeded' | 'failed' | 'uncertain'
  before_snapshot_json: string
  after_snapshot_json: string
  upstream_status?: number | null
  response_status: number
  response_reason: string
  error_message: string
  resolution: string
  reserved_at: string
  completed_at?: string | null
  confirmed_at?: string | null
  confirmed_by_user_id?: number | null
  created_at: string
  updated_at: string
  username?: string
  email?: string
  period?: SubscriptionResetPeriodDetail
  bonus_grant?: SubscriptionResetBonusGrant
  before_snapshot?: SubscriptionQuotaWindow[]
  after_snapshot?: SubscriptionQuotaWindow[]
  current_snapshot?: SubscriptionQuotaWindow[]
  snapshot_error?: string
  current_snapshot_error?: string
}

export interface SubscriptionResetBonusGrant {
  id: number
  batch_id: number
  batch_detail_id: number
  upstream_user_id: number
  sub2api_group_id: number
  upstream_subscription_id: number
  reset_limit: number
  reset_used: number
  starts_at: string
  expires_at: string
  status: string
  subscription_snapshot_json: string
  last_synced_at?: string | null
  last_error: string
  created_at: string
  updated_at: string
}

export interface SubscriptionResetBonusPreviewInput {
  target_scope: 'all' | 'selected'
  selected_user_ids: number[]
  group_ids: number[]
  reset_count: number
  note: string
}

export interface SubscriptionResetBonusPreview {
  user_count: number
  subscription_count: number
  group_counts: Record<string, number>
  missing_user_ids: number[]
  skipped_counts: Record<string, number>
  preview_digest: string
  preview_token: string
  expires_at: string
}

export interface SubscriptionResetBonusBatch {
  id: number
  batch_key: string
  target_scope: 'all' | 'selected'
  selected_user_ids: number[]
  group_ids: number[]
  reset_count: number
  note: string
  preview_digest: string
  status: string
  total_candidates: number
  processed_candidates: number
  granted_subscriptions: number
  skipped_subscriptions: number
  failed_subscriptions: number
  operator_upstream_user_id: number
  operator_email: string
  operator_username: string
  error_message: string
  created_at: string
  started_at?: string | null
  completed_at?: string | null
  updated_at: string
}

export interface SubscriptionResetBonusBatchDetail {
  id: number
  batch_id: number
  upstream_user_id: number
  sub2api_group_id: number
  upstream_subscription_id: number
  subscription_starts_at: string
  subscription_expires_at: string
  subscription_status: string
  subscription_snapshot_json: string
  status: string
  reason: string
  error_message: string
  bonus_grant_id?: number | null
  created_at: string
  updated_at: string
}

export interface SubscriptionExtensionEvent {
  id: number
  event_key: string
  source_type: string
  compensation_batch_id?: number | null
  compensation_detail_id?: number | null
  upstream_user_id: number
  sub2api_group_id: number
  upstream_subscription_id: number
  extension_days: number
  before_expires_at?: string | null
  after_expires_at?: string | null
  status: 'reserved' | 'succeeded' | 'failed' | 'uncertain'
  resolution: '' | 'applied' | 'released'
  applied_base_periods: number
  applied_bonus_grants: number
  inferred_from_legacy: boolean
  migration_version: number
  error_message: string
  reserved_at: string
  completed_at?: string | null
  confirmed_at?: string | null
  confirmed_by_user_id?: number | null
  created_at: string
  updated_at: string
}

export interface SubscriptionResetPeriodDetail extends SubscriptionResetPeriodSummary {
  access_request_id: number
  upstream_user_id: number
  tier_id: number
  sub2api_group_id: number
  upstream_subscription_id?: number | null
  validity_days: number
  fulfilled_at: string
  fulfillment_order: number
  inferred_from_legacy: boolean
  migration_version: number
  legacy_reset_backfilled: boolean
  last_synced_at?: string | null
  last_error: string
  created_at: string
  updated_at: string
}

export interface SubscriptionResetResult {
  operation: SubscriptionResetAttempt
  subscription?: SubscriptionCard | null
}

export interface SubscriptionResetBackfillRun {
  id: number
  tier_id: number
  reset_limit: number
  status: 'pending' | 'running' | 'succeeded' | 'failed'
  total_records: number
  processed_records: number
  granted_records: number
  error_message: string
  retry_count: number
  last_error_at?: string | null
  triggered_at: string
  started_at?: string | null
  completed_at?: string | null
  updated_at: string
}

export interface SubscriptionConcurrencyMonitorDetail {
  upstream_user_id: number
  username: string
  email: string
  current_concurrency?: number | null
  target_concurrency: number
  manual_override: boolean
  active_grants: number
  pending_grants: number
  inactive_grants: number
  last_synced_at?: string | null
  last_error: string
}

export interface CompensationBatch {
  id: number
  batch_key: string
  compensate_subscriptions: boolean
  compensate_balance: boolean
  subscription_days: number
  balance_amount: number
  excluded_domains: string[]
  note: string
  operator_upstream_user_id: number
  operator_email: string
  operator_username: string
  status: string
  total_users: number
  excluded_users: number
  subscription_compensated_users: number
  balance_compensated_users: number
  skipped_zero_balance_users: number
  failed_users: number
  detail_count: number
  upstream_error: string
  created_at: string
  updated_at: string
  completed_at?: string | null
}

export interface CompensationBatchDetail {
  id: number
  batch_id: number
  detail_key: string
  upstream_user_id: number
  user_email: string
  user_username: string
  user_balance: number
  excluded: boolean
  excluded_domain: string
  has_active_subscriptions: boolean
  active_subscription_count: number
  active_subscription_ids: number[]
  decision_type: string
  action_type: string
  subscription_days: number
  balance_amount: number
  status: string
  result_reason: string
  upstream_reference_json: string
  remark_requested: boolean
  remark_applied: boolean
  remark_error: string
  created_at: string
  updated_at: string
}

export interface SiteBranding {
  title: string
  subtitle: string
  mail_subject_prefix: string
}

export interface RedeemIssueResponse {
  request: RedeemRequest
  code?: RedeemCode | null
}

export interface AccessApprovalResponse {
  request: AccessRequest
  code?: RedeemCode | null
}
