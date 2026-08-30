<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Folder, Star, MoreVertical, Check } from 'lucide-vue-next'
import type { FileNode } from '@/lib/api'
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

// A plain desktop click previews the folder in the side details panel;
// double-click navigates into it. Selection is via the marquee, Ctrl/Cmd/Shift+
// click, or checkbox mode — a plain click never selects. On touch the tap gesture
// opens, so the synthetic click is ignored.
function onClick(ev: MouseEvent) {
  if (files.selectionMode || ev.ctrlKey || ev.metaKey || ev.shiftKey) {
    emit('select', props.node, ev)
    return
  }
  if (ui.coarse) return
  emit('select', props.node, ev)
}
const ui = useUiStore()
const gestures = useItemGestures({
  onTap: () => (files.selectionMode ? files.toggleSel(props.node) : emit('open', props.node)),
  onLongPress: () => files.enterSelection(props.node),
})
const isDropTarget = computed(() => files.dragOverId === props.node.id)

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
</script>

<template>
  <div
    class="folder-chip"
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
    <Folder :size="18" />
    <span class="name">{{ node.name }}</span>
    <span v-if="node.starred" class="star-mark"><Star :size="12" /></span>
    <button
      v-if="ui.coarse && !files.selectionMode"
      class="chip-menu-btn"
      :aria-label="t('common.actions')"
      @click.stop="$emit('menu', node, $event)"
    >
      <MoreVertical :size="15" />
    </button>
  </div>
</template>
