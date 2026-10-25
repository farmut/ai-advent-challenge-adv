// Типы контент-модулей. Их создаёт build/content-plugin.ts из *.md на этапе сборки.
// Поле kind — дискриминант: по нему content/index.ts сужает unknown из import.meta.glob.

export interface SkillGroup {
  group: string
  items: string[]
}

export interface ProfileLink {
  label: string
  url: string
}

export interface Profile {
  kind: 'profile'
  name: string
  role: string
  location: string
  fictional: true
  skills: SkillGroup[]
  links: ProfileLink[]
  /** Биография: очищенный HTML из тела Markdown. */
  html: string
}

export interface ExperienceItem {
  kind: 'experience'
  /** Имя файла без расширения. */
  id: string
  company: string
  position: string
  /** YYYY-MM */
  start: string
  /** YYYY-MM или null — по настоящее время. */
  end: string | null
  html: string
}

export interface Project {
  kind: 'project'
  /** Имя файла без расширения, ^[a-z0-9-]+$. */
  slug: string
  title: string
  summary: string
  tags: string[]
  year: number
  cover: string | null
  coverAlt: string | null
  repo: string | null
  demo: string | null
  order: number
  html: string
}

export type ContentModule = Profile | ExperienceItem | Project
export type ContentKind = ContentModule['kind']
