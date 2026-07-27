<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import axios from 'axios'
import { Sun, Moon } from 'lucide-vue-next'
import { useUiStore } from '@/stores/ui'
import { bannerFor } from '@/lib/branding'
import LanguageMenu from '@/components/LanguageMenu.vue'

const { t } = useI18n()
const ui = useUiStore()
const config = ref<{ oidc: boolean; dev: boolean }>({ oidc: true, dev: false })
// While true we are bouncing straight to the IdP, so the login card is skipped.
const redirecting = ref(false)

onMounted(async () => {
  try {
    const { data } = await axios.get('/api/v1/auth/config')
    config.value = data.data
  } catch {}
  // SSO is the only real login method: when it is the sole option, launch it
  // immediately instead of showing an interstitial. In debug mode the dev-login
  // shortcut coexists, so we keep the picker to let developers choose.
  if (config.value.oidc && !config.value.dev) {
    redirecting.value = true
    loginOidc()
  }
})

function loginOidc() {
  window.location.href = '/api/v1/auth/oidc/login'
}
function loginDev() {
  window.location.href = '/api/v1/auth/dev-login'
}
</script>

<template>
  <div class="login-screen">
    <div v-if="redirecting" class="login-card">
      <img class="brand-banner" :src="bannerFor(ui.theme)" alt="OrionDrive" />
      <p>{{ t('login.redirecting') }}</p>
    </div>
    <template v-else>
    <div class="login-tools">
      <LanguageMenu />
      <button class="icon-btn" @click="ui.toggleTheme">
        <component :is="ui.theme === 'dark' ? Sun : Moon" :size="16" />
      </button>
    </div>
    <div class="login-card">
      <img class="brand-banner" :src="bannerFor(ui.theme)" alt="OrionDrive" />
      <h1>{{ t('login.welcome') }}</h1>
      <p>{{ t('login.subtitle') }}</p>
      <button v-if="config.oidc" class="btn btn-primary login-btn" @click="loginOidc">
        {{ t('login.sso') }}
      </button>
      <button v-if="config.dev" class="btn btn-secondary login-btn" @click="loginDev">
        {{ t('login.dev') }}
      </button>
      <p v-if="!config.oidc && !config.dev" class="login-error">
        {{ t('login.noAuth') }}
      </p>
    </div>
    </template>
  </div>
</template>
