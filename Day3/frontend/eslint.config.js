// ESLint flat config. Файл на JS, а не TS: ESLint 10 грузит eslint.config.ts только через jiti
// (не входит в стек) или с флагом unstable_native_nodejs_ts_config, а скрипт lint обязан быть ровно `eslint .`.
import { defineConfig, globalIgnores } from 'eslint/config'
import pluginVue from 'eslint-plugin-vue'
import tseslint from 'typescript-eslint'

const BUILD_ONLY_PACKAGES = ['markdown-it', 'gray-matter', 'sanitize-html']

/** Сетевые и storage-API, запрещённые в коде приложения (AGENTS.md: «Запрещено», «Форма»). */
const FORBIDDEN_GLOBALS = [
  { name: 'fetch', message: 'Сетевые запросы из приложения запрещены.' },
  { name: 'XMLHttpRequest', message: 'Сетевые запросы из приложения запрещены.' },
  { name: 'WebSocket', message: 'Сетевые запросы из приложения запрещены.' },
  { name: 'EventSource', message: 'Сетевые запросы из приложения запрещены.' },
  { name: 'localStorage', message: 'Сохранять данные пользователя запрещено.' },
  { name: 'sessionStorage', message: 'Сохранять данные пользователя запрещено.' },
  { name: 'indexedDB', message: 'Сохранять данные пользователя запрещено.' },
  { name: 'process', message: 'Node API недоступны в браузерном коде.' },
  { name: 'Buffer', message: 'Node API недоступны в браузерном коде.' },
  { name: 'require', message: 'Node API недоступны в браузерном коде.' },
]

const FORBIDDEN_MEMBERS = [
  ['window', 'fetch'],
  ['globalThis', 'fetch'],
  ['self', 'fetch'],
  ['navigator', 'sendBeacon'],
  ['window', 'localStorage'],
  ['window', 'sessionStorage'],
  ['window', 'indexedDB'],
  ['globalThis', 'localStorage'],
  ['globalThis', 'sessionStorage'],
  ['document', 'cookie'],
].map(([object, property]) => ({ object, property, message: `${object}.${property} запрещён в приложении.` }))

const coreRules = {
  'no-console': 'error',
  'no-debugger': 'error',
  'no-eval': 'error',
  'no-implied-eval': 'error',
  'no-new-func': 'error',
  'no-script-url': 'error',
  'no-var': 'error',
  'prefer-const': 'error',
  eqeqeq: ['error', 'always'],
  'no-restricted-syntax': [
    'error',
    { selector: "NewExpression[callee.name='XMLHttpRequest']", message: 'XHR запрещён.' },
    { selector: "NewExpression[callee.name='WebSocket']", message: 'WebSocket запрещён.' },
    { selector: "NewExpression[callee.name='EventSource']", message: 'EventSource запрещён.' },
    { selector: "MemberExpression[property.name='sendBeacon']", message: 'navigator.sendBeacon запрещён.' },
    { selector: "MemberExpression[property.name='cookie']", message: 'Cookies запрещены.' },
    { selector: "MemberExpression[property.name='innerHTML']", message: 'innerHTML запрещён: HTML приходит только из контент-модуля.' },
  ],
}

export default defineConfig([
  // src/components.d.ts генерирует unplugin-vue-components (в нём свои директивы отключения проверок).
  globalIgnores(['dist/', 'node_modules/', '.vite-ssg-temp/', 'test-results/', 'playwright-report/', 'src/components.d.ts']),

  ...tseslint.configs.recommended,
  ...pluginVue.configs['flat/recommended-error'],

  {
    files: ['**/*.{ts,vue,js}'],
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
    },
    rules: {
      ...coreRules,
      '@typescript-eslint/no-explicit-any': 'error',
      '@typescript-eslint/consistent-type-imports': 'error',
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
      '@typescript-eslint/ban-ts-comment': [
        'error',
        { 'ts-ignore': true, 'ts-nocheck': true, 'ts-expect-error': 'allow-with-description', minimumDescriptionLength: 10 },
      ],
    },
  },

  {
    files: ['**/*.vue'],
    languageOptions: {
      parserOptions: { parser: tseslint.parser, extraFileExtensions: ['.vue'] },
    },
    rules: {
      'vue/block-lang': ['error', { script: { lang: 'ts' } }],
      'vue/component-api-style': ['error', ['script-setup']],
      'vue/define-macros-order': 'error',
      'vue/no-v-html': 'error',
      'vue/require-typed-ref': 'error',
    },
  },

  // Код приложения (попадает в браузер).
  {
    files: ['src/**/*.{ts,vue}'],
    rules: {
      'no-restricted-globals': ['error', ...FORBIDDEN_GLOBALS],
      'no-restricted-properties': ['error', ...FORBIDDEN_MEMBERS],
      'no-restricted-imports': [
        'error',
        {
          paths: BUILD_ONLY_PACKAGES.map((name) => ({
            name,
            message: 'Только build-time: Markdown разбирается в build/content-plugin.ts.',
          })),
          patterns: [
            { group: ['node:*', 'fs', 'fs/*', 'path', 'url', 'child_process', 'os'], message: 'Node API запрещены в браузерном коде.' },
            { group: ['**/build/*', '**/build/**'], message: 'build/ работает только при сборке.' },
          ],
        },
      ],
    },
  },

  // Тесты безопасности используют XSS-строки ("javascript:alert(1)") как входные данные-фикстуры.
  // Это данные, а не исполняемый код; в src/ правило действует.
  {
    files: ['tests/**/*.ts'],
    rules: { 'no-script-url': 'off' },
  },

  // Единственное место, где разрешён v-html: поле html из контент-модуля.
  {
    files: ['src/components/ui/MarkdownContent.vue'],
    rules: { 'vue/no-v-html': 'off' },
  },

  // Секции и ui получают данные через props.
  {
    files: ['src/components/**/*.{ts,vue}'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          paths: BUILD_ONLY_PACKAGES.map((name) => ({ name, message: 'Только build-time.' })),
          patterns: [
            {
              // Индекс контента и сами .md. Типы (content/types) разрешены.
              regex: '(^|/)content(/index)?$|\\.md$',
              message: 'Секции и ui получают данные через props. Контент импортируют только страницы.',
            },
            { group: ['**/build/*', '**/build/**'], message: 'build/ работает только при сборке.' },
            { group: ['node:*', 'fs', 'path'], message: 'Node API запрещены в браузерном коде.' },
          ],
        },
      ],
    },
  },
])
