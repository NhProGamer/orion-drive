<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Star } from 'lucide-vue-next'
import { api, type FileNode } from '@/lib/api'
import { kindFromName, fmtSize, fmtDate, canThumbnail } from '@/lib/format'
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
</script>

<template>
  <div
    class="card"
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
  </div>
</template>
