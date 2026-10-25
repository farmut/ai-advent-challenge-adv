// Глобальная (не модульная) декларация: в файле с import верхнего уровня объявление '*.md' стало бы аугментацией.
declare module '*.md' {
  import type { ContentModule } from '@/content/types'

  // Модуль генерирует build/content-plugin.ts: frontmatter + очищенный HTML.
  const content: ContentModule
  export default content
}
