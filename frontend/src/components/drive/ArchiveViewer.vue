<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Folder, FileText, ChevronRight, FileArchive, House } from 'lucide-vue-next'
import { api, type FileNode, type ArchiveEntry } from '@/lib/api'
import { fmtSize } from '@/lib/format'

const { t } = useI18n()
const props = defineProps<{ node: FileNode }>()

const entries = ref<ArchiveEntry[]>([])
const loading = ref(false)
const error = ref(false)
// Current location inside the archive, as path segments.
const cwd = ref<string[]>([])

// The archive is listed from its index only (ZIP central directory, tar/7z
// headers) — never extracted — so navigating the tree costs nothing.
watch(
  () => props.node.id,
  async () => {
    entries.value = []
    cwd.value = []
    error.value = false
    loading.value = true
    try {
      entries.value = await api.archiveEntries(props.node.id)
    } catch {
      error.value = true
    } finally {
      loading.value = false
    }
  },
  { immediate: true },
)

// Folders and files directly under the current path, derived from the flat entry
// list (folders are inferred from deeper paths even without explicit dir entries).
const level = computed(() => {
  const prefix = cwd.value.length ? cwd.value.join('/') + '/' : ''
  const folders = new Set<string>()
  const files: { name: string; size: number }[] = []
  for (const e of entries.value) {
    const path = e.name.replace(/\/+$/, '')
    if (!path.startsWith(prefix)) continue
    const rest = path.slice(prefix.length)
    if (!rest) continue
    const slash = rest.indexOf('/')
    if (slash >= 0) folders.add(rest.slice(0, slash))
    else if (e.is_dir) folders.add(rest)
    else files.push({ name: rest, size: e.size })
  }
  return {
    folders: [...folders].sort((a, b) => a.localeCompare(b)),
    files: files.sort((a, b) => a.name.localeCompare(b.name)),
  }
})

const isEmpty = computed(() => !loading.value && !error.value && !level.value.folders.length && !level.value.files.length)

function enter(folder: string) {
  cwd.value = [...cwd.value, folder]
}
function goTo(index: number) {
  // -1 = archive root; 0.. = a segment of the current path.
  cwd.value = cwd.value.slice(0, index + 1)
}
</script>

<template>
  <div class="ov-arch">
    <nav class="ov-arch-crumbs" :aria-label="t('archive.title')">
      <button class="ov-arch-crumb" @click="goTo(-1)">
        <House :size="14" /><span>{{ node.name }}</span>
      </button>
      <template v-for="(seg, i) in cwd" :key="i">
        <ChevronRight :size="14" class="ov-arch-sep" />
        <button class="ov-arch-crumb" @click="goTo(i)">{{ seg }}</button>
      </template>
    </nav>

    <div class="ov-arch-body">
      <div v-if="loading" class="ov-empty">{{ t('common.loading') }}</div>
      <div v-else-if="error" class="ov-empty">
        <FileArchive :size="40" />
        <p>{{ t('archive.error') }}</p>
      </div>
      <div v-else-if="isEmpty" class="ov-empty">
        <Folder :size="40" />
        <p>{{ t('archive.empty') }}</p>
      </div>
      <div v-else class="ov-arch-list">
        <button
          v-for="f in level.folders"
          :key="'d/' + f"
          class="ov-arch-row is-dir"
          @click="enter(f)"
        >
          <Folder :size="16" class="ov-arch-folder" />
          <span class="ov-arch-name">{{ f }}</span>
          <ChevronRight :size="15" class="ov-arch-go" />
        </button>
        <div v-for="f in level.files" :key="'f/' + f.name" class="ov-arch-row is-file">
          <FileText :size="16" class="tint-neutral" />
          <span class="ov-arch-name">{{ f.name }}</span>
          <span class="mono ov-arch-size">{{ fmtSize(f.size) }}</span>
        </div>
      </div>
    </div>
  </div>
</template>
