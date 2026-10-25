import { projectSlugs } from '@/content'
import { createRoutes } from './routes'

export { createScrollBehavior } from './scrollBehavior'

export const routes = createRoutes(projectSlugs)
