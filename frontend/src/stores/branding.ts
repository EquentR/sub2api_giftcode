import { defineStore } from 'pinia'
import { getSiteBranding } from '@/api/branding'
import type { SiteBranding } from '@/api/types'

const DEFAULT_TITLE = 'sub2api'
const DEFAULT_SUBTITLE = '兑换码系统'

function normalizeBranding(data?: Partial<SiteBranding> | null): SiteBranding {
  return {
    title: data?.title?.trim() || DEFAULT_TITLE,
    subtitle: data?.subtitle?.trim() || DEFAULT_SUBTITLE,
    mail_subject_prefix: data?.mail_subject_prefix?.trim() || '',
  }
}

export const useBrandingStore = defineStore('branding', {
  state: () => ({
    title: DEFAULT_TITLE,
    subtitle: DEFAULT_SUBTITLE,
    mailSubjectPrefix: '',
    bootstrapped: false,
    loading: false,
  }),
  actions: {
    applyBranding(data?: Partial<SiteBranding> | null) {
      const branding = normalizeBranding(data)
      this.title = branding.title
      this.subtitle = branding.subtitle
      this.mailSubjectPrefix = branding.mail_subject_prefix
    },
    async bootstrap() {
      if (this.bootstrapped) return
      this.loading = true
      try {
        const branding = await getSiteBranding()
        this.applyBranding(branding)
      } catch {
        this.applyBranding(null)
      } finally {
        this.loading = false
        this.bootstrapped = true
      }
    },
  },
})
