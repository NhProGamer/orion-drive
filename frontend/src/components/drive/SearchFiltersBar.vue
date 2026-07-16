<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Star, X, Filter } from 'lucide-vue-next'
import { useFilesStore, type SearchType, type SearchKind, type SearchSince } from '@/stores/files'

const { t } = useI18n()
const files = useFilesStore()

const types: { v: SearchType; label: string }[] = [
  { v: '', label: 'search.typeAll' },
  { v: 'folder', label: 'shell.folders' },
  { v: 'file', label: 'shell.files' },
]
const kinds: SearchKind[] = ['images', 'media', 'documents', 'archives']
const sinces: SearchSince[] = ['1d', '7d', '30d', '365d']

function setType(v: SearchType) {
  files.setFilters({ type: v })
}
function toggleKind(k: SearchKind) {
  files.setFilters({ kind: files.filters.kind === k ? '' : k })
}
function toggleStarred() {
  files.setFilters({ starred: !files.filters.starred })
}
function setSince(e: Event) {
  files.setFilters({ since: (e.target as HTMLSelectElement).value as SearchSince })
}
</script>

<template>
  <div class="filter-bar">
    <span class="filter-icon"><Filter :size="14" /></span>

    <!-- Type -->
    <div class="filter-group">
      <button
        v-for="ty in types"
        :key="ty.v || 'all'"
        class="filter-chip"
        :class="{ active: files.filters.type === ty.v }"
        @click="setType(ty.v)"
      >
        {{ t(ty.label) }}
      </button>
    </div>

    <span class="filter-sep"></span>

    <!-- Category -->
    <div class="filter-group">
      <button
        v-for="k in kinds"
        :key="k"
        class="filter-chip"
        :class="{ active: files.filters.kind === k }"
        @click="toggleKind(k)"
      >
        {{ t('storage.cat.' + k) }}
      </button>
    </div>

    <span class="filter-sep"></span>

    <!-- Starred -->
    <button class="filter-chip" :class="{ active: files.filters.starred }" @click="toggleStarred">
      <Star :size="13" />{{ t('search.starredOnly') }}
    </button>

    <!-- Modified since -->
    <select class="input filter-select" :value="files.filters.since" @change="setSince">
      <option value="">{{ t('search.anyTime') }}</option>
      <option v-for="s in sinces" :key="s" :value="s">{{ t('search.since' + s) }}</option>
    </select>

    <button v-if="files.hasFilters" class="filter-clear" :title="t('search.clearFilters')" @click="files.clearFilters()">
      <X :size="14" />
    </button>
  </div>
</template>
