<script setup lang="ts">
/**
 * Collaborative whiteboard. Hosts the Excalidraw canvas — a React component —
 * inside this Vue view, and keeps it wired to the relay socket for as long as
 * the panel is open.
 */
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useUiStore } from '@/stores/ui'
import type { BoardHandle } from '@/lib/board/mount'
import type { BoardCanvasProps } from '@/lib/board/BoardCanvas'

const props = defineProps<{
  /** WebSocket URL of the board's relay session. */
  url: string
  /** Opens the board in view mode even when the relay would allow drawing. */
  viewOnly?: boolean
}>()

const { t } = useI18n()
const ui = useUiStore()

const host = ref<HTMLElement | null>(null)
const failed = ref(false)
let handle: BoardHandle | null = null

function canvasProps(): BoardCanvasProps {
  return {
    url: props.url,
    theme: ui.theme,
    viewOnly: props.viewOnly,
    labels: {
      connecting: t('board.connecting'),
      reconnecting: t('board.reconnecting'),
      offline: t('board.offline'),
      saved: t('board.saved'),
      readOnly: t('board.readOnly'),
      aloneOnBoard: t('board.alone'),
      // Interpolated inside the React component, which has no vue-i18n.
      peers: t('board.peers', { count: '{count}' }),
    },
  }
}

onMounted(async () => {
  if (!host.value) return
  try {
    const { mountBoard } = await import('@/lib/board/mount')
    handle = await mountBoard(host.value, canvasProps())
  } catch {
    failed.value = true
  }
})

// The canvas is re-rendered (not remounted) when the theme or the session URL
// changes, so switching theme mid-drawing keeps the scene and the connection.
watch(
  () => [props.url, props.viewOnly, ui.theme],
  () => handle?.update(canvasProps()),
)

onUnmounted(() => {
  handle?.unmount()
  handle = null
})
</script>

<template>
  <div class="ov-board">
    <div v-if="failed" class="ov-empty">{{ t('board.loadFailed') }}</div>
    <div v-show="!failed" ref="host" class="ov-board-host"></div>
  </div>
</template>
