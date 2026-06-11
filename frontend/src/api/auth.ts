import { request } from './http'
import type { AuthState } from './types'

export function embeddedLogin(token: string, userId?: number) {
  return request<AuthState>({
    method: 'POST',
    url: '/auth/embedded/login',
    data: {
      token,
      user_id: userId,
    },
  })
}

export function login(email: string, password: string) {
  return request<AuthState>({
    method: 'POST',
    url: '/auth/login',
    data: { email, password },
  })
}

export function me() {
  return request<AuthState>({
    method: 'GET',
    url: '/auth/me',
  })
}

export function logout() {
  return request<{ message: string }>({
    method: 'POST',
    url: '/auth/logout',
  })
}
