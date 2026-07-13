import { defineStore } from 'pinia'
import { api, ApiError, type Me } from '@/lib/api'

/** Authentication state: the current user, loaded from /user/me. */
export const useAuthStore = defineStore('auth', {
  state: () => ({
    me: null as Me | null,
    ready: false,
    oidcEnabled: true,
  }),
  getters: {
    isAuthenticated: (s) => s.me !== null,
    canShare: (s) => s.me?.can_share !== false,
    wopiEnabled: (s) => s.me?.wopi === true,
    initials: (s) => {
      const name = s.me?.nick || s.me?.email || '?'
      return name
        .split(/[\s@.]+/)
        .filter(Boolean)
        .slice(0, 2)
        .map((p) => p[0]?.toUpperCase())
        .join('')
    },
  },
  actions: {
    async load() {
      try {
        this.me = await api.me()
        this.oidcEnabled = this.me!.oidc_enabled
      } catch (e) {
        this.me = null
        if (e instanceof ApiError && e.code === 40100) {
          // Not authenticated — the /user/me endpoint reports OIDC availability
          // only when authenticated, so probe conservatively.
        }
      } finally {
        this.ready = true
      }
    },
    async logout() {
      await api.logout()
      this.me = null
      window.location.href = '/'
    },
  },
})
