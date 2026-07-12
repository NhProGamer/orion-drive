<script setup lang="ts">
import { computed } from 'vue'
import { Star, X, Download } from 'lucide-vue-next'
import type { FileNode } from '@/lib/api'
import { kindFromName, ext, fmtSize, fmtDate } from '@/lib/format'
import { metaFor } from '@/lib/icons'

const props = defineProps<{ node: FileNode; location: string }>()
defineEmits<{ close: []; download: []; star: [] }>()

const kind = computed(() => (props.node.type === 'folder' ? 'folder' : kindFromName(props.node.name)))
const meta = computed(() => metaFor(kind.value))
const isImage = computed(() => kind.value === 'image')
const sizeLabel = computed(() => (props.node.type === 'folder' ? '—' : fmtSize(props.node.size)))
const dateLabel = computed(() => fmtDate(props.node.modified))
</script>

<template>
  <aside class="preview">
    <div class="preview-head">
      <component :is="meta.icon" :size="16" :class="'tint-' + meta.tint" />
      <span class="title">{{ node.name }}</span>
      <button class="icon-btn" :title="node.starred ? 'Ne plus suivre' : 'Suivre'" @click="$emit('star')">
        <span :class="node.starred ? 'star-mark' : ''" style="display: flex"><Star :size="15" /></span>
      </button>
      <button class="icon-btn" title="Fermer" @click="$emit('close')"><X :size="15" /></button>
    </div>
    <div class="preview-visual">
      <div v-if="isImage" class="thumb-img" style="height: 100%"><span>aperçu · {{ ext(node.name) }}</span></div>
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
    <div class="preview-actions">
      <button class="btn btn-secondary" @click="$emit('download')"><Download :size="15" />Télécharger</button>
    </div>
  </aside>
</template>
