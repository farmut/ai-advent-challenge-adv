import { readdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import {
  collectByKind,
  experience,
  findProjectBySlug,
  getProjectSlugs,
  profile,
  projectSlugs,
  projects,
  sortExperience,
  sortProjects,
} from '@/content'
import type { ExperienceItem, Project } from '@/content/types'

function project(slug: string, order: number): Project {
  return {
    kind: 'project',
    slug,
    title: slug,
    summary: 's',
    tags: [],
    year: 2024,
    cover: null,
    coverAlt: null,
    repo: null,
    demo: null,
    order,
    html: '',
  }
}

function job(id: string, start: string, end: string | null): ExperienceItem {
  return { kind: 'experience', id, company: id, position: 'p', start, end, html: '' }
}

describe('sortProjects', () => {
  it('сортирует по order по возрастанию', () => {
    const sorted = sortProjects([project('c', 3), project('a', 1), project('b', 2)])
    expect(sorted.map((p) => p.slug)).toEqual(['a', 'b', 'c'])
  })

  it('при равном order — по slug, исходный массив не меняется', () => {
    const input = [project('zeta', 1), project('alpha', 1)]
    const sorted = sortProjects(input)
    expect(sorted.map((p) => p.slug)).toEqual(['alpha', 'zeta'])
    expect(input.map((p) => p.slug)).toEqual(['zeta', 'alpha'])
  })

  it('пустой список', () => {
    expect(sortProjects([])).toEqual([])
  })
})

describe('sortExperience', () => {
  it('новые сверху по start', () => {
    const sorted = sortExperience([job('old', '2018-06', '2020-08'), job('new', '2023-03', null), job('mid', '2020-09', '2023-02')])
    expect(sorted.map((j) => j.id)).toEqual(['new', 'mid', 'old'])
  })

  it('при равном start текущая работа (end: null) выше, затем более поздний end', () => {
    const sorted = sortExperience([job('a', '2020-01', '2020-06'), job('b', '2020-01', null), job('c', '2020-01', '2021-01')])
    expect(sorted.map((j) => j.id)).toEqual(['b', 'c', 'a'])
  })

  it('сравнивает месяцы, а не только годы', () => {
    const sorted = sortExperience([job('jan', '2022-01', null), job('nov', '2022-11', null)])
    expect(sorted.map((j) => j.id)).toEqual(['nov', 'jan'])
  })
})

describe('findProjectBySlug / getProjectSlugs', () => {
  const list = [project('one', 1), project('two', 2)]

  it('находит проект по slug', () => {
    expect(findProjectBySlug(list, 'two')?.slug).toBe('two')
  })

  it('неизвестный slug → undefined', () => {
    expect(findProjectBySlug(list, 'three')).toBeUndefined()
    expect(findProjectBySlug(list, '')).toBeUndefined()
  })

  it('getProjectSlugs возвращает slug в порядке списка', () => {
    expect(getProjectSlugs(list)).toEqual(['one', 'two'])
  })
})

describe('collectByKind', () => {
  it('модуль чужого вида → ошибка с именем файла', () => {
    expect(() => collectByKind({ './projects/x.md': { kind: 'experience' } }, 'project')).toThrow('./projects/x.md')
  })

  it('не объект → ошибка', () => {
    expect(() => collectByKind({ './projects/x.md': null }, 'project')).toThrow()
  })
})

describe('реальный контент (через контент-плагин)', () => {
  const projectsDir = fileURLToPath(new URL('../../src/content/projects', import.meta.url))
  const fileSlugs = readdirSync(projectsDir)
    .filter((name) => name.endsWith('.md'))
    .map((name) => name.slice(0, -3))

  it('профиль вымышленный и содержит био в HTML', () => {
    expect(profile.fictional).toBe(true)
    expect(profile.html).toMatch(/^<p>/)
  })

  it('проекты загружены все и отсортированы по order', () => {
    expect([...projectSlugs].sort()).toEqual([...fileSlugs].sort())
    const orders = projects.map((p) => p.order)
    expect(orders).toEqual([...orders].sort((a, b) => a - b))
  })

  it('опыт отсортирован: новые сверху', () => {
    const starts = experience.map((j) => j.start)
    expect(starts).toEqual([...starts].sort().reverse())
  })

  it('все ссылки контента ведут на example.com, mailto @example.com или #', () => {
    const urls = [
      ...profile.links.map((l) => l.url),
      ...projects.flatMap((p) => [p.repo, p.demo]).filter((u): u is string => u !== null),
    ]
    for (const url of urls) {
      expect(url).toMatch(/^(https:\/\/([a-z0-9-]+\.)*example\.com\/|mailto:[^@]+@example\.com$|#)/)
    }
  })
})
