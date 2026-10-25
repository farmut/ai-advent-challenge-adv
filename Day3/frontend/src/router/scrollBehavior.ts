import type { RouterScrollBehavior } from 'vue-router'

/** Отступ под липкую шапку плюс небольшой зазор. Вызывается только в браузере. */
export function getHeaderOffset(): number {
  const header = document.querySelector<HTMLElement>('[data-app-header]')
  const gap = 8
  return header === null ? 0 : Math.round(header.getBoundingClientRect().height) + gap
}

export function prefersReducedMotion(): boolean {
  return typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

/** Прокрутить к элементу по хэшу с отступом под шапку. Возвращает false, если элемента нет. */
export function scrollToHash(hash: string): boolean {
  const target = hash === '' ? null : document.getElementById(decodeURIComponent(hash.slice(1)))
  if (target === null) return false
  const top = target.getBoundingClientRect().top + window.scrollY - getHeaderOffset()
  window.scrollTo({ top: Math.max(0, top), behavior: prefersReducedMotion() ? 'auto' : 'smooth' })
  return true
}

export interface ScrollDeps {
  getOffset: () => number
  reducedMotion: () => boolean
}

const browserDeps: ScrollDeps = { getOffset: getHeaderOffset, reducedMotion: prefersReducedMotion }

/**
 * Хэш → прокрутка к элементу с учётом высоты липкой шапки; «назад/вперёд» → сохранённая позиция;
 * иначе — в начало страницы.
 */
export function createScrollBehavior(deps: ScrollDeps = browserDeps): RouterScrollBehavior {
  return (to, _from, savedPosition) => {
    if (savedPosition !== null) return savedPosition
    if (to.hash !== '') {
      return {
        el: to.hash,
        top: deps.getOffset(),
        behavior: deps.reducedMotion() ? 'auto' : 'smooth',
      }
    }
    return { top: 0 }
  }
}
