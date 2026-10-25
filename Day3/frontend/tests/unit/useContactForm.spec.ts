import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope } from 'vue'
import {
  LIMITS,
  MESSAGES,
  textLength,
  useContactForm,
  validateEmail,
  validateMessage,
  validateName,
} from '@/composables/useContactForm'

const VALID = { name: 'Иван', email: 'ivan@example.com', message: 'Здравствуйте, есть задача.' }

function fill(form: ReturnType<typeof useContactForm>, values: Partial<typeof VALID> = VALID): void {
  Object.assign(form.values, values)
}

describe('validateName', () => {
  it.each([
    ['', MESSAGES.nameRequired],
    ['   ', MESSAGES.nameRequired],
    ['И', MESSAGES.nameLength],
    ['  И  ', MESSAGES.nameLength],
    ['x'.repeat(LIMITS.name.max + 1), MESSAGES.nameLength],
  ])('%j → ошибка', (value, message) => {
    expect(validateName(value)).toBe(message)
  })

  it.each(['Ив', 'x'.repeat(LIMITS.name.max), '  Иван  '])('%j → валидно', (value) => {
    expect(validateName(value)).toBeUndefined()
  })

  it('длина считается в символах, а не в UTF-16: два эмодзи — это 2 символа', () => {
    expect(textLength('😀😀')).toBe(2)
    expect(validateName('😀😀')).toBeUndefined()
    expect(validateName('😀'.repeat(LIMITS.name.max))).toBeUndefined()
    expect(validateName('😀'.repeat(LIMITS.name.max + 1))).toBe(MESSAGES.nameLength)
  })
})

describe('validateEmail', () => {
  it.each(['a@example.com', 'first.last+tag@sub.example.com', '  ivan@example.ru  '])('%j → валидно', (value) => {
    expect(validateEmail(value)).toBeUndefined()
  })

  it('пустой → «Укажите email»', () => {
    expect(validateEmail('')).toBe(MESSAGES.emailRequired)
  })

  it.each([
    'ivan',
    'ivan@',
    '@example.com',
    'ivan@example',
    'ivan@example.c',
    'iv an@example.com',
    'ivan@@example.com',
    'ivan@example..com',
    'ivan..x@example.com',
  ])('%j → неверный формат', (value) => {
    expect(validateEmail(value)).toBe(MESSAGES.emailInvalid)
  })

  it('длиннее 254 символов → неверный формат', () => {
    const local = 'a'.repeat(64)
    const domain = `${'b'.repeat(63)}.${'c'.repeat(63)}.${'d'.repeat(63)}.com`
    const email = `${local}@${domain}`
    expect(email.length).toBeGreaterThan(LIMITS.emailMax)
    expect(validateEmail(email)).toBe(MESSAGES.emailInvalid)
  })
})

describe('validateMessage', () => {
  it.each([
    ['', MESSAGES.messageRequired],
    ['x'.repeat(LIMITS.message.min - 1), MESSAGES.messageLength],
    [`  ${'x'.repeat(LIMITS.message.min - 1)}  `, MESSAGES.messageLength],
    ['x'.repeat(LIMITS.message.max + 1), MESSAGES.messageLength],
  ])('%#: ошибка', (value, message) => {
    expect(validateMessage(value)).toBe(message)
  })

  it.each([LIMITS.message.min, LIMITS.message.max])('ровно %i символов → валидно', (length) => {
    expect(validateMessage('x'.repeat(length))).toBeUndefined()
  })
})

describe('useContactForm', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('начальное состояние: idle, ошибки есть, но не показаны', () => {
    const form = useContactForm()
    expect(form.status.value).toBe('idle')
    expect(form.isValid.value).toBe(false)
    expect(Object.keys(form.errors.value)).toEqual(['name', 'email', 'message'])
    expect(form.visibleErrors.value).toEqual({})
  })

  it('ошибка поля видна после blur, остальные — нет', () => {
    const form = useContactForm()
    form.onBlur('email')
    expect(form.visibleErrors.value).toEqual({ email: MESSAGES.emailRequired })
  })

  it('пустая отправка: остаётся idle, видны все три ошибки, фокус-цель — первое поле', async () => {
    const form = useContactForm()
    await expect(form.submit()).resolves.toBe(false)
    expect(form.status.value).toBe('idle')
    expect(Object.keys(form.visibleErrors.value)).toEqual(['name', 'email', 'message'])
    expect(form.firstInvalidField.value).toBe('name')
  })

  it('idle → sending → success через 800 мс по умолчанию, поля очищаются', async () => {
    const form = useContactForm()
    fill(form)
    const result = form.submit()
    expect(form.status.value).toBe('sending')

    vi.advanceTimersByTime(799)
    expect(form.status.value).toBe('sending')

    vi.advanceTimersByTime(1)
    await expect(result).resolves.toBe(true)
    expect(form.status.value).toBe('success')
    expect({ ...form.values }).toEqual({ name: '', email: '', message: '' })
    expect(form.visibleErrors.value).toEqual({})
  })

  it('simulate: error → idle → sending → error, введённые данные сохраняются', async () => {
    const form = useContactForm({ simulate: 'error', delayMs: 100 })
    fill(form)
    const result = form.submit()
    expect(form.status.value).toBe('sending')
    vi.advanceTimersByTime(100)
    await expect(result).resolves.toBe(false)
    expect(form.status.value).toBe('error')
    expect({ ...form.values }).toEqual(VALID)
  })

  it('после ошибки повторная отправка снова проходит через sending', async () => {
    const form = useContactForm({ simulate: 'error', delayMs: 10 })
    fill(form)
    const first = form.submit()
    vi.advanceTimersByTime(10)
    await first
    expect(form.status.value).toBe('error')
    const second = form.submit()
    expect(form.status.value).toBe('sending')
    vi.advanceTimersByTime(10)
    await second
    expect(form.status.value).toBe('error')
  })

  it('повторный submit во время sending игнорируется', async () => {
    const form = useContactForm({ delayMs: 50 })
    fill(form)
    const first = form.submit()
    await expect(form.submit()).resolves.toBe(false)
    expect(vi.getTimerCount()).toBe(1)
    vi.advanceTimersByTime(50)
    await expect(first).resolves.toBe(true)
  })

  it('reset возвращает всё в исходное состояние и отменяет «отправку»', async () => {
    const form = useContactForm({ delayMs: 50 })
    fill(form)
    const pending = form.submit()
    form.reset()
    await expect(pending).resolves.toBe(false)
    expect(form.status.value).toBe('idle')
    expect(vi.getTimerCount()).toBe(0)
    expect({ ...form.values }).toEqual({ name: '', email: '', message: '' })
  })

  it('уничтожение scope (размонтирование) отменяет таймер', () => {
    const scope = effectScope()
    const form = scope.run(() => useContactForm({ delayMs: 50 }))
    if (form === undefined) throw new Error('scope.run вернул undefined')
    fill(form)
    void form.submit()
    expect(vi.getTimerCount()).toBe(1)
    scope.stop()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('не обращается к сети и хранилищам', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
    const form = useContactForm({ delayMs: 10 })
    fill(form)
    const result = form.submit()
    vi.advanceTimersByTime(10)
    await result
    expect(fetchSpy).not.toHaveBeenCalled()
  })
})
