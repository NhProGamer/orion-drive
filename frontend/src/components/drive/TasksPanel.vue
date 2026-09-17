<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { FileArchive, X } from 'lucide-vue-next'
import type { BgTask } from '@/stores/files'
import { fmtEta, fmtSize } from '@/lib/format'
import { useFilesStore } from '@/stores/files'

const { t } = useI18n()
defineProps<{ tasks: BgTask[] }>()
const files = useFilesStore()

function label(tk: BgTask): string {
  return tk.type === 'extract' ? t('tasks.extracting') : t('tasks.compressing')
}

// What the task has got through, when it knows: bytes read as sizes, anything
// else as a plain count.
function amount(tk: BgTask): string {
  if (!tk.total) return ''
  if (tk.unit === 'bytes') return `${fmtSize(tk.done)} / ${fmtSize(tk.total)}`
  return `${tk.done} / ${tk.total}`
}

// The estimate comes from the server, which is the only side that knows how
// long the job has actually been running.
function remaining(tk: BgTask): string {
  return tk.eta > 0 ? t('tasks.remaining', { time: fmtEta(tk.eta) }) : ''
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
            <span v-if="amount(tk)" class="task-msg"> · {{ amount(tk) }}</span>
            <span v-if="remaining(tk)" class="task-msg"> · {{ remaining(tk) }}</span>
          </span>
          <div class="upload-track">
            <div v-if="tk.progress >= 0" class="upload-fill" :style="{ width: tk.progress + '%' }"></div>
            <div v-else class="upload-fill indeterminate"></div>
          </div>
        </div>
        <span class="upload-pct">{{ tk.progress >= 0 ? Math.round(tk.progress) + '%' : '…' }}</span>
        <button
          v-if="tk.cancellable"
          class="icon-btn task-cancel"
          :title="t('tasks.cancel')"
          @click="files.cancelTask(tk.id)"
        >
          <X :size="14" />
        </button>
      </div>
    </div>
  </div>
</template>
