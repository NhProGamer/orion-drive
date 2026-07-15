<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Star, X, Download, Lock, Unlock, Link as LinkIcon, History, RotateCcw, Trash2, FolderInput, Folder, FileText } from 'lucide-vue-next'
import { api, type FileNode, type Version, type ArchiveEntry } from '@/lib/api'
import { kindFromName, fmtSize, fmtDate, isArchive, canThumbnail } from '@/lib/format'
import { metaFor } from '@/lib/icons'
import { useFilesStore } from '@/stores/files'

const props = defineProps<{ node: FileNode; location: string }>()
defineEmits<{ close: []; download: []; star: [] }>()

const files = useFilesStore()
const kind = computed(() => (props.node.type === 'folder' ? 'folder' : kindFromName(props.node.name)))
const meta = computed(() => metaFor(kind.value))
const showThumb = computed(() => props.node.type === 'file' && canThumbnail(props.node.name))
const thumbUrl = computed(() => api.thumbUrl(props.node.id))
const thumbFailed = ref(false)
watch(() => [props.node.id, props.node.modified], () => (thumbFailed.value = false))
const sizeLabel = computed(() => (props.node.type === 'folder' ? '—' : fmtSize(props.node.size)))
const dateLabel = computed(() => fmtDate(props.node.modified))

const versions = ref<Version[]>([])
const entries = ref<ArchiveEntry[]>([])
const archive = computed(() => props.node.type === 'file' && isArchive(props.node.name))

async function loadVersions() {
  versions.value = []
  if (props.node.type !== 'file') return
  try {
    versions.value = await api.listVersions(props.node.id)
  } catch {
    versions.value = []
  }
}
async function loadEntries() {
  entries.value = []
  if (!archive.value) return
  try {
    entries.value = await api.archiveEntries(props.node.id)
  } catch {
    entries.value = []
  }
}
watch(() => props.node.id, () => { loadVersions(); loadEntries() }, { immediate: true })

async function restore(v: Version) {
  await api.restoreVersion(props.node.id, v.id)
  await Promise.all([files.load(), loadVersions()])
  files.ui().toast('Version restaurée', 'restore')
}
async function removeVersion(v: Version) {
  await api.deleteVersion(props.node.id, v.id)
  await Promise.all([files.load(), files.loadCapacity(), loadVersions()])
}
async function toggleLock() {
  await files.setLock(props.node, !props.node.locked)
}
</script>

<template>
  <aside class="preview">
    <div class="preview-head">
      <component :is="meta.icon" :size="16" :class="'tint-' + meta.tint" />
      <span class="title">{{ node.name }}</span>
      <Lock v-if="node.locked" :size="14" class="tint-warn" title="Verrouillé" />
      <button class="icon-btn" :title="node.starred ? 'Ne plus suivre' : 'Suivre'" @click="$emit('star')">
        <span :class="node.starred ? 'star-mark' : ''" style="display: flex"><Star :size="15" /></span>
      </button>
      <button class="icon-btn" title="Fermer" @click="$emit('close')"><X :size="15" /></button>
    </div>
    <div class="preview-visual">
      <img
        v-if="showThumb && !thumbFailed"
        :src="thumbUrl"
        class="preview-thumb"
        alt=""
        @error="thumbFailed = true"
      />
      <component :is="meta.icon" v-else :size="44" />
    </div>
    <dl class="preview-details">
      <div class="detail-row"><dt>Type</dt><dd>{{ meta.label }}</dd></div>
      <div class="detail-row"><dt>Taille</dt><dd class="mono">{{ sizeLabel }}</dd></div>
      <div class="detail-row"><dt>Propriétaire</dt><dd>{{ node.owner }}</dd></div>
      <div class="detail-row"><dt>Modifié</dt><dd class="mono">{{ dateLabel }}</dd></div>
      <div class="detail-row"><dt>Emplacement</dt><dd>{{ location }}</dd></div>
      <div class="detail-row"><dt>ID</dt><dd class="mono">od_{{ node.id }}</dd></div>
    </dl>

    <div v-if="node.type === 'file' && versions.length > 1" class="preview-versions">
      <div class="pv-head"><History :size="14" />Versions <span class="pv-count">{{ versions.length }}</span></div>
      <div v-for="v in versions" :key="v.id" class="pv-item" :class="{ current: v.current }">
        <div class="pv-meta">
          <span class="mono">{{ fmtSize(v.size) }}</span>
          <span class="pv-date">{{ fmtDate(v.created) }}</span>
        </div>
        <span v-if="v.current" class="pv-badge">actuelle</span>
        <template v-else>
          <button class="icon-btn" title="Restaurer cette version" @click="restore(v)"><RotateCcw :size="14" /></button>
          <button class="icon-btn" title="Supprimer cette version" @click="removeVersion(v)"><Trash2 :size="14" /></button>
        </template>
      </div>
    </div>

    <div v-if="archive && entries.length" class="preview-versions">
      <div class="pv-head"><FolderInput :size="14" />Contenu de l’archive <span class="pv-count">{{ entries.length }}</span></div>
      <div v-for="(e, i) in entries.slice(0, 50)" :key="i" class="pv-item">
        <component :is="e.is_dir ? Folder : FileText" :size="14" class="tint-neutral" />
        <div class="pv-meta">
          <span class="pv-name">{{ e.name }}</span>
        </div>
        <span v-if="!e.is_dir" class="mono pv-date">{{ fmtSize(e.size) }}</span>
      </div>
    </div>

    <div class="preview-actions">
      <button class="btn btn-secondary" @click="$emit('download')"><Download :size="15" />Télécharger</button>
      <button v-if="archive" class="btn btn-secondary" @click="files.extract(node)"><FolderInput :size="15" />Extraire</button>
      <button v-if="node.type === 'file'" class="btn btn-secondary" @click="files.createDirectLink(node)">
        <LinkIcon :size="15" />Lien direct
      </button>
      <button class="btn btn-secondary" @click="toggleLock">
        <component :is="node.locked ? Unlock : Lock" :size="15" />{{ node.locked ? 'Déverrouiller' : 'Verrouiller' }}
      </button>
    </div>
  </aside>
</template>
