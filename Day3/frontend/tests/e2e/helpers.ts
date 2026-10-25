import { readdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { expect } from '@playwright/test'
import type { Page, TestInfo } from '@playwright/test'

export const SECTION_IDS = ['hero', 'about', 'skills', 'projects', 'experience', 'contact'] as const

export const frontendRoot = fileURLToPath(new URL('../../', import.meta.url))

/** slug проектов — имена файлов src/content/projects/*.md (источник правды для пререндера). */
export function projectSlugsFromContent(): string[] {
  return readdirSync(new URL('../../src/content/projects/', import.meta.url))
    .filter((name) => name.endsWith('.md'))
    .map((name) => name.slice(0, -3))
    .sort()
}

export function isMobile(testInfo: TestInfo): boolean {
  return testInfo.project.name === 'mobile'
}

/** Переход и ожидание монтирования Vue-приложения (html[data-app-mounted]). */
export async function gotoApp(page: Page, path: string): Promise<void> {
  await page.goto(path)
  await expect(page.locator('html[data-app-mounted="true"]')).toBeAttached()
}

/** На мобильной ширине пункты меню спрятаны за кнопкой-бургером Menubar. */
export async function openMenuIfMobile(page: Page, testInfo: TestInfo): Promise<void> {
  if (!isMobile(testInfo)) return
  await page.getByRole('button', { name: 'Меню', exact: true }).click()
}

export async function headerHeight(page: Page): Promise<number> {
  const box = await page.locator('[data-app-header]').boundingBox()
  if (box === null) throw new Error('шапка не найдена')
  return box.height
}

/** Ошибки консоли и необработанные исключения страницы (в том числе нарушения CSP). */
export function collectPageErrors(page: Page): string[] {
  const errors: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error') errors.push(`console: ${message.text()}`)
  })
  page.on('pageerror', (error) => errors.push(`pageerror: ${error.message}`))
  return errors
}
