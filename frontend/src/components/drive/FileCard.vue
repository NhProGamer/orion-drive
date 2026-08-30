<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Star, FolderClosed, MoreVertical, Check } from 'lucide-vue-next'
import { api, type FileNode } from '@/lib/api'
import { kindFromName, fmtSize, fmtDate, canThumbnail } from '@/lib/format'
import { metaFor } from '@/lib/icons'
import { useFilesStore } from '@/stores/files'
import { useUiStore } from '@/stores/ui'
import { useItemGestures } from '@/composables/useItemGestures'

const { t } = useI18n()
const props = defineProps<{ node: FileNode; selected: boolean }>()
const emit = defineEmits<{
  select: [node: FileNode, ev: MouseEvent]
  open: [node: FileNode]
  menu: [node: FileNode, ev: MouseEvent]
}>()

const files = useFilesStore()
const ui = useUiStore()
const gestures = useItemGestures({
  onTap: () => (files.selectionMode ? files.toggleSel(props.node) : emit('open', props.node)),
  onLongPress: () => files.enterSelection(props.node),
})
const isDropTarget = computed(() => files.dragOverId === props.node.id)

// A plain desktop click previews the item in the side details panel; double-click
// opens it (folder → navigate, file → full preview). Selection is via the marquee,
// Ctrl/Cmd/Shift+click, or the checkbox mode — a plain click never selects. On
// touch the tap gesture already opens, so the synthetic click is ignored.
function onClick(ev: MouseEvent) {
  if (files.selectionMode || ev.ctrlKey || ev.metaKey || ev.shiftKey) {
    emit('select', props.node, ev)
    return
  }
  if (ui.coarse) return
  emit('select', props.node, ev)
}

function onDragStart(ev: DragEvent) {
  if (!files.dndEnabled) return
  files.beginDrag(props.node)
  if (ev.dataTransfer) {
    ev.dataTransfer.effectAllowed = 'move'
    ev.dataTransfer.setData('text/plain', String(props.node.id))
  }
}
function onDragOver(ev: DragEvent) {
  if (files.canDropInto(props.node)) {
    ev.preventDefault()
    files.dragOverId = props.node.id
  }
}
function onDragLeave() {
  if (files.dragOverId === props.node.id) files.dragOverId = null
}
function onDrop() {
  if (files.canDropInto(props.node)) files.dropInto(props.node)
}

const kind = computed(() => kindFromName(props.node.name))
const meta = computed(() => metaFor(kind.value))
const canThumb = computed(() => canThumbnail(props.node.name))
const thumbUrl = computed(() => api.thumbUrl(props.node.id))
const thumbFailed = ref(false)
// Reset the fallback when the file (or its content) changes.
watch(() => [props.node.id, props.node.modified], () => (thumbFailed.value = false))
const metaLine = computed(() => {
  const size = props.node.size ? fmtSize(props.node.size) + ' · ' : ''
  return size + fmtDate(props.node.modified)
})
// In search results, show where the item lives (path relative to the drive root).
const locLabel = computed(() =>
  t('shell.myDrive') + (props.node.location ? ' / ' + props.node.location : ''),
)
</script>

<template>
  <div
    class="card"
    :class="{ selected, 'drop-target': isDropTarget, 'select-mode': files.selectionMode }"
    :draggable="files.dndEnabled"
    @click.stop="onClick"
    @dblclick="$emit('open', node)"
    @contextmenu.stop="$emit('menu', node, $event)"
    @touchstart="gestures.onTouchStart"
    @touchmove="gestures.onTouchMove"
    @touchend="gestures.onTouchEnd"
    @touchcancel="gestures.onTouchCancel"
    @dragstart="onDragStart"
    @dragend="files.endDrag()"
    @dragover="onDragOver"
    @dragleave="onDragLeave"
    @drop.stop="onDrop"
  >
    <span v-if="files.selectionMode" class="item-check" :class="{ on: selected }"><Check :size="13" /></span>
    <button
      v-else-if="ui.coarse"
      class="card-menu-btn"
      :aria-label="t('common.actions')"
      @click.stop="$emit('menu', node, $event)"
    >
      <MoreVertical :size="16" />
    </button>
    <div class="card-thumb">
      <img
        v-if="canThumb && !thumbFailed"
        :src="thumbUrl"
        class="thumb-real"
        loading="lazy"
        alt=""
        draggable="false"
        @error="thumbFailed = true"
      />
      <component :is="meta.icon" v-else :size="34" />
    </div>
    <div class="card-body">
      <component :is="meta.icon" :size="15" :class="'tint-' + meta.tint" />
      <span class="card-name">{{ node.name }}</span>
      <span v-if="node.starred" class="star-mark"><Star :size="12" /></span>
    </div>
    <div class="card-meta">{{ metaLine }}</div>
    <div v-if="files.searching" class="card-loc" :title="locLabel">
      <FolderClosed :size="11" />{{ locLabel }}
    </div>
  </div>
</template>
