<script setup lang="ts">
import { computed } from 'vue'
import { Folder, Star } from 'lucide-vue-next'
import type { FileNode } from '@/lib/api'
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
</script>

<template>
  <div
    class="folder-chip"
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
    <Folder :size="18" />
    <span class="name">{{ node.name }}</span>
    <span v-if="node.starred" class="star-mark"><Star :size="12" /></span>
  </div>
</template>
