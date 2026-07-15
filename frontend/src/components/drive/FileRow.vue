<script setup lang="ts">
import { computed } from 'vue'
import { Star, MoreVertical } from 'lucide-vue-next'
import type { FileNode } from '@/lib/api'
import { kindFromName, fmtSize, fmtDate } from '@/lib/format'
import { metaFor } from '@/lib/icons'
import { useFilesStore } from '@/stores/files'

const props = defineProps<{ node: FileNode; selected: boolean }>()
defineEmits<{
  select: [node: FileNode, ev: MouseEvent]
  open: [node: FileNode]
  menu: [node: FileNode, ev: MouseEvent]
}>()

const files = useFilesStore()
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

const meta = computed(() => (props.node.type === 'folder' ? metaFor('folder') : metaFor(kindFromName(props.node.name))))
const sizeLabel = computed(() => (props.node.type === 'folder' ? '—' : fmtSize(props.node.size)))
const dateLabel = computed(() => fmtDate(props.node.modified))
</script>

<template>
  <div
    class="list-row"
    :class="{ selected, 'drop-target': isDropTarget }"
    :draggable="files.dndEnabled"
    @click.stop="$emit('select', node, $event)"
    @dblclick="$emit('open', node)"
    @contextmenu.stop="$emit('menu', node, $event)"
    @dragstart="onDragStart"
    @dragend="files.endDrag()"
    @dragover="onDragOver"
    @dragleave="onDragLeave"
    @drop.stop="onDrop"
  >
    <div class="row-name">
      <component :is="meta.icon" :size="16" :class="'tint-' + meta.tint" />
      <span class="name">{{ node.name }}</span>
      <span v-if="node.starred" class="star-mark"><Star :size="11" /></span>
    </div>
    <span class="row-cell">{{ node.owner }}</span>
    <span class="row-cell mono">{{ dateLabel }}</span>
    <span class="row-cell mono">{{ sizeLabel }}</span>
    <button class="icon-btn" title="Actions" @click.stop="$emit('menu', node, $event)">
      <MoreVertical :size="15" />
    </button>
  </div>
</template>
