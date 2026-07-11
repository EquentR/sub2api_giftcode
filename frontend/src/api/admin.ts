import { request } from './http'
import { asArray } from './arrays'
import type {
  AccessApprovalResponse,
  AccessRequest,
  BalanceTier,
  CompensationBatch,
  CompensationBatchDetail,
  DashboardStats,
  OpenAIAccount,
  RedeemCode,
  RedeemTier,
  SubscriptionGroup,
  SubscriptionConcurrencyMonitorDetail,
  SubscriptionConcurrencyMonitorStatus,
  UserSummary,
} from './types'

export function stats() {
  return request<DashboardStats>({
    method: 'GET',
    url: '/admin/stats',
  })
}

export function listSubscriptionConcurrencyStatus() {
  return request<SubscriptionConcurrencyMonitorStatus>({
    method: 'GET',
    url: '/admin/subscription-concurrency/status',
  })
}

export function listSubscriptionConcurrencyDetails() {
  return request<SubscriptionConcurrencyMonitorDetail[] | null>({
    method: 'GET',
    url: '/admin/subscription-concurrency/details',
  }).then(asArray)
}

export function listUsers() {
  return request<UserSummary[] | null>({
    method: 'GET',
    url: '/admin/users',
  }).then(asArray)
}

export function listUserRedeemCodes(userId: number) {
  return request<RedeemCode[] | null>({
    method: 'GET',
    url: `/admin/users/${userId}/redeem-codes`,
  }).then(asArray)
}

export function listAccessRequests() {
  return request<AccessRequest[] | null>({
    method: 'GET',
    url: '/admin/redeem-access-requests',
  }).then(asArray)
}

export function getAccessRequest(id: number) {
  return request<AccessRequest>({
    method: 'GET',
    url: `/admin/redeem-access-requests/${id}`,
  })
}

export function approveAccessRequest(id: number) {
  return request<AccessApprovalResponse>({
    method: 'POST',
    url: `/admin/redeem-access-requests/${id}/approve`,
  })
}

export function rejectAccessRequest(id: number) {
  return request<AccessRequest>({
    method: 'POST',
    url: `/admin/redeem-access-requests/${id}/reject`,
  })
}

export function listRedeemCodes() {
  return request<RedeemCode[] | null>({
    method: 'GET',
    url: '/admin/redeem-codes',
  }).then(asArray)
}

export function syncRedeemCodes() {
  return request<{ updated: number }>({
    method: 'POST',
    url: '/admin/sync/redeem-codes',
  })
}

export function listBalanceTiers() {
  return request<BalanceTier[] | null>({
    method: 'GET',
    url: '/admin/redeem-balance-tiers',
  }).then(asArray)
}

export function listRedeemTiers() {
  return request<RedeemTier[] | null>({
    method: 'GET',
    url: '/admin/redeem-tiers',
  }).then(asArray)
}

export function updateBalanceTiers(tiers: BalanceTier[]) {
  return request<BalanceTier[]>({
    method: 'PUT',
    url: '/admin/redeem-balance-tiers',
    data: tiers,
  })
}

export function updateRedeemTiers(tiers: RedeemTier[]) {
  return request<RedeemTier[]>({
    method: 'PUT',
    url: '/admin/redeem-tiers',
    data: tiers,
  })
}

export function listSubscriptionGroups() {
  return request<SubscriptionGroup[] | null>({
    method: 'GET',
    url: '/admin/sub2api-subscription-groups',
  }).then(asArray)
}

export function listOpenAIAccounts() {
  return request<OpenAIAccount[] | null>({
    method: 'GET',
    url: '/admin/openai-accounts',
  }).then(asArray)
}

export function updateOpenAIAccountUserAgent(id: number, userAgent: string) {
  return request<OpenAIAccount>({
    method: 'PUT',
    url: `/admin/openai-accounts/${id}/user-agent`,
    data: { user_agent: userAgent },
  })
}

export function createCompensationBatch(payload: {
  subscription_days: number
  balance_amount: number
  excluded_domains: string[]
  note: string
}) {
  return request<CompensationBatch>({
    method: 'POST',
    url: '/admin/compensation-batches',
    data: payload,
  })
}

export function listCompensationBatches() {
  return request<CompensationBatch[] | null>({
    method: 'GET',
    url: '/admin/compensation-batches',
  }).then(asArray)
}

export function listCompensationBatchDetails(id: number) {
  return request<CompensationBatchDetail[] | null>({
    method: 'GET',
    url: `/admin/compensation-batches/${id}/details`,
  }).then(asArray)
}
