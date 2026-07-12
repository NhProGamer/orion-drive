<script setup lang="ts">
import { computed } from 'vue'
import { Star, MoreVertical } from 'lucide-vue-next'
import type { FileNode } from '@/lib/api'
import { kindFromName, fmtSize, fmtDate } from '@/lib/format'
import { metaFor } from '@/lib/icons'

const props = defineProps<{ node: FileNode; selected: boolean }>()
defineEmits<{
  select: [node: FileNode, ev: MouseEvent]
  open: [node: FileNode]
  menu: [node: FileNode, ev: MouseEvent]
}>()

const meta = computed(() => (props.node.type === 'folder' ? metaFor('folder') : metaFor(kindFromName(props.node.name))))
const sizeLabel = computed(() => (props.node.type === 'folder' ? '—' : fmtSize(props.node.size)))
const dateLabel = computed(() => fmtDate(props.node.modified))
</script>

<template>
  <div
    class="list-row"
    :class="{ selected }"
    @click.stop="$emit('select', node, $event)"
    @dblclick="$emit('open', node)"
    @contextmenu="$emit('menu', node, $event)"
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
