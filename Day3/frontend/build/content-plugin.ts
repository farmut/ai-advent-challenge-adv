// Vite-плагин контента: превращает src/content/**/*.md в JS-модуль `export default { kind, ...frontmatter, html }`.
// Вся логика разбора — в content-core.ts, здесь только обвязка хуков Vite.
import { existsSync } from 'node:fs'
import { readFile } from 'node:fs/promises'
import path from 'node:path'
import type { Plugin } from 'vite'
import { detectContentKind, parseContentFile } from './content-core.ts'

/** Библиотеки, которые работают только при сборке и не должны оказаться в бандле. */
export const BUILD_ONLY_PACKAGES = ['markdown-it', 'gray-matter', 'sanitize-html'] as const

const BUILD_ONLY_MODULE = new RegExp(`[\\\\/]node_modules[\\\\/](${BUILD_ONLY_PACKAGES.join('|')})[\\\\/]`)

/** Найти в списке id модулей те, что принадлежат build-only библиотекам. */
export function findBuildOnlyModules(moduleIds: readonly string[]): string[] {
  return moduleIds.filter((id) => BUILD_ONLY_MODULE.test(id))
}

export function contentPlugin(): Plugin {
  let contentDir = ''
  let publicDir = ''

  return {
    name: 'day3:content',
    enforce: 'pre',

    configResolved(config) {
      contentDir = path.resolve(config.root, 'src/content')
      publicDir = config.publicDir
    },

    async load(id) {
      const file = id.split('?', 1)[0] ?? id
      if (!file.endsWith('.md')) return null

      const relative = path.relative(contentDir, file)
      if (relative.startsWith('..') || path.isAbsolute(relative)) {
        this.error(`[content] ${file}: Markdown разрешён только внутри src/content`)
      }

      const kind = detectContentKind(relative)
      const raw = await readFile(file, 'utf8')
      const module = parseContentFile(kind, relative.replaceAll('\\', '/'), raw, {
        assetExists: (publicPath) => existsSync(path.join(publicDir, publicPath)),
      })

      return {
        code: `export default ${JSON.stringify(module)}\n`,
        map: null,
        moduleType: 'js',
      }
    },

    generateBundle(_options, bundle) {
      for (const output of Object.values(bundle)) {
        if (output.type !== 'chunk') continue
        const leaked = findBuildOnlyModules(output.moduleIds)
        if (leaked.length > 0) {
          this.error(
            `[content] в чанк ${output.fileName} попали build-only модули: ${leaked.join(', ')}. ` +
              'Markdown должен разбираться только в build/content-plugin.ts',
          )
        }
      }
    },
  }
}

/** CSP для собранной статики. Только при build: dev-сервер Vite использует инлайн-скрипты и WebSocket HMR. */
export const CONTENT_SECURITY_POLICY = [
  "default-src 'self'",
  "script-src 'self'",
  // PrimeVue в styled mode вставляет <style> в рантайме, nonce на статике невозможен.
  "style-src 'self' 'unsafe-inline'",
  "img-src 'self'",
  "font-src 'self'",
  "connect-src 'none'",
  "form-action 'none'",
  "base-uri 'none'",
  "object-src 'none'",
  "frame-src 'none'",
  "worker-src 'none'",
  "manifest-src 'self'",
].join('; ')

export function cspPlugin(): Plugin {
  return {
    name: 'day3:csp',
    apply: 'build',
    transformIndexHtml: {
      order: 'post',
      handler() {
        return [
          {
            tag: 'meta',
            attrs: { 'http-equiv': 'Content-Security-Policy', content: CONTENT_SECURITY_POLICY },
            injectTo: 'head-prepend',
          },
        ]
      },
    },
  }
}
