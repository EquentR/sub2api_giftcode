import { request } from './http'
import { asArray } from './arrays'
import type { SubscriptionCard, SubscriptionResetResult } from './types'

export function listSubscriptions() {
  return request<SubscriptionCard[] | null>({
    method: 'GET',
    url: '/subscriptions',
  }).then(asArray)
}

export function resetSubscriptionQuota(subscriptionId: number, requestId: string) {
  return request<SubscriptionResetResult>({
    method: 'POST',
    url: `/subscriptions/${subscriptionId}/reset-quota`,
    data: { request_id: requestId },
  })
}
