<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { Download, Lock, TriangleAlert, Sun, Moon, Folder, FileText, ChevronRight, FolderArchive, FolderOpen } from 'lucide-vue-next'
import { api, type ShareView as ShareViewData, type ShareEntry } from '@/lib/api'
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

// Folder browsing state.
const opened = ref(false)
const entries = ref<ShareEntry[]>([])
const curPath = ref('')

const meta = computed(() => (data.value ? metaFor(kindFromName(data.value.name)) : metaFor('file')))
const unavailable = computed(() => data.value && (data.value.expired || data.value.exhausted))
const crumbs = computed(() => {
  const parts = curPath.value ? curPath.value.split('/') : []
  const acc: { name: string; path: string }[] = [{ name: data.value?.name || '', path: '' }]
  let p = ''
  for (const seg of parts) {
    p = p ? `${p}/${seg}` : seg
    acc.push({ name: seg, path: p })
  }
  return acc
})

onMounted(async () => {
  try {
    data.value = await api.shareView(token)
    if (data.value.is_dir && !data.value.has_password && !unavailable.value) openList('')
  } catch {
    notFound.value = true
  }
})

async function openList(p: string) {
  error.value = ''
  try {
    const res = await api.shareList(token, p, password.value || undefined)
    entries.value = res.entries
    curPath.value = p
    opened.value = true
  } catch (e: any) {
    error.value = e?.code === 401 ? 'Mot de passe incorrect ou requis.' : e?.message || 'Accès impossible.'
  }
}

function downloadFile(entry: ShareEntry) {
  window.location.href = api.shareContentUrl(token, entry.path, password.value || undefined)
}
function downloadFolderArchive() {
  window.location.href = api.shareArchiveUrl(token, curPath.value, password.value || undefined)
}

// Single-file share download (with inline password probe).
function download() {
  error.value = ''
  const url = api.shareContentUrl(token, undefined, password.value || undefined)
  fetch(url, { redirect: 'manual' })
    .then((r) => {
      if (r.status === 401) return void (error.value = 'Mot de passe incorrect ou requis.')
      if (r.status === 403) return void (error.value = 'Ce lien n’est plus disponible.')
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

    <div class="login-card" :style="{ gap: '20px', maxWidth: opened ? '620px' : undefined, width: opened ? '92vw' : undefined }">
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
        <!-- Folder browser -->
        <template v-if="data.is_dir && opened">
          <div class="share-browser">
            <div class="share-crumbs">
              <template v-for="(c, i) in crumbs" :key="c.path">
                <ChevronRight v-if="i > 0" :size="13" class="tint-neutral" />
                <button class="crumb-btn" @click="openList(c.path)">{{ i === 0 ? data.name : c.name }}</button>
              </template>
            </div>
            <div class="share-list">
              <div v-if="!entries.length" class="share-empty">Dossier vide</div>
              <button
                v-for="e in entries"
                :key="e.path"
                class="share-row"
                @click="e.is_dir ? openList(e.path) : downloadFile(e)"
              >
                <component :is="e.is_dir ? Folder : FileText" :size="16" :class="e.is_dir ? 'tint-folder' : 'tint-neutral'" />
                <span class="share-name">{{ e.name }}</span>
                <span class="mono share-size">{{ e.is_dir ? '' : fmtSize(e.size) }}</span>
                <component :is="e.is_dir ? ChevronRight : Download" :size="15" class="tint-neutral" />
              </button>
            </div>
            <p v-if="error" style="color: var(--danger); font-size: 12.5px; margin: 0">{{ error }}</p>
            <button class="btn btn-secondary" style="width: 100%" @click="downloadFolderArchive">
              <FolderArchive :size="15" />Télécharger ce dossier (.zip)
            </button>
          </div>
        </template>

        <!-- Landing (file share, or locked folder awaiting password) -->
        <template v-else>
          <div class="preview-visual" style="width: 100%; margin: 0; height: 120px">
            <component :is="data.is_dir ? Folder : meta.icon" :size="44" :class="data.is_dir ? 'tint-folder' : 'tint-' + meta.tint" />
          </div>
          <div style="text-align: center">
            <h1 style="font-size: 20px">{{ data.name }}</h1>
            <p class="mono">
              {{ data.is_dir ? 'Dossier partagé' : fmtSize(data.size) }} · partagé par {{ data.owner }}
            </p>
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
              <input v-model="password" class="input" type="password" placeholder="Requis"
                @keyup.enter="data.is_dir ? openList('') : download()" />
            </label>
            <p v-if="error" style="color: var(--danger); font-size: 12.5px; margin: 0">{{ error }}</p>
            <button class="btn btn-primary" style="width: 100%; height: 42px" @click="data.is_dir ? openList('') : download()">
              <component :is="data.is_dir ? FolderOpen : Download" :size="16" />
              {{ data.is_dir ? 'Ouvrir le dossier' : 'Télécharger' }}
            </button>
          </template>
        </template>
      </template>

      <template v-else>
        <p>Chargement…</p>
      </template>
    </div>
  </div>
</template>
