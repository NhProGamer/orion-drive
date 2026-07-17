import { onBeforeUnmount } from 'vue'

interface GestureOptions {
  /** Primary action: open the item (or toggle it while in selection mode). */
  onTap: () => void
  /** Long-press: enter selection mode on the pressed item. */
  onLongPress: () => void
}

const LONG_MS = 450
const MOVE_TOL = 10 // px of finger travel that turns a press into a scroll

/**
 * Touch gesture recogniser shared by file/folder items. It distinguishes a tap
 * from a long-press and from a scroll, and suppresses the browser's synthetic
 * click so the desktop mouse handlers never double-fire on touch. Taps landing
 * on interactive children (the ⋮ button, links) are ignored so they keep their
 * own behaviour.
 */
export function useItemGestures(opts: GestureOptions) {
  let timer: ReturnType<typeof setTimeout> | null = null
  let longFired = false
  let ignore = false
  let moved = false
  let startX = 0
  let startY = 0

  function clearTimer() {
    if (timer) {
      clearTimeout(timer)
      timer = null
    }
  }

  function onTouchStart(e: TouchEvent) {
    if ((e.target as HTMLElement).closest('button, a, input, [data-no-gesture]')) {
      ignore = true
      return
    }
    ignore = false
    longFired = false
    moved = false
    const t = e.touches[0]
    startX = t.clientX
    startY = t.clientY
    timer = setTimeout(() => {
      longFired = true
      timer = null
      try {
        navigator.vibrate?.(12)
      } catch {}
      opts.onLongPress()
    }, LONG_MS)
  }

  function onTouchMove(e: TouchEvent) {
    if (ignore || moved) return
    const t = e.touches[0]
    if (Math.abs(t.clientX - startX) > MOVE_TOL || Math.abs(t.clientY - startY) > MOVE_TOL) {
      moved = true
      clearTimer()
    }
  }

  function onTouchEnd(e: TouchEvent) {
    if (ignore) {
      ignore = false
      return
    }
    const wasLong = longFired
    clearTimer()
    if (moved) return // it was a scroll/drag, not a tap
    // Stop the synthetic click/dblclick (desktop select) and the iOS callout.
    e.preventDefault()
    if (!wasLong) opts.onTap()
  }

  function onTouchCancel() {
    ignore = false
    moved = false
    clearTimer()
  }

  onBeforeUnmount(clearTimer)

  return { onTouchStart, onTouchMove, onTouchEnd, onTouchCancel }
}
