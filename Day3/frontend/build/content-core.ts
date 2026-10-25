// Ядро контент-пайплайна: чистые функции без зависимости от Vite.
// *.md → gray-matter (только YAML) → строгая валидация frontmatter → markdown-it (html: false) → sanitize-html.
// Выполняется только в Node на этапе сборки. В браузерный бандл не попадает (см. content-plugin.ts).
import matter from 'gray-matter'
import markdownIt from 'markdown-it'
import type { MarkdownIt, Token } from 'markdown-it'
import sanitizeHtml from 'sanitize-html'
import type {
  ContentKind,
  ContentModule,
  ExperienceItem,
  Profile,
  ProfileLink,
  Project,
  SkillGroup,
} from '../src/content/types.ts'

/** Ошибка контента: всегда содержит имя файла и поле, чтобы сборка падала с понятным сообщением. */
export class ContentError extends Error {
  readonly fileName: string
  readonly field: string

  constructor(fileName: string, field: string, reason: string) {
    super(`[content] ${fileName}: поле "${field}": ${reason}`)
    this.name = 'ContentError'
    this.fileName = fileName
    this.field = field
  }
}

export interface ParseOptions {
  /** Проверка, что файл существует в public/. Путь вида "/images/x.svg". */
  assetExists: (publicPath: string) => boolean
}

type Data = Record<string, unknown>

export const SLUG_PATTERN = /^[a-z0-9-]+$/
const YEAR_MONTH_PATTERN = /^\d{4}-(0[1-9]|1[0-2])$/
const COVER_PATTERN = /^\/[a-z0-9][a-z0-9/_-]*\.(svg|png|jpe?g|webp|avif)$/
const ALLOWED_HOST = 'example.com'

/** Белый список тегов из AGENTS.md. h1 и картинки отсекаются раньше, на уровне токенов markdown-it. */
export const ALLOWED_TAGS = [
  'p',
  'h2',
  'h3',
  'h4',
  'ul',
  'ol',
  'li',
  'a',
  'code',
  'pre',
  'strong',
  'em',
  'blockquote',
] as const

// ---------------------------------------------------------------------------
// Определение вида контента по пути
// ---------------------------------------------------------------------------

/**
 * Вид контента по пути относительно src/content (разделители — "/").
 * profile.md → profile, experience/<id>.md → experience, projects/<slug>.md → project.
 */
export function detectContentKind(relativePath: string): ContentKind {
  const normalized = relativePath.replaceAll('\\', '/')
  if (normalized === 'profile.md') return 'profile'
  if (/^experience\/[^/]+\.md$/.test(normalized)) return 'experience'
  if (/^projects\/[^/]+\.md$/.test(normalized)) return 'project'
  throw new ContentError(
    relativePath,
    'path',
    'неизвестное расположение. Допустимо: profile.md, experience/<id>.md, projects/<slug>.md',
  )
}

/** Имя файла без расширения .md. */
export function baseName(fileName: string): string {
  const last = fileName.replaceAll('\\', '/').split('/').pop() ?? fileName
  return last.endsWith('.md') ? last.slice(0, -3) : last
}

// ---------------------------------------------------------------------------
// Frontmatter
// ---------------------------------------------------------------------------

function refuseEngine(language: string): () => object {
  return () => {
    throw new Error(`frontmatter на языке "${language}" запрещён, допустим только YAML`)
  }
}

function isPlainObject(value: unknown): value is Data {
  return typeof value === 'object' && value !== null && !Array.isArray(value) && !(value instanceof Date)
}

/**
 * Отделяет YAML-frontmatter от тела.
 * Первая строка должна быть ровно "---": так отсекаются "---js" / "---coffee",
 * которые gray-matter по умолчанию выполняет через eval. Движки JS дополнительно заменены на отказ.
 */
export function splitFrontmatter(fileName: string, raw: string): { data: Data; body: string } {
  const source = raw.startsWith('﻿') ? raw.slice(1) : raw
  const firstLine = source.split(/\r?\n/, 1)[0]
  if (firstLine !== '---') {
    throw new ContentError(fileName, 'frontmatter', 'файл должен начинаться с YAML-блока: первая строка ровно "---"')
  }

  let parsed: { data: unknown; content: string }
  try {
    parsed = matter(source, {
      language: 'yaml',
      excerpt: false,
      engines: {
        javascript: refuseEngine('javascript'),
        js: refuseEngine('js'),
        coffee: refuseEngine('coffee'),
        coffeescript: refuseEngine('coffeescript'),
        cson: refuseEngine('cson'),
      },
    })
  } catch (error) {
    const reason = error instanceof Error ? error.message : String(error)
    throw new ContentError(fileName, 'frontmatter', `не удалось разобрать YAML: ${reason}`)
  }

  if (!isPlainObject(parsed.data)) {
    throw new ContentError(fileName, 'frontmatter', 'ожидается объект ключ: значение')
  }
  return { data: parsed.data, body: parsed.content }
}

