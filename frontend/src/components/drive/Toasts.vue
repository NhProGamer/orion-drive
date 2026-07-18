<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useUiStore } from '@/stores/ui'
import { notifIcon } from '@/lib/notifIcons'

const ui = useUiStore()

// Cap how many toasts float at once so they never run off the screen; fewer on
// short viewports and touch devices. Overflow still lives in the bell dropdown.
const winH = ref(window.innerHeight)
const onResize = () => (winH.value = window.innerHeight)
onMounted(() => window.addEventListener('resize', onResize))
onUnmounted(() => window.removeEventListener('resize', onResize))

const maxVisible = computed(() => {
  const fit = Math.floor((winH.value - 120) / 64)
  return Math.max(1, Math.min(ui.coarse ? 3 : 5, fit))
})
const visible = computed(() => ui.toasts.slice(-maxVisible.value))
</script>

<template>
  <div class="toasts">
    <div v-for="t in visible" :key="t.id" class="toast">
      <component :is="notifIcon(t.icon)" :size="15" />
      <span>{{ t.msg }}</span>
      <button v-if="t.action" class="toast-action" @click="ui.runAction(t)">{{ t.action.label }}</button>
    </div>
  </div>
</template>
