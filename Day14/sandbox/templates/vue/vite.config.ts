import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

// The sandbox root filesystem and node_modules are read-only: every cache
// lives under /tmp, the only writable place.
export default defineConfig({
  plugins: [vue()],
  cacheDir: '/tmp/vite',
  test: {
    environment: 'jsdom',
    globals: true,
    include: ['src/**/*.{test,spec}.ts'],
  },
})
