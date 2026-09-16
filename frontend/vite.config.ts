import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { VitePWA } from 'vite-plugin-pwa'

// Entry points of the heavy in-browser editors: Excalidraw with the React
// runtime it needs, Milkdown with its CRDT bindings, and our own hosts for
// both. Everything reachable *only* through these is editor code.
const EDITOR_ROOT =
  /(node_modules[\\/](@excalidraw|react|react-dom|scheduler|@milkdown|yjs|y-protocols|lib0)[\\/]|src[\\/]lib[\\/](board|doc)[\\/])/

/**
 * Splits the module graph into "the app" and "only an editor needs this", so
 * the emitted chunks can be routed into separate directories.
 *
 * These editors drag in a long tail of transitive dependencies (mermaid, katex
 * and cytoscape behind Excalidraw; ProseMirror and CodeMirror behind Milkdown)
 * that no other part of OrionDrive imports. Listing them by hand would rot on
 * the next upgrade, so they are derived: walk the graph from the real entry
 * points without ever stepping into an editor root, and whatever the walk never
 * reaches belongs to the editors alone.
 */
function editorGraph() {
  const editorOnly = new Set<string>()
  return {
    plugin: {
      name: 'orion:editor-graph',
      buildEnd() {
        // @ts-expect-error — plugin context typing differs between bundlers.
        const ids: string[] = [...this.getModuleIds()]
        // @ts-expect-error — see above.
        const info = (id: string) => this.getModuleInfo(id)
        const reachable = new Set<string>()
        // Seed with the real entry points only. A module with no *static*
        // importer is not a root: an editor's lazily-imported dependencies look
        // exactly like that, and seeding them would declare them shared.
        const queue = ids.filter((id) => info(id)?.isEntry)
        while (queue.length) {
          const id = queue.pop() as string
          if (reachable.has(id) || EDITOR_ROOT.test(id)) continue
          reachable.add(id)
          const mod = info(id)
          queue.push(...(mod?.importedIds ?? []), ...(mod?.dynamicallyImportedIds ?? []))
        }
        editorOnly.clear()
        for (const id of ids) if (!reachable.has(id)) editorOnly.add(id)
      },
    },
    /** Whether every real module of a chunk belongs to the editors alone. */
    isEditorChunk(moduleIds: string[] = []): boolean {
      // Bundler-internal modules ("\0rolldown/runtime.js") have no place in the
      // decision: they are shared by every chunk.
      const real = moduleIds.filter((id) => !id.startsWith('\0'))
      return real.length > 0 && real.every((id) => editorOnly.has(id))
    },
  }
}

const editors = editorGraph()

// The build emits directly into the Go backend's embedded static directory so a
// single binary can serve the SPA.
export default defineConfig({
  plugins: [
    vue(),
    editors.plugin,
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
        // editors and the Excalidraw fonts — they are only needed once a
        // whiteboard or a document is opened, and precaching them would weigh
        // down every install.
        globIgnores: ['**/*.map', 'excalidraw-assets/**', 'assets/editors/**'],
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
        // The editors are only reachable once a user opens a whiteboard or a
        // Markdown file. Routing their chunks under assets/editors/ lets the
        // service worker skip precaching several megabytes most visitors never
        // load (see workbox.globIgnores above), while leaving each library's own
        // lazy sub-chunks split as it intends them to be.
        chunkFileNames: (chunk) =>
          editors.isEditorChunk(chunk.moduleIds) ? 'assets/editors/[name]-[hash].js' : 'assets/[name]-[hash].js',
        // Stylesheets are attributed to the module that imported them, so the
        // editors' own hosts count as editor origins too — otherwise Milkdown's
        // stylesheet lands in the precached bundle.
        assetFileNames: (asset) =>
          (asset.originalFileNames ?? []).some((f) =>
            /@excalidraw|@milkdown|codemirror|src[\\/]lib[\\/](board|doc)[\\/]/.test(f),
          )
            ? 'assets/editors/[name]-[hash][extname]'
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
