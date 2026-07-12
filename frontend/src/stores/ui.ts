import { defineStore } from 'pinia'

export interface Toast {
  id: number
  msg: string
  icon: string
  action: { label: string; fn: () => void } | null
}

export type Density = 'confortable' | 'compact'
export type ThumbSize = 's' | 'm' | 'l'

let toastSeq = 0

/** UI-only state: theme, layout tweaks, view mode and transient toasts. */
export const useUiStore = defineStore('ui', {
  state: () => ({
    theme: (document.documentElement.dataset.theme as 'dark' | 'light') || 'dark',
    mode: (localStorage.getItem('od-mode') as 'grid' | 'list') || 'grid',
    density: (localStorage.getItem('od-density') as Density) || 'confortable',
    thumb: (localStorage.getItem('od-thumb') as ThumbSize) || 'm',
    halo: localStorage.getItem('od-halo') !== 'off',
    tweaksOpen: false,
    toasts: [] as Toast[],
  }),
  actions: {
    applyTweaks() {
      const h = document.documentElement
      h.dataset.density = this.density
      h.dataset.thumb = this.thumb
      h.dataset.halo = this.halo ? 'on' : 'off'
    },
    init() {
      this.applyTweaks()
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
    setDensity(d: Density) {
      this.density = d
      localStorage.setItem('od-density', d)
      this.applyTweaks()
    },
    setThumb(t: ThumbSize) {
      this.thumb = t
      localStorage.setItem('od-thumb', t)
      this.applyTweaks()
    },
    setHalo(on: boolean) {
      this.halo = on
      localStorage.setItem('od-halo', on ? 'on' : 'off')
      this.applyTweaks()
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
