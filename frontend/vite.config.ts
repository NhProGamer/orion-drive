import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { VitePWA } from 'vite-plugin-pwa'

// Entry points of the whiteboard: Excalidraw, the React runtime it needs, and
// our canvas host. Everything reachable *only* through these is whiteboard code.
const BOARD_ROOT = /(node_modules[\\/](@excalidraw|react|react-dom|scheduler)[\\/]|src[\\/]lib[\\/]board[\\/])/

/**
 * Splits the module graph into "the app" and "only the whiteboard needs this",
 * so the emitted chunks can be routed into separate directories.
 *
 * Excalidraw drags in a long tail of transitive dependencies (mermaid, katex,
 * cytoscape, d3, ...) that no other part of OrionDrive imports. Listing them by
 * hand would rot on the next upgrade, so they are derived: walk the graph from
 * the real entry points without ever stepping into a whiteboard root, and
 * whatever the walk never reaches belongs to the whiteboard alone.
 */
function boardGraph() {
  const boardOnly = new Set<string>()
  return {
    plugin: {
      name: 'orion:board-graph',
      buildEnd() {
        // @ts-expect-error — plugin context typing differs between bundlers.
        const ids: string[] = [...this.getModuleIds()]
        // @ts-expect-error — see above.
        const info = (id: string) => this.getModuleInfo(id)
        const reachable = new Set<string>()
        // Seed with the real entry points only. A module with no *static*
        // importer is not a root: Excalidraw's lazily-imported dependencies look
        // exactly like that, and seeding them would declare them shared.
        const queue = ids.filter((id) => info(id)?.isEntry)
        while (queue.length) {
          const id = queue.pop() as string
          if (reachable.has(id) || BOARD_ROOT.test(id)) continue
          reachable.add(id)
          const mod = info(id)
          queue.push(...(mod?.importedIds ?? []), ...(mod?.dynamicallyImportedIds ?? []))
        }
        boardOnly.clear()
        for (const id of ids) if (!reachable.has(id)) boardOnly.add(id)
      },
    },
    /** Whether every real module of a chunk is whiteboard-only. */
    isBoardChunk(moduleIds: string[] = []): boolean {
      // Bundler-internal modules ("\0rolldown/runtime.js") have no place in the
      // decision: they are shared by every chunk.
      const real = moduleIds.filter((id) => !id.startsWith('\0'))
      return real.length > 0 && real.every((id) => boardOnly.has(id))
    },
  }
}

const board = boardGraph()

// The build emits directly into the Go backend's embedded static directory so a
// single binary can serve the SPA.
export default defineConfig({
  plugins: [
    vue(),
    board.plugin,
    VitePWA({
      registerType: 'autoUpdate',
      // Static assets to precache in addition to the built JS/CSS.
      includeAssets: ['favicon.svg', 'favicon-32x32.png', 'apple-touch-icon.png'],
      manifest: {
        name: 'OrionDrive',
        short_name: 'OrionDrive',
        description: 'Your self-hosted drive — files, sharing and WebDAV.',
        lang: 'fr',
        theme_color: '#17151f',
        background_color: '#17151f',
        display: 'standalone',
        orientation: 'any',
        start_url: '/',
        scope: '/',
        categories: ['productivity', 'utilities'],
        icons: [
          { src: 'pwa-192x192.png', sizes: '192x192', type: 'image/png' },
          { src: 'pwa-512x512.png', sizes: '512x512', type: 'image/png' },
          { src: 'maskable-512x512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
        ],
      },
      workbox: {
        // SPA fallback for client-side routes, but never for the API or WebDAV.
        navigateFallback: '/index.html',
        navigateFallbackDenylist: [/^\/api/, /^\/dav/],
        globPatterns: ['**/*.{js,css,html,svg,png,webp,woff2}'],
        // Don't try to precache the (large) hashed source maps, nor the
        // Excalidraw fonts — they are only needed once a whiteboard is opened,
        // and precaching them would weigh down every install.
        globIgnores: ['**/*.map', 'excalidraw-assets/**', 'assets/board/**'],
      },
    }),
  ],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  build: {
    outDir: '../application/statics/dist',
    emptyOutDir: true,
    rolldownOptions: {
      output: {
        // React, Excalidraw and the whiteboard code are only reachable once a
        // user opens a board. Routing their chunks under assets/board/ lets the
        // service worker skip precaching several megabytes most visitors never
        // load (see workbox.globIgnores above), while leaving Excalidraw's own
        // lazy sub-chunks split as it intends them to be.
        chunkFileNames: (chunk) =>
          board.isBoardChunk(chunk.moduleIds) ? 'assets/board/[name]-[hash].js' : 'assets/[name]-[hash].js',
        assetFileNames: (asset) =>
          (asset.originalFileNames ?? []).some((f) => f.includes('@excalidraw'))
            ? 'assets/board/[name]-[hash][extname]'
            : 'assets/[name]-[hash][extname]',
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:5212',
        changeOrigin: true,
      },
    },
  },
})