function checkKnownKeys(fileName: string, data: Data, allowed: readonly string[]): void {
  for (const key of Object.keys(data)) {
    if (!allowed.includes(key)) {
      throw new ContentError(fileName, key, `неизвестное поле. Допустимые: ${allowed.join(', ')}`)
    }
  }
}

function hasField(data: Data, field: string): boolean {
  return Object.prototype.hasOwnProperty.call(data, field)
}

function requireField(fileName: string, data: Data, field: string): unknown {
  if (!hasField(data, field) || data[field] === undefined) {
    throw new ContentError(fileName, field, 'обязательное поле отсутствует')
  }
  return data[field]
}

function readString(fileName: string, data: Data, field: string): string {
  const value = requireField(fileName, data, field)
  return asNonEmptyString(fileName, field, value)
}

function asNonEmptyString(fileName: string, field: string, value: unknown): string {
  if (typeof value !== 'string') {
    throw new ContentError(fileName, field, `ожидается строка, получено: ${describe(value)}`)
  }
  const trimmed = value.trim()
  if (trimmed.length === 0) {
    throw new ContentError(fileName, field, 'строка не должна быть пустой')
  }
  return trimmed
}

/** Поле обязано присутствовать, значение — строка или явный null. */
function readNullableString(fileName: string, data: Data, field: string): string | null {
  const value = requireField(fileName, data, field)
  if (value === null) return null
  return asNonEmptyString(fileName, field, value)
}

function readStringArray(fileName: string, data: Data, field: string): string[] {
  const value = requireField(fileName, data, field)
  return asStringArray(fileName, field, value)
}

function asStringArray(fileName: string, field: string, value: unknown): string[] {
  if (!Array.isArray(value)) {
    throw new ContentError(fileName, field, `ожидается список строк, получено: ${describe(value)}`)
  }
  return value.map((item: unknown, index) => asNonEmptyString(fileName, `${field}[${index}]`, item))
}

function readInteger(fileName: string, data: Data, field: string, min: number, max: number): number {
  const value = requireField(fileName, data, field)
  if (typeof value !== 'number' || !Number.isInteger(value)) {
    throw new ContentError(fileName, field, `ожидается целое число, получено: ${describe(value)}`)
  }
  if (value < min || value > max) {
    throw new ContentError(fileName, field, `ожидается число от ${min} до ${max}, получено: ${value}`)
  }
  return value
}

function readYearMonth(fileName: string, field: string, value: unknown): string {
  if (typeof value !== 'string' || !YEAR_MONTH_PATTERN.test(value)) {
    throw new ContentError(fileName, field, `ожидается строка в формате YYYY-MM, получено: ${describe(value)}`)
  }
  return value
}

function describe(value: unknown): string {
  if (value === null) return 'null'
  if (Array.isArray(value)) return 'список'
  if (value instanceof Date) return 'дата (значение нужно взять в кавычки или указать YYYY-MM)'
  if (typeof value === 'string') return `строка "${value}"`
  return typeof value
}

// ---------------------------------------------------------------------------
// URL
// ---------------------------------------------------------------------------

function isExampleHost(hostname: string): boolean {
  return hostname === ALLOWED_HOST || hostname.endsWith(`.${ALLOWED_HOST}`)
}

/**
 * Проверка URL из контента. Разрешено только:
 * - https://example.com/... (и поддомены example.com), без логина и пароля;
 * - mailto:<адрес>@example.com;
 * - "#" или "#якорь", если allowHash.
 * Возвращает нормализованную строку или бросает ContentError.
 */
