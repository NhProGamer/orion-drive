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

onMounted(async () => {
  try {
    const { data } = await axios.get('/api/v1/auth/config')
    config.value = data.data
  } catch {}
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
    <div style="position: fixed; top: 16px; right: 16px; display: flex; gap: 8px">
      <LanguageMenu />
      <button class="icon-btn" @click="ui.toggleTheme">
        <component :is="ui.theme === 'dark' ? Sun : Moon" :size="16" />
      </button>
    </div>
    <div class="login-card">
      <img class="brand-banner" :src="bannerFor(ui.theme)" alt="OrionDrive" />
      <h1>{{ t('login.welcome') }}</h1>
      <p>{{ t('login.subtitle') }}</p>
      <button v-if="config.oidc" class="btn btn-primary" style="width: 100%; height: 42px" @click="loginOidc">
        {{ t('login.sso') }}
      </button>
      <button v-if="config.dev" class="btn btn-secondary" style="width: 100%" @click="loginDev">
        {{ t('login.dev') }}
      </button>
      <p v-if="!config.oidc && !config.dev" style="color: var(--danger)">
        {{ t('login.noAuth') }}
      </p>
    </div>
  </div>
</template>
