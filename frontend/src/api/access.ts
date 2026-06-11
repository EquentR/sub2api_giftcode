import { request } from './http'
import { asArray } from './arrays'
import type { AccessRequest } from './types'

export function createAccessRequest(tierId: number, note: string) {
  return request<AccessRequest>({
    method: 'POST',
    url: '/redeem-access-requests',
    data: { tier_id: tierId, note },
  })
}

export function listAccessRequests() {
  return request<AccessRequest[] | null>({
    method: 'GET',
    url: '/redeem-access-requests',
  }).then(asArray)
}

export function previewAccessRequest(token: string) {
  return request<AccessRequest>({
    method: 'GET',
    url: '/redeem-access-requests/confirm/preview',
    params: { token },
  })
}

export function confirmAccessRequest(token: string) {
  return request<AccessRequest>({
    method: 'POST',
    url: '/redeem-access-requests/confirm',
    data: { token },
  })
}
