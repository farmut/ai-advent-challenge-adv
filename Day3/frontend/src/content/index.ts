// Загрузка контента. Модули *.md собирает build/content-plugin.ts; здесь — сортировка, поиск и проверка вида.
import type { ContentModule, ExperienceItem, Profile, Project } from './types'
import profileModule from './profile.md'

export type { ExperienceItem, Profile, Project, ProfileLink, SkillGroup } from './types'

// ---------------------------------------------------------------------------
// Чистые функции (тестируются без Vite)
// ---------------------------------------------------------------------------

/** Проекты по полю order (по возрастанию), при равенстве — по slug. Исходный массив не меняется. */
export function sortProjects(projects: readonly Project[]): Project[] {
  return [...projects].sort((a, b) => a.order - b.order || a.slug.localeCompare(b.slug))
}

/**
 * Опыт: новые сверху. Сначала по start по убыванию; при равном start текущая работа (end: null) выше,
 * затем более поздний end. Формат YYYY-MM сравнивается лексикографически.
 */
export function sortExperience(items: readonly ExperienceItem[]): ExperienceItem[] {
  return [...items].sort((a, b) => {
    if (a.start !== b.start) return a.start < b.start ? 1 : -1
    if (a.end === b.end) return a.id.localeCompare(b.id)
    if (a.end === null) return -1
    if (b.end === null) return 1
    return a.end < b.end ? 1 : -1
  })
}

export function findProjectBySlug(projects: readonly Project[], slug: string): Project | undefined {
  return projects.find((project) => project.slug === slug)
}

export function getProjectSlugs(projects: readonly Project[]): string[] {
  return projects.map((project) => project.slug)
}

/** Сужение модулей из import.meta.glob: неожиданный вид — ошибка, а не тихий пропуск. */
export function collectByKind<K extends ContentModule['kind']>(
  modules: Record<string, unknown>,
  kind: K,
): Extract<ContentModule, { kind: K }>[] {
  const result: Extract<ContentModule, { kind: K }>[] = []
  for (const [file, value] of Object.entries(modules)) {
    if (!isContentOfKind(value, kind)) {
      throw new Error(`[content] ${file}: ожидался модуль вида "${kind}"`)
    }
    result.push(value)
  }
  return result
}

function isContentOfKind<K extends ContentModule['kind']>(
  value: unknown,
  kind: K,
): value is Extract<ContentModule, { kind: K }> {
  return typeof value === 'object' && value !== null && 'kind' in value && value.kind === kind
}

// ---------------------------------------------------------------------------
// Данные сайта
// ---------------------------------------------------------------------------

const projectModules: Record<string, unknown> = import.meta.glob('./projects/*.md', { eager: true, import: 'default' })
const experienceModules: Record<string, unknown> = import.meta.glob('./experience/*.md', {
  eager: true,
  import: 'default',
})

function requireProfile(value: ContentModule): Profile {
  if (value.kind !== 'profile') throw new Error('[content] profile.md: ожидался модуль вида "profile"')
  return value
}

export const profile: Profile = requireProfile(profileModule)
export const projects: readonly Project[] = sortProjects(collectByKind(projectModules, 'project'))
export const experience: readonly ExperienceItem[] = sortExperience(collectByKind(experienceModules, 'experience'))
export const projectSlugs: readonly string[] = getProjectSlugs(projects)

export function getProject(slug: string): Project | undefined {
  return findProjectBySlug(projects, slug)
}
