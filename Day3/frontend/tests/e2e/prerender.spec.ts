import { existsSync, readFileSync, readdirSync } from 'node:fs'
import path from 'node:path'
import { expect, test } from '@playwright/test'
import { frontendRoot, projectSlugsFromContent } from './helpers'

const dist = path.join(frontendRoot, 'dist')

test.describe('Пререндер (без JS)', () => {
  test.use({ javaScriptEnabled: false })

  test('прямой заход на /projects/<slug> отдаёт HTML с заголовком проекта', async ({ page, request }) => {
    const response = await request.get('/projects/task-tracker')
    expect(response.status()).toBe(200)
    expect(response.headers()['content-type']).toContain('text/html')
    const html = await response.text()
    expect(html).toMatch(/<h1[^>]*data-testid="project-title"[^>]*>\s*Трекер задач для небольших команд/)
    expect(html).toMatch(/<title>Трекер задач для небольших команд — Алексей Вымыслов<\/title>/)
    expect(html).toContain('<html lang="ru">')
    expect(html).toMatch(/<meta name="description" content="[^"]+"/)

    await page.goto('/projects/task-tracker')
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Трекер задач для небольших команд')
    await expect(page.getByTestId('project-body').getByRole('heading', { name: 'Что сделано' })).toBeVisible()
  })

  test('главная пререндерена со всеми разделами, кнопка формы неактивна без JS', async ({ page }) => {
    await page.goto('/')
    for (const id of ['hero', 'about', 'skills', 'projects', 'experience', 'contact']) {
      await expect(page.getByTestId(`section-${id}`)).toBeAttached()
    }
    await expect(page.getByTestId('contact-submit')).toBeDisabled()
    await expect(page.getByTestId('demo-note')).toBeVisible()
  })
})

test.describe('Артефакты сборки', () => {
  test.beforeEach(() => {
    test.skip(test.info().project.name !== 'desktop', 'проверка файлов сборки — один раз, в проекте desktop')
  })

  test('в dist есть index.html, 404.html и страница на каждый проект', () => {
    expect(existsSync(path.join(dist, 'index.html'))).toBe(true)
    expect(existsSync(path.join(dist, '404.html'))).toBe(true)
    for (const slug of projectSlugsFromContent()) {
      expect(existsSync(path.join(dist, 'projects', `${slug}.html`)), `projects/${slug}.html`).toBe(true)
    }
  })

  test('404.html — страница «не найдено» с noindex', () => {
    const html = readFileSync(path.join(dist, '404.html'), 'utf8')
    expect(html).toContain('data-testid="not-found"')
    expect(html).toContain('<meta name="robots" content="noindex">')
  })

  test('в браузерный бандл не попали markdown-it, gray-matter, sanitize-html', () => {
    const assets = path.join(dist, 'assets')
    const scripts = readdirSync(assets).filter((name) => name.endsWith('.js'))
    expect(scripts.length).toBeGreaterThan(0)
    for (const name of scripts) {
      const code = readFileSync(path.join(assets, name), 'utf8')
      expect(code, name).not.toMatch(/markdown-it|gray-matter|sanitize-html|js-yaml|htmlparser2|linkify-it/)
    }
  })

  test('каждая страница содержит CSP без сетевых соединений и отправки форм', () => {
    const pages = ['index.html', '404.html', ...projectSlugsFromContent().map((slug) => `projects/${slug}.html`)]
    for (const name of pages) {
      const html = readFileSync(path.join(dist, name), 'utf8')
      expect(html, name).toMatch(/<meta http-equiv="Content-Security-Policy" content="[^"]*connect-src 'none'[^"]*form-action 'none'/)
    }
  })
})
