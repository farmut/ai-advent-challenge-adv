import { expect, test } from '@playwright/test'
import type { Request } from '@playwright/test'
import { gotoApp } from './helpers'

test.describe('Форма обратной связи', () => {
  test.beforeEach(async ({ page }) => {
    await gotoApp(page, '/#contact')
    await expect(page.getByTestId('contact-submit')).toBeEnabled()
  })

  test('пустая отправка показывает ошибки под каждым полем и фокусирует первое', async ({ page }) => {
    await page.getByTestId('contact-submit').click()
    await expect(page.getByTestId('field-error-name')).toHaveText('Укажите имя')
    await expect(page.getByTestId('field-error-email')).toHaveText('Укажите email')
    await expect(page.getByTestId('field-error-message')).toHaveText('Напишите сообщение')
    await expect(page.getByTestId('field-name')).toBeFocused()
    await expect(page.getByTestId('field-name')).toHaveAttribute('aria-invalid', 'true')
    await expect(page.getByTestId('form-status')).toHaveAttribute('data-status', 'idle')
  })

  test('ошибка email появляется после ухода с поля', async ({ page }) => {
    await page.getByTestId('field-email').fill('not-an-email')
    await expect(page.getByTestId('field-error-email')).toHaveCount(0)
    await page.getByTestId('field-email').blur()
    await expect(page.getByTestId('field-error-email')).toBeVisible()
  })

  test('валидные данные: «отправка» успешна и без единого сетевого запроса', async ({ page }) => {
    await page.getByTestId('field-name').fill('Иван')
    await page.getByTestId('field-email').fill('ivan@example.com')
    await page.getByTestId('field-message').fill('Здравствуйте! Хочу обсудить проект.')

    const requests: Request[] = []
    page.on('request', (request) => requests.push(request))

    await page.getByTestId('contact-submit').click()
    await expect(page.getByTestId('form-status')).toHaveAttribute('data-status', 'sending')
    await expect(page.getByTestId('form-status')).toHaveAttribute('data-status', 'success')
    await expect(page.getByTestId('form-status')).toContainText('Спасибо')

    expect(requests.map((request) => request.url())).toEqual([])
    await expect(page.getByTestId('field-name')).toHaveValue('')
    await expect(page.getByTestId('demo-note')).toHaveText('Это демо: сообщение никуда не отправляется')
  })

  test('введённые данные не сохраняются в localStorage, sessionStorage и cookies', async ({ page, context }) => {
    await page.getByTestId('field-name').fill('Иван')
    await page.getByTestId('field-email').fill('ivan@example.com')
    await page.getByTestId('field-message').fill('Здравствуйте! Хочу обсудить проект.')
    await page.getByTestId('contact-submit').click()
    await expect(page.getByTestId('form-status')).toHaveAttribute('data-status', 'success')

    const storage = await page.evaluate(() => ({ local: localStorage.length, session: sessionStorage.length }))
    expect(storage).toEqual({ local: 0, session: 0 })
    expect(await context.cookies()).toEqual([])
  })
})
