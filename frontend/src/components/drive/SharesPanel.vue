<script setup lang="ts">
import { ref } from 'vue'
import { Copy, Check, ExternalLink, Trash2, Pencil, Lock, Folder, FileText, Eye, Download, Clock, Share2 } from 'lucide-vue-next'
import type { ShareInfo } from '@/lib/api'
import { fmtDate } from '@/lib/format'
import { useFilesStore } from '@/stores/files'
import ShareEditDialog from './ShareEditDialog.vue'

defineProps<{ shares: ShareInfo[] }>()
const files = useFilesStore()

const editing = ref<ShareInfo | null>(null)
const copied = ref<string | null>(null)
async function copy(s: ShareInfo) {
  try {
    await navigator.clipboard.writeText(s.url)
    copied.value = s.token
    setTimeout(() => (copied.value === s.token ? (copied.value = null) : null), 1500)
  } catch {
    files.ui().toast(`Lien : ${s.url}`, 'link')
  }
}

function status(s: ShareInfo): { label: string; cls: string } | null {
  if (s.expired) return { label: 'Expiré', cls: 'danger' }
  if (s.exhausted) return { label: 'Épuisé', cls: 'danger' }
  return null
}
</script>

<template>
  <div class="shares">
    <div v-if="!shares.length" class="empty">
      <Share2 :size="36" />
      <span class="empty-title">Aucun partage</span>
      <span class="empty-sub">Les liens que tu crées (clic droit → Partager) apparaîtront ici.</span>
    </div>

    <div v-for="s in shares" :key="s.token" class="share-card" :class="{ inactive: !!status(s) }">
      <component :is="s.is_dir ? Folder : FileText" :size="20" :class="s.is_dir ? 'tint-folder' : 'tint-neutral'" />

      <div class="share-main">
        <div class="share-line1">
          <span class="share-name">{{ s.name }}</span>
          <span v-if="status(s)" class="badge" :class="status(s)!.cls">{{ status(s)!.label }}</span>
          <span v-if="s.has_password" class="badge"><Lock :size="11" /> mot de passe</span>
        </div>
        <div class="share-stats">
          <span title="Vues"><Eye :size="13" /> {{ s.views }}</span>
          <span title="Téléchargements">
            <Download :size="13" /> {{ s.downloads }}<template v-if="s.remain_downloads !== null"> / {{ s.downloads + s.remain_downloads }}</template>
          </span>
          <span v-if="s.expires" title="Expiration"><Clock :size="13" /> {{ fmtDate(s.expires) }}</span>
          <span class="share-created">créé le {{ fmtDate(s.created_at) }}</span>
        </div>
      </div>

      <div class="share-actions">
        <button class="icon-btn" title="Copier le lien" @click="copy(s)">
          <component :is="copied === s.token ? Check : Copy" :size="16" />
        </button>
        <a class="icon-btn" title="Ouvrir" :href="s.url" target="_blank" rel="noopener"><ExternalLink :size="16" /></a>
        <button class="icon-btn" title="Modifier" @click="editing = s"><Pencil :size="16" /></button>
        <button class="icon-btn danger" title="Révoquer" @click="files.revokeShare(s.token)"><Trash2 :size="16" /></button>
      </div>
    </div>

    <ShareEditDialog v-if="editing" :share="editing" @close="editing = null" @saved="files.load()" />
  </div>
</template>
