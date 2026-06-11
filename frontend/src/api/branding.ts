import { request } from './http'
import type { SiteBranding } from './types'

export function getSiteBranding() {
  return request<SiteBranding>({
    method: 'GET',
    url: '/site-branding',
  })
}

export function updateSiteBranding(payload: SiteBranding) {
  return request<SiteBranding>({
    method: 'PUT',
    url: '/admin/site-branding',
    data: payload,
  })
}
