<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { Download, Lock, TriangleAlert, Sun, Moon } from 'lucide-vue-next'
import { api, type ShareView as ShareViewData } from '@/lib/api'
import { kindFromName, fmtSize } from '@/lib/format'
import { metaFor } from '@/lib/icons'
import { useUiStore } from '@/stores/ui'

const route = useRoute()
const ui = useUiStore()
const token = String(route.params.token)

const data = ref<ShareViewData | null>(null)
const notFound = ref(false)
const password = ref('')
const error = ref('')

const meta = computed(() => (data.value ? metaFor(kindFromName(data.value.name)) : metaFor('file')))
const unavailable = computed(() => data.value && (data.value.expired || data.value.exhausted))

onMounted(async () => {
  ui.init()
  try {
    data.value = await api.shareView(token)
  } catch {
    notFound.value = true
  }
})

function download() {
  error.value = ''
  // Probe first so we can surface password errors inline; then navigate.
  const url = api.shareContentUrl(token, password.value || undefined)
  fetch(url, { redirect: 'manual' })
    .then((r) => {
      if (r.status === 401) {
        error.value = 'Mot de passe incorrect ou requis.'
        return
      }
      if (r.status === 403) {
        error.value = 'Ce lien n’est plus disponible.'
        return
      }
      window.location.href = url
    })
    .catch(() => (window.location.href = url))
}
</script>

<template>
  <div class="login-screen">
    <button class="icon-btn" style="position: fixed; top: 16px; right: 16px" @click="ui.toggleTheme">
      <component :is="ui.theme === 'dark' ? Sun : Moon" :size="16" />
    </button>

    <div class="login-card" style="gap: 20px">
      <div class="logo">
        <span class="glyph">◆</span>
        <span class="logo-word"><em>Orion</em><strong>Drive</strong></span>
      </div>

      <template v-if="notFound">
        <TriangleAlert :size="40" style="color: var(--danger)" />
        <h1>Lien introuvable</h1>
        <p>Ce lien de partage n’existe pas ou a été supprimé.</p>
      </template>

      <template v-else-if="data">
        <div class="preview-visual" style="width: 100%; margin: 0; height: 120px">
          <component :is="meta.icon" :size="44" :class="'tint-' + meta.tint" />
        </div>
        <div style="text-align: center">
          <h1 style="font-size: 20px">{{ data.name }}</h1>
          <p class="mono">{{ fmtSize(data.size) }} · partagé par {{ data.owner }}</p>
        </div>

        <template v-if="unavailable">
          <p style="color: var(--danger)">
            {{ data.expired ? 'Ce lien a expiré.' : 'La limite de téléchargements est atteinte.' }}
          </p>
        </template>
        <template v-else>
          <label v-if="data.has_password" style="width: 100%; display: flex; flex-direction: column; gap: 6px">
            <span class="tweak-label" style="letter-spacing: 0.06em; display: flex; align-items: center; gap: 6px">
              <Lock :size="12" />Mot de passe
            </span>
            <input v-model="password" class="input" type="password" placeholder="Requis" @keyup.enter="download" />
          </label>
          <p v-if="error" style="color: var(--danger); font-size: 12.5px; margin: 0">{{ error }}</p>
          <button class="btn btn-primary" style="width: 100%; height: 42px" @click="download">
            <Download :size="16" />Télécharger
          </button>
        </template>
      </template>

      <template v-else>
        <p>Chargement…</p>
      </template>
    </div>
  </div>
</template>
