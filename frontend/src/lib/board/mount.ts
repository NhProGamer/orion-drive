/**
 * Bridges the Vue application to the React-only Excalidraw canvas.
 *
 * Everything here is loaded on demand: React, Excalidraw and its stylesheet are
 * behind dynamic imports, so a user who never opens a whiteboard never
 * downloads them.
 */
import type { BoardCanvasProps } from './BoardCanvas'

/** A mounted canvas: props can be pushed in, and it must be unmounted. */
export interface BoardHandle {
  update: (props: BoardCanvasProps) => void
  unmount: () => void
}

/**
 * Where Excalidraw loads its hand-drawn fonts from. They are served by
 * OrionDrive itself (see scripts/copy-excalidraw-assets.mjs): the default is a
 * public CDN, which a deployment with a strict Content-Security-Policy blocks —
 * and shipping the fonts locally keeps a self-hosted drive self-hosted.
 */
const ASSET_PATH = '/excalidraw-assets/'

export async function mountBoard(target: HTMLElement, props: BoardCanvasProps): Promise<BoardHandle> {
  ;(window as unknown as { EXCALIDRAW_ASSET_PATH: string }).EXCALIDRAW_ASSET_PATH = ASSET_PATH

  const [{ createElement }, { createRoot }, { default: BoardCanvas }] = await Promise.all([
    import('react'),
    import('react-dom/client'),
    import('./BoardCanvas'),
    import('@excalidraw/excalidraw/index.css'),
  ])

  const root = createRoot(target)
  root.render(createElement(BoardCanvas, props))

  return {
    update: (next) => root.render(createElement(BoardCanvas, next)),
    // Unmounting is deferred: React refuses to tear a root down from inside its
    // own render/commit, and a Vue `onUnmounted` can land in that window.
    unmount: () => window.setTimeout(() => root.unmount(), 0),
  }
}
