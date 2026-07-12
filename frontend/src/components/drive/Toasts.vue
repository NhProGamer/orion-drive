<script setup lang="ts">
import { computed } from 'vue'
import { Info, Trash2, RotateCcw, FolderPlus, Download, X } from 'lucide-vue-next'
import { useUiStore } from '@/stores/ui'
import type { LucideIcon } from 'lucide-vue-next'

const ui = useUiStore()

const ICONS: Record<string, LucideIcon> = {
  info: Info,
  trash: Trash2,
  restore: RotateCcw,
  'folder-plus': FolderPlus,
  download: Download,
  x: X,
}
const iconFor = computed(() => (name: string) => ICONS[name] || Info)
</script>

<template>
  <div class="toasts">
    <div v-for="t in ui.toasts" :key="t.id" class="toast">
      <component :is="iconFor(t.icon)" :size="15" />
      <span>{{ t.msg }}</span>
      <button v-if="t.action" class="toast-action" @click="ui.runAction(t)">{{ t.action.label }}</button>
    </div>
  </div>
</template>
