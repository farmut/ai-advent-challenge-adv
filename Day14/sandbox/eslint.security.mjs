// Security lint profile for the sandbox (eslint -c /opt/sandbox/eslint.security.mjs).
// Only security rules: eslint-plugin-security's recommended set plus
// vue/no-v-html as an error. Style rules are deliberately absent — this run
// feeds security findings, not code review nits.
import security from 'eslint-plugin-security'
import vue from 'eslint-plugin-vue'
import vueParser from 'vue-eslint-parser'
import tseslint from 'typescript-eslint'

const rules = {
  ...security.configs.recommended.rules,
  'vue/no-v-html': 'error',
}

export default [
  { ignores: ['node_modules/**', 'dist/**', 'coverage/**'] },
  {
    files: ['**/*.{js,mjs,cjs}'],
    plugins: { security, vue },
    rules: security.configs.recommended.rules,
  },
  {
    files: ['**/*.{ts,mts,cts}'],
    languageOptions: { parser: tseslint.parser, sourceType: 'module' },
    plugins: { security, vue },
    rules: security.configs.recommended.rules,
  },
  {
    files: ['**/*.vue'],
    languageOptions: {
      parser: vueParser,
      parserOptions: { parser: tseslint.parser, sourceType: 'module', extraFileExtensions: ['.vue'] },
    },
    plugins: { security, vue },
    rules,
  },
]
