import { expect, test } from '@playwright/test'
import { SECTION_IDS, collectPageErrors, gotoApp, headerHeight, openMenuIfMobile } from './helpers'

test.describe('Главная', () => {
  test('открывается, все 6 разделов на месте, один h1', async ({ page }) => {
    const errors = collectPageErrors(page)
    await gotoApp(page, '/')
    await expect(page).toHaveTitle(/Алексей Вымыслов/)
    for (const id of SECTION_IDS) {
      await expect(page.getByTestId(`section-${id}`)).toBeVisible()
    }
    await expect(page.locator('h1')).toHaveCount(1)
    await expect(page.getByTestId('fictional-note')).toHaveText('Демонстрационный сайт, персона вымышлена')
    expect(errors).toEqual([])
  })

  test('якорная навигация прокручивает к каждому разделу и не прячет его под шапку', async ({ page }, testInfo) => {
    await gotoApp(page, '/')
    const header = await headerHeight(page)

    // Сначала уходим вниз, затем возвращаемся «Главной» — проверяем прокрутку в обе стороны.
    const order = ['about', 'skills', 'projects', 'experience', 'contact', 'hero'] as const
    for (const id of order) {
      await openMenuIfMobile(page, testInfo)
      await page.getByTestId(`nav-${id}`).click()
      await expect(page).toHaveURL(new RegExp(`/#${id}$`))
      const section = page.getByTestId(`section-${id}`)
      await expect(section).toBeInViewport()
      await expect
        .poll(async () => (await section.boundingBox())?.y ?? -1, { message: `раздел #${id} под шапкой` })
        .toBeGreaterThanOrEqual(id === 'hero' ? 0 : header - 1)
    }
  })

  test('мобильное меню закрывается после выбора пункта', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'mobile', 'только для мобильной ширины')
    await gotoApp(page, '/')
    const toggle = page.getByRole('button', { name: 'Меню', exact: true })
    // Подпись статична, состояние передаёт aria-expanded.
    await expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await openMenuIfMobile(page, testInfo)
    await expect(page.getByTestId('nav-skills')).toBeVisible()
    await expect(toggle).toHaveAttribute('aria-expanded', 'true')
    await expect(toggle).toHaveAccessibleName('Меню')
    await page.getByTestId('nav-skills').click()
    await expect(page.getByTestId('nav-skills')).toBeHidden()
    await expect(toggle).toHaveAttribute('aria-expanded', 'false')
  })

  test('прямой заход с хэшем прокручивает к разделу сразу под шапку', async ({ page }) => {
    await gotoApp(page, '/#experience')
    const section = page.getByTestId('section-experience')
    await expect(section).toBeInViewport()
    const header = await headerHeight(page)
    const top = async () => (await section.boundingBox())?.y ?? -1
    await expect.poll(top, { message: 'раздел под шапкой' }).toBeGreaterThanOrEqual(header - 1)
    await expect.poll(top, { message: 'раздел далеко ниже шапки' }).toBeLessThanOrEqual(header + 16)
  })
})
