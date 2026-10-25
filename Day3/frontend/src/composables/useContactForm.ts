// Логика формы обратной связи. Без разметки и без сети: «отправка» — имитация по таймеру.
import { computed, getCurrentScope, onScopeDispose, reactive, readonly, ref } from 'vue'

export type ContactField = 'name' | 'email' | 'message'
export type ContactValues = Record<ContactField, string>
export type ContactErrors = Partial<Record<ContactField, string>>
export type FormStatus = 'idle' | 'sending' | 'success' | 'error'

export const CONTACT_FIELDS: readonly ContactField[] = ['name', 'email', 'message']

export const LIMITS = {
  name: { min: 2, max: 100 },
  message: { min: 10, max: 2000 },
  emailMax: 254,
} as const

export const MESSAGES = {
  nameRequired: 'Укажите имя',
  nameLength: `Имя: от ${LIMITS.name.min} до ${LIMITS.name.max} символов`,
  emailRequired: 'Укажите email',
  emailInvalid: 'Введите email в формате name@example.com',
  messageRequired: 'Напишите сообщение',
  messageLength: `Сообщение: от ${LIMITS.message.min} до ${LIMITS.message.max} символов`,
} as const

/** Длина в кодовых точках: эмодзи и суррогатные пары считаются одним символом. */
export function textLength(value: string): number {
  return [...value].length
}

const EMAIL_PATTERN = /^[^\s@]+@(?:[^\s@.]+\.)+[^\s@.]{2,}$/

export function validateName(value: string): string | undefined {
  const length = textLength(value.trim())
  if (length === 0) return MESSAGES.nameRequired
  if (length < LIMITS.name.min || length > LIMITS.name.max) return MESSAGES.nameLength
  return undefined
}

export function validateEmail(value: string): string | undefined {
  const trimmed = value.trim()
  if (trimmed.length === 0) return MESSAGES.emailRequired
  if (trimmed.length > LIMITS.emailMax || trimmed.includes('..') || !EMAIL_PATTERN.test(trimmed)) {
    return MESSAGES.emailInvalid
  }
  return undefined
}

export function validateMessage(value: string): string | undefined {
  const length = textLength(value.trim())
  if (length === 0) return MESSAGES.messageRequired
  if (length < LIMITS.message.min || length > LIMITS.message.max) return MESSAGES.messageLength
  return undefined
}

export function validateContactForm(values: ContactValues): ContactErrors {
  const errors: ContactErrors = {}
  const name = validateName(values.name)
  const email = validateEmail(values.email)
  const message = validateMessage(values.message)
  if (name !== undefined) errors.name = name
  if (email !== undefined) errors.email = email
  if (message !== undefined) errors.message = message
  return errors
}

export interface UseContactFormOptions {
  /** Длительность имитации отправки, мс. */
  delayMs?: number
  /** Итог имитации. 'error' нужен, чтобы проверить UI ошибки. */
  simulate?: 'success' | 'error'
}

const emptyValues = (): ContactValues => ({ name: '', email: '', message: '' })
const untouched = (): Record<ContactField, boolean> => ({ name: false, email: false, message: false })

export function useContactForm(options: UseContactFormOptions = {}) {
  const delayMs = options.delayMs ?? 800
  const simulate = options.simulate ?? 'success'

  const values = reactive<ContactValues>(emptyValues())
  const touched = reactive<Record<ContactField, boolean>>(untouched())
  const submitAttempted = ref(false)
  const status = ref<FormStatus>('idle')

  const errors = computed<ContactErrors>(() => validateContactForm(values))
  const isValid = computed(() => Object.keys(errors.value).length === 0)

  /** Ошибки, которые пора показать: поле покинуто (blur) или была попытка отправки. */
  const visibleErrors = computed<ContactErrors>(() => {
    const visible: ContactErrors = {}
    for (const field of CONTACT_FIELDS) {
      const error = errors.value[field]
      if (error !== undefined && (touched[field] || submitAttempted.value)) visible[field] = error
    }
    return visible
  })

  const firstInvalidField = computed<ContactField | undefined>(() =>
    CONTACT_FIELDS.find((field) => errors.value[field] !== undefined),
  )

  let timer: ReturnType<typeof setTimeout> | undefined
  let pendingResolve: ((value: boolean) => void) | undefined

  function onBlur(field: ContactField): void {
    touched[field] = true
  }

  function resetFields(): void {
    Object.assign(values, emptyValues())
    Object.assign(touched, untouched())
    submitAttempted.value = false
  }

  /** Полный сброс, включая статус. */
  function reset(): void {
    cancelPending()
    resetFields()
    status.value = 'idle'
  }

  function cancelPending(): void {
    if (timer !== undefined) clearTimeout(timer)
    timer = undefined
    pendingResolve?.(false)
    pendingResolve = undefined
  }

  /**
   * Имитация отправки. Возвращает true при «успехе».
   * Невалидная форма не уходит в sending; повторный вызов во время sending игнорируется.
   */
  function submit(): Promise<boolean> {
    if (status.value === 'sending') return Promise.resolve(false)
    submitAttempted.value = true
    if (!isValid.value) {
      status.value = 'idle'
      return Promise.resolve(false)
    }

    status.value = 'sending'
    return new Promise<boolean>((resolve) => {
      pendingResolve = resolve
      timer = setTimeout(() => {
        timer = undefined
        pendingResolve = undefined
        if (simulate === 'error') {
          status.value = 'error'
          resolve(false)
          return
        }
        status.value = 'success'
        resetFields()
        resolve(true)
      }, delayMs)
    })
  }

  // Компонент размонтирован во время «отправки» — таймер не должен пережить его.
  if (getCurrentScope() !== undefined) onScopeDispose(cancelPending)

  return {
    values,
    touched: readonly(touched),
    submitAttempted: readonly(submitAttempted),
    status: readonly(status),
    errors,
    visibleErrors,
    isValid,
    firstInvalidField,
    onBlur,
    submit,
    reset,
  }
}

export type ContactForm = ReturnType<typeof useContactForm>
