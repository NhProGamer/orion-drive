import { defineStore } from 'pinia'

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
  }),
  actions: {
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
