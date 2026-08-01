<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { SlidersHorizontal, Star, X, Image, Film, FileText, Archive, Calendar, HardDrive } from 'lucide-vue-next'
import { useFilesStore, type SearchType, type SearchKind, type SearchSince } from '@/stores/files'

const { t } = useI18n()
const files = useFilesStore()
const emit = defineEmits<{ close: [] }>()

const types: { v: SearchType; label: string }[] = [
  { v: '', label: 'search.typeAll' },
  { v: 'folder', label: 'shell.folders' },
  { v: 'file', label: 'shell.files' },
]
const kinds: { v: SearchKind; icon: typeof Image }[] = [
  { v: 'images', icon: Image },
  { v: 'media', icon: Film },
  { v: 'documents', icon: FileText },
  { v: 'archives', icon: Archive },
]
const sinces: SearchSince[] = ['1d', '7d', '30d', '365d']

// Size presets, in bytes (SI, matching fmtSize's 1000-based units).
const MB = 1_000_000
const sizePresets = [
  { key: 'under', min: 0, max: MB, label: 'search.sizeUnder' },
  { key: 'mid', min: MB, max: 100 * MB, label: 'search.sizeMid' },
  { key: 'over', min: 100 * MB, max: 0, label: 'search.sizeOver' },
]

function setType(v: SearchType) {
  files.setFilters({ type: v })
}
function toggleKind(k: SearchKind) {
  files.setFilters({ kind: files.filters.kind === k ? '' : k })
}
// A relative preset and a custom range are mutually exclusive.
function toggleSince(s: SearchSince) {
  files.setFilters({ since: files.filters.since === s ? '' : s, after: '', before: '' })
}
function setRange(patch: Partial<{ after: string; before: string }>) {
  files.setFilters({ ...patch, since: '' })
}
function togglePreset(p: (typeof sizePresets)[number]) {
  const active = presetActive(p)
  files.setFilters({ minSize: active ? 0 : p.min, maxSize: active ? 0 : p.max })
}
function presetActive(p: (typeof sizePresets)[number]) {
  return files.filters.minSize === p.min && files.filters.maxSize === p.max
}
// Custom size inputs are expressed in MB; the store keeps bytes.
const minMB = computed({
  get: () => (files.filters.minSize ? files.filters.minSize / MB : null),
  set: (v: number | null) => files.setFilters({ minSize: v && v > 0 ? Math.round(v * MB) : 0 }),
})
const maxMB = computed({
  get: () => (files.filters.maxSize ? files.filters.maxSize / MB : null),
  set: (v: number | null) => files.setFilters({ maxSize: v && v > 0 ? Math.round(v * MB) : 0 }),
})
function toggleStarred() {
  files.setFilters({ starred: !files.filters.starred })
}

function sizeLabel(): string {
  const f = files.filters
  const mo = (b: number) => b / MB
  if (f.minSize && f.maxSize) return `${mo(f.minSize)}–${mo(f.maxSize)} Mo`
  if (f.maxSize) return `< ${mo(f.maxSize)} Mo`
  return `> ${mo(f.minSize)} Mo`
}

// Active-filter summary chips, each removable.
const chips = computed(() => {
  const f = files.filters
  const out: { label: string; clear: () => void }[] = []
  if (f.type) out.push({ label: t(f.type === 'folder' ? 'shell.folders' : 'shell.files'), clear: () => files.setFilters({ type: '' }) })
  if (f.kind) out.push({ label: t('storage.cat.' + f.kind), clear: () => files.setFilters({ kind: '' }) })
  if (f.since) out.push({ label: t('search.since' + f.since), clear: () => files.setFilters({ since: '' }) })
  else if (f.after || f.before) out.push({ label: `${f.after || '…'} → ${f.before || '…'}`, clear: () => files.setFilters({ after: '', before: '' }) })
  if (f.minSize || f.maxSize) out.push({ label: sizeLabel(), clear: () => files.setFilters({ minSize: 0, maxSize: 0 }) })
  if (f.starred) out.push({ label: t('search.starredOnly'), clear: () => files.setFilters({ starred: false }) })
  return out
})

