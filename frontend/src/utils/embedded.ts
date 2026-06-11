export interface EmbeddedLaunchContext {
  token: string
  userId?: number
  theme?: string
  lang?: string
  srcHost?: string
  srcUrl?: string
}

const EMBEDDED_QUERY_KEYS = ['ui_mode', 'token', 'user_id', 'theme', 'lang', 'src_host', 'src_url']

function parseUserId(raw: string | null) {
  if (!raw) {
    return undefined
  }
  const value = Number.parseInt(raw, 10)
  return Number.isFinite(value) && value > 0 ? value : undefined
}

export function parseEmbeddedLaunchContext(url = window.location.href): EmbeddedLaunchContext | null {
  const parsed = new URL(url)
  if (parsed.searchParams.get('ui_mode') !== 'embedded') {
    return null
  }
  const token = parsed.searchParams.get('token')?.trim() ?? ''
  if (!token) {
    return null
  }
  return {
    token,
    userId: parseUserId(parsed.searchParams.get('user_id')),
    theme: parsed.searchParams.get('theme')?.trim() || undefined,
    lang: parsed.searchParams.get('lang')?.trim() || undefined,
    srcHost: parsed.searchParams.get('src_host')?.trim() || undefined,
    srcUrl: parsed.searchParams.get('src_url')?.trim() || undefined,
  }
}

export function stripEmbeddedLaunchParams(url = window.location.href) {
  const parsed = new URL(url)
  let changed = false
  for (const key of EMBEDDED_QUERY_KEYS) {
    if (parsed.searchParams.has(key)) {
      parsed.searchParams.delete(key)
      changed = true
    }
  }
  if (!changed) {
    return
  }
  const next = `${parsed.pathname}${parsed.search ? `?${parsed.searchParams.toString()}` : ''}${parsed.hash}`
  window.history.replaceState({}, '', next)
}

export function applyEmbeddedContext(context: EmbeddedLaunchContext | null) {
  if (!context) {
    return
  }
  document.documentElement.dataset.embedded = 'true'
  if (context.lang) {
    document.documentElement.lang = context.lang
  }
  if (context.theme) {
    document.documentElement.dataset.theme = context.theme
    document.documentElement.style.colorScheme = context.theme.toLowerCase() === 'dark' ? 'dark' : 'light'
  }
}

export function isEmbeddedDocument() {
  return typeof document !== 'undefined' && document.documentElement.dataset.embedded === 'true'
}
