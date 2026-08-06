<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { FileArchive } from 'lucide-vue-next'
import type { BgTask } from '@/stores/files'

const { t } = useI18n()
defineProps<{ tasks: BgTask[] }>()

function label(tk: BgTask): string {
  return tk.type === 'extract' ? t('tasks.extracting') : t('tasks.compressing')
}
</script>

<template>
  <div class="uploads">
    <div class="uploads-head">
      <FileArchive :size="14" />
      <span class="uploads-title">{{ t('tasks.title') }}</span>
    </div>
    <div class="uploads-list">
      <div v-for="tk in tasks" :key="tk.id" class="upload-item">
        <FileArchive :size="15" />
        <div class="upload-info">
          <span class="upload-name">
            {{ label(tk) }}<span v-if="tk.message" class="task-msg"> · {{ tk.message }}</span>
          </span>
          <div class="upload-track">
            <div v-if="tk.progress >= 0" class="upload-fill" :style="{ width: tk.progress + '%' }"></div>
            <div v-else class="upload-fill indeterminate"></div>
          </div>
        </div>
        <span class="upload-pct">{{ tk.progress >= 0 ? Math.round(tk.progress) + '%' : '…' }}</span>
      </div>
    </div>
  </div>
</template>