export function assertSafeUrl(
  fileName: string,
  field: string,
  value: string,
  options: { allowHash: boolean },
): string {
  const trimmed = value.trim()
  if (trimmed.startsWith('#')) {
    if (options.allowHash && /^#[A-Za-z0-9_-]*$/.test(trimmed)) return trimmed
    throw new ContentError(fileName, field, `недопустимая ссылка-якорь: ${value}`)
  }

  let url: URL
  try {
    url = new URL(trimmed)
  } catch {
    throw new ContentError(fileName, field, `некорректный URL: ${value}`)
  }

  if (url.protocol === 'https:') {
    if (url.username !== '' || url.password !== '') {
      throw new ContentError(fileName, field, 'URL не должен содержать логин или пароль')
    }
    if (!isExampleHost(url.hostname)) {
      throw new ContentError(fileName, field, `разрешены только ссылки на ${ALLOWED_HOST}, получено: ${url.hostname}`)
    }
    return url.href
  }

  if (url.protocol === 'mailto:') {
    const address = decodeURIComponent(url.pathname)
    const match = /^[^\s@,]+@([^\s@,?]+)$/.exec(address)
    if (match === null || match[1] === undefined || !isExampleHost(match[1].toLowerCase()) || url.search !== '') {
      throw new ContentError(fileName, field, `mailto разрешён только на один адрес @${ALLOWED_HOST} без параметров`)
    }
    return `mailto:${address}`
  }

  throw new ContentError(fileName, field, `недопустимая схема ${url.protocol} — разрешены https: и mailto:`)
}

// ---------------------------------------------------------------------------
// Markdown → HTML
// ---------------------------------------------------------------------------

/** Схемы, которые markdown-it вообще превращает в ссылку. Остальное (javascript:, data: …) остаётся текстом. */
function isLinkableUrl(url: string): boolean {
  const value = url.trim().toLowerCase()
  return value.startsWith('https://') || value.startsWith('mailto:') || value.startsWith('#')
}

function createMarkdown(): MarkdownIt {
  const md = markdownIt('default', { html: false, linkify: false, typographer: false, breaks: false })
  md.validateLink = isLinkableUrl
  return md
}

const markdown = createMarkdown()

function checkTokens(fileName: string, tokens: Token[]): void {
  for (const token of tokens) {
    if (token.type === 'heading_open' || token.type === 'heading_close') {
      if (token.tag === 'h1') {
        throw new ContentError(
          fileName,
          'body',
          'заголовок первого уровня (#) запрещён: h1 на странице один и задаётся шаблоном. Начинайте с ##',
        )
      }
      if (token.tag === 'h5' || token.tag === 'h6') token.tag = 'h4'
    }
    if (token.type === 'image') {
      throw new ContentError(fileName, 'body', 'изображения в тексте запрещены, используйте поле cover')
    }
    if (token.type === 'link_open') {
      const href = token.attrGet('href')
      if (typeof href === 'string') assertSafeUrl(fileName, 'body', href, { allowHash: true })
    }
    if (token.children !== null) checkTokens(fileName, token.children)
  }
}

/** Markdown → очищенный HTML. h1 и картинки — ошибка, h5/h6 → h4, ссылки только на разрешённые адреса. */
export function renderMarkdown(fileName: string, body: string): string {
  const tokens = markdown.parse(body, {})
  checkTokens(fileName, tokens)
  const html = markdown.renderer.render(tokens, markdown.options, {})
  return sanitizeContentHtml(html)
}

function isAllowedHref(href: string): boolean {
  const value = href.trim()
  if (/^#[A-Za-z0-9_-]*$/.test(value)) return true
  try {
    const url = new URL(value)
    return url.protocol === 'https:' || url.protocol === 'mailto:'
  } catch {
    return false
  }
}

/** Второй рубеж после markdown-it: белый список тегов и жёсткий фильтр href. */
export function sanitizeContentHtml(html: string): string {
  return sanitizeHtml(html, {
    allowedTags: [...ALLOWED_TAGS],
    allowedAttributes: { a: ['href', 'rel'] },
    allowedSchemes: ['https', 'mailto'],
    allowedSchemesByTag: {},
    allowedSchemesAppliedToAttributes: ['href'],
    allowProtocolRelative: false,
    disallowedTagsMode: 'discard',
    transformTags: {
      a: (_tagName, attribs) => {
        const href = attribs.href
        const result: Record<string, string> = {}
        if (typeof href === 'string' && isAllowedHref(href)) {
          result.href = href
          if (href.trim().toLowerCase().startsWith('https:')) result.rel = 'noopener noreferrer'
        }
        return { tagName: 'a', attribs: result }
      },
    },
  })
}

// ---------------------------------------------------------------------------
// Валидаторы по видам
// ---------------------------------------------------------------------------

const PROFILE_KEYS = ['name', 'role', 'location', 'fictional', 'skills', 'links'] as const
const EXPERIENCE_KEYS = ['company', 'position', 'start', 'end'] as const
const PROJECT_KEYS = ['title', 'summary', 'tags', 'year', 'cover', 'coverAlt', 'repo', 'demo', 'order'] as const

export function validateProfile(fileName: string, data: Data, html: string): Profile {
  checkKnownKeys(fileName, data, PROFILE_KEYS)

  if (requireField(fileName, data, 'fictional') !== true) {
    throw new ContentError(fileName, 'fictional', 'персона должна быть вымышленной: fictional: true')
  }

  const skillsRaw = requireField(fileName, data, 'skills')
  if (!Array.isArray(skillsRaw)) {
    throw new ContentError(fileName, 'skills', `ожидается список групп, получено: ${describe(skillsRaw)}`)
  }
  const skills: SkillGroup[] = skillsRaw.map((entry: unknown, index) => {
    const field = `skills[${index}]`
    if (!isPlainObject(entry)) throw new ContentError(fileName, field, 'ожидается объект { group, items }')
    checkKnownKeys(fileName, entry, ['group', 'items'])
    return {
      group: readString(fileName, entry, 'group'),
      items: asStringArray(fileName, `${field}.items`, requireField(fileName, entry, 'items')),
    }
  })

  const linksRaw = requireField(fileName, data, 'links')
  if (!Array.isArray(linksRaw)) {
    throw new ContentError(fileName, 'links', `ожидается список ссылок, получено: ${describe(linksRaw)}`)
  }
  const links: ProfileLink[] = linksRaw.map((entry: unknown, index) => {
    const field = `links[${index}]`
    if (!isPlainObject(entry)) throw new ContentError(fileName, field, 'ожидается объект { label, url }')
    checkKnownKeys(fileName, entry, ['label', 'url'])
    const url = asNonEmptyString(fileName, `${field}.url`, requireField(fileName, entry, 'url'))
    return {
      label: asNonEmptyString(fileName, `${field}.label`, requireField(fileName, entry, 'label')),
      url: assertSafeUrl(fileName, `${field}.url`, url, { allowHash: true }),
    }
  })

  return {
    kind: 'profile',
    name: readString(fileName, data, 'name'),
    role: readString(fileName, data, 'role'),
    location: readString(fileName, data, 'location'),
    fictional: true,
    skills,
    links,
    html,
  }
}

export function validateExperience(fileName: string, data: Data, html: string): ExperienceItem {
  checkKnownKeys(fileName, data, EXPERIENCE_KEYS)
  const id = baseName(fileName)
  if (!SLUG_PATTERN.test(id)) {
    throw new ContentError(fileName, 'id', `имя файла должно соответствовать ${SLUG_PATTERN.source}`)
  }
  const start = readYearMonth(fileName, 'start', requireField(fileName, data, 'start'))
  const endRaw = requireField(fileName, data, 'end')
  const end = endRaw === null ? null : readYearMonth(fileName, 'end', endRaw)
  if (end !== null && end < start) {
    throw new ContentError(fileName, 'end', `дата окончания ${end} раньше даты начала ${start}`)
  }
  return {
    kind: 'experience',
    id,
    company: readString(fileName, data, 'company'),
    position: readString(fileName, data, 'position'),
    start,
    end,
    html,
  }
}

export function validateProject(fileName: string, data: Data, html: string, options: ParseOptions): Project {
  checkKnownKeys(fileName, data, PROJECT_KEYS)
  const slug = baseName(fileName)
  if (!SLUG_PATTERN.test(slug)) {
    throw new ContentError(fileName, 'slug', `имя файла должно соответствовать ${SLUG_PATTERN.source}`)
  }

  const cover = readNullableString(fileName, data, 'cover')
  const coverAlt = readNullableString(fileName, data, 'coverAlt')
  if (cover !== null) {
    if (!COVER_PATTERN.test(cover) || cover.includes('..')) {
      throw new ContentError(fileName, 'cover', `ожидается путь в public/ вида /images/name.svg, получено: ${cover}`)
    }
    if (!options.assetExists(cover)) {
      throw new ContentError(fileName, 'cover', `файл public${cover} не найден`)
    }
    if (coverAlt === null) {
      throw new ContentError(fileName, 'coverAlt', 'обязателен, если указан cover')
    }
  } else if (coverAlt !== null) {
    throw new ContentError(fileName, 'coverAlt', 'указан без cover: уберите coverAlt или добавьте cover')
  }

  const repo = readNullableString(fileName, data, 'repo')
  const demo = readNullableString(fileName, data, 'demo')

  return {
    kind: 'project',
    slug,
    title: readString(fileName, data, 'title'),
    summary: readString(fileName, data, 'summary'),
    tags: readStringArray(fileName, data, 'tags'),
    year: readInteger(fileName, data, 'year', 1970, 2100),
    cover,
    coverAlt,
    repo: repo === null ? null : assertSafeUrl(fileName, 'repo', repo, { allowHash: true }),
    demo: demo === null ? null : assertSafeUrl(fileName, 'demo', demo, { allowHash: true }),
    order: readInteger(fileName, data, 'order', 0, 10_000),
    html,
  }
}

/** Полный разбор одного файла. fileName — путь для сообщений об ошибках (например, projects/x.md). */
export function parseContentFile(
  kind: ContentKind,
  fileName: string,
  raw: string,
  options: ParseOptions,
): ContentModule {
  const { data, body } = splitFrontmatter(fileName, raw)
  const html = renderMarkdown(fileName, body)
  switch (kind) {
    case 'profile':
      return validateProfile(fileName, data, html)
    case 'experience':
      return validateExperience(fileName, data, html)
    case 'project':
      return validateProject(fileName, data, html, options)
  }
}
