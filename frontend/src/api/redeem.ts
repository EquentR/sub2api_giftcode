import { request } from './http'
import { asArray } from './arrays'
import type { BalanceTier, RedeemCode, RedeemIssueResponse, RedeemRequest, RedeemTier } from './types'

export function listRedeemTiers() {
  return request<RedeemTier[] | null>({
    method: 'GET',
    url: '/redeem-tiers',
  }).then(asArray)
}

export function listBalanceTiers() {
  return request<BalanceTier[] | null>({
    method: 'GET',
    url: '/redeem-balance-tiers',
  }).then(asArray)
}

export function createRedeemRequest(tierId: number, note: string) {
  return request<RedeemIssueResponse>({
    method: 'POST',
    url: '/redeem-requests',
    data: { tier_id: tierId, note },
  })
}

export function listRedeemRequests() {
  return request<RedeemRequest[] | null>({
    method: 'GET',
    url: '/redeem-requests',
  }).then(asArray)
}

export function listRedeemCodes() {
  return request<RedeemCode[] | null>({
    method: 'GET',
    url: '/redeem-codes',
  }).then(asArray)
}
