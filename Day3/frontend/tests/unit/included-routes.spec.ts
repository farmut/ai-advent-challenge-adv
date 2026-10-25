import { describe, expect, it } from 'vitest'
import type { RouteRecordRaw } from 'vue-router'
import { projectSlugs } from '@/content'
import { NOT_FOUND_PRERENDER_PATH, buildIncludedRoutes } from '@/router/includedRoutes'
import { routes } from '@/router'

/** Пути маршрутов так же, как их собирает vite-ssg (routesToPaths) для плоского списка. */
function pathsOf(records: readonly RouteRecordRaw[]): string[] {
  return records.map((record) => record.path)
}

describe('buildIncludedRoutes', () => {
  it('убирает маршруты с параметрами и catch-all', () => {
    const result = buildIncludedRoutes(['/', '/projects/:slug', '/:pathMatch(.*)*'], [])
    expect(result).not.toContain('/projects/:slug')
    expect(result).not.toContain('/:pathMatch(.*)*')
    expect(result).toEqual(['/', NOT_FOUND_PRERENDER_PATH])
  })

  it('разворачивает /projects/:slug по списку slug', () => {
    expect(buildIncludedRoutes(['/', '/projects/:slug'], ['a', 'b-2'])).toEqual([
      '/',
      '/projects/a',
      '/projects/b-2',
      '/404',
    ])
  })

  it('всегда добавляет /404 и не дублирует пути', () => {
    expect(buildIncludedRoutes(['/', '/404', '/'], ['a', 'a'])).toEqual(['/', '/404', '/projects/a'])
  })

  it('пустой список проектов: главная и 404', () => {
    expect(buildIncludedRoutes(['/'], [])).toEqual(['/', '/404'])
  })
})

describe('includedRoutes с реальными маршрутами и контентом', () => {
  const result = buildIncludedRoutes(pathsOf(routes), projectSlugs)

  it('страница на каждый проект, главная и 404 — без ручного списка', () => {
    expect(result).toEqual(['/', ...projectSlugs.map((slug) => `/projects/${slug}`), '/404'])
  })

  it('в списке нет путей с параметрами', () => {
    expect(result.every((path) => !path.includes(':') && !path.includes('*'))).toBe(true)
  })
})
