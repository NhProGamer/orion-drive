import { defineStore } from 'pinia'
import { api } from '@/lib/api'
import { ext } from '@/lib/format'

export interface Toast {
  id: number
  msg: string
  icon: string
  action: { label: string; fn: () => void } | null
}

let toastSeq = 0

/** UI-only state: theme, view mode and transient toasts. */
export const useUiStore = defineStore('ui', {
  state: () => ({
    theme: (document.documentElement.dataset.theme as 'dark' | 'light') || 'dark',
    mode: (localStorage.getItem('od-mode') as 'grid' | 'list') || 'grid',
    toasts: [] as Toast[],
    // Touch device (coarse pointer): drives the mobile interaction model
    // (tap-to-open, long-press selection, bottom-sheet menus).
    coarse: window.matchMedia?.('(pointer: coarse)').matches ?? false,
    // Office formats the configured document server can edit/view (from WOPI
    // discovery). Empty until loaded, or when Office editing is not configured.
    officeEdit: [] as string[],
    officeView: [] as string[],
    officeLoaded: false,
  }),
  getters: {
    // Whether a file can be edited / opened in the online Office editor. The
    // discovery extension lists are lower-cased; ext() upper-cases, so normalise.
    canEditOffice:
      (s) =>
      (name: string): boolean =>
        s.officeEdit.includes(ext(name).toLowerCase()),
    canViewOffice:
      (s) =>
      (name: string): boolean => {
        const e = ext(name).toLowerCase()
        return s.officeView.includes(e) || s.officeEdit.includes(e)
      },
  },
  actions: {
    // Load the online Office format lists once (best-effort).
    async loadOffice() {
      if (this.officeLoaded) return
      this.officeLoaded = true
      try {
        const r = await api.officeFormats()
        this.officeEdit = r.edit || []
        this.officeView = r.view || []
      } catch {
        this.officeLoaded = false
      }
    },
    // Track pointer changes for hybrid devices (e.g. tablet + keyboard).
    watchPointer() {
      const mq = window.matchMedia?.('(pointer: coarse)')
      mq?.addEventListener('change', (e) => (this.coarse = e.matches))
    },
    toggleTheme() {
      this.theme = this.theme === 'dark' ? 'light' : 'dark'
      document.documentElement.dataset.theme = this.theme
      try {
        localStorage.setItem('od-theme', this.theme)
      } catch {}
    },
    setMode(m: 'grid' | 'list') {
      this.mode = m
      localStorage.setItem('od-mode', m)
    },
    toast(msg: string, icon = 'info', action: Toast['action'] = null) {
      const t: Toast = { id: ++toastSeq, msg, icon, action }
      this.toasts.push(t)
      setTimeout(() => this.dismiss(t.id), 4000)
    },
    dismiss(id: number) {
      this.toasts = this.toasts.filter((t) => t.id !== id)
    },
    runAction(t: Toast) {
      t.action?.fn()
      this.dismiss(t.id)
    },
  },
})
