<script setup lang="ts">
import { computed } from 'vue'
import { Upload, Check, File as FileIcon } from 'lucide-vue-next'
import type { Upload as UploadItem } from '@/stores/files'

const props = defineProps<{ uploads: UploadItem[] }>()

const title = computed(() => {
  const active = props.uploads.filter((u) => !u.done).length
  if (!active) return 'Importation terminée'
  return 'Importation de ' + active + ' fichier' + (active > 1 ? 's' : '') + '…'
})
</script>

<template>
  <div class="uploads">
    <div class="uploads-head"><Upload :size="14" />{{ title }}</div>
    <div v-for="u in uploads" :key="u.id" class="upload-item">
      <Check v-if="u.done" :size="15" class="done-ic" />
      <FileIcon v-else :size="15" />
      <div class="upload-info">
        <span class="upload-name">{{ u.name }}</span>
        <div class="upload-track"><div class="upload-fill" :style="{ width: u.progress + '%' }"></div></div>
      </div>
      <span class="upload-pct">{{ Math.round(u.progress) }}%</span>
    </div>
  </div>
</template>
