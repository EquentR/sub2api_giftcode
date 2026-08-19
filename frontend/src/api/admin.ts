import { request } from './http'
import { asArray } from './arrays'
import type {
  AccessApprovalResponse,
  AccessRequest,
  AuxSchedulerRule,
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
  SubscriptionExtensionEvent,
  SubscriptionResetAttempt,
  SubscriptionResetEntitlementAdminView,
  SubscriptionResetBonusBatch,
  SubscriptionResetBonusBatchDetail,
  SubscriptionResetBonusPreview,
  SubscriptionResetBonusPreviewInput,
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

export function listSubscriptionResetAttempts() {
  return request<SubscriptionResetAttempt[] | null>({
    method: 'GET',
    url: '/admin/subscription-reset-attempts',
  }).then(asArray)
}

export function listSubscriptionResetEntitlements() {
  return request<SubscriptionResetEntitlementAdminView[] | null>({
    method: 'GET',
    url: '/admin/subscription-reset-entitlements',
  }).then(asArray)
}

export function resolveSubscriptionResetAttempt(id: number, resolution: 'consumed' | 'released') {
  return request<SubscriptionResetAttempt>({
    method: 'POST',
    url: `/admin/subscription-reset-attempts/${id}/resolve`,
    data: { resolution },
  })
}

export function previewSubscriptionResetBonus(payload: SubscriptionResetBonusPreviewInput) {
  return request<SubscriptionResetBonusPreview>({
    method: 'POST',
    url: '/admin/subscription-reset-bonus-batches/preview',
    data: payload,
  })
}

export function createSubscriptionResetBonusBatch(payload: { preview_token: string }) {
  return request<SubscriptionResetBonusBatch>({
    method: 'POST',
    url: '/admin/subscription-reset-bonus-batches',
    data: payload,
  })
}

export function listSubscriptionResetBonusBatches() {
  return request<SubscriptionResetBonusBatch[] | null>({
    method: 'GET',
    url: '/admin/subscription-reset-bonus-batches',
  }).then(asArray)
}

export function listSubscriptionResetBonusBatchDetails(id: number) {
  return request<SubscriptionResetBonusBatchDetail[] | null>({
    method: 'GET',
    url: `/admin/subscription-reset-bonus-batches/${id}/details`,
  }).then(asArray)
}

export function listSubscriptionExtensionEvents() {
  return request<SubscriptionExtensionEvent[] | null>({
    method: 'GET',
    url: '/admin/subscription-extension-events',
  }).then(asArray)
}

export function resolveSubscriptionExtensionEvent(id: number, resolution: 'applied' | 'released') {
  return request<SubscriptionExtensionEvent>({
    method: 'POST',
    url: `/admin/subscription-extension-events/${id}/resolve`,
    data: { resolution },
  })
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

export type AuxSchedulerRulePayload = {
  name: string
  enabled: boolean
  model_names: string[]
  lanes: number[][]
  maximum_auto_lane: number
}

export function listAuxSchedulerRules() {
  return request<AuxSchedulerRule[] | null>({
    method: 'GET',
    url: '/admin/aux-scheduler/rules',
  }).then(asArray)
}

export function createAuxSchedulerRule(payload: AuxSchedulerRulePayload) {
  return request<AuxSchedulerRule>({
    method: 'POST',
    url: '/admin/aux-scheduler/rules',
    data: payload,
  })
}

export function updateAuxSchedulerRule(id: number, payload: AuxSchedulerRulePayload) {
  return request<AuxSchedulerRule>({
    method: 'PUT',
    url: `/admin/aux-scheduler/rules/${id}`,
    data: payload,
  })
}

export function deleteAuxSchedulerRule(id: number) {
  return request<{ deleted: boolean }>({
    method: 'DELETE',
    url: `/admin/aux-scheduler/rules/${id}`,
  })
}

export function checkAuxSchedulerRule(id: number) {
  return request<AuxSchedulerRule>({
    method: 'POST',
    url: `/admin/aux-scheduler/rules/${id}/check`,
  })
}

export function listAuxSchedulerDispatchLogs(id: number) {
  return request<import('./types').AuxSchedulerDispatchLog[] | null>({
    method: 'GET',
    url: `/admin/aux-scheduler/rules/${id}/dispatch-logs`,
  }).then(asArray)
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
