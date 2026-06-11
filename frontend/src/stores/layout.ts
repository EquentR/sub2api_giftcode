import { defineStore } from 'pinia'
import { isEmbeddedDocument } from '@/utils/embedded'

const SIDEBAR_VISIBLE_KEY = 'giftcode_sidebar_visible'
const EMBEDDED_SIDEBAR_VISIBLE_KEY = 'giftcode_sidebar_visible_embedded'

function getSidebarStorageKey() {
  return isEmbeddedDocument() ? EMBEDDED_SIDEBAR_VISIBLE_KEY : SIDEBAR_VISIBLE_KEY
}

function readStoredSidebarVisible() {
  if (typeof window === 'undefined') {
    return null
  }
  try {
    const raw = window.localStorage.getItem(getSidebarStorageKey())
    if (raw === null) {
      return null
    }
    return raw === '1'
  } catch {
    return null
  }
}

function writeStoredSidebarVisible(visible: boolean) {
  if (typeof window === 'undefined') {
    return
  }
  try {
    window.localStorage.setItem(getSidebarStorageKey(), visible ? '1' : '0')
  } catch {
    // Ignore storage failures in restricted contexts.
  }
}

export function resolveInitialSidebarVisible() {
  const stored = readStoredSidebarVisible()
  if (stored !== null) {
    return stored
  }
  return !isEmbeddedDocument()
}

export const useLayoutStore = defineStore('layout', {
  state: () => ({
    sidebarVisible: resolveInitialSidebarVisible(),
  }),
  actions: {
    setSidebarVisible(visible: boolean) {
      this.sidebarVisible = visible
      writeStoredSidebarVisible(visible)
    },
    toggleSidebar() {
      this.setSidebarVisible(!this.sidebarVisible)
    },
  },
})
