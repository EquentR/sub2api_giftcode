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
  amount: number
  pay_amount_cny: number
  note: string
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
  last_synced_at?: string | null
  created_at: string
  updated_at: string
}

export interface BalanceTier {
  id: number
  amount: number
  pay_amount_cny: number
  label: string
  enabled: boolean
  sort_order: number
  created_at: string
  updated_at: string
}

export interface UserSummary extends UserProfile {
  profile_json: string
  last_seen_at: string
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
  redeem_requests: number
  redeem_codes_total: number
  redeem_codes_unused: number
  redeem_codes_used: number
  active_tiers: number
  last_sync_at?: string | null
}

export interface RedeemIssueResponse {
  request: RedeemRequest
  code?: RedeemCode | null
}

export interface AccessApprovalResponse {
  request: AccessRequest
  code?: RedeemCode | null
}
