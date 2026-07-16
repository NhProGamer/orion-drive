import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { VitePWA } from 'vite-plugin-pwa'

// The build emits directly into the Go backend's embedded static directory so a
// single binary can serve the SPA.
export default defineConfig({
  plugins: [
    vue(),
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
        // Don't try to precache the (large) hashed source maps.
        globIgnores: ['**/*.map'],
      },
    }),
  ],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  build: {
    outDir: '../application/statics/dist',
    emptyOutDir: true,
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
