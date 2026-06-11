import axios, { type AxiosRequestConfig } from 'axios'
import { translateMessage } from '@/utils/messages'

type Envelope<T> = {
  code: number
  message: string
  reason?: string
  data?: T
}

export class ApiError extends Error {
  status: number
  reason?: string
  payload?: unknown

  constructor(message: string, status: number, reason?: string, payload?: unknown) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.reason = reason
    this.payload = payload
  }
}

const client = axios.create({
  baseURL: '/api',
  withCredentials: true,
  timeout: 30000,
  validateStatus: () => true,
})

const SESSION_TOKEN_KEY = 'giftcode_session_token'

function readSessionToken() {
  if (typeof window === 'undefined') {
    return ''
  }
  return window.localStorage.getItem(SESSION_TOKEN_KEY)?.trim() ?? ''
}

client.interceptors.request.use((config) => {
  const token = readSessionToken()
  if (!token) {
    return config
  }
  config.headers = config.headers ?? {}
  ;(config.headers as any).Authorization = `Bearer ${token}`
  return config
})

async function request<T>(config: AxiosRequestConfig): Promise<T> {
  try {
    const response = await client.request<Envelope<T>>(config)
    const payload = response.data
    if (!payload || response.status >= 400 || payload.code !== 0) {
      throw new ApiError(
        translateMessage(payload?.message),
        response.status,
        payload?.reason,
        payload,
      )
    }
    return payload.data as T
  } catch (error: any) {
    if (error instanceof ApiError) {
      throw error
    }
    const response = error?.response
    const payload = response?.data
    throw new ApiError(
      translateMessage(payload?.message ?? error?.message),
      response?.status ?? 0,
      payload?.reason,
      payload ?? error,
    )
  }
}

export { request }
