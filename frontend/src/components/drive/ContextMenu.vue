<script setup lang="ts">
import type { LucideIcon } from 'lucide-vue-next'

export interface MenuItem {
  id?: string
  label?: string
  icon?: LucideIcon
  danger?: boolean
  sep?: boolean
}

defineProps<{ items: MenuItem[]; x: number; y: number }>()
defineEmits<{ action: [id: string] }>()
</script>

<template>
  <div class="menu" :style="{ left: x + 'px', top: y + 'px' }" @click.stop @contextmenu.prevent>
    <template v-for="(it, i) in items" :key="i">
      <div v-if="it.sep" class="menu-sep"></div>
      <button v-else class="menu-item" :class="{ danger: it.danger }" @click="$emit('action', it.id!)">
        <component :is="it.icon" :size="15" />{{ it.label }}
      </button>
    </template>
  </div>
</template>
