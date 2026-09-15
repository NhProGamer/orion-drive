import { onUnmounted, ref, type Ref } from 'vue'

/**
 * Drives the browser's Fullscreen API for one element.
 *
 * The state is read back from `fullscreenchange` rather than set optimistically:
 * the user can leave fullscreen with Escape or the browser's own control, and a
 * locally-tracked flag would then lie about it.
 */
export function useFullscreen(target: Ref<HTMLElement | null>) {
  const active = ref(false)

  const sync = () => {
    active.value = document.fullscreenElement !== null
  }
  document.addEventListener('fullscreenchange', sync)
  onUnmounted(() => {
    document.removeEventListener('fullscreenchange', sync)
    // Leaving the view while still fullscreen would strand the browser there.
    if (document.fullscreenElement) void document.exitFullscreen().catch(() => {})
  })

  /** Whether the browser offers fullscreen at all (iOS Safari on iPhone does not). */
  const supported = typeof document.documentElement.requestFullscreen === 'function'

  async function toggle() {
    try {
      if (document.fullscreenElement) {
        await document.exitFullscreen()
      } else if (target.value) {
        await target.value.requestFullscreen()
      }
    } catch {
      // A refused request (permissions policy, not user-initiated) leaves the
      // view exactly as it was; the button simply does nothing.
    }
    sync()
  }

  return { active, supported, toggle }
}
