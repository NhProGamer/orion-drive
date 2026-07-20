<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Upload, Check, File as FileIcon } from 'lucide-vue-next'
import type { Upload as UploadItem } from '@/stores/files'
import { fmtSize, fmtEta } from '@/lib/format'

const { t } = useI18n()
const props = defineProps<{ uploads: UploadItem[] }>()

const active = computed(() => props.uploads.filter((u) => !u.done))
const title = computed(() =>
  active.value.length ? t('uploads.active', active.value.length) : t('uploads.done'),
)

// Aggregate throughput and remaining time across the in-flight uploads. The
// per-file speed is already smoothed (time-constant EMA in the store); the ETA
// is smoothed once more here so the label doesn't twitch between updates.
const speed = computed(() => active.value.reduce((s, u) => s + (u.speed || 0), 0))
const remaining = computed(() => active.value.reduce((s, u) => s + Math.max(0, u.size - u.loaded), 0))
const eta = computed(() => (speed.value > 0 ? remaining.value / speed.value : 0))

const smoothEta = ref(0)
watch(eta, (v) => {
  if (!active.value.length || v <= 0) {
    smoothEta.value = 0
    return
  }
  smoothEta.value = smoothEta.value > 0 ? smoothEta.value * 0.7 + v * 0.3 : v
})

const stats = computed(() => {
  if (!active.value.length || speed.value <= 0) return ''
  const rate = `${fmtSize(speed.value)}/s`
  const left = smoothEta.value > 0 ? ` · ${t('uploads.left', { time: fmtEta(smoothEta.value) })}` : ''
  return rate + left
})
</script>

<template>
  <div class="uploads">
    <div class="uploads-head">
      <Upload :size="14" />
      <span class="uploads-title">{{ title }}</span>
    </div>
    <!-- Stats on their own fixed line so a changing rate/ETA never reflows the header. -->
    <div v-if="stats" class="uploads-stats">{{ stats }}</div>
    <div class="uploads-list">
      <div v-for="u in uploads" :key="u.id" class="upload-item">
        <Check v-if="u.done" :size="15" class="done-ic" />
        <FileIcon v-else :size="15" />
        <div class="upload-info">
          <span class="upload-name">{{ u.name }}</span>
          <div class="upload-track"><div class="upload-fill" :style="{ width: u.progress + '%' }"></div></div>
        </div>
        <span class="upload-pct">
          {{ !u.done && u.speed ? fmtSize(u.speed) + '/s' : Math.round(u.progress) + '%' }}
        </span>
      </div>
    </div>
  </div>
</template>
