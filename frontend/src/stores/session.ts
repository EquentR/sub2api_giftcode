import { defineStore } from 'pinia'
import { embeddedLogin, login, logout, me } from '@/api/auth'
import type { AuthState, UserProfile } from '@/api/types'
import type { EmbeddedLaunchContext } from '@/utils/embedded'

const SESSION_TOKEN_KEY = 'giftcode_session_token'

function readStoredSessionToken() {
  if (typeof window === 'undefined') {
    return ''
  }
  return window.localStorage.getItem(SESSION_TOKEN_KEY)?.trim() ?? ''
}

function writeStoredSessionToken(token: string) {
  if (typeof window === 'undefined') {
    return
  }
  if (token) {
    window.localStorage.setItem(SESSION_TOKEN_KEY, token)
    return
  }
  window.localStorage.removeItem(SESSION_TOKEN_KEY)
}

type SessionState = {
  user: UserProfile | null
  isAdmin: boolean
  sessionExpiresAt: string
  sessionToken: string
  embeddedMode: boolean
  embeddedLaunchError: string
  loading: boolean
  bootstrapped: boolean
}

export const useSessionStore = defineStore('session', {
  state: (): SessionState => ({
    user: null,
    isAdmin: false,
    sessionExpiresAt: '',
    sessionToken: readStoredSessionToken(),
    embeddedMode: false,
    embeddedLaunchError: '',
    loading: false,
    bootstrapped: false,
  }),
  getters: {
    isLoggedIn: (state) => state.user !== null,
  },
  actions: {
    clearAuth() {
      this.user = null
      this.isAdmin = false
      this.sessionExpiresAt = ''
      this.sessionToken = ''
      writeStoredSessionToken('')
    },
    applyAuth(auth: AuthState) {
      this.user = auth.user
      this.isAdmin = auth.is_admin
      this.sessionExpiresAt = auth.session_expires_at
      this.sessionToken = auth.session_token ?? ''
      this.embeddedLaunchError = ''
      writeStoredSessionToken(this.sessionToken)
    },
    async bootstrap(embeddedContext?: EmbeddedLaunchContext | null) {
      if (this.bootstrapped) return
      this.embeddedMode = Boolean(embeddedContext?.token)
      try {
        const auth = embeddedContext?.token
          ? await embeddedLogin(embeddedContext.token, embeddedContext.userId)
          : await me()
        this.applyAuth(auth)
      } catch (error: any) {
        this.clearAuth()
        if (embeddedContext?.token) {
          this.embeddedMode = true
          this.embeddedLaunchError = error?.message ?? '嵌入式登录失败'
        } else {
          this.embeddedMode = false
          this.embeddedLaunchError = ''
        }
      } finally {
        this.bootstrapped = true
      }
    },
    async signIn(email: string, password: string) {
      this.loading = true
      try {
        const auth = await login(email, password)
        this.applyAuth(auth)
        return auth
      } finally {
        this.loading = false
      }
    },
    async signOut() {
      try {
        await logout()
      } finally {
        this.clear()
      }
    },
    clear() {
      this.clearAuth()
      this.embeddedMode = false
      this.embeddedLaunchError = ''
    },
  },
})
