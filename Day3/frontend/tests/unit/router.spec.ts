import { describe, expect, it } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import type { RouteLocationNormalized } from 'vue-router'
import { projectSlugs } from '@/content'
import { routes } from '@/router'
import { NOT_FOUND_ROUTE_NAME, createRoutes } from '@/router/routes'
import { createScrollBehavior } from '@/router/scrollBehavior'

function makeRouter(slugs: readonly string[] = ['known']) {
  return createRouter({ history: createMemoryHistory(), routes: createRoutes(slugs) })
}

describe('маршруты', () => {
  it('/ → home', async () => {
    const router = makeRouter()
    await router.push('/')
    expect(router.currentRoute.value.name).toBe('home')
  })

  it('известный slug → страница проекта', async () => {
    const router = makeRouter(['known'])
    await router.push('/projects/known')
    expect(router.currentRoute.value.name).toBe('project')
    expect(router.currentRoute.value.params.slug).toBe('known')
  })

  it('неизвестный slug → 404, URL сохраняется', async () => {
    const router = makeRouter(['known'])
    await router.push('/projects/unknown?x=1#top')
    const current = router.currentRoute.value
    expect(current.name).toBe(NOT_FOUND_ROUTE_NAME)
    expect(current.path).toBe('/projects/unknown')
    expect(current.query).toEqual({ x: '1' })
    expect(current.hash).toBe('#top')
  })

  it('регрессия: смена параметра /projects/known → /projects/unknown тоже даёт 404', async () => {
    const router = makeRouter(['known'])
    await router.push('/projects/known')
    expect(router.currentRoute.value.name).toBe('project')
    await router.push('/projects/unknown')
    expect(router.currentRoute.value.name).toBe(NOT_FOUND_ROUTE_NAME)
  })

  it('пустой список проектов: любой /projects/* — 404', async () => {
    const router = makeRouter([])
    await router.push('/projects/anything')
    expect(router.currentRoute.value.name).toBe(NOT_FOUND_ROUTE_NAME)
  })

  it('произвольный путь → 404', async () => {
    const router = makeRouter()
    await router.push('/some/missing/page')
    expect(router.currentRoute.value.name).toBe(NOT_FOUND_ROUTE_NAME)
  })

  it('/404 (путь пререндера) попадает в catch-all', async () => {
    const router = makeRouter()
    await router.push('/404')
    expect(router.currentRoute.value.name).toBe(NOT_FOUND_ROUTE_NAME)
  })

  it('реальные маршруты: каждый проект из контента открывается, чужой slug — 404', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes })
    for (const slug of projectSlugs) {
      await router.push(`/projects/${slug}`)
      expect(router.currentRoute.value.name).toBe('project')
    }
    await router.push('/projects/definitely-not-a-project')
    expect(router.currentRoute.value.name).toBe(NOT_FOUND_ROUTE_NAME)
  })
})

describe('scrollBehavior', () => {
  async function location(hash: string): Promise<RouteLocationNormalized> {
    const router = makeRouter()
    await router.push({ path: '/', hash })
    return router.currentRoute.value
  }

  it('хэш → прокрутка к элементу с отступом под шапку', async () => {
    const behavior = createScrollBehavior({ getOffset: () => 72, reducedMotion: () => false })
    expect(behavior(await location('#about'), await location(''), null)).toEqual({
      el: '#about',
      top: 72,
      behavior: 'smooth',
    })
  })

  it('prefers-reduced-motion → без плавной прокрутки', async () => {
    const behavior = createScrollBehavior({ getOffset: () => 0, reducedMotion: () => true })
    expect(behavior(await location('#contact'), await location(''), null)).toEqual({
      el: '#contact',
      top: 0,
      behavior: 'auto',
    })
  })

  it('«назад/вперёд» → сохранённая позиция', async () => {
    const behavior = createScrollBehavior({ getOffset: () => 72, reducedMotion: () => false })
    expect(behavior(await location('#about'), await location(''), { left: 0, top: 500 })).toEqual({ left: 0, top: 500 })
  })

  it('без хэша → в начало страницы', async () => {
    const behavior = createScrollBehavior({ getOffset: () => 72, reducedMotion: () => false })
    expect(behavior(await location(''), await location('#about'), null)).toEqual({ top: 0 })
  })
})
