// Список путей для пререндера vite-ssg. Чистая функция: тестируется без сборки.

export const NOT_FOUND_PRERENDER_PATH = '/404'

/**
 * Пути маршрутов → пути для пререндера:
 * - маршруты с параметрами (":") и catch-all ("*") убираются — vite-ssg их не умеет рендерить;
 * - /projects/:slug разворачивается по списку slug проектов;
 * - добавляется /404: он попадает в catch-all и даёт dist/404.html.
 */
export function buildIncludedRoutes(paths: readonly string[], slugs: readonly string[]): string[] {
  const staticPaths = paths.filter((path) => !path.includes(':') && !path.includes('*'))
  const projectPaths = slugs.map((slug) => `/projects/${slug}`)
  return [...new Set([...staticPaths, ...projectPaths, NOT_FOUND_PRERENDER_PATH])]
}
