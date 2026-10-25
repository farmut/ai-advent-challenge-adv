import { expect, test } from '@playwright/test'
import type { Request } from '@playwright/test'
import { collectPageErrors, gotoApp, isMobile, openMenuIfMobile } from './helpers'

const PAGES = ['/', '/projects/task-tracker', '/projects/weather-dashboard', '/projects/unknown-slug'] as const

test.describe('Вёрстка и рантайм', () => {
  for (const path of PAGES) {
    test(`нет горизонтального скролла: ${path}`, async ({ page }) => {
      await gotoApp(page, path)
      const { scrollWidth, innerWidth } = await page.evaluate(() => ({
        scrollWidth: document.documentElement.scrollWidth,
        innerWidth: window.innerWidth,
      }))
      expect(scrollWidth).toBeLessThanOrEqual(innerWidth)
    })

    test(`без ошибок в консоли и только same-origin запросы: ${path}`, async ({ page, baseURL }) => {
      const errors = collectPageErrors(page)
      const foreign: string[] = []
      page.on('request', (request: Request) => {
        if (baseURL !== undefined && !request.url().startsWith(baseURL)) foreign.push(request.url())
      })
      await gotoApp(page, path)
      expect(errors).toEqual([])
      expect(foreign).toEqual([])
    })
  }

  test('нет лицензионного баннера и предупреждений PrimeUI (PrimeVue 4.x, MIT)', async ({ page }) => {
    const warnings: string[] = []
    page.on('console', (message) => {
      if (message.type() === 'warning' && /PrimeUI|license/i.test(message.text())) warnings.push(message.text())
    })
    await gotoApp(page, '/')
    await expect(page.locator('#p-license-host')).toHaveCount(0)
    await expect(page.getByText('Invalid PrimeUI License')).toHaveCount(0)
    expect(warnings).toEqual([])
  })

  test('тап-цели навигации и формы не меньше 44×44', async ({ page }, testInfo) => {
    await gotoApp(page, '/')
    if (isMobile(testInfo)) {
      const toggle = await page.getByRole('button', { name: 'Меню', exact: true }).boundingBox()
      expect(toggle?.width).toBeGreaterThanOrEqual(44)
      expect(toggle?.height).toBeGreaterThanOrEqual(44)
    }
    await openMenuIfMobile(page, testInfo)
    // Мобильное меню раскрывается с анимацией: размер проверяется после её завершения (poll), порог тот же.
    for (const id of ['hero', 'about', 'skills', 'projects', 'experience', 'contact']) {
      const link = page.getByTestId(`nav-${id}`)
      await expect.poll(async () => (await link.boundingBox())?.height ?? 0, { message: `nav-${id} height` }).toBeGreaterThanOrEqual(44)
      await expect.poll(async () => (await link.boundingBox())?.width ?? 0, { message: `nav-${id} width` }).toBeGreaterThanOrEqual(44)
    }
    const submit = await page.getByTestId('contact-submit').boundingBox()
    expect(submit?.height).toBeGreaterThanOrEqual(44)
  })

  test('тёмная системная тема не перекрашивает компоненты PrimeVue', async ({ browser }) => {
    const colors: string[] = []
    for (const colorScheme of ['light', 'dark'] as const) {
      const context = await browser.newContext({ colorScheme })
      const page = await context.newPage()
      await gotoApp(page, '/')
      colors.push(
        await page.getByTestId('contact-submit').evaluate((element) => getComputedStyle(element).backgroundColor),
      )
      await context.close()
    }
    expect(colors[0]).toBe(colors[1])
  })
})
