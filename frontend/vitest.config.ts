import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vitest/config'

// Unit-test config, kept separate from vite.config so the production build
// (vue-tsc + vite) never picks up test files or test-only tooling.
export default defineConfig({
  test: {
    environment: 'jsdom',
    globals: true,
    include: ['src/**/*.test.ts'],
  },
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
})
