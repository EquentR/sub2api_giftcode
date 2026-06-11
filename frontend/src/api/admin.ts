import { request } from './http'
import { asArray } from './arrays'
import type { AccessApprovalResponse, AccessRequest, BalanceTier, DashboardStats, RedeemCode, UserSummary } from './types'

export function stats() {
  return request<DashboardStats>({
    method: 'GET',
    url: '/admin/stats',
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

export function updateBalanceTiers(tiers: BalanceTier[]) {
  return request<BalanceTier[]>({
    method: 'PUT',
    url: '/admin/redeem-balance-tiers',
    data: tiers,
  })
}
