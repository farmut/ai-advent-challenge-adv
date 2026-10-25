import PrimeVue from 'primevue/config'
import { ViteSSG } from 'vite-ssg'
import App from './App.vue'
import { projectSlugs } from './content'
import { routes } from './router'
import { buildIncludedRoutes } from './router/includedRoutes'
import { createScrollBehavior } from './router/scrollBehavior'
import { primeVueOptions } from './theme/primevue'
import './styles/tokens.css'

export const createApp = ViteSSG(
  App,
  { routes, scrollBehavior: createScrollBehavior() },
  ({ app }) => {
    app.use(PrimeVue, primeVueOptions)
  },
)

/** vite-ssg берёт этот хук из entry: /projects/:slug разворачивается по контенту, плюс /404. */
export function includedRoutes(paths: string[]): string[] {
  return buildIncludedRoutes(paths, projectSlugs)
}
