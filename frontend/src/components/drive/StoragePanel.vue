<script setup lang="ts">
import { computed } from 'vue'
import type { FileNode } from '@/lib/api'
import { kindFromName, fmtSize } from '@/lib/format'
import { metaFor } from '@/lib/icons'

const props = defineProps<{ files: FileNode[]; total: number }>()
defineEmits<{ open: [node: FileNode] }>()

interface Cat {
  label: string
  kinds: string[] | null
  color: string
}
const CATS: Cat[] = [
  { label: 'Images', kinds: ['image', 'design'], color: 'var(--accent)' },
  { label: 'Vidéos & audio', kinds: ['video', 'audio'], color: 'var(--info)' },
  { label: 'Documents', kinds: ['doc', 'text', 'pdf', 'sheet', 'slides'], color: 'var(--success)' },
  { label: 'Archives & disques', kinds: ['archive', 'disc'], color: 'var(--warn)' },
  { label: 'Autres', kinds: null, color: 'var(--fg-3)' },
]

const used = computed(() => props.files.reduce((s, n) => s + n.size, 0))

const cats = computed(() => {
  const known = CATS.slice(0, -1).flatMap((c) => c.kinds!)
  return CATS.map((c) => ({
    label: c.label,
    color: c.color,
    size: props.files
      .filter((f) => {
        const k = kindFromName(f.name)
        return c.kinds ? c.kinds.includes(k) : !known.includes(k)
      })
      .reduce((s, n) => s + n.size, 0),
  }))
})

const top = computed(() => props.files.slice().sort((a, b) => b.size - a.size).slice(0, 6))

function segWidth(size: number) {
  return Math.max(0.5, props.total ? (size / props.total) * 100 : 0) + '%'
}
</script>

<template>
  <div class="storage">
    <div class="storage-card">
      <span class="storage-big"><strong>{{ fmtSize(used) }}</strong> utilisés sur {{ fmtSize(total) }}</span>
      <div class="storage-bar">
        <div
          v-for="c in cats"
          :key="c.label"
          class="storage-seg"
          :style="{ width: segWidth(c.size), background: c.color }"
          :title="c.label"
        ></div>
      </div>
      <div class="storage-legend">
        <span v-for="c in cats" :key="'l-' + c.label" class="legend-item">
          <span class="legend-dot" :style="{ background: c.color }"></span>{{ c.label }}
          <span class="legend-size">{{ fmtSize(c.size) }}</span>
        </span>
      </div>
    </div>
    <section class="section">
      <span class="eyebrow">Fichiers les plus volumineux</span>
      <div class="storage-card top-files">
        <button v-for="f in top" :key="f.id" class="top-file" @click="$emit('open', f)">
          <component :is="metaFor(kindFromName(f.name)).icon" :size="16" :class="'tint-' + metaFor(kindFromName(f.name)).tint" />
          <span class="name">{{ f.name }}</span>
          <span class="size">{{ fmtSize(f.size) }}</span>
        </button>
      </div>
    </section>
  </div>
</template>
