<script setup lang="ts">
import { computed } from 'vue'
import { Star } from 'lucide-vue-next'
import type { FileNode } from '@/lib/api'
import { kindFromName, ext, fmtSize, fmtDate } from '@/lib/format'
import { metaFor } from '@/lib/icons'

const props = defineProps<{ node: FileNode; selected: boolean }>()
defineEmits<{
  select: [node: FileNode, ev: MouseEvent]
  open: [node: FileNode]
  menu: [node: FileNode, ev: MouseEvent]
}>()

const kind = computed(() => kindFromName(props.node.name))
const meta = computed(() => metaFor(kind.value))
const isImage = computed(() => kind.value === 'image')
const extLabel = computed(() => ext(props.node.name) || 'IMG')
const metaLine = computed(() => {
  const size = props.node.size ? fmtSize(props.node.size) + ' · ' : ''
  return size + fmtDate(props.node.modified)
})
</script>

<template>
  <div
    class="card"
    :class="{ selected }"
    @click.stop="$emit('select', node, $event)"
    @dblclick="$emit('open', node)"
    @contextmenu="$emit('menu', node, $event)"
  >
    <div class="card-thumb">
      <div v-if="isImage" class="thumb-img"><span>{{ extLabel }}</span></div>
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
