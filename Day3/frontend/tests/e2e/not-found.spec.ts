import { expect, test } from '@playwright/test'
import { gotoApp } from './helpers'

test.describe('404', () => {
  test('неизвестный slug → страница 404, URL сохраняется', async ({ page }) => {
    await gotoApp(page, '/projects/unknown-slug')
    await expect(page.getByTestId('not-found')).toBeVisible()
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Страница не найдена')
    await expect(page).toHaveURL(/\/projects\/unknown-slug$/)
    await expect(page).toHaveTitle(/Страница не найдена/)
    await expect(page.getByTestId('project-page')).toHaveCount(0)
  })

  test('произвольный путь → 404, «На главную» ведёт на /', async ({ page }) => {
    await gotoApp(page, '/some/missing/page')
    await expect(page.getByTestId('not-found')).toBeVisible()
    await page.getByTestId('home-link').click()
    await expect(page).toHaveURL(/\/$/)
    await expect(page.getByTestId('section-hero')).toBeVisible()
  })

  test('пререндеренный /404 отдаёт страницу «не найдено»', async ({ request }) => {
    const html = await (await request.get('/404')).text()
    expect(html).toContain('data-testid="not-found"')
    expect(html).toContain('noindex')
  })
})
