<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Star } from 'lucide-vue-next'
import { api, type FileNode } from '@/lib/api'
import { kindFromName, fmtSize, fmtDate } from '@/lib/format'
import { metaFor } from '@/lib/icons'

const props = defineProps<{ node: FileNode; selected: boolean }>()
defineEmits<{
  select: [node: FileNode, ev: MouseEvent]
  open: [node: FileNode]
  menu: [node: FileNode, ev: MouseEvent]
}>()

const kind = computed(() => kindFromName(props.node.name))
const meta = computed(() => metaFor(kind.value))
const canThumb = computed(() => ['image', 'video', 'audio'].includes(kind.value))
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
    :class="{ selected }"
    @click.stop="$emit('select', node, $event)"
    @dblclick="$emit('open', node)"
    @contextmenu.stop="$emit('menu', node, $event)"
  >
    <div class="card-thumb">
      <img
        v-if="canThumb && !thumbFailed"
        :src="thumbUrl"
        class="thumb-real"
        loading="lazy"
        alt=""
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
