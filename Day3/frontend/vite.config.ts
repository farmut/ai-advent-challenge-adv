import { fileURLToPath, URL } from 'node:url'
import { PrimeVueResolver } from '@primevue/auto-import-resolver'
import vue from '@vitejs/plugin-vue'
import Components from 'unplugin-vue-components/vite'
import { defineConfig } from 'vitest/config'
import { contentPlugin, cspPlugin } from './build/content-plugin.ts'

export default defineConfig({
  plugins: [
    contentPlugin(),
    vue(),
    Components({
      // Авто-импорт только компонентов PrimeVue; свои компоненты импортируются явно.
      dirs: [],
      resolvers: [PrimeVueResolver()],
      // Типы авто-импортированных компонентов для vue-tsc. Файл генерируется плагином и хранится в git.
      dts: 'src/components.d.ts',
    }),
    cspPlugin(),
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
    // Один экземпляр Vue и @unhead/vue: vite-ssg тянет свой @unhead/vue, мажоры должны совпадать.
    dedupe: ['vue', 'vue-router', '@unhead/vue'],
  },
  build: {
    // Полифил modulepreload использует fetch(); Chromium/Firefox/Safari поддерживают modulepreload нативно.
    modulePreload: { polyfill: false },
  },
  ssgOptions: {
    script: 'async',
    formatting: 'none',
    dirStyle: 'flat',
    // beasties (critical CSS) не установлен — явно выключаем.
    beastiesOptions: false,
  },
  test: {
    include: ['tests/unit/**/*.spec.ts'],
    // Окружение по умолчанию — node. Тесты компонентов объявляют jsdom докблоком @vitest-environment.
    environment: 'node',
    restoreMocks: true,
  },
})
