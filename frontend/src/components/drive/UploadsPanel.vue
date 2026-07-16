<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Upload, Check, File as FileIcon } from 'lucide-vue-next'
import type { Upload as UploadItem } from '@/stores/files'

const { t } = useI18n()
const props = defineProps<{ uploads: UploadItem[] }>()

const title = computed(() => {
  const active = props.uploads.filter((u) => !u.done).length
  if (!active) return t('uploads.done')
  return t('uploads.active', active)
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