const count = computed(() => files.nodes.length)
</script>

<template>
  <section class="sf">
    <header class="sf-head">
      <SlidersHorizontal :size="16" />
      <h2>{{ t('search.advanced') }}</h2>
      <span v-if="files.searching" class="sf-count">{{ t('search.results', { n: count }) }}</span>
      <span class="sf-spacer"></span>
      <button v-if="files.hasFilters" class="sf-clear" @click="files.clearFilters()">
        <X :size="14" />{{ t('search.clearFilters') }}
      </button>
      <button class="sf-close" :title="t('common.close')" @click="emit('close')">
        <X :size="16" />
      </button>
    </header>

    <div class="sf-groups">
      <!-- Type -->
      <div class="sf-group">
        <div class="sf-label">{{ t('search.secType') }}</div>
        <div class="sf-seg">
          <button v-for="ty in types" :key="ty.v || 'all'" :aria-pressed="files.filters.type === ty.v" @click="setType(ty.v)">
            {{ t(ty.label) }}
          </button>
        </div>
      </div>

      <!-- Category -->
      <div class="sf-group">
        <div class="sf-label">{{ t('search.secCategory') }}</div>
        <div class="sf-chips">
          <button
            v-for="k in kinds"
            :key="k.v"
            class="sf-chip"
            :aria-pressed="files.filters.kind === k.v"
            @click="toggleKind(k.v)"
          >
            <component :is="k.icon" :size="14" />{{ t('storage.cat.' + k.v) }}
          </button>
        </div>
      </div>

      <!-- Date -->
      <div class="sf-group sf-span">
        <div class="sf-label"><Calendar :size="13" />{{ t('search.secDate') }}</div>
        <div class="sf-chips">
          <button
            v-for="s in sinces"
            :key="s"
            class="sf-chip"
            :aria-pressed="files.filters.since === s"
            @click="toggleSince(s)"
          >
            {{ t('search.since' + s) }}
          </button>
          <span class="sf-vsep"></span>
          <label class="sf-field"><span>{{ t('search.from') }}</span>
            <input type="date" :value="files.filters.after" @input="setRange({ after: ($event.target as HTMLInputElement).value })" />
          </label>
          <label class="sf-field"><span>{{ t('search.to') }}</span>
            <input type="date" :value="files.filters.before" @input="setRange({ before: ($event.target as HTMLInputElement).value })" />
          </label>
        </div>
      </div>

      <!-- Size -->
      <div class="sf-group sf-span">
        <div class="sf-label"><HardDrive :size="13" />{{ t('search.secSize') }}</div>
        <div class="sf-chips">
          <button
            v-for="p in sizePresets"
            :key="p.key"
            class="sf-chip"
            :aria-pressed="presetActive(p)"
            @click="togglePreset(p)"
          >
            {{ t(p.label) }}
          </button>
          <span class="sf-vsep"></span>
          <label class="sf-field sf-mono"><span>{{ t('search.min') }}</span>
            <input type="number" min="0" placeholder="0" v-model.number="minMB" /><small>Mo</small>
          </label>
          <label class="sf-field sf-mono"><span>{{ t('search.max') }}</span>
            <input type="number" min="0" placeholder="∞" v-model.number="maxMB" /><small>Mo</small>
          </label>
        </div>
      </div>

      <!-- Attributes -->
      <div class="sf-group sf-span">
        <div class="sf-label">{{ t('search.secAttributes') }}</div>
        <button class="sf-switch" :aria-pressed="files.filters.starred" @click="toggleStarred">
          <span class="sf-track"></span>
          <span class="sf-switch-txt"><Star :size="13" />{{ t('search.starredOnly') }}</span>
        </button>
      </div>
    </div>

    <!-- Active filters summary -->
    <div v-if="chips.length" class="sf-active">
      <span class="sf-active-lead">{{ t('search.activeFilters') }}</span>
      <span v-for="(c, i) in chips" :key="i" class="sf-tag">
        {{ c.label }}
        <button :aria-label="t('search.clearFilters')" @click="c.clear()"><X :size="12" /></button>
      </span>
    </div>
  </section>
</template>
