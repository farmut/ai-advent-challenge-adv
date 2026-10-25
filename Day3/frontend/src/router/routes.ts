import type { RouteRecordRaw } from 'vue-router'
import HomePage from '@/pages/HomePage.vue'
import NotFoundPage from '@/pages/NotFoundPage.vue'
import ProjectPage from '@/pages/ProjectPage.vue'

export const NOT_FOUND_ROUTE_NAME = 'not-found'

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

/**
 * Маршруты. Параметр :slug ограничен списком известных проектов прямо в пути.
 * Так неизвестный slug сам попадает в catch-all (404) при любой навигации, включая смену параметра
 * /projects/a → /projects/b: guard beforeEnter в этом случае не вызывается.
 */
export function createRoutes(slugs: readonly string[]): RouteRecordRaw[] {
  const projectRoutes: RouteRecordRaw[] =
    slugs.length === 0
      ? []
      : [
          {
            path: `/projects/:slug(${slugs.map(escapeRegExp).join('|')})`,
            name: 'project',
            component: ProjectPage,
          },
        ]

  return [
    { path: '/', name: 'home', component: HomePage },
    ...projectRoutes,
    { path: '/:pathMatch(.*)*', name: NOT_FOUND_ROUTE_NAME, component: NotFoundPage },
  ]
}
