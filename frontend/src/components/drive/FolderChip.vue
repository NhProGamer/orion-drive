<script setup lang="ts">
import { Folder, Star } from 'lucide-vue-next'
import type { FileNode } from '@/lib/api'

defineProps<{ node: FileNode; selected: boolean }>()
defineEmits<{
  select: [node: FileNode, ev: MouseEvent]
  open: [node: FileNode]
  menu: [node: FileNode, ev: MouseEvent]
}>()
</script>

<template>
  <div
    class="folder-chip"
    :class="{ selected }"
    @click.stop="$emit('select', node, $event)"
    @dblclick="$emit('open', node)"
    @contextmenu="$emit('menu', node, $event)"
  >
    <Folder :size="18" />
    <span class="name">{{ node.name }}</span>
    <span v-if="node.starred" class="star-mark"><Star :size="12" /></span>
  </div>
</template>
