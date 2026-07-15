<script setup lang="ts">
import { ref, onMounted } from 'vue'
import axios from 'axios'
import { Sun, Moon } from 'lucide-vue-next'
import { useUiStore } from '@/stores/ui'
import { bannerFor } from '@/lib/branding'

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
    <button class="icon-btn" style="position: fixed; top: 16px; right: 16px" @click="ui.toggleTheme">
      <component :is="ui.theme === 'dark' ? Sun : Moon" :size="16" />
    </button>
    <div class="login-card">
      <img class="brand-banner" :src="bannerFor(ui.theme)" alt="OrionDrive" />
      <h1>Bienvenue</h1>
      <p>Connecte-toi pour accéder à ton espace de stockage.</p>
      <button v-if="config.oidc" class="btn btn-primary" style="width: 100%; height: 42px" @click="loginOidc">
        Se connecter avec le SSO
      </button>
      <button v-if="config.dev" class="btn btn-secondary" style="width: 100%" @click="loginDev">
        Connexion développeur
      </button>
      <p v-if="!config.oidc && !config.dev" style="color: var(--danger)">
        Aucune méthode d'authentification n'est configurée.
      </p>
    </div>
  </div>
</template>
