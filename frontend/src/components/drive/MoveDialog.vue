<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { FolderInput, Folder, ChevronRight } from 'lucide-vue-next'
import { api, type FileNode } from '@/lib/api'
import { useFilesStore } from '@/stores/files'
import { useUiStore } from '@/stores/ui'

const props = defineProps<{ nodes: FileNode[] }>()
const emit = defineEmits<{ close: []; moved: [] }>()
const { t } = useI18n()
const files = useFilesStore()
const ui = useUiStore()

// The folders being moved cannot be their own destination (nor can we descend
// into them — they are hidden from every listing, which blocks moving into a
// descendant and creating a cycle).
const movingIds = new Set(props.nodes.map((n) => n.id))

const crumbs = ref<{ id: number | null; name: string }[]>([{ id: null, name: t('common.myDrive') }])
const subfolders = ref<FileNode[]>([])
const loading = ref(true)
const busy = ref(false)

const cur = computed(() => crumbs.value[crumbs.value.length - 1])
const curParam = computed(() => (cur.value.id === null ? 'root' : String(cur.value.id)))

// The items already live in this folder → moving here is a no-op.
const sameParent = computed(() => (props.nodes[0]?.parent_id ?? null) === cur.value.id)

async function load() {
  loading.value = true
  try {
    const list = await api.list({ parent: curParam.value })
    subfolders.value = list.filter((n) => n.type === 'folder' && !movingIds.has(n.id))
  } catch (e: any) {
    ui.toast(t('move.unreadable') + (e?.message ? ` : ${e.message}` : ''), 'x')
  } finally {
    loading.value = false
  }
}
function enter(f: FileNode) {
  crumbs.value.push({ id: f.id, name: f.name })
  load()
}
function goTo(i: number) {
  crumbs.value = crumbs.value.slice(0, i + 1)
  load()
}

async function confirm() {
  busy.value = true
  try {
    await files.move([...movingIds], curParam.value)
    emit('moved')
  } catch (e: any) {
    ui.toast(t('move.failed') + (e?.message ? ` : ${e.message}` : ''), 'x')
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="overlay" @click.self="emit('close')">
    <div class="dialog" style="max-width: 520px; width: 92vw">
      <h2>
        <FolderInput :size="18" style="vertical-align: -3px; margin-right: 6px" />{{
          t('move.header', nodes.length)
        }}
      </h2>

      <div class="move-crumbs">
        <template v-for="(c, i) in crumbs" :key="i">
          <ChevronRight v-if="i > 0" :size="13" class="tint-neutral" />
          <button class="crumb-btn" :class="{ current: i === crumbs.length - 1 }" @click="goTo(i)">{{ c.name }}</button>
        </template>
      </div>

      <div class="move-list">
        <div v-if="loading" class="move-empty">{{ t('common.loading') }}</div>
        <div v-else-if="!subfolders.length" class="move-empty">{{ t('move.noSubfolders') }}</div>
        <button v-for="f in subfolders" :key="f.id" class="move-row" @click="enter(f)">
          <Folder :size="16" class="tint-folder" />
          <span class="move-name">{{ f.name }}</span>
          <ChevronRight :size="15" class="tint-neutral" />
        </button>
      </div>

      <div class="dialog-actions">
        <button class="btn btn-ghost" @click="emit('close')">{{ t('common.cancel') }}</button>
        <button class="btn btn-primary" :disabled="busy || sameParent" @click="confirm">
          {{ sameParent ? t('move.alreadyHere') : t('move.moveInto', { name: cur.name }) }}
        </button>
      </div>
    </div>
  </div>
</template>
