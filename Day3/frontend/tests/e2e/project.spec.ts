import { expect, test } from '@playwright/test'
import { gotoApp, headerHeight, projectSlugsFromContent } from './helpers'

test.describe('Страница проекта', () => {
  test('клик по карточке → /projects/<slug>, заголовок и описание из Markdown, «Назад» → главная', async ({ page }) => {
    await gotoApp(page, '/')
    const card = page.getByTestId('project-card-task-tracker')
    const cardTitle = (await card.getByRole('heading', { level: 3 }).innerText()).trim()

    await card.getByRole('link').click()
    await expect(page).toHaveURL(/\/projects\/task-tracker$/)
    await expect(page.getByTestId('project-title')).toHaveText(cardTitle)
    await expect(page).toHaveTitle(new RegExp(cardTitle))

    const body = page.getByTestId('project-body')
    await expect(body.getByRole('heading', { level: 2, name: 'Что сделано' })).toBeVisible()
    await expect(body.getByRole('listitem').first()).toBeVisible()
    await expect(page.getByRole('img', { name: /канбан-доска/i })).toBeVisible()

    await page.getByTestId('back-link').click()
    await expect(page).toHaveURL(/\/#projects$/)
    const section = page.getByTestId('section-projects')
    await expect(section).toBeInViewport()
    const header = await headerHeight(page)
    await expect.poll(async () => (await section.boundingBox())?.y ?? -1).toBeGreaterThanOrEqual(header - 1)
  })

  test('вся карточка кликабельна (растянутая ссылка)', async ({ page }) => {
    await gotoApp(page, '/')
    const card = page.getByTestId('project-card-weather-dashboard')
    const cardBox = await card.boundingBox()
    const summaryBox = await card.locator('p').first().boundingBox()
    if (cardBox === null || summaryBox === null) throw new Error('карточка не отрисована')
    // Клик в точку над текстом описания (не по заголовку-ссылке). Hit-target — ::after ссылки внутри карточки.
    await card.click({
      position: { x: summaryBox.x - cardBox.x + 4, y: summaryBox.y - cardBox.y + summaryBox.height / 2 },
    })
    await expect(page).toHaveURL(/\/projects\/weather-dashboard$/)
  })

  test('карточка на каждый проект из контента ведёт на свою страницу', async ({ page }) => {
    for (const slug of projectSlugsFromContent()) {
      await gotoApp(page, '/')
      const card = page.getByTestId(`project-card-${slug}`)
      await expect(card).toBeVisible()
      const title = (await card.getByRole('heading', { level: 3 }).innerText()).trim()
      await card.getByRole('link').click()
      await expect(page).toHaveURL(new RegExp(`/projects/${slug}$`))
      await expect(page.getByTestId('project-title')).toHaveText(title)
      await expect(page.locator('h1')).toHaveCount(1)
    }
  })
})
